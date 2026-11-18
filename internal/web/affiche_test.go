package web

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"concerts/internal/store"
)

// importInto makes the group of secret follow source and imports events from it.
func (ts *testServer) importInto(t *testing.T, secret, source string, events ...store.Concert) {
	t.Helper()
	ctx := context.Background()
	g, err := ts.store.GroupBySecret(ctx, secret)
	if err != nil {
		t.Fatal(err)
	}
	if err := ts.store.Follow(ctx, g.ID, source); err != nil {
		t.Fatal(err)
	}
	var es []store.Event
	for _, c := range events {
		es = append(es, store.Event{SourceID: source + "/" + c.Artist, Concert: c})
	}
	if _, err := ts.store.ApplyImports(ctx, g.ID, source, es, "2026-06-01"); err != nil {
		t.Fatal(err)
	}
}

// afficheGroup is a member's group with a concert added by hand (1) and imports from two
// venues: 2 and 3 from salle-a (3 sold out), 4 from salle-b, 5 tomorrow from salle-a.
func afficheGroup(t *testing.T, ts *testServer) (*browser, string) {
	t.Helper()
	b, secret := ts.member(t, "Léa")
	b.postRedirect(t, "/g/"+secret+"/concerts", concertValues("Manuel", "2026-07-01", "", "V", "", ""))
	ts.importInto(t, secret, "salle-a",
		store.Concert{Artist: "Importé A", Date: "2026-07-02", Venue: "Salle A"},
		store.Concert{Artist: "Complet A", Date: "2026-07-03", Time: "20:00", Venue: "Salle A", Price: "29 €", SoldOut: true})
	ts.importInto(t, secret, "salle-b", store.Concert{Artist: "Importé B", Date: "2026-08-01", Venue: "Salle B"})
	ts.importInto(t, secret, "salle-a", store.Concert{Artist: "Demain A", Date: "2026-06-02", Venue: "Salle A"})
	return b, secret
}

func TestAfficheViews(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	b, secret := afficheGroup(t, ts)
	g := "/g/" + secret
	cards := func(body string) string { // ids in page order
		var ids []string
		for _, m := range regexp.MustCompile(`<article class="card" id="c(\d+)"`).FindAllStringSubmatch(body, -1) {
			ids = append(ids, m[1])
		}
		return strings.Join(ids, ",")
	}
	for _, tc := range []struct {
		name, query, cards string
		want               []string
	}{
		{"all", "", "1", []string{`href="` + g + `" aria-current="page">Tous</a>`, `href="` + g + `?affiche=1">À l'affiche</a>`}},
		{"mine", "?mine=1", "", nil},
		{"affiche", "?affiche=1", "5,2,3,4", []string{
			"<title>À l'affiche · g · Concerts</title>", `<h2 class="visually-hidden">À l'affiche</h2>`,
			"Concerts annoncés par les salles suivies. Réponds pour les ajouter à la liste du groupe.",
			`href="` + g + `?affiche=1" aria-current="page">À l'affiche</a>`,
			`<nav class="filters venues" aria-labelledby="venues-label"><span id="venues-label">Annoncé par</span>`,
			`<a href="` + g + `?affiche=1&amp;salle=salle-b">salle-b</a>`,
			`<p id="affiche-status" class="visually-hidden" role="status"></p>`,
			`<h2 class="month">juin 2026</h2>`, `<span class="sold">Complet</span>`, `datetime="2026-07-03T20:00"`,
			// The list's ticket, with its source and both answers.
			`<article class="card" id="c2">`, `<p class="meta announced">annoncé par salle-a</p>`,
			`<div role="group" aria-label="Ma réponse pour Importé A">`,
			`id="c2-interested" name="status" value="interested" aria-pressed="false"><svg class="icon" aria-hidden="true"><use href="#i-interested"/></svg>Ça m&#39;intéresse</button>`,
			`id="c2-going" name="status" value="going" aria-pressed="false"><svg class="icon" aria-hidden="true"><use href="#i-going"/></svg>J&#39;y vais</button>`,
			`name="back" value="affiche"`, `<input type="hidden" name="salle" value="">`}},
		{"affiche wins over mine", "?affiche=1&mine=1", "5,2,3,4", []string{`aria-current="page">À l'affiche</a>`}},
		{"one venue", "?affiche=1&salle=salle-b", "4", []string{
			`<a href="` + g + `?affiche=1&amp;salle=salle-b" aria-current="page">salle-b</a>`,
			`<input type="hidden" name="salle" value="salle-b">`}},
		{"venue not followed: ignored", "?affiche=1&salle=autre", "5,2,3,4", nil},
	} {
		_, body := b.get(t, g+tc.query)
		if got := cards(body); got != tc.cards {
			t.Errorf("%s: cards %q, want %q", tc.name, got, tc.cards)
		}
		assertContains(t, body, tc.want...)
		assertLacks(t, body, "Rien à l'affiche pour", `class="who"`)
		if n := strings.Count(body, `aria-current="page"`); n != 1+strings.Count(tc.query, "salle=salle-") {
			t.Errorf("%s: %d aria-current", tc.name, n)
		}
	}
	// No past section.
	_, body := b.get(t, g+"?affiche=1")
	assertLacks(t, body, "Passés")

	// An answer moves the import to the list; once past, an unanswered one shows nowhere.
	b.postRedirect(t, g+"/concerts/2/status", url.Values{"status": {"interested"}, "back": {"affiche"}})
	if _, body := b.get(t, g); cards(body) != "1,2" {
		t.Errorf("answered import not in « Tous »: cards %s", cards(body))
	}
	if _, body := b.get(t, g+"?affiche=1"); cards(body) != "5,3,4" {
		t.Errorf("answered import still « À l'affiche »: cards %s", cards(body))
	}
	ts.clock.Advance(3 * 24 * time.Hour) // June 4th: 5 is past
	for _, q := range []string{"", "?affiche=1", "?past=all"} {
		if _, body := b.get(t, g+q); strings.Contains(body, `id="c5"`) {
			t.Errorf("%q shows a past import nobody answered", q)
		}
	}
}

// TestAfficheSalle: the venue filter names only a followed venue, never what the URL says.
func TestAfficheSalle(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	b, secret := afficheGroup(t, ts)
	g := "/g/" + secret
	ts.importInto(t, secret, "salle-c")
	_, body := b.get(t, g+"?affiche=1&salle=salle-c")
	assertContains(t, body, "Rien à l'affiche pour salle-c.")
	_, body = b.get(t, g+"?affiche=1&salle=%3Cb%3Einvent%C3%A9e")
	assertLacks(t, body, "inventée", "Rien à l'affiche pour")
}

// TestAfficheAfterUnfollow: imports kept after an unfollow (edited) are still reachable.
func TestAfficheAfterUnfollow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ts := newTestServer(t)
	b, secret := afficheGroup(t, ts)
	g := "/g/" + secret
	b.postRedirect(t, g+"/concerts/2", concertValues("Importé A", "2026-07-02", "21:00", "Salle A", "", ""))
	gr, err := ts.store.GroupBySecret(ctx, secret)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{"salle-a", "salle-b"} {
		if _, err := ts.store.Unfollow(ctx, gr.ID, v); err != nil {
			t.Fatal(err)
		}
	}
	_, body := b.get(t, g)
	assertContains(t, body, `href="`+g+`?affiche=1">À l'affiche</a>`)
	_, body = b.get(t, g+"?affiche=1")
	assertContains(t, body, `id="c2"`)
	assertLacks(t, body, "Annoncé par")
}

func TestAfficheWithoutVenues(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	b, secret := ts.member(t, "Léa")
	g := "/g/" + secret
	_, body := b.get(t, g)
	assertLacks(t, body, "affiche")
	_, body = b.get(t, g+"?affiche=1")
	assertContains(t, body, "Rien à l'affiche pour l'instant.")
	assertLacks(t, body, "Annoncé par")
}

func TestAfficheToggle(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	b, secret := afficheGroup(t, ts)
	g := "/g/" + secret
	marc := ts.browser(t)
	marc.join(t, secret, "Marc")

	htmx := func(who *browser, id, status string) string {
		t.Helper()
		req := who.postRequest(t, g+"/concerts/"+id+"/status", url.Values{"status": {status}, "back": {"affiche"}})
		req.Header.Set("HX-Request", "true")
		resp, body := who.do(t, req)
		if resp.StatusCode != http.StatusOK || !strings.HasPrefix(body, `<div class="statuses">`) {
			t.Fatalf("want the status block, got %d:\n%s", resp.StatusCode, body)
		}
		return body
	}
	said := func(s string) string { return `<p id="affiche-status" data-hx-swap-oob="innerHTML">` + s + `</p>` }
	for _, tc := range []struct {
		name, id, status string
		who              *browser
		want             []string
	}{
		{"interested", "2", "interested", b, []string{`id="c2-interested" name="status" value="" aria-pressed="true"`,
			`id="c2-going" name="status" value="going" aria-pressed="false"`,
			said("Importé A\u202f: Ça m&#39;intéresse. Ajouté à la liste du groupe.")}},
		// Exclusive: « J'y vais » replaces « Ça m'intéresse ».
		{"going", "2", "going", b, []string{`id="c2-interested" name="status" value="interested" aria-pressed="false"`,
			`id="c2-going" name="status" value="" aria-pressed="true"`,
			said("Importé A\u202f: J&#39;y vais. Ajouté à la liste du groupe.")}},
		{"remove", "2", "", b, []string{`id="c2-going" name="status" value="going" aria-pressed="false"`,
			said("Retiré de la liste du groupe.")}},
		// Another member's answer keeps it in the list.
		{"remove mine, not Marc's", "3", "", b, []string{`aria-pressed="false"`,
			said("Complet A\u202f: réponse retirée, toujours dans la liste du groupe.")}},
	} {
		if tc.id == "3" {
			htmx(marc, "3", "going")
		}
		assertContains(t, htmx(tc.who, tc.id, tc.status), tc.want...)
	}
}

// TestAfficheAnswerNoJS: without JS, the answered card leaves « À l'affiche »: back there, at the next card of the same view.
func TestAfficheAnswerNoJS(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	b, secret := afficheGroup(t, ts) // cards 5, 2, 3 (salle-a), 4 (salle-b)
	g := "/g/" + secret
	for _, tc := range []struct {
		name, id, status, salle, want string
	}{
		{"next card", "2", "going", "", g + "?affiche=1#c3"},
		{"next card of the venue", "5", "interested", "salle-a", g + "?affiche=1&salle=salle-a#c3"},
		{"last card of the venue", "3", "going", "salle-a", g + "?affiche=1&salle=salle-a"},
		{"last card", "4", "interested", "", g + "?affiche=1"},
		{"venue not followed: ignored", "6", "going", "autre", g + "?affiche=1"},
	} {
		if tc.id == "6" {
			ts.importInto(t, secret, "salle-b", store.Concert{Artist: "Dernier", Date: "2026-09-01", Venue: "Salle B"})
		}
		resp, _ := b.post(t, g+"/concerts/"+tc.id+"/status", url.Values{"status": {tc.status}, "back": {"affiche"}, "salle": {tc.salle}})
		if loc := location(t, resp); loc != tc.want {
			t.Errorf("%s: Location = %q, want %q", tc.name, loc, tc.want)
		}
	}
	// Clearing it again from a stale page: the card is back, so is the anchor.
	resp, _ := b.post(t, g+"/concerts/4/status", url.Values{"status": {""}, "back": {"affiche"}})
	if loc := location(t, resp); loc != g+"?affiche=1#c4" {
		t.Errorf("cleared: Location = %q", loc)
	}
}

// TestClearLastAnswer: without JS, clearing the last answer of an import follows it back to « À l'affiche ».
func TestClearLastAnswer(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	b, secret := afficheGroup(t, ts)
	g := "/g/" + secret
	marc := ts.browser(t)
	marc.join(t, secret, "Marc")
	for _, id := range []string{"1", "2", "3", "4"} {
		b.postRedirect(t, g+"/concerts/"+id+"/status", url.Values{"status": {"going"}})
	}
	marc.postRedirect(t, g+"/concerts/3/status", url.Values{"status": {"going"}})
	for _, tc := range []struct {
		name, id, back, want string
	}{
		{"import from the list", "2", "list", g + "?affiche=1#c2"},
		{"import from mine", "4", "mine", g + "?affiche=1#c4"},
		{"import Marc still answers", "3", "list", g + "#c3"},
		{"added by hand", "1", "list", g + "#c1"},
	} {
		resp, _ := b.post(t, g+"/concerts/"+tc.id+"/status", url.Values{"status": {""}, "back": {tc.back}})
		if loc := location(t, resp); loc != tc.want {
			t.Errorf("%s: Location = %q, want %q", tc.name, loc, tc.want)
		}
	}

	// A past import shows nowhere once unanswered: back to the list, at no card.
	ts.clock.Advance(2 * 24 * time.Hour)
	for back, want := range map[string]string{"list": g, "mine": g + "?mine=1"} {
		b.postRedirect(t, g+"/concerts/5/status", url.Values{"status": {"going"}})
		resp, _ := b.post(t, g+"/concerts/5/status", url.Values{"status": {""}, "back": {back}})
		if loc := location(t, resp); loc != want {
			t.Errorf("past import from %s: Location = %q, want %q", back, loc, want)
		}
	}
}

// TestClearLastAnswerHTMX: the swapped block says where the card went.
func TestClearLastAnswerHTMX(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	b, secret := afficheGroup(t, ts)
	g := "/g/" + secret
	for _, id := range []string{"1", "2"} {
		b.postRedirect(t, g+"/concerts/"+id+"/status", url.Values{"status": {"going"}})
	}
	unanswer := func(id string) string {
		t.Helper()
		req := b.postRequest(t, g+"/concerts/"+id+"/status", url.Values{"status": {""}, "back": {"list"}})
		req.Header.Set("HX-Request", "true")
		_, body := b.do(t, req)
		return body
	}
	assertContains(t, unanswer("2"), `<p class="note" role="status">Retourné à l&#39;affiche.</p>`)
	assertLacks(t, unanswer("1"), "Retourné")
}

func TestImportConcertPage(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	b, secret := afficheGroup(t, ts)
	g := "/g/" + secret
	_, body := b.follow(t, g+"/concerts/2")
	assertContains(t, body, `<a class="back" href="`+g+`?affiche=1#c2">← À l'affiche</a>`, `<p class="meta">annoncé par salle-a</p>`)
	assertLacks(t, body, "ajouté par", "Tous les concerts")

	b.postRedirect(t, g+"/concerts/2/status", url.Values{"status": {"going"}, "back": {"concert"}})
	_, body = b.follow(t, g+"/concerts/2")
	assertContains(t, body, `href="`+g+`#c2">← Tous les concerts</a>`, "annoncé par salle-a")
	_, body = b.follow(t, g+"/concerts/1")
	assertContains(t, body, "ajouté par Léa")
	assertLacks(t, body, "annoncé par")

	// A past import nobody answered has no card to go back to.
	ts.clock.Advance(2 * 24 * time.Hour)
	_, body = b.follow(t, g+"/concerts/5")
	assertContains(t, body, `<a class="back" href="`+g+`">← Tous les concerts</a>`)
	assertLacks(t, body, "#c5", "À l'affiche</a>")
}

func TestVenueName(t *testing.T) {
	t.Parallel()
	names := map[string]string{"mc2": "MC2"}
	for _, tc := range []struct {
		lookup    func(string) string
		key, want string
	}{
		{nil, "mc2", "mc2"},
		{func(k string) string { return names[k] }, "mc2", "MC2"},
		{func(k string) string { return names[k] }, "ancienne", "ancienne"}, // no longer compiled in
	} {
		if got := (&server{venueName: tc.lookup}).venue(tc.key); got != tc.want {
			t.Errorf("venue(%q) = %q, want %q", tc.key, got, tc.want)
		}
	}
}

func TestDuplicateOfImport(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	b, secret := afficheGroup(t, ts)
	g := "/g/" + secret
	resp, body := b.post(t, g+"/concerts", concertValues("importé a", "2026-07-02", "", "Ailleurs", "", ""))
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", resp.StatusCode)
	}
	assertContains(t, body, "<title>Déjà à l&#39;affiche – ",
		"Importé A, jeu. 2 juil., est à l'affiche (annoncé par salle-a), pas encore dans la liste. Réponds-y pour l'ajouter.",
		`<a href="`+g+`/concerts/2" aria-describedby="duplicate" autofocus>Voir ce concert</a>`)
	assertLacks(t, body, "Ajouter quand même")

	b.postRedirect(t, g+"/concerts/2/status", url.Values{"status": {"interested"}})
	_, body = b.post(t, g+"/concerts", concertValues("importé a", "2026-07-02", "", "Ailleurs", "", ""))
	assertContains(t, body, "est déjà dans la liste (Salle A)", "Ajouter quand même")

	// A past import nobody answered is in no view: it can be added anyway.
	ts.clock.Advance(2 * 24 * time.Hour)
	_, body = b.post(t, g+"/concerts", concertValues("demain a", "2026-06-02", "", "Ailleurs", "", ""))
	assertContains(t, body, "Ajouter quand même")
	assertLacks(t, body, "est à l'affiche")
}

func TestRestoreImport(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	b, secret := afficheGroup(t, ts)
	g := "/g/" + secret
	b.postRedirect(t, g+"/concerts/2/delete", nil)
	resp, _ := b.post(t, g+"/concerts/2/restore", nil)
	if loc := location(t, resp); loc != g+"?affiche=1&restored=2#c2" {
		t.Fatalf("Location = %q", loc)
	}
	_, body := b.get(t, g+"?affiche=1&restored=2")
	assertContains(t, body, "Concert rétabli\u202f: Importé A.", `id="c2"`)

	// Banners show only with their card: not in a view that lacks it.
	for _, q := range []string{"?restored=2", "?added=2", "?affiche=1&restored=1", "?affiche=1&salle=salle-b&restored=2"} {
		_, body := b.get(t, g+q)
		assertLacks(t, body, "Concert rétabli", "Concert ajouté")
	}
	_, body = b.get(t, g+"?mine=1&added=1")
	assertLacks(t, body, "Concert ajouté")
	_, body = b.get(t, fmt.Sprintf("%s?added=%d", g, 1))
	assertContains(t, body, "Concert ajouté\u202f: Manuel.")

	// A past import nobody answered shows nowhere: no banner nor anchor to aim at.
	ts.clock.Advance(2 * 24 * time.Hour)
	b.postRedirect(t, g+"/concerts/5/delete", nil)
	resp, _ = b.post(t, g+"/concerts/5/restore", nil)
	if loc := location(t, resp); loc != g {
		t.Errorf("past import restored: Location = %q, want %q", loc, g)
	}
}
