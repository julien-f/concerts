package web

import (
	"net/http"
	"strings"
	"testing"
)

func TestRoutes(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	secret := ts.newGroup(t, "Gig buddies")

	tests := []struct {
		name     string
		path     string
		status   int
		contains []string
	}{
		{"healthz", "/healthz", http.StatusOK, []string{"ok"}},
		{"known secret", "/g/" + secret, http.StatusOK, []string{"Gig buddies", "No concerts yet", "/static/htmx-2.0.4.min.js", "/static/style.css"}},
		{"unknown secret", "/g/" + NewSecret(), http.StatusNotFound, nil},
		{"htmx", "/static/htmx-2.0.4.min.js", http.StatusOK, []string{"htmx"}},
		{"css", "/static/style.css", http.StatusOK, []string{":root"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			resp, body := ts.get(t, tt.path)
			if resp.StatusCode != tt.status {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tt.status)
			}
			for _, s := range tt.contains {
				if !strings.Contains(body, s) {
					t.Errorf("body lacks %q", s)
				}
			}
		})
	}
}

func TestSecurityHeaders(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	secret := ts.newGroup(t, "g")

	for _, path := range []string{"/healthz", "/g/" + secret, "/g/unknown", "/static/style.css", "/nope"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			resp, _ := ts.get(t, path)
			for k, want := range map[string]string{"Referrer-Policy": "no-referrer", "X-Robots-Tag": "noindex"} {
				if got := resp.Header.Get(k); got != want {
					t.Errorf("%s = %q, want %q", k, got, want)
				}
			}
		})
	}
}

func TestLogsNeverContainSecret(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	secret := ts.newGroup(t, "g")
	ts.get(t, "/g/"+secret)
	ts.get(t, "/g/"+secret+"/sub")

	logs := ts.logs.String()
	if strings.Contains(logs, secret) {
		t.Fatalf("logs leak the secret:\n%s", logs)
	}
	if !strings.Contains(logs, "/g/<secret>") {
		t.Fatalf("logs lack the redacted path:\n%s", logs)
	}
}

func TestRedactPath(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"/healthz":        "/healthz",
		"/g/abc":          "/g/<secret>",
		"/g/abc/concerts": "/g/<secret>/concerts",
		"/g/":             "/g/<secret>",
	}
	for in, want := range tests {
		if got := redactPath(in); got != want {
			t.Errorf("redactPath(%q) = %q, want %q", in, got, want)
		}
	}
}
