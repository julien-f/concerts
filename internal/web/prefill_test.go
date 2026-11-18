package web

import (
	"context"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"concerts/internal/store"
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
				strings.Repeat("x", 101) + `","startDate":"2026-07-01T20:00","location":{"name":"Salle` + "\u200b" + `","address":"` + strings.Repeat("x", 101) + `"},"genre":"` + strings.Repeat("x", 61) + `"}</script>`))
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
			`value="20:00"`, `value="Salle Exemple"`, `id="address" name="address" type="text" value="Paris"`, `id="genre" name="genre" type="text" value="Rock &amp; Roll"`, `id="ticket_url" name="ticket_url" type="url" value="` + site.URL + `/dice.html"`, "autofocus"}},
		{"blocked site", site.URL + "/blocked", []string{"Impossible de lire cette page\u202f: essaie le lien de la salle ou de l", "ou remplis \u00e0 la main.", `name="ticket_url" type="url" value="` + site.URL + `/blocked"`}},
		{"og:title only", site.URL + "/accor.html", []string{`value="Duo Fictif"`}},
		{"price and description", site.URL + "/belle-electrique.html", []string{`id="price" name="price" type="text" value="26–29 €"`,
			"maxlength=\"2000\">Une soirée, cinq figures du collectif.\nL&#39;une d’elles chante.</textarea>"}},
		// 70 sentences of 30 runes: cut after the 66th, so with « … » it stays within 2,000 runes.
		{"long description cut at a sentence end", site.URL + "/long", []string{
			`maxlength="2000">` + strings.Repeat("Une phrase de vingt-neuf car. ", 65) + "Une phrase de vingt-neuf car.…</textarea>"}},
		{"nothing found", site.URL + "/empty", []string{"Rien trouvé sur cette page\u202f: remplis", `aria-describedby="prefill-msg" autofocus`}},
		{"nothing acceptable", site.URL + "/rejected", []string{"Rien trouvé sur cette page"}},
		// Too long: dropped; an invisible character: stripped by prefill, the rest kept.
		{"hostile values dropped", site.URL + "/hostile", []string{`id="artist" name="artist" type="text" value=""`, `id="venue" name="venue" type="text" value="Salle"`, `id="address" name="address" type="text" value=""`, `value="2026-07-01"`,
			`id="genre" name="genre" type="text" value=""`}},
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
	same := concertValues("Nova Test", "2027-01-24", "20:00", "Arène Exemple", zenith, "2026-12-01")
	same.Set("address", "12, esplanade Exemple, 38000 GRENOBLE")
	// « Utiliser » takes the value the form carries, without reading the page again (/blocked would fail).
	use := concertValues("Mine", "2027-01-25", "21:00", "V", site.URL+"/blocked", "")
	use.Set("use", "date")
	use.Set("source_date", "2027-01-24")
	use.Set("source_artist", "Nova Test")
	tests := []struct {
		name string
		form url.Values
		want []string
		lack []string
	}{
		{"empty fields filled, typed ones kept", concertValues("Nova Test", "", "", "Arène Exemple", zenith, ""), []string{
			"Complété depuis la billetterie : Date, Heure, Adresse. Vérifie puis enregistre.",
			`id="time" name="time" type="time" value="20:00">`,
			`id="date" name="date" type="date" value="2027-01-24" required aria-describedby="prefill-msg" autofocus`,
			`value="12, esplanade Exemple, 38000 GRENOBLE"`}, []string{"refresh-summary", "La billetterie indique"}},
		{"filled values differing: offered under the field", full, []string{
			`<p id="refresh-summary" class="prefill-msg">5 champs diffèrent : Artiste, Date, Heure, Salle, Adresse. Rien n&#39;est enregistré avant « Enregistrer ».</p>`,
			`id="artist" name="artist" type="text" value="Mine" required maxlength="100" aria-describedby="refresh-summary artist-source" autofocus>`,
			`id="venue" name="venue" type="text" value="V" required maxlength="100" aria-describedby="venue-source">`,
			`<input type="hidden" name="source_artist" value="Nova Test">`,
			`<p id="artist-source" class="source">La billetterie indique : Nova Test`,
			`<p id="date-source" class="source">La billetterie indique : dim. 24 janv. 2027`,
			`<button class="button secondary" name="use" value="date" formaction="` + g + `/concerts/1/refresh" formnovalidate`,
			`Utiliser<span class="visually-hidden"> pour Date</span></button>`,
			// Enter saves: the first submit button of the form is « Enregistrer », not « Utiliser ».
			`<button class="visually-hidden" tabindex="-1" aria-hidden="true">Enregistrer</button>`},
			[]string{"sold_out-source", "on_sale_date-source", "Complété"}},
		{"filled and differing", concertValues("Mine", "", "", "Arène Exemple", zenith, ""), []string{
			"Complété depuis la billetterie : Date, Heure, Adresse. 1 champ diffère : Artiste. Rien n&#39;est"}, nil},
		{"use one", use, []string{
			`<p id="refresh-summary" class="prefill-msg">Date : valeur de la billetterie reprise. 1 champ diffère : Artiste.`,
			`id="date" name="date" type="date" value="2027-01-24" required aria-describedby="refresh-summary" autofocus>`,
			`id="artist" name="artist" type="text" value="Mine" required maxlength="100" aria-describedby="artist-source">`,
			`<input type="hidden" name="source_artist" value="Nova Test">`},
			[]string{"date-source", "Impossible"}},
		{"price and description filled", concertValues("Mine", "2027-01-25", "21:00", "V", site.URL+"/belle-electrique.html", ""), []string{
			"Complété depuis la billetterie\u202f: Adresse, Prix, Description.", `value="26–29 €"`,
			">Une soirée, cinq figures du collectif."}, nil},
		// The page's artist (Nova Test) does not replace one typed but invalid.
		{"invalid field kept", concertValues("Mi\u200bne", "2027-01-25", "21:00", "V", zenith, ""), []string{
			`id="artist" name="artist" type="text" value="Mi` + "\u200b" + `ne"`}, nil},
		{"nothing new", same, []string{
			"Rien de nouveau sur la billetterie.", `aria-describedby="prefill-msg" autofocus>Relire la billetterie`},
			[]string{"refresh-summary"}},
		{"unreadable page", concertValues("Mine", "2027-01-25", "", "V", site.URL+"/blocked", ""), []string{
			"Impossible de lire cette page : essaie le lien de la salle", `aria-describedby="prefill-msg" autofocus>Relire la billetterie`}, nil},
		{"no ticket link", concertValues("", "", "", "V", "", ""), []string{
			`id="ticket_url-error" class="error">Indique d&#39;abord le lien de billetterie.`}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, body := b.post(t, g+"/concerts/1/refresh", tt.form)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d", resp.StatusCode)
			}
			assertContains(t, body, tt.want...)
			assertLacks(t, body, tt.lack...)
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

// TestRefreshFromVenue: an import compares with what its venue's agenda last said, without reading any page.
func TestRefreshFromVenue(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	b, secret := ts.member(t, "Léa")
	g := "/g/" + secret
	ts.importInto(t, secret, "salle-a", store.Concert{Artist: "Importé A", Date: "2026-07-02", Time: "20:00", Venue: "Salle A", Price: "20 €"})
	edited := concertValues("Importé A", "2026-07-02", "21:00", "Salle A", "", "")
	edited.Set("price", "18 €")
	edited.Set("genre", "Pop")
	b.postRedirect(t, g+"/concerts/1", edited)
	// Since then the venue moved it a day, sold out, no longer gives a price, and calls it rock.
	if err := ts.store.PutVenueEvents(context.Background(), "salle-a", []store.Event{{SourceID: "salle-a/Importé A",
		Concert: store.Concert{Artist: "Importé A", Date: "2026-07-03", Time: "20:00", Venue: "Salle A", Genre: "Rock", SoldOut: true}}}); err != nil {
		t.Fatal(err)
	}
	with := func(kv ...string) url.Values {
		v := url.Values{}
		for k, vs := range edited {
			v[k] = vs
		}
		for i := 0; i < len(kv); i += 2 {
			v.Set(kv[i], kv[i+1])
		}
		return v
	}
	for _, tc := range []struct {
		name       string
		form       url.Values
		want, lack []string
	}{
		{"differences, no link needed", edited, []string{
			"4 champs diffèrent : Date, Heure, Genre, Complet.",
			`id="date" name="date" type="date" value="2026-07-02" required aria-describedby="refresh-summary date-source" autofocus>`,
			"La billetterie indique : ven. 3 juil.", "La billetterie indique : 20 h",
			`<p id="genre-source" class="source">La billetterie indique : Rock`, `<input type="hidden" name="source_genre" value="Rock">`,
			`<p id="sold_out-source" class="source">La billetterie indique : complet`,
			`<input type="hidden" name="source_sold_out" value="1">`,
			`id="sold_out" name="sold_out" type="checkbox" value="1" aria-describedby="sold_out-source">`},
			// A price the venue no longer gives is never offered to clear.
			[]string{"price-source", "Indique d&#39;abord", "Rien de nouveau"}},
		{"use complet", with("use", "sold_out", "source_sold_out", "1", "source_date", "2026-07-03"), []string{
			`id="sold_out" name="sold_out" type="checkbox" value="1" checked aria-describedby="refresh-summary" autofocus>`,
			"Complet : valeur de la billetterie reprise. 1 champ diffère : Date."}, []string{"sold_out-source"}},
		{"use pas complet", with("sold_out", "1", "use", "sold_out", "source_sold_out", ""), []string{
			`id="sold_out" name="sold_out" type="checkbox" value="1" aria-describedby="refresh-summary" autofocus>`}, nil},
		{"« pas complet » still offered after another use", with("sold_out", "1", "use", "date", "source_date", "2026-07-03", "source_sold_out", ""), []string{
			"La billetterie indique : pas complet", `<input type="hidden" name="source_sold_out" value="">`}, nil},
	} {
		resp, body := b.post(t, g+"/concerts/1/refresh", tc.form)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: status = %d", tc.name, resp.StatusCode)
		}
		assertContains(t, body, tc.want...)
		assertLacks(t, body, tc.lack...)
	}
	// The same values: nothing new.
	_, body := b.post(t, g+"/concerts/1/refresh", with("date", "2026-07-03", "time", "20:00", "sold_out", "1", "genre", "Rock"))
	assertContains(t, body, "Rien de nouveau sur la billetterie.")
}

// TestFieldAttrs: an invalid field also described by a message gets one aria-describedby naming both.
func TestFieldAttrs(t *testing.T) {
	t.Parallel()
	tpl := template.Must(template.New("").Funcs(template.FuncMap{"when": func(any) string { return "" }, "venue": func(string) string { return "" }}).
		ParseFS(templatesFS, "templates/form.html"))
	var buf strings.Builder
	if err := tpl.ExecuteTemplate(&buf, "fieldAttrs", field{Name: "date", Error: "Date invalide.", DescribedBy: "refresh-summary date-source"}); err != nil {
		t.Fatal(err)
	}
	if got, want := buf.String(), ` required aria-invalid="true" aria-describedby="date-error refresh-summary date-source"`; got != want {
		t.Errorf("fieldAttrs = %q, want %q", got, want)
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
