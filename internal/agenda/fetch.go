package agenda

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	neturl "net/url"
	"slices"
	"strings"
	"time"

	"concerts/internal/prefill"
)

const (
	maxBody = 4 << 20
	// minDelay is the least wait between two requests to one host, whatever robots.txt says.
	minDelay = 3 * time.Second
	// maxRedirects a request follows, to the venue's site hosts only.
	maxRedirects = 3
	userAgent    = "concerts (agenda des salles, une fois par jour)"
)

// errBusy is a 429 or 503: the site asks to slow down, so the venue is left for this run.
var errBusy = errors.New("site busy")

// NewClient returns the HTTP client for venue sites: prefill's dial checks (public addresses,
// ports 80 and 443), with timeouts for a daily job rather than a form waiting on it.
func NewClient() *http.Client {
	c := prefill.NewClient()
	t := c.Transport.(*http.Transport).Clone()
	t.ResponseHeaderTimeout = 20 * time.Second // a listing built on demand (WordPress JSON) can be slow
	return &http.Client{Timeout: 30 * time.Second, Transport: t}
}

// Sleep waits d, or less if ctx is done first.
func Sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// fetcher makes a run's requests, one at a time, waiting the venue's delay between two to one host.
type fetcher struct {
	client *http.Client
	sleep  func(context.Context, time.Duration) error
	seen   map[string]bool // hosts already requested in this run
}

// get reads u, which must be https on one of v's site hosts, and checks its media type.
// It returns the body and the URL actually read, after redirects. Errors name no URL: logs get them.
func (f *fetcher) get(ctx context.Context, v *Venue, u *neturl.URL, mediaType string) ([]byte, *neturl.URL, error) {
	if !v.site(u) {
		return nil, nil, errors.New("page off the venue's site")
	}
	host := hostname(u)
	if f.seen[host] {
		if err := f.sleep(ctx, max(v.Delay, minDelay)); err != nil {
			return nil, nil, err
		}
	}
	f.seen[host] = true
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", mediaType)
	c := *f.client
	c.Jar = nil
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		req.Header.Del("Referer") // set by the client before this check
		switch {
		case len(via) > maxRedirects:
			return errors.New("too many redirects")
		case !v.site(req.URL):
			return errors.New("redirect off the venue's site")
		}
		return nil
	}
	resp, err := c.Do(req)
	if ue := (*neturl.Error)(nil); errors.As(err, &ue) {
		err = ue.Err // its message holds the URL
	}
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable:
		return nil, nil, fmt.Errorf("%w: status %d", errBusy, resp.StatusCode)
	case resp.StatusCode != http.StatusOK:
		return nil, nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	if mt, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type")); mt != mediaType {
		return nil, nil, fmt.Errorf("content type %q, want %q", mt, mediaType)
	}
	// One byte over the limit tells a cut page from a whole one: a cut listing would parse as fewer events.
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, nil, err
	}
	if len(body) > maxBody {
		return nil, nil, fmt.Errorf("body over %d bytes", maxBody)
	}
	return body, resp.Request.URL, nil
}

// hostname is u's host, lower case, without port or trailing dot.
func hostname(u *neturl.URL) string {
	return strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
}

// site reports whether u is https on one of v's site hosts.
func (v *Venue) site(u *neturl.URL) bool {
	return u.Scheme == "https" && slices.Contains(v.Site, hostname(u))
}

// ticketing reports whether a ticket link to u may be kept: on the venue's site or a ticketing domain.
func (v *Venue) ticketing(u *neturl.URL) bool {
	if u.Scheme != "https" && u.Scheme != "http" {
		return false
	}
	h := hostname(u)
	return slices.Contains(v.Site, h) || slices.ContainsFunc(v.Ticketing, func(d string) bool {
		return h == d || strings.HasSuffix(h, "."+d)
	})
}

// listing reads v's listing pages, following the next pages its parser finds within v.MaxPages.
// It returns what it parsed even with an error, and how many pages it read.
func (f *fetcher) listing(ctx context.Context, v *Venue, today string) ([]listed, int, error) {
	var queue []*neturl.URL
	for _, s := range v.Listings {
		u, err := neturl.Parse(s)
		if err != nil {
			return nil, 0, err
		}
		queue = append(queue, u)
	}
	done := map[string]bool{}
	var items []listed
	pages := 0
	for len(queue) > 0 {
		u := queue[0]
		queue = queue[1:]
		u.Fragment = ""
		if done[u.String()] || !v.site(u) {
			continue // a loop, or a link off the venue's site
		}
		if pages == v.MaxPages {
			return items, pages, fmt.Errorf("more than %d listing pages", v.MaxPages)
		}
		done[u.String()] = true
		pages++
		body, final, err := f.get(ctx, v, u, v.Format)
		if err != nil {
			return items, pages, err
		}
		got, next, err := v.parse(final, body, today)
		if err != nil {
			return items, pages, err
		}
		items = append(items, got...)
		queue = append(queue, next...)
	}
	return items, pages, nil
}
