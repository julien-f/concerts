package web

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
	store *store.Store
	clock *fakeClock
	logs  *syncBuffer
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

// newTestServer starts the real router on a fresh database with a fake clock.
func newTestServer(t *testing.T) *testServer {
	t.Helper()
	clock := &fakeClock{t: time.Date(2026, 6, 1, 20, 0, 0, 0, time.UTC)}
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"), clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	logs := &syncBuffer{}
	h, err := New(st, slog.New(slog.NewTextHandler(logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(func() {
		srv.Close()
		_ = st.Close()
	})
	return &testServer{Server: srv, store: st, clock: clock, logs: logs}
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

func (ts *testServer) get(t *testing.T, path string) (*http.Response, string) {
	t.Helper()
	resp, err := ts.Client().Get(ts.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var body bytes.Buffer
	if _, err := body.ReadFrom(resp.Body); err != nil {
		t.Fatal(err)
	}
	return resp, body.String()
}
