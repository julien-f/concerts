package prefill

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	neturl "net/url"
	"syscall"
	"time"
)

// maxPage caps what is read of a page; metadata sits in the <head>.
const maxPage = 1 << 20

func prefixes(ss ...string) []netip.Prefix {
	var ps []netip.Prefix
	for _, s := range ss {
		ps = append(ps, netip.MustParsePrefix(s))
	}
	return ps
}

var (
	// globalIPv6 is the only IPv6 range handed out as public unicast: an allowlist,
	// since ::/96 (IPv4-compatible), fec0::/10 and others can reach internal networks.
	globalIPv6 = netip.MustParsePrefix("2000::/3")
	// notPublic lists special-purpose ranges that netip's predicates do not cover.
	notPublic = prefixes(
		"0.0.0.0/8", "100.64.0.0/10" /* CGNAT */, "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", /* benchmarking */
		"198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4",
		"2001::/32" /* Teredo */, "2001:10::/28", "2001:20::/28" /* ORCHID */, "2001:db8::/32", "2002::/16", /* 6to4 */
		"3fff::/20", /* documentation */
	)
)

// public reports whether ip is a public unicast address, IPv4-mapped IPv6 included.
func public(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || (ip.Is6() && !globalIPv6.Contains(ip)) {
		return false
	}
	for _, p := range notPublic {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}

// dialControl runs on the resolved address of every connection, redirects
// included, so DNS cannot point the fetcher at the server's own network.
func dialControl(_, address string, _ syscall.RawConn) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return err
	}
	if (port != "80" && port != "443") || !public(ip) {
		return fmt.Errorf("refusing to dial %s", address)
	}
	return nil
}

func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 3 {
		return errors.New("too many redirects")
	}
	if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
		return fmt.Errorf("redirect to %s scheme", req.URL.Scheme)
	}
	return nil
}

// NewClient returns the HTTP client for ticketing pages: public addresses on
// ports 80 and 443 only, no proxy, few redirects, short timeouts.
func NewClient() *http.Client {
	d := &net.Dialer{Timeout: 5 * time.Second, Control: dialControl}
	return &http.Client{
		Timeout:       8 * time.Second, // total, so a slow drip cannot hold a fetch slot
		CheckRedirect: checkRedirect,
		Transport: &http.Transport{
			Proxy:                  nil, // a proxy would be dialed instead of the checked target
			DialContext:            d.DialContext,
			TLSHandshakeTimeout:    5 * time.Second,
			ResponseHeaderTimeout:  5 * time.Second,
			MaxResponseHeaderBytes: 64 << 10,
			DisableKeepAlives:      true,
		},
	}
}

// Fetch reads a ticketing page with client and parses it. Errors are for logs and
// name the host only: the URL may hold tokens, or even a group secret pasted by mistake.
func Fetch(ctx context.Context, client *http.Client, url string, now time.Time) (Result, error) {
	res, err := fetch(ctx, client, url, now)
	if ue := (*neturl.Error)(nil); errors.As(err, &ue) {
		err = ue.Err
	}
	if err != nil {
		host := "?"
		if u, perr := neturl.Parse(url); perr == nil {
			host = u.Host
		}
		err = fmt.Errorf("fetch from %s: %w", host, err)
	}
	return res, err
}

func fetch(ctx context.Context, client *http.Client, url string, now time.Time) (Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Result{}, err
	}
	// Honest and cookie-less; no Referer either, since the request starts here.
	req.Header.Set("User-Agent", "concerts (link preview)")
	req.Header.Set("Accept", "text/html")
	resp, err := client.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("status %d", resp.StatusCode)
	}
	if mt, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type")); mt != "text/html" && mt != "application/xhtml+xml" {
		return Result{}, fmt.Errorf("content type %q", mt)
	}
	return Parse(io.LimitReader(resp.Body, maxPage), now), nil
}
