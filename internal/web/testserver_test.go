package web

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"concerts/internal/store"
)

// fakeClock is a settable clock, safe for concurrent use.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type testServer struct {
	*httptest.Server
	store  *store.Store
	dbPath string
	clock  *fakeClock
	logs   *syncBuffer
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

// exec runs SQL on the test database: bulk inserts in one statement, where hundreds of
// creates through the API would take a second.
func (ts *testServer) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	db, err := sql.Open("sqlite", ts.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

// newTestServer starts the real router on a fresh database with a fake clock.
func newTestServer(t *testing.T) *testServer {
	t.Helper()
	return newTestServerWith(t, "http://example.test")
}

// newTestServerWith is newTestServer with a chosen public base URL.
func newTestServerWith(t *testing.T, baseURL string) *testServer {
	t.Helper()
	clock := &fakeClock{t: time.Date(2026, 6, 1, 20, 0, 0, 0, time.UTC)}
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(context.Background(), dbPath, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	logs := &syncBuffer{}
	h, err := New(st, slog.New(slog.NewTextHandler(logs, nil)), Config{BaseURL: baseURL, Now: clock.Now, Fetch: http.DefaultClient})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(func() {
		srv.Close()
		_ = st.Close()
	})
	return &testServer{Server: srv, store: st, dbPath: dbPath, clock: clock, logs: logs}
}

// newGroup creates a group and returns its secret.
func (ts *testServer) newGroup(t *testing.T, name string) string {
	t.Helper()
	secret := NewSecret()
	if _, err := ts.store.CreateGroup(context.Background(), name, secret); err != nil {
		t.Fatal(err)
	}
	return secret
}

// concertURL is the public link path of the group's concert id.
func (ts *testServer) concertURL(t *testing.T, secret string, id int64) string {
	t.Helper()
	ctx := context.Background()
	g, err := ts.store.GroupBySecret(ctx, secret)
	if err != nil {
		t.Fatal(err)
	}
	c, err := ts.store.Concert(ctx, g.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	return "/c/" + c.Token
}

func (ts *testServer) get(t *testing.T, path string) (*http.Response, string) {
	t.Helper()
	return ts.browser(t).get(t, path)
}

// browser is a client with its own cookie jar that does not follow redirects,
// like a browser without JS whose every step the test checks.
type browser struct {
	ts     *testServer
	client *http.Client
}

func (ts *testServer) browser(t *testing.T) *browser {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &browser{ts: ts, client: &http.Client{
		Jar:           jar,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func (b *browser) do(t *testing.T, req *http.Request) (*http.Response, string) {
	t.Helper()
	resp, err := b.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, string(body)
}

func (b *browser) get(t *testing.T, path string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, b.ts.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	return b.do(t, req)
}

// follow gets path and follows its redirects, e.g. a group concert URL to its public link.
func (b *browser) follow(t *testing.T, path string) (*http.Response, string) {
	t.Helper()
	resp, body := b.get(t, path)
	for i := 0; resp.StatusCode == http.StatusSeeOther; i++ {
		if i == 3 {
			t.Fatalf("GET %s: too many redirects", path)
		}
		resp, body = b.get(t, resp.Header.Get("Location"))
	}
	return resp, body
}

// postRequest builds a form submission.
func (b *browser) postRequest(t *testing.T, path string, form url.Values) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, b.ts.URL+path, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

func (b *browser) post(t *testing.T, path string, form url.Values) (*http.Response, string) {
	t.Helper()
	return b.do(t, b.postRequest(t, path, form))
}

// postRedirect submits a form, expects a 303 and returns the page it leads to.
func (b *browser) postRedirect(t *testing.T, path string, form url.Values) (*http.Response, string) {
	t.Helper()
	resp, body := b.post(t, path, form)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST %s: status = %d, want 303; body:\n%s", path, resp.StatusCode, body)
	}
	return b.follow(t, resp.Header.Get("Location"))
}

// join picks or creates a first name in the group and returns the resulting page.
func (b *browser) join(t *testing.T, secret, name string) string {
	t.Helper()
	_, body := b.postRedirect(t, "/g/"+secret+"/me", url.Values{"name": {name}})
	return body
}
