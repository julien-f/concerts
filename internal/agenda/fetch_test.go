package agenda

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	neturl "net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// testSite serves handler over TLS for every host name: the client dials the test server
// whatever the URL says, and its certificate covers example.com and *.example.com.
func testSite(t *testing.T, handler http.HandlerFunc) *http.Client {
	t.Helper()
	srv := httptest.NewTLSServer(handler)
	t.Cleanup(srv.Close)
	client := srv.Client()
	tr := client.Transport.(*http.Transport)
	tr.TLSClientConfig.ServerName = "example.com" // the test certificate's: venue hosts too
	tr.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
	}
	return client
}

// sleeps records the waits asked for, without waiting.
type sleeps struct {
	mu sync.Mutex
	d  []time.Duration
}

func (s *sleeps) sleep(ctx context.Context, d time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.d = append(s.d, d)
	return ctx.Err()
}

func newFetcher(client *http.Client, s *sleeps) *fetcher {
	return &fetcher{client: client, sleep: s.sleep, seen: map[string]bool{}}
}

// linesParser reads a test listing: one item per page, ids from the path; each line of the body is a next page.
func linesParser(page *neturl.URL, body []byte, _ string) ([]listed, []*neturl.URL, error) {
	var next []*neturl.URL
	for l := range strings.Lines(string(body)) {
		u, err := page.Parse(strings.TrimSpace(l))
		if err != nil {
			return nil, nil, err
		}
		next = append(next, u)
	}
	return []listed{{id: page.Path}}, next, nil
}

func testVenue() *Venue {
	return &Venue{
		Key: "test", Name: "Test", Listings: []string{"https://www.example.com/agenda"}, Format: "text/html",
		Site: []string{"www.example.com"}, MaxPages: 5, parse: linesParser,
	}
}

func mustURL(t *testing.T, s string) *neturl.URL {
	t.Helper()
	u, err := neturl.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestGet(t *testing.T) {
	t.Parallel()
	client := testSite(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != userAgent || r.Header.Get("Cookie") != "" || r.Header.Get("Referer") != "" {
			t.Errorf("%s: headers %v", r.URL.Path, r.Header)
		}
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "x", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode}) // not sent back after a redirect
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		switch r.URL.Path {
		case "/ok":
		case "/max":
			_, _ = w.Write(bytes.Repeat([]byte("a"), maxBody))
		case "/over":
			_, _ = w.Write(bytes.Repeat([]byte("a"), maxBody+1))
		case "/json":
			w.Header().Set("Content-Type", "application/json")
		case "/redirect":
			http.Redirect(w, r, "/ok", http.StatusFound)
		case "/redirect-again":
			http.Redirect(w, r, "/redirect", http.StatusFound)
		case "/redirect-loop":
			http.Redirect(w, r, "/redirect-loop", http.StatusFound)
		case "/off-site":
			http.Redirect(w, r, "https://other.example.com/ok", http.StatusFound)
		case "/plain-http":
			http.Redirect(w, r, "http://www.example.com/ok", http.StatusFound)
		case "/busy":
			w.WriteHeader(http.StatusTooManyRequests)
		case "/down":
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			http.NotFound(w, r)
		}
	})
	for _, tc := range []struct {
		path  string
		ok    bool
		final string
		busy  bool
	}{
		{path: "/ok", ok: true, final: "/ok"},
		{path: "/max", ok: true, final: "/max"},
		{path: "/over"},
		{path: "/json"},
		{path: "/redirect", ok: true, final: "/ok"},
		{path: "/redirect-again", ok: true, final: "/ok"}, // Referer dropped on the second hop too
		{path: "/redirect-loop"},
		{path: "/off-site"},
		{path: "/plain-http"},
		{path: "/missing"},
		{path: "/busy", busy: true},
		{path: "/down", busy: true},
	} {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			f := newFetcher(client, &sleeps{})
			body, final, err := f.get(context.Background(), testVenue(), mustURL(t, "https://www.example.com"+tc.path+"?q=SECRET"), "text/html")
			if (err == nil) != tc.ok || errors.Is(err, errBusy) != tc.busy {
				t.Fatalf("get: %v; want ok %v, busy %v", err, tc.ok, tc.busy)
			}
			if err != nil && strings.Contains(err.Error(), "SECRET") {
				t.Errorf("error holds the URL: %v", err)
			}
			if tc.path == "/max" && len(body) != maxBody {
				t.Errorf("body = %d bytes, want %d", len(body), maxBody)
			}
			if tc.ok && final.Path != tc.final {
				t.Errorf("final URL %s", final)
			}
		})
	}
}

func TestGetOffSite(t *testing.T) {
	t.Parallel()
	f := newFetcher(testSite(t, func(http.ResponseWriter, *http.Request) { t.Error("requested") }), &sleeps{})
	for _, u := range []string{"http://www.example.com/", "https://other.example.com/"} {
		if _, _, err := f.get(context.Background(), testVenue(), mustURL(t, u), "text/html"); err == nil {
			t.Errorf("get(%s) accepted", u)
		}
	}
}

func TestDelay(t *testing.T) {
	t.Parallel()
	client := testSite(t, func(w http.ResponseWriter, _ *http.Request) { w.Header().Set("Content-Type", "text/html") })
	for _, tc := range []struct {
		delay, want time.Duration
	}{
		{0, minDelay},
		{time.Second, minDelay},
		{10 * time.Second, 10 * time.Second},
	} {
		s := &sleeps{}
		f := newFetcher(client, s)
		v := testVenue()
		v.Delay = tc.delay
		v.Site = append(v.Site, "tickets.example.com")
		for _, u := range []string{"https://www.example.com/a", "https://tickets.example.com/b", "https://www.example.com/c"} {
			if _, _, err := f.get(context.Background(), v, mustURL(t, u), "text/html"); err != nil {
				t.Fatal(err)
			}
		}
		// Only the second request to a host waits.
		if len(s.d) != 1 || s.d[0] != tc.want {
			t.Errorf("delay %v: sleeps %v, want [%v]", tc.delay, s.d, tc.want)
		}
	}
}

func TestDelayCancelled(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	client := testSite(t, func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("/next\n"))
	})
	ctx, cancel := context.WithCancel(context.Background())
	f := &fetcher{client: client, seen: map[string]bool{}, sleep: func(ctx context.Context, d time.Duration) error {
		cancel() // SIGTERM during the wait
		return Sleep(ctx, d)
	}}
	_, pages, err := f.listing(ctx, testVenue(), "2026-11-18")
	if !errors.Is(err, context.Canceled) || requests.Load() != 1 || pages != 2 {
		t.Errorf("listing: %v after %d requests, %d pages; want cancelled after 1 request", err, requests.Load(), pages)
	}
}

func TestListing(t *testing.T) {
	t.Parallel()
	pages := map[string]string{
		"/agenda":        "/agenda?page=2\nhttps://other.example.com/agenda?page=3\nhttp://www.example.com/agenda?page=4\n",
		"/agenda?page=2": "/agenda\n/agenda?page=2#top\n/agenda?page=5\n", // back to the first page, and itself
		"/agenda?page=5": "",
	}
	var mu sync.Mutex
	var got []string
	client := testSite(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got = append(got, r.URL.RequestURI())
		mu.Unlock()
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(pages[r.URL.RequestURI()]))
	})
	items, n, err := newFetcher(client, &sleeps{}).listing(context.Background(), testVenue(), "2026-11-18")
	if err != nil || n != 3 || len(items) != 3 || strings.Join(got, " ") != "/agenda /agenda?page=2 /agenda?page=5" {
		t.Errorf("listing = %d items, %d pages, %v; requested %v", len(items), n, err, got)
	}

	v := testVenue()
	v.MaxPages = 2
	items, n, err = newFetcher(client, &sleeps{}).listing(context.Background(), v, "2026-11-18")
	if err == nil || n != 2 || len(items) != 2 {
		t.Errorf("over the page cap: %d items, %d pages, %v; want 2, 2 and an error", len(items), n, err)
	}
}

// TestNewClientRefusesLoopback: the production client keeps prefill's dial checks.
func TestNewClientRefusesLoopback(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("reached") }))
	t.Cleanup(srv.Close)
	_, err := NewClient().Get(srv.URL) //nolint:noctx // the dial is refused before any request
	if err == nil || !strings.Contains(err.Error(), "refusing to dial") {
		t.Errorf("Get(%s) = %v, want a refused dial", srv.URL, err)
	}
}

func TestTicketing(t *testing.T) {
	t.Parallel()
	v := &Venue{Site: []string{"www.example.com"}, Ticketing: []string{"billetweb.fr"}}
	for u, want := range map[string]bool{
		"https://www.example.com/x": true, "https://www.billetweb.fr/x": true, "http://billetweb.fr/x": true,
		"https://example.com/x": false, "https://evilbilletweb.fr/x": false, "javascript:alert(1)": false,
		"https://billetweb.fr.evil.example/x": false,
	} {
		if got := v.ticketing(mustURL(t, u)); got != want {
			t.Errorf("ticketing(%s) = %v, want %v", u, got, want)
		}
	}
}
