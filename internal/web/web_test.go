package web

import (
	"net/http"
	"net/url"
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
		{"known secret", "/g/" + secret, http.StatusOK, []string{`lang="fr"`, "Gig buddies", "Qui es-tu", "Pas de compte ni de mot de passe\u202f: ce lien suffit. Ne le partage pas hors du groupe.", "/static/htmx-2.0.11.min.", "/static/style.", `"allowEval":false`}},
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
	// Unclean spellings get a ServeMux redirect, logged without a route.
	for _, p := range []string{"/g/" + secret, "/g/" + secret + "/sub", "//g/" + secret, "/./g/" + secret, "/g/" + secret + "/../x"} {
		ts.get(t, p)
	}
	ts.browser(t).post(t, "/g/"+secret+"/me", url.Values{"name": {"Léa"}})

	logs := ts.logs.String()
	if strings.Contains(logs, secret) {
		t.Fatalf("logs leak the secret:\n%s", logs)
	}
	for _, want := range []string{`route="GET /g/{secret}"`, `route="POST /g/{secret}/me"`, `route="GET (unmatched)"`} {
		if !strings.Contains(logs, want) {
			t.Errorf("logs lack %s:\n%s", want, logs)
		}
	}
}

func TestPlainHTTPBaseURLWarns(t *testing.T) {
	t.Parallel()
	for base, warn := range map[string]bool{
		"http://concerts.example.com":  true,
		"https://concerts.example.com": false,
		"http://localhost:8080":        false,
		"http://127.0.0.1:8080":        false,
		"http://[::1]:8080":            false,
		"http://LOCALHOST:8080":        false,
		"http://192.168.1.5":           true,
	} {
		t.Run(base, func(t *testing.T) {
			t.Parallel()
			ts := newTestServerWith(t, base)
			if got := strings.Contains(ts.logs.String(), "level=WARN"); got != warn {
				t.Errorf("warning logged = %v, want %v:\n%s", got, warn, ts.logs.String())
			}
		})
	}
}
