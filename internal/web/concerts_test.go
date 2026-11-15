package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func concertValues(artist, date, tm, venue, ticket, onSale string) url.Values {
	return url.Values{"artist": {artist}, "date": {date}, "time": {tm}, "venue": {venue},
		"ticket_url": {ticket}, "on_sale_date": {onSale}}
}

// member returns a browser identified as name in a fresh group, and the group secret.
func (ts *testServer) member(t *testing.T, name string) (*browser, string) {
	t.Helper()
	secret := ts.newGroup(t, "g")
	b := ts.browser(t)
	b.join(t, secret, name)
	return b, secret
}

func assertContains(t *testing.T, body string, want ...string) {
	t.Helper()
	for _, s := range want {
		if !strings.Contains(body, s) {
			t.Errorf("body lacks %q:\n%s", s, body)
		}
	}
}

// TestConcertLifecycle walks add, show, edit, delete and undo as a browser without JS.
func TestConcertLifecycle(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t) // clock: 2026-06-01
	b, secret := ts.member(t, "Léa")
	g := "/g/" + secret

	_, body := b.postRedirect(t, g+"/concerts", concertValues("Fontaines D.C.", "2026-06-13", "20:30", "Zénith Paris", "https://tickets.example/f?a=1&b=2", "2026-06-05"))
	assertContains(t, body, "<!doctype html>", "<title>Concert ajouté – ", "Concert ajouté\u202f: Fontaines D.C.",
		"sam. 13 juin · 20\u00a0h\u00a030", "Zénith Paris", "ajouté par Léa",
		// The share text holds the absolute concert link, %20-encoded.
		"https://wa.me/?text=Fontaines%20D.C.%2C%20sam.%2013%20juin", "http%3A%2F%2Fexample.test%2Fg%2F"+secret+"%2Fconcerts%2F1")

	_, body = b.get(t, g+"/concerts/1")
	assertContains(t, body, "<h1>Fontaines D.C.</h1>", `href="https://tickets.example/f?a=1&amp;b=2"`, "Mise en vente\u202f: ", "ven. 5 juin", "Modifier")

	_, body = b.postRedirect(t, g+"/concerts/1", concertValues("Fontaines D.C.", "2027-01-09", "", "Olympia", "", ""))
	assertContains(t, body, "<h1>Fontaines D.C.</h1>", "sam. 9 janv. 2027", "Olympia")
	if strings.Contains(body, "Billetterie") || strings.Contains(body, "Mise en vente") {
		t.Errorf("cleared optional fields still shown:\n%s", body)
	}

	_, body = b.postRedirect(t, g+"/concerts/1/delete", nil)
	assertContains(t, body, "Concert supprimé\u202f: Fontaines D.C.", "/concerts/1/restore", "Aucun concert")
	if resp, _ := b.get(t, g+"/concerts/1"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("deleted concert page: status = %d, want 404", resp.StatusCode)
	}
	if resp, _ := b.post(t, g+"/concerts/1", concertValues("X", "2027-01-09", "", "V", "", "")); resp.StatusCode != http.StatusNotFound {
		t.Errorf("editing a deleted concert: status = %d, want 404", resp.StatusCode)
	}

	_, body = b.postRedirect(t, g+"/concerts/1/restore", nil)
	assertContains(t, body, `id="c1"`, "Fontaines D.C.")
	// The banner follows the state: once restored, « Annuler » is gone.
	if _, body = b.get(t, g+"?deleted=1"); strings.Contains(body, "restore") {
		t.Errorf("restored concert still offers « Annuler »")
	}
	if resp, _ := b.post(t, g+"/concerts/1/restore", nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("second restore: status = %d, want 404", resp.StatusCode)
	}
}

func TestConcertValidation(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	b, secret := ts.member(t, "Léa")
	tests := []struct {
		name  string
		form  url.Values
		field string
	}{
		{"missing artist", concertValues(" ", "2026-07-01", "", "V", "", ""), "artist"},
		{"artist too long", concertValues(strings.Repeat("a", 101), "2026-07-01", "", "V", "", ""), "artist"},
		{"missing date", concertValues("A", "", "", "V", "", ""), "date"},
		{"impossible date", concertValues("A", "2026-02-30", "", "V", "", ""), "date"},
		{"bad time", concertValues("A", "2026-07-01", "25:00", "V", "", ""), "time"},
		{"missing venue", concertValues("A", "2026-07-01", "", "", "", ""), "venue"},
		{"javascript link", concertValues("A", "2026-07-01", "", "V", "javascript:alert(1)", ""), "ticket_url"},
		{"ftp link", concertValues("A", "2026-07-01", "", "V", "ftp://x.example/f", ""), "ticket_url"},
		{"relative link", concertValues("A", "2026-07-01", "", "V", "/billets", ""), "ticket_url"},
		{"bad on-sale date", concertValues("A", "2026-07-01", "", "V", "", "demain"), "on_sale_date"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			resp, body := b.post(t, "/g/"+secret+"/concerts", tt.form)
			if resp.StatusCode != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422", resp.StatusCode)
			}
			assertContains(t, body, "<title>Erreur – ", `id="`+tt.field+`-error"`,
				`aria-describedby="`+tt.field+`-error"`, "autofocus")
		})
	}
	if _, body := b.get(t, "/g/"+secret); !strings.Contains(body, "Aucun concert") {
		t.Errorf("an invalid form created a concert")
	}
}

func TestConcertAccess(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	b, secret := ts.member(t, "Léa")
	g := "/g/" + secret
	b.postRedirect(t, g+"/concerts", concertValues("A", "2026-07-01", "", "V", "", ""))
	intruder, other := ts.member(t, "Intrus")
	anon := ts.browser(t)

	tests := []struct {
		name   string
		b      *browser
		method string
		path   string
		status int
	}{
		{"anonymous add form", anon, "GET", g + "/concerts/new", http.StatusSeeOther},
		{"anonymous create", anon, "POST", g + "/concerts", http.StatusSeeOther},
		{"anonymous edit", anon, "GET", g + "/concerts/1/edit", http.StatusSeeOther},
		{"anonymous delete", anon, "POST", g + "/concerts/1/delete", http.StatusSeeOther},
		{"anonymous concert page", anon, "GET", g + "/concerts/1", http.StatusOK},
		{"other group's concert", intruder, "GET", "/g/" + other + "/concerts/1", http.StatusNotFound},
		{"other group's delete", intruder, "POST", "/g/" + other + "/concerts/1/delete", http.StatusNotFound},
		{"other group's restore", intruder, "POST", "/g/" + other + "/concerts/1/restore", http.StatusNotFound},
		{"unknown concert", b, "GET", g + "/concerts/99", http.StatusNotFound},
		{"non-numeric id", b, "GET", g + "/concerts/abc", http.StatusNotFound},
		{"unknown secret", b, "GET", "/g/" + NewSecret() + "/concerts/1", http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var resp *http.Response
			if tt.method == "GET" {
				resp, _ = tt.b.get(t, tt.path)
			} else {
				resp, _ = tt.b.post(t, tt.path, concertValues("B", "2026-07-02", "", "W", "", ""))
			}
			if resp.StatusCode != tt.status {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tt.status)
			}
		})
	}
	for _, path := range []string{"/concerts", "/concerts/1", "/concerts/1/delete", "/concerts/1/restore"} {
		req := b.postRequest(t, g+path, concertValues("B", "2026-07-02", "", "W", "", ""))
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		if resp, _ := b.do(t, req); resp.StatusCode != http.StatusForbidden {
			t.Errorf("cross-site POST %s: status = %d, want 403", path, resp.StatusCode)
		}
	}
}

// TestPickerReturnsToConcert: a concert link opened without identity leads back to it after picking.
func TestPickerReturnsToConcert(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	b, secret := ts.member(t, "Léa")
	g := "/g/" + secret
	b.postRedirect(t, g+"/concerts", concertValues("A", "2026-07-01", "", "V", "", ""))

	newcomer := ts.browser(t)
	_, body := newcomer.get(t, g+"/concerts/1")
	assertContains(t, body, `href="`+g+`?next=/concerts/1"`, "Choisis ton prénom")
	if strings.Contains(body, "Modifier") {
		t.Errorf("edit offered without identity")
	}
	_, body = newcomer.get(t, g+"?next=/concerts/1")
	assertContains(t, body, `name="next" value="/concerts/1"`)

	resp, _ := newcomer.post(t, g+"/me", url.Values{"name": {"Sam"}, "next": {"/concerts/1"}})
	if loc := resp.Header.Get("Location"); loc != g+"/concerts/1" {
		t.Fatalf("Location = %q, want the concert page", loc)
	}
	for _, next := range []string{"//evil.example", "/concerts/1/../../x", "https://evil.example", "/concerts/1x"} {
		resp, _ := ts.browser(t).post(t, g+"/me", url.Values{"member_id": {"1"}, "next": {next}})
		if loc := resp.Header.Get("Location"); loc != g {
			t.Errorf("next %q: Location = %q, want %q", next, loc, g)
		}
	}
}
