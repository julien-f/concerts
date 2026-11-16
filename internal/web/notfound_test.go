package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// A concert link that no longer works inside a valid group (shared link to a deleted
// concert, stale tab) gets a page with the group header and a way back.
func TestConcertNotFoundPage(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	b, secret := ts.member(t, "Léa")
	g := "/g/" + secret
	b.postRedirect(t, g+"/concerts", validConcert())
	b.postRedirect(t, g+"/concerts/1/delete", nil)
	other, otherSecret := ts.member(t, "Zoé")
	other.postRedirect(t, "/g/"+otherSecret+"/concerts", validConcert()) // concert 2, another group

	page := []string{"<title>Erreur – Concert introuvable · g</title>", "<h1>Concert introuvable</h1>",
		`<a class="back" href="` + g + `">← Tous les concerts</a>`}
	check := func(t *testing.T, resp *http.Response, body string) {
		t.Helper()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Errorf("Content-Type = %q", ct)
		}
		if cc := resp.Header.Get("Cache-Control"); cc != "private, no-cache" {
			t.Errorf("Cache-Control = %q", cc)
		}
		assertContains(t, body, page...)
		if strings.Contains(body, "og:") {
			t.Errorf("Open Graph tags on the 404 page")
		}
	}

	for _, tt := range []struct{ name, method, path string }{
		{"deleted concert", "GET", g + "/concerts/1"},
		{"edit form", "GET", g + "/concerts/1/edit"},
		{"agenda", "GET", g + "/concerts/1/agenda.ics"},
		{"another group's concert", "GET", g + "/concerts/2"},
		{"not a number", "GET", g + "/concerts/abc"},
		{"update", "POST", g + "/concerts/1"},
		{"status", "POST", g + "/concerts/1/status"},
		{"delete again", "POST", g + "/concerts/1/delete"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var resp *http.Response
			var body string
			if tt.method == "GET" {
				resp, body = b.get(t, tt.path)
			} else {
				resp, body = b.post(t, tt.path, url.Values{"artist": {"A"}, "date": {"2026-07-01"}, "venue": {"V"}, "status": {"going"}})
			}
			check(t, resp, body)
			assertContains(t, body, "Ce n'est pas moi")
		})
	}

	t.Run("without identity", func(t *testing.T) {
		resp, body := ts.browser(t).get(t, g+"/concerts/1")
		check(t, resp, body)
		if strings.Contains(body, "Ce n'est pas moi") {
			t.Errorf("« Ce n'est pas moi » shown without identity")
		}
	})

	t.Run("htmx reloads", func(t *testing.T) {
		req := b.postRequest(t, g+"/concerts/1/status", url.Values{"status": {"going"}})
		req.Header.Set("HX-Request", "true")
		resp, _ := b.do(t, req)
		if resp.StatusCode != http.StatusNotFound || resp.Header.Get("HX-Refresh") != "true" {
			t.Errorf("status = %d, HX-Refresh = %q; want 404 and true", resp.StatusCode, resp.Header.Get("HX-Refresh"))
		}
	})

	t.Run("unknown secret stays plain", func(t *testing.T) {
		resp, body := ts.get(t, "/g/unknown/concerts/1")
		if resp.StatusCode != http.StatusNotFound || body != "Page introuvable.\n" {
			t.Errorf("status = %d, body = %q; want 404 and the plain message", resp.StatusCode, body)
		}
	})
}
