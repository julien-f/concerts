package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// ticketSite serves the prefill fixtures, plus a blocked page and a hostile one.
func ticketSite(t *testing.T) *httptest.Server {
	t.Helper()
	fixtures := http.FileServer(http.Dir("../prefill/testdata"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		switch r.URL.Path {
		case "/empty":
			_, _ = w.Write([]byte("<p>Just a moment...</p>"))
		case "/blocked":
			http.Error(w, "Just a moment...", http.StatusForbidden)
		case "/hostile":
			_, _ = w.Write([]byte(`<script type="application/ld+json">{"@type":"MusicEvent","name":"` +
				strings.Repeat("x", 101) + `","startDate":"2026-07-01T20:00","location":{"name":"Salle` + "\u200b" + `","address":"` + strings.Repeat("x", 101) + `"}}</script>`))
		default:
			fixtures.ServeHTTP(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestPrefill(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	site := ticketSite(t)
	b, secret := ts.member(t, "Léa")
	newURL := "/g/" + secret + "/concerts/new?url="

	tests := []struct {
		name, page string
		want       []string
	}{
		{"dice", site.URL + "/dice.html", []string{"Vérifie les champs pré-remplis", `value="Les Hiboux"`, `value="2026-11-27"`,
			`value="20:00"`, `value="Salle Exemple"`, `id="address" name="address" type="text" value="Paris"`, `id="ticket_url" name="ticket_url" type="url" value="` + site.URL + `/dice.html"`, "autofocus"}},
		{"blocked site", site.URL + "/blocked", []string{"Impossible de lire cette page\u202f: remplis", `name="ticket_url" type="url" value="` + site.URL + `/blocked"`}},
		{"og:title only", site.URL + "/accor.html", []string{`value="Duo Fictif"`}},
		{"nothing found", site.URL + "/empty", []string{"Rien trouvé sur cette page\u202f: remplis", `aria-describedby="prefill-msg" autofocus`}},
		{"hostile values dropped", site.URL + "/hostile", []string{`id="artist" name="artist" type="text" value=""`, `id="venue" name="venue" type="text" value=""`, `id="address" name="address" type="text" value=""`, `value="2026-07-01"`}},
		{"not a web link", "javascript:alert(1)", []string{"Lien invalide\u202f: il doit",
			`id="url" name="url" type="url" value="javascript:alert(1)" maxlength="2000" inputmode="url" aria-invalid="true" aria-describedby="prefill-msg" autofocus`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, body := b.get(t, newURL+url.QueryEscape(tt.page))
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d", resp.StatusCode)
			}
			assertContains(t, body, tt.want...)
			if strings.Contains(body, `href="javascript:`) {
				t.Errorf("javascript: link rendered")
			}
		})
		ts.clock.Advance(10 * time.Second) // so the table never hits fetchesPerMinute
	}

	if resp, _ := ts.browser(t).get(t, newURL+url.QueryEscape(site.URL+"/dice.html")); resp.StatusCode != http.StatusSeeOther {
		t.Errorf("anonymous prefill: status = %d, want 303 to the picker", resp.StatusCode)
	}
}

func TestFetchLimiter(t *testing.T) {
	t.Parallel()
	l := newFetchLimiter()
	t0 := time.Date(2026, 6, 1, 20, 0, 0, 0, time.UTC)

	release, ok := l.acquire(1, t0)
	if !ok {
		t.Fatal("first fetch refused")
	}
	if _, ok := l.acquire(1, t0); ok {
		t.Error("second fetch in flight for the same group accepted")
	}
	release2, ok := l.acquire(2, t0)
	if !ok {
		t.Fatal("another group's fetch refused")
	}
	if _, ok := l.acquire(3, t0); ok {
		t.Error("third fetch in flight overall accepted")
	}
	release()
	release2()

	for i := 1; i < fetchesPerMinute; i++ { // the first one above counts too
		r, ok := l.acquire(1, t0.Add(time.Duration(i)*time.Second))
		if !ok {
			t.Fatalf("fetch %d within the minute refused", i+1)
		}
		r()
	}
	if _, ok := l.acquire(1, t0.Add(30*time.Second)); ok {
		t.Error("11th fetch within a minute accepted")
	}
	if r, ok := l.acquire(1, t0.Add(61*time.Second)); !ok {
		t.Error("fetch after the minute refused")
	} else {
		r()
	}
}
