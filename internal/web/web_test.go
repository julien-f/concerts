package web

import (
	"net/http"
	"regexp"
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
		{"known secret", "/g/" + secret, http.StatusOK, []string{"Gig buddies", "No concerts yet", "/static/htmx-2.0.11.min.", "/static/style.", `"allowEval":false`}},
		{"unknown secret", "/g/" + NewSecret(), http.StatusNotFound, nil},
		{"unhashed static name", "/static/style.css", http.StatusNotFound, nil},
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

func TestStaticAssets(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	_, page := ts.get(t, "/g/"+ts.newGroup(t, "g"))

	tests := []struct {
		pattern     string
		contentType string
		contains    string
	}{
		{`/static/style\.[0-9a-f]{16}\.css`, "text/css; charset=utf-8", ":root"},
		{`/static/htmx-2\.0\.11\.min\.[0-9a-f]{16}\.js`, "text/javascript; charset=utf-8", "htmx"},
	}
	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			t.Parallel()
			path := regexp.MustCompile(tt.pattern).FindString(page)
			if path == "" {
				t.Fatalf("page does not reference %s", tt.pattern)
			}
			resp, body := ts.get(t, path)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d", resp.StatusCode)
			}
			for k, want := range map[string]string{
				"Cache-Control": "public, max-age=31536000, immutable",
				"Content-Type":  tt.contentType,
			} {
				if got := resp.Header.Get(k); got != want {
					t.Errorf("%s = %q, want %q", k, got, want)
				}
			}
			if !strings.Contains(body, tt.contains) {
				t.Errorf("body lacks %q", tt.contains)
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
			for k, want := range map[string]string{
				"Referrer-Policy":         "no-referrer",
				"X-Robots-Tag":            "noindex",
				"X-Content-Type-Options":  "nosniff",
				"Content-Security-Policy": contentSecurityPolicy,
			} {
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
