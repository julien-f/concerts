package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestStatusWithoutJS(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	lea, secret := ts.member(t, "Léa")
	g := "/g/" + secret
	lea.postRedirect(t, g+"/concerts", validConcert())
	marc := ts.browser(t)
	marc.join(t, secret, "Marc")
	sam := ts.browser(t)
	sam.join(t, secret, "Sam")

	marc.postRedirect(t, g+"/concerts/1/status", url.Values{"status": {"going"}, "back": {"list"}})
	_, body := sam.postRedirect(t, g+"/concerts/1/status", url.Values{"status": {"going"}, "back": {"list"}})
	assertContains(t, body, "<!doctype html>", "Y vont : Marc, Sam",
		`id="c1-going" name="status" value="" aria-pressed="true"`, `id="c1-interested" name="status" value="interested" aria-pressed="false"`)

	// Same answer twice is harmless; the pressed button (empty status) clears it.
	resp, _ := sam.post(t, g+"/concerts/1/status", url.Values{"status": {"going"}, "back": {"concert"}})
	if loc := resp.Header.Get("Location"); loc != g+"/concerts/1" {
		t.Fatalf("back=concert: Location = %q", loc)
	}
	_, body = sam.postRedirect(t, g+"/concerts/1/status", url.Values{"status": {""}, "back": {"concert"}})
	assertContains(t, body, "<h1>A</h1>", "Y va : Marc", `id="c1-going" name="status" value="going" aria-pressed="false"`)

	lea.postRedirect(t, g+"/concerts/1/status", url.Values{"status": {"going"}})
	lea.postRedirect(t, g+"/concerts/1/status", url.Values{"status": {"interested"}})
	_, body = lea.get(t, g)
	assertContains(t, body, "Intérêt : Léa")
	if strings.Contains(body, "Léa, Marc") {
		t.Errorf("a changed answer left the old one:\n%s", body)
	}

	// Without identity, names show but no buttons.
	_, body = ts.browser(t).follow(t, g+"/concerts/1")
	assertContains(t, body, "Y va : Marc")
	if strings.Contains(body, `name="status"`) {
		t.Errorf("status buttons offered without identity")
	}
}

func TestStatusWithHTMX(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	b, secret := ts.member(t, "Léa")
	g := "/g/" + secret
	b.postRedirect(t, g+"/concerts", validConcert())

	req := b.postRequest(t, g+"/concerts/1/status", url.Values{"status": {"going"}, "back": {"concert"}})
	req.Header.Set("HX-Request", "true")
	resp, body := b.do(t, req)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if !strings.HasPrefix(body, `<div class="statuses">`) || strings.Contains(body, "<html") {
		t.Fatalf("want only the status block, got:\n%s", body)
	}
	// who-list: the desktop card lays out the swapped block's lines through it.
	assertContains(t, body, `<div class="who-list">`, "Y va : Léa", `id="c1-going" name="status" value="" aria-pressed="true"`,
		`name="back" value="concert"`, `data-hx-target="closest .statuses"`)
}

func TestStatusRejected(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	b, secret := ts.member(t, "Léa")
	g := "/g/" + secret
	b.postRedirect(t, g+"/concerts", validConcert())
	second := validConcert()
	second.Set("anyway", "1")
	b.postRedirect(t, g+"/concerts", second)
	b.postRedirect(t, g+"/concerts/2/delete", nil)
	intruder, other := ts.member(t, "Intrus")

	tests := []struct {
		name   string
		b      *browser
		path   string
		status string
		want   int
	}{
		{"unknown status", b, g + "/concerts/1/status", "king", http.StatusBadRequest},
		{"retired ticket status", b, g + "/concerts/1/status", "ticket", http.StatusBadRequest},
		{"deleted concert", b, g + "/concerts/2/status", "going", http.StatusNotFound},
		{"unknown concert", b, g + "/concerts/9/status", "going", http.StatusNotFound},
		{"other group's concert", intruder, "/g/" + other + "/concerts/1/status", "going", http.StatusNotFound},
		{"anonymous", ts.browser(t), g + "/concerts/1/status", "going", http.StatusSeeOther},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if resp, _ := tt.b.post(t, tt.path, url.Values{"status": {tt.status}}); resp.StatusCode != tt.want {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tt.want)
			}
		})
	}
	// htmx follows a 303 silently and would swap the picker page into the card,
	// and swaps no 404: both become full navigations.
	for _, tt := range []struct {
		who          *browser
		path, header string
	}{{ts.browser(t), g + "/concerts/1/status", "HX-Redirect"}, {b, g + "/concerts/2/status", "HX-Refresh"}} {
		path, header := tt.path, tt.header
		req := tt.who.postRequest(t, path, url.Values{"status": {"going"}})
		req.Header.Set("HX-Request", "true")
		resp, body := tt.who.do(t, req)
		if resp.Header.Get(header) == "" || strings.Contains(body, "<html") {
			t.Errorf("htmx POST %s: want %s and no page, got %d %v", path, header, resp.StatusCode, resp.Header)
		}
	}

	req := b.postRequest(t, g+"/concerts/1/status", url.Values{"status": {"going"}})
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	if resp, _ := b.do(t, req); resp.StatusCode != http.StatusForbidden {
		t.Errorf("cross-site: status = %d, want 403", resp.StatusCode)
	}
	if _, body := b.get(t, g); strings.Contains(body, "Y va") {
		t.Errorf("a rejected answer was recorded")
	}
}
