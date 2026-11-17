package web

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"concerts/internal/store"
)

func concertValues(artist, date, tm, venue, ticket, onSale string) url.Values {
	return url.Values{"artist": {artist}, "date": {date}, "time": {tm}, "venue": {venue},
		"ticket_url": {ticket}, "on_sale_date": {onSale}}
}

// validConcert is a minimal valid concert form.
func validConcert() url.Values { return concertValues("A", "2026-07-01", "", "V", "", "") }

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
		// The stub shows the date: the meta line keeps it for screen readers only.
		`<time datetime="2026-06-13T20:30"><span class="visually-hidden">sam. 13 juin · </span>20`+"\u00a0h\u00a0"+`30</time> · Zénith Paris`,
		// The share text holds the absolute concert link, %20-encoded.
		"https://wa.me/?text=Fontaines%20D.C.%2C%20sam.%2013%20juin", "http%3A%2F%2Fexample.test"+strings.ReplaceAll(ts.concertURL(t, secret, 1), "/", "%2F"),
		// The same « Partager » as the concert page.
		`data-share-url="http://example.test`+ts.concertURL(t, secret, 1)+`"`, `<script src="/static/share.`)
	assertLacks(t, body, "ajouté par") // the concert page shows it
	_, body = b.get(t, g)
	assertLacks(t, body, "share.js", "Partager")

	_, body = b.follow(t, g+"/concerts/1")
	assertContains(t, body, "<h1>Fontaines D.C.</h1>", "ajouté par Léa", "sam. 13 juin · 20\u00a0h\u00a030", `href="https://tickets.example/f?a=1&amp;b=2"`, "Mise en vente\u202f: ", "ven. 5 juin", "Modifier",
		`<header class="head `+stubStyle("Fontaines D.C.")+`">`,
		// The stub, drawn for the eyes; the year only when it is not this one.
		"<span class=\"label\" aria-hidden=\"true\"><span>sam.</span><b>13</b><span>juin</span><span class=\"hour\">20\u00a0h\u00a030</span></span>",
		// « Partager »: WhatsApp without JS, the share sheet's text and link for share.js.
		`href="https://wa.me/?text=Fontaines%20D.C.%2C%20sam.%2013%20juin`,
		"data-share-text=\"Fontaines D.C., sam. 13 juin · 20\u00a0h\u00a030, Zénith Paris\" data-share-url=\"http://example.test"+ts.concertURL(t, secret, 1)+`">`,
		`<script src="/static/share.`)

	// The edit form, and its re-rendering after an invalid edit, post back to this concert.
	_, body = b.get(t, g+"/concerts/1/edit")
	assertContains(t, body, "<title>Modifier le concert", `action="`+g+`/concerts/1"`, `value="Fontaines D.C."`, `value="20:30"`,
		`value="https://tickets.example/f?a=1&amp;b=2"`, `value="2026-06-05"`)
	resp, body := b.post(t, g+"/concerts/1", concertValues(" ", "2026-06-13", "20:30", "Zénith Paris", "", ""))
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("invalid edit: status = %d, want 422", resp.StatusCode)
	}
	assertContains(t, body, "<title>Erreur – Modifier le concert", `action="`+g+`/concerts/1"`, `value="2026-06-13"`, `value="Zénith Paris"`,
		`id="artist" name="artist" type="text" value="" required maxlength="100" aria-invalid="true" aria-describedby="artist-error" autofocus`)

	_, body = b.postRedirect(t, g+"/concerts/1", concertValues("Fontaines D.C.", "2027-01-09", "9:05", "Olympia", "", ""))
	assertContains(t, body, "<title>Concert modifié – ", "Modifications enregistrées", "<h1>Fontaines D.C.</h1>",
		"sam. 9 janv. 2027 · 9\u00a0h\u00a005", `datetime="2027-01-09T09:05"`, "Olympia",
		"<span>janv.</span><span>2027</span><span class=\"hour\">9\u00a0h\u00a005</span>")
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
	assertContains(t, body, "<title>Concert rétabli – ", "Concert rétabli\u202f: Fontaines D.C.", `id="c1"`)
	// The banner follows the state: once restored, « Annuler » is gone.
	if _, body = b.get(t, g+"?deleted=1"); strings.Contains(body, "restore") {
		t.Errorf("restored concert still offers « Annuler »")
	}
	if resp, _ := b.post(t, g+"/concerts/1/restore", nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("second restore: status = %d, want 404", resp.StatusCode)
	}
}

func TestConcertAddress(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	b, secret := ts.member(t, "Léa")
	g := "/g/" + secret

	form := concertValues("Zaz", "2026-06-13", "", "L'Olympia", "", "")
	form.Set("address", " 28 bd des Capucines & co, Paris ")
	b.postRedirect(t, g+"/concerts", form)
	_, body := b.follow(t, g+"/concerts/1")
	assertContains(t, body, "<p>28 bd des Capucines &amp; co, Paris<br>",
		`href="https://www.openstreetmap.org/search?query=L%27Olympia%2C&#43;28&#43;bd&#43;des&#43;Capucines&#43;%26&#43;co%2C&#43;Paris"`)
	_, body = b.get(t, g+"/concerts/1/edit")
	assertContains(t, body, `id="address" name="address" type="text" value="28 bd des Capucines &amp; co, Paris" maxlength="100" autocomplete="off"`)

	b.postRedirect(t, g+"/concerts/1", concertValues("Zaz", "2026-06-13", "", "L'Olympia", "", ""))
	if _, body = b.follow(t, g+"/concerts/1"); strings.Contains(body, "openstreetmap") {
		t.Errorf("map link shown without an address")
	}
}

func TestConcertListenLink(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	b, secret := ts.member(t, "Léa")
	g := "/g/" + secret

	b.postRedirect(t, g+"/concerts", concertValues("Simon & Garfunkel", "2026-06-13", "", "L'Olympia", "", ""))
	_, body := b.follow(t, g+"/concerts/1")
	assertContains(t, body,
		`href="https://www.youtube.com/results?search_query=Simon&#43;%26&#43;Garfunkel" rel="noopener noreferrer"><svg class="icon" aria-hidden="true"><use href="#i-play"/></svg>Écouter<span class="visually-hidden"> sur YouTube</span></a>`)

	// Several artists: one link each, named; « AC/DC » is not split.
	b.postRedirect(t, g+"/concerts", concertValues("Les Hiboux + AC/DC /  Zaz +", "2026-06-14", "", "L'Olympia", "", ""))
	_, body = b.follow(t, g+"/concerts/2")
	assertContains(t, body,
		`href="https://www.youtube.com/results?search_query=Les&#43;Hiboux" rel="noopener noreferrer"><svg class="icon" aria-hidden="true"><use href="#i-play"/></svg><span class="visually-hidden">Écouter </span>Les Hiboux<span class="visually-hidden"> sur YouTube</span></a>`,
		`href="https://www.youtube.com/results?search_query=AC%2FDC" rel="noopener noreferrer"><svg class="icon" aria-hidden="true"><use href="#i-play"/></svg><span class="visually-hidden">Écouter </span>AC/DC<span class="visually-hidden"> sur YouTube</span></a>`,
		`href="https://www.youtube.com/results?search_query=Zaz" rel="noopener noreferrer"><svg class="icon" aria-hidden="true"><use href="#i-play"/></svg><span class="visually-hidden">Écouter </span>Zaz<span class="visually-hidden"> sur YouTube</span></a>`)
}

func TestConcertDescriptionPrice(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	b, secret := ts.member(t, "Léa")
	g := "/g/" + secret

	form := concertValues("Zaz", "2026-06-13", "", "L'Olympia", "", "")
	form.Set("price", " 29–32 € ")
	form.Set("description", "Première <b>ligne</b>\r\n\r\n\r\nDeux & trois\r\n")
	b.postRedirect(t, g+"/concerts", form)

	_, body := b.get(t, g)
	assertContains(t, body, `L&#39;Olympia <span class="price">· 29–32 €</span>`)
	_, body = b.follow(t, g+"/concerts/1")
	assertContains(t, body, "<p>Prix : 29–32 €</p>",
		"<details class=\"about\"><summary>En savoir plus</summary>\n<p>Première &lt;b&gt;ligne&lt;/b&gt;\n\nDeux &amp; trois</p></details>")
	_, body = b.get(t, g+"/concerts/1/edit")
	assertContains(t, body, `id="price" name="price" type="text" value="29–32 €" maxlength="30"`,
		"<textarea id=\"description\" name=\"description\" rows=\"6\" maxlength=\"2000\">Première &lt;b&gt;ligne&lt;/b&gt;\n\nDeux &amp; trois</textarea>")

	b.postRedirect(t, g+"/concerts/1", concertValues("Zaz", "2026-06-13", "", "L'Olympia", "", ""))
	for path, absent := range map[string]string{g: `class="price"`, g + "/concerts/1": "Prix :"} {
		if _, body = b.follow(t, path); strings.Contains(body, absent) || strings.Contains(body, `class="about"`) {
			t.Errorf("%s: price or description shown once cleared", path)
		}
	}
	// The clearing edit is in the history, and undoing it brings both back.
	_, body = b.get(t, g+"/concerts/1/history")
	assertContains(t, body, "<li>Prix : 29–32 € → (vide)</li>")
	b.postRedirect(t, g+"/concerts/1/revisions/1/revert", nil)
	_, body = b.follow(t, g+"/concerts/1")
	assertContains(t, body, "<p>Prix : 29–32 €</p>", `<details class="about">`)
}

func TestConcertValidation(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	b, secret := ts.member(t, "Léa")
	tests := []struct{ name, field, value string }{
		{"missing artist", "artist", " "},
		{"artist too long", "artist", strings.Repeat("a", 101)},
		{"missing date", "date", ""},
		{"impossible date", "date", "2026-02-30"},
		{"bad time", "time", "25:00"},
		{"missing venue", "venue", ""},
		{"javascript link", "ticket_url", "javascript:alert(1)"},
		{"ftp link", "ticket_url", "ftp://x.example/f"},
		{"relative link", "ticket_url", "/billets"},
		{"non-ASCII host", "ticket_url", "https://x.example\u202e/a"},
		{"invalid UTF-8 link", "ticket_url", "https://x.example/\x80\x80"},
		{"address too long", "address", strings.Repeat("a", 101)},
		{"invisible character in address", "address", "1 rue X\u200b"},
		{"bad on-sale date", "on_sale_date", "demain"},
		{"price too long", "price", strings.Repeat("1", 31)},
		{"description too long", "description", strings.Repeat("a", 2001)},
		{"invisible character in description", "description", "Ex\u00ademple"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			form := validConcert()
			form.Set(tt.field, tt.value)
			resp, body := b.post(t, "/g/"+secret+"/concerts", form)
			if resp.StatusCode != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422", resp.StatusCode)
			}
			assertContains(t, body, "<title>Erreur – ", `id="`+tt.field+`-error"`,
				`aria-describedby="`+tt.field+`-error" autofocus`)
		})
	}
	if _, body := b.get(t, "/g/"+secret); !strings.Contains(body, "Aucun concert") {
		t.Errorf("an invalid form created a concert")
	}
}

// TestDuplicateConcert: a live concert with the same artist and date asks before adding another.
func TestDuplicateConcert(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t) // clock: 2026-06-01
	lea, secret := ts.member(t, "Léa")
	g := "/g/" + secret
	marc := ts.browser(t)
	marc.join(t, secret, "Marc")
	lea.postRedirect(t, g+"/concerts", concertValues("Fontaines D.C.", "2026-06-13", "20:30", "Zénith Paris", "", ""))

	dup := concertValues("FONTAINES d.c.", "2026-06-13", "21:00", "Zénith", "https://t.example/f", "2026-06-05")
	dup.Set("address", "1 rue X")
	resp, body := marc.post(t, g+"/concerts", dup)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", resp.StatusCode)
	}
	assertContains(t, body, "<title>Déjà dans la liste – ",
		"Fontaines D.C., sam. 13 juin · 20 h 30, est déjà dans la liste (Zénith Paris, ajouté par Léa).",
		`ajouté par Léa).
<a href="`+g+`/concerts/1" aria-describedby="duplicate" autofocus>Voir ce concert</a>`,
		`name="anyway" value="1" aria-describedby="duplicate">Ajouter quand même</button>`,
		// Every posted field is kept.
		`value="FONTAINES d.c."`, `value="21:00"`, `value="Zénith"`, `value="1 rue X"`, `value="https://t.example/f"`, `value="2026-06-05"`)
	if strings.Contains(body, "Corrige les champs") {
		t.Error("a duplicate is reported as invalid fields")
	}
	// « Enregistrer » stays the default button, so Enter in a field does not add the duplicate.
	if strings.Index(body, ">Enregistrer<") > strings.Index(body, ">Ajouter quand même<") {
		t.Error("« Ajouter quand même » comes before « Enregistrer »")
	}

	invalid := concertValues("Fontaines D.C.", "2026-06-13", "", "", "", "")
	invalid.Set("anyway", "1")
	resp, body = marc.post(t, g+"/concerts", invalid)
	if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, `id="venue-error"`) || strings.Contains(body, `id="duplicate"`) {
		t.Errorf("« Ajouter quand même » with an invalid field: status %d, want 422 with the field error only", resp.StatusCode)
	}

	marc.postRedirect(t, g+"/concerts", concertValues("Fontaines D.C.", "2026-06-14", "", "Zénith Paris", "", ""))

	ts.exec(t, "DELETE FROM members WHERE name = 'Léa'")
	_, body = marc.post(t, g+"/concerts", dup)
	assertContains(t, body, "est déjà dans la liste (Zénith Paris).")

	dup.Set("anyway", "1")
	_, body = marc.postRedirect(t, g+"/concerts", dup)
	assertContains(t, body, "Concert ajouté : FONTAINES d.c.")
}

func TestConcertAccess(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	b, secret := ts.member(t, "Léa")
	g := "/g/" + secret
	b.postRedirect(t, g+"/concerts", validConcert())
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
		{"anonymous concert page", anon, "GET", g + "/concerts/1", http.StatusSeeOther}, // to the public link
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
	b.postRedirect(t, g+"/concerts", validConcert())

	newcomer := ts.browser(t)
	_, body := newcomer.follow(t, g+"/concerts/1")
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

// TestOpenGraph: a link previewer never has the identity cookie, so the group link gives it
// the picker and a concert link the public card; both carry preview tags, escaped.
func TestOpenGraph(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	b, secret := ts.member(t, "Léa")
	g := "/g/" + secret
	b.postRedirect(t, g+"/concerts", concertValues(`Les "Rita" <Mitsouko>`, "2026-06-13", "20:30", "Zénith", "", ""))
	c := ts.concertURL(t, secret, 1)
	common := []string{
		`<meta property="og:image:width" content="1200">`,
		`<meta name="twitter:card" content="summary_large_image">`,
	}
	image := regexp.MustCompile(`<meta property="og:image" content="http://example\.test/static/og\.[0-9a-f]{16}\.png">`)
	previewer := ts.browser(t)

	for _, tt := range []struct {
		name string
		get  func() (*http.Response, string)
		want []string
	}{
		{"concert link", func() (*http.Response, string) { return previewer.get(t, c) }, []string{
			`<meta property="og:site_name" content="Concerts">`,
			`<link rel="canonical" href="http://example.test` + c + `">`,
			`<meta property="og:title" content="Les &#34;Rita&#34; &lt;Mitsouko&gt;">`,
			"<meta property=\"og:description\" content=\"sam. 13 juin · 20\u00a0h\u00a030 · Zénith\">",
			`<meta property="og:url" content="http://example.test` + c + `">`,
		}},
		{"group link", func() (*http.Response, string) { return previewer.get(t, g) }, []string{
			`<meta property="og:site_name" content="Concerts · g">`,
			`<meta property="og:title" content="g">`,
			"<meta property=\"og:description\" content=\"Qui va à quel concert\u202f? Ouvre le lien et choisis ton prénom.\">",
			`<meta property="og:url" content="http://example.test` + g + `">`,
		}},
		{"picker after an error", func() (*http.Response, string) {
			return previewer.post(t, g+"/me", url.Values{"name": {""}})
		}, []string{`<meta property="og:site_name" content="Concerts · g">`, `<meta property="og:title" content="g">`}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, body := tt.get()
			assertContains(t, body, append(tt.want, common...)...)
			if !image.MatchString(body) {
				t.Errorf("no absolute og:image")
			}
			head := body[:strings.Index(body, "</head>")]
			if strings.Contains(head, "Léa") {
				t.Errorf("a first name reaches the preview tags:\n%s", head)
			}
		})
	}
	if _, home := b.get(t, g); strings.Contains(home, "og:") {
		t.Errorf("the list page carries preview tags")
	}
}

// TestUpcomingLimit: only upcoming concerts count, as of the clock's Paris date.
func TestUpcomingLimit(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t) // clock: 2026-06-01
	b, secret := ts.member(t, "Léa")
	g := "/g/" + secret
	b.postRedirect(t, g+"/concerts", concertValues("Past", "2026-05-31", "", "V", "", "")) // id 1
	ts.exec(t, `WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < ?)
		INSERT INTO concerts (group_id, token, artist, date, venue, created_at)
		SELECT 1, lower(hex(randomblob(12))), 'A', '2026-06-01', 'V', '2026-01-01T00:00:00Z' FROM n`, store.MaxConcerts)
	for name, path := range map[string]string{"create": g + "/concerts", "edit into the future": g + "/concerts/1"} {
		resp, body := b.post(t, path, validConcert())
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Fatalf("%s at the limit: status = %d, want 422", name, resp.StatusCode)
		}
		assertContains(t, body, "Le groupe a atteint 500 concerts à venir : supprimes-en d'abord.")
	}
	if resp, _ := b.post(t, g+"/concerts", concertValues("Past", "2026-05-30", "", "V", "", "")); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("past create at the limit: status = %d, want 303", resp.StatusCode)
	}
	ts.clock.Advance(24 * time.Hour) // the 500 concerts of 2026-06-01 are now past
	if resp, _ := b.post(t, g+"/concerts", validConcert()); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("create the next day: status = %d, want 303", resp.StatusCode)
	}
}
