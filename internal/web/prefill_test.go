package web

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

const (
	belleVenue  = "https://www.la-belle-electrique.com/fr/agenda/les-hiboux"
	belleAgenda = "https://billetterie.la-belle-electrique.com/agenda/747-Les-Hiboux?session=747"
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
		case "/rejected": // something found, but nothing the form accepts
			_, _ = w.Write([]byte("<title>" + strings.Repeat("Concert ", 20) + "</title>"))
		case "/long":
			_, _ = w.Write([]byte(`<script type="application/ld+json">{"@type":"MusicEvent","name":"Long","description":"` +
				strings.Repeat("Une phrase de vingt-neuf car. ", 70) + `"}</script>`))
		default:
			fixtures.ServeHTTP(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// belleSites serves La Belle Électrique's venue and billetterie hosts from the prefill fixtures,
// other hosts as usual, so tests never reach the real sites. A venue path ending in « -gone »
// links to a billetterie page that does not exist.
type belleSites struct{}

func (belleSites) RoundTrip(r *http.Request) (*http.Response, error) {
	var file string
	switch r.URL.Host {
	case "www.la-belle-electrique.com":
		file = "belle-electrique-site.html"
	case "billetterie.la-belle-electrique.com":
		if r.URL.Path == "/agenda/747-Les-Hiboux" {
			file = "belle-electrique.html"
		}
	default:
		return http.DefaultTransport.RoundTrip(r)
	}
	w := httptest.NewRecorder()
	if file == "" {
		http.NotFound(w, r)
	} else {
		b, err := os.ReadFile("../prefill/testdata/" + file)
		if err != nil {
			return nil, err
		}
		if strings.HasSuffix(r.URL.Path, "-gone") {
			b = bytes.ReplaceAll(b, []byte("/747-"), []byte("/404-"))
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write(b)
	}
	resp := w.Result()
	resp.Request = r
	return resp, nil
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
		{"dice", site.URL + "/dice.html", []string{"Vérifie les champs pré-remplis", `value="Les Hiboux &#43; Nova Test"`, `value="2026-11-27"`,
			`value="20:00"`, `value="Salle Exemple"`, `id="address" name="address" type="text" value="Paris"`, `id="ticket_url" name="ticket_url" type="url" value="` + site.URL + `/dice.html"`, "autofocus"}},
		{"blocked site", site.URL + "/blocked", []string{"Impossible de lire cette page\u202f: essaie le lien de la salle ou de l", "ou remplis \u00e0 la main.", `name="ticket_url" type="url" value="` + site.URL + `/blocked"`}},
		{"og:title only", site.URL + "/accor.html", []string{`value="Duo Fictif"`}},
		{"price and description", site.URL + "/belle-electrique.html", []string{`id="price" name="price" type="text" value="26–29 €"`,
			"maxlength=\"2000\">Une soirée, cinq figures du collectif.\nL&#39;une d’elles chante.</textarea>"}},
		// 70 sentences of 30 runes: cut after the 66th, so with « … » it stays within 2,000 runes.
		{"long description cut at a sentence end", site.URL + "/long", []string{
			`maxlength="2000">` + strings.Repeat("Une phrase de vingt-neuf car. ", 65) + "Une phrase de vingt-neuf car.…</textarea>"}},
		{"venue page: its billetterie read and linked", belleVenue, []string{"Lien de billetterie trouvé sur la page de la salle.",
			`value="2027-04-23"`, `id="ticket_url" name="ticket_url" type="url" value="` + belleAgenda + `"`}},
		{"venue page, billetterie gone: the pasted link kept", belleVenue + "-gone", []string{"Vérifie les champs pré-remplis",
			`value="Les Hiboux &#43; Nova Test"`, `id="ticket_url" name="ticket_url" type="url" value="` + belleVenue + `-gone"`}},
		{"nothing found", site.URL + "/empty", []string{"Rien trouvé sur cette page\u202f: remplis", `aria-describedby="prefill-msg" autofocus`}},
		{"nothing acceptable", site.URL + "/rejected", []string{"Rien trouvé sur cette page"}},
		// Too long: dropped; an invisible character: stripped by prefill, the rest kept.
		{"hostile values dropped", site.URL + "/hostile", []string{`id="artist" name="artist" type="text" value=""`, `id="venue" name="venue" type="text" value="Salle"`, `id="address" name="address" type="text" value=""`, `value="2026-07-01"`}},
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
	// The failed hop is logged, with the host only.
	logs := ts.logs.String()
	assertContains(t, logs, `msg="prefill hop failed" err="fetch from billetterie.la-belle-electrique.com: status 404"`)
	if strings.Contains(logs, "session") {
		t.Errorf("the log holds the billetterie URL:\n%s", logs)
	}

	if resp, _ := ts.browser(t).get(t, newURL+url.QueryEscape(site.URL+"/dice.html")); resp.StatusCode != http.StatusSeeOther {
		t.Errorf("anonymous prefill: status = %d, want 303 to the picker", resp.StatusCode)
	}
}

func TestRefreshConcert(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	site := ticketSite(t)
	b, secret := ts.member(t, "Léa")
	g := "/g/" + secret
	zenith := site.URL + "/zenith.html" // Nova Test, 2027-01-24 20:00, Arène Exemple, with an address
	b.postRedirect(t, g+"/concerts", concertValues("Mine", "2027-01-25", "", "V", zenith, ""))

	full := concertValues("Mine", "2027-01-25", "21:00", "V", zenith, "2026-12-01")
	full.Set("address", "Ici")
	tests := []struct {
		name string
		form url.Values
		want []string
	}{
		{"empty fields filled, typed ones kept", concertValues("Mine", "", "", "Ma salle", zenith, ""), []string{
			"Complété depuis la billetterie : Date, Heure, Adresse. Vérifie puis enregistre.",
			`value="Mine"`, `value="Ma salle"`, `id="time" name="time" type="time" value="20:00">`,
			`id="date" name="date" type="date" value="2027-01-24" required aria-describedby="prefill-msg" autofocus`,
			`value="12, esplanade Exemple, 38000 GRENOBLE"`}},
		{"price and description filled", concertValues("Mine", "2027-01-25", "21:00", "V", site.URL+"/belle-electrique.html", ""), []string{
			"Complété depuis la billetterie\u202f: Adresse, Prix, Description.", `value="26–29 €"`,
			">Une soirée, cinq figures du collectif."}},
		// The page's artist (Nova Test) does not replace one typed but invalid.
		{"invalid field kept", concertValues("Mi\u200bne", "2027-01-25", "21:00", "V", zenith, ""), []string{
			`id="artist" name="artist" type="text" value="Mi` + "\u200b" + `ne"`}},
		{"nothing to fill", full, []string{
			"Rien de plus sur la billetterie.", `aria-describedby="prefill-msg" autofocus>Relire la billetterie`}},
		{"unreadable page", concertValues("Mine", "2027-01-25", "", "V", site.URL+"/blocked", ""), []string{
			"Impossible de lire cette page : essaie le lien de la salle", `aria-describedby="prefill-msg" autofocus>Relire la billetterie`}},
		// A venue page saved as the link: replaced by the billetterie it names, the one exception to empty fields only.
		{"venue link replaced", concertValues("Mine", "2027-01-25", "21:00", "V", belleVenue, ""), []string{
			"Adresse, Lien de billetterie, Prix, Description.", `id="ticket_url" name="ticket_url" type="url" value="` + belleAgenda + `"`}},
		{"no ticket link", concertValues("", "", "", "V", "", ""), []string{
			`id="ticket_url-error" class="error">Indique d&#39;abord le lien de billetterie.`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, body := b.post(t, g+"/concerts/1/refresh", tt.form)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d", resp.StatusCode)
			}
			assertContains(t, body, tt.want...)
			if strings.Contains(body, `id="artist-error"`) {
				t.Errorf("other fields' errors shown before « Enregistrer »")
			}
		})
		ts.clock.Advance(10 * time.Second) // so the table never hits fetchesPerMinute
	}

	// Nothing is saved before « Enregistrer ».
	if _, body := b.follow(t, g+"/concerts/1"); strings.Contains(body, "GRENOBLE") || !strings.Contains(body, "2027-01-25") {
		t.Errorf("refresh saved the concert:\n%s", body)
	}
	if _, body := b.get(t, g+"/concerts/new"); strings.Contains(body, "Relire") {
		t.Errorf("« Relire la billetterie » on the add form")
	}
	if resp, _ := b.post(t, g+"/concerts/99/refresh", validConcert()); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown concert: status = %d, want 404", resp.StatusCode)
	}
	if resp, _ := ts.browser(t).post(t, g+"/concerts/1/refresh", validConcert()); resp.StatusCode != http.StatusSeeOther {
		t.Errorf("anonymous refresh: status = %d, want 303 to the picker", resp.StatusCode)
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

// TestFitParagraphsCut: a long description is cut at a line or sentence end, else between words,
// either in the second half of what fits; else at the limit.
func TestFitParagraphsCut(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		in, want string
		max      int
	}{
		{strings.Repeat("motus ", 400), strings.Repeat("motus ", 332) + "motus…", 2000},                  // the cut falls inside a word
		{strings.Repeat("mot ", 600), strings.Repeat("mot ", 499) + "mot…", 2000},                        // the cut ends a word: kept
		{"aaaa bbbb\ncc", "aaaa bbbb…", 10},                                                              // ended by a line feed too
		{"Un. Deux trois quatre\nfin", "Un. Deux trois quatre…", 22},                                     // a whole line, not the sentence before it
		{"Concert\n" + strings.Repeat("mot ", 30), "Concert\n" + strings.Repeat("mot ", 7) + "mot…", 40}, // a line end too early: between words
		{"Concert\n" + strings.Repeat("x", 60), "Concert\n" + strings.Repeat("x", 31) + "…", 40},         // nothing in the second half: at the limit
		{strings.Repeat("a", 22) + "\n" + strings.Repeat("é ", 20), strings.Repeat("a", 22) + "…", 40},   // past half in runes, not in bytes
	} {
		if got := fitParagraphs(tt.in, tt.max); got != tt.want {
			tail := func(s string) string { r := []rune(s); return string(r[max(0, len(r)-8):]) }
			t.Errorf("fitParagraphs: %d runes ending %q, want %d ending %q", utf8.RuneCountInString(got), tail(got), utf8.RuneCountInString(tt.want), tail(tt.want))
		}
	}
}
