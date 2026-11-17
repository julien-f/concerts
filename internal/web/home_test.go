package web

import (
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

var h2 = regexp.MustCompile(`<h2[^>]*>([^<]*)</h2>`)

// headings returns the h2 texts of a page, in order.
func headings(body string) []string {
	var hs []string
	for _, m := range h2.FindAllStringSubmatch(body, -1) {
		hs = append(hs, m[1])
	}
	return hs
}

func TestHomeMonthsAndPast(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t) // 2026-06-01 20:00 UTC, 22:00 in Paris
	b, secret := ts.member(t, "Léa")
	g := "/g/" + secret
	for _, c := range []url.Values{
		concertValues("Juillet", "2026-07-03", "", "V", "", ""),     // 1
		concertValues("Aujourd'hui", "2026-06-01", "", "V", "", ""), // 2
		concertValues("Hier", "2026-05-31", "", "V", "", ""),        // 3
		concertValues("Avril", "2026-04-10", "", "V", "", ""),       // 4
		concertValues("Juin", "2026-06-20", "21:00", "V", "", ""),   // 5
	} {
		b.postRedirect(t, g+"/concerts", c)
	}

	_, body := b.get(t, g)
	if got := strings.Join(headings(body), " | "); got != "juin 2026 | juillet 2026 | Passés (2)" {
		t.Fatalf("headings = %q", got)
	}
	order := regexp.MustCompile(`id="c(\d+)"`).FindAllStringSubmatch(body, -1)
	var ids string
	for _, m := range order {
		ids += m[1]
	}
	if ids != "25134" { // today and June, July, then past newest first
		t.Errorf("card order = %s, want 25134", ids)
	}
	if want := `class="stub ` + stubStyle("Juillet") + `"`; !strings.Contains(body, want) {
		t.Errorf("no %s in the list", want)
	}
	// Without a time, the meta line starts with the venue; screen readers still get the date.
	assertContains(t, body, `<time datetime="2026-07-03"><span class="visually-hidden">ven. 3 juil. · </span></time>V</p>`)
	past := body[strings.Index(body, "Passés (2)"):]
	if strings.Contains(past, `name="status"`) {
		t.Errorf("past concerts offer status buttons")
	}
	if _, page := b.follow(t, g+"/concerts/3"); strings.Contains(page, `name="status"`) {
		t.Errorf("a past concert page offers status buttons")
	}

	// Past midnight in Paris (still June 1st in UTC), today's concert becomes past.
	ts.clock.Advance(150 * time.Minute) // 22:30 UTC, 00:30 in Paris (UTC+2)
	if _, body = b.get(t, g); !strings.Contains(body, "Passés (3)") {
		t.Errorf("at 22:30 UTC (00:30 in Paris) the concert of June 1st is not past:\n%s", body)
	}

	// Restoring a past concert opens the section so its card is in view.
	b.postRedirect(t, g+"/concerts/4/delete", nil)
	_, body = b.postRedirect(t, g+"/concerts/4/restore", nil)
	assertContains(t, body, `<details class="past" open>`)
}

func TestHomeMineFilter(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	b, secret := ts.member(t, "Léa")
	g := "/g/" + secret

	_, body := b.get(t, g+"?mine=1")
	assertContains(t, body, `href="`+g+`?mine=1" aria-current="page"`, "Tu n'as encore réagi à aucun concert.")

	b.postRedirect(t, g+"/concerts", concertValues("Mien", "2026-07-01", "", "V", "", ""))
	b.postRedirect(t, g+"/concerts", concertValues("Autre", "2026-07-02", "", "V", "", ""))
	b.postRedirect(t, g+"/concerts", concertValues("Mien passé", "2026-05-01", "", "V", "", ""))
	marc := ts.browser(t)
	marc.join(t, secret, "Marc")
	marc.postRedirect(t, g+"/concerts/2/status", url.Values{"status": {"going"}})
	// Only a past concert answered: the filter says nothing upcoming is mine, and lists it.
	b.postRedirect(t, g+"/concerts/3/status", url.Values{"status": {"going"}}) // past: no button, but accepted
	_, body = b.get(t, g+"?mine=1")
	assertContains(t, body, "Aucun concert à venir parmi les tiens.", "Passés (1)")
	b.postRedirect(t, g+"/concerts/1/status", url.Values{"status": {"interested"}})

	resp, _ := b.post(t, g+"/concerts/1/status", url.Values{"status": {"going"}, "back": {"mine"}})
	if loc := resp.Header.Get("Location"); loc != g+"?mine=1#c1" {
		t.Errorf("back=mine: Location = %q", loc)
	}
	_, body = b.get(t, g+"?mine=1")
	assertContains(t, body, ">Mien</a>", "Passés (1)", `name="back" value="mine"`)
	if strings.Contains(body, ">Autre</a>") {
		t.Errorf("the filter shows a concert I did not answer")
	}
	_, body = b.get(t, g)
	assertContains(t, body, `href="`+g+`" aria-current="page"`, ">Autre</a>")
}

func TestHomePastShown(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t) // 2026-06-01
	b, secret := ts.member(t, "Léa")
	g := "/g/" + secret
	// Concert i is i days ago, so ids run newest first; Léa answered all but the first.
	ts.exec(t, `WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 32)
		INSERT INTO concerts (id, group_id, token, artist, date, venue, created_at)
		SELECT i, 1, lower(hex(randomblob(12))), 'A', date('2026-06-01', '-' || i || ' days'), 'V', '2026-01-01T00:00:00Z' FROM n`)
	ts.exec(t, "INSERT INTO statuses SELECT id, 1, 'going' FROM concerts WHERE id > 1")
	cards := regexp.MustCompile(`id="c\d+"`)

	for _, tc := range []struct {
		name, path string
		cards      int
		want       []string
		link       bool
	}{
		{"30 newest", g, 30, []string{"Passés (32)", `<details class="past">`,
			`<a class="more" href="` + g + `?past=all#c31">Afficher les 32 concerts passés</a>`}, true},
		{"all", g + "?past=all", 32, []string{"Passés (32)", `<details class="past" open>`, `id="c32"`}, false},
		{"filtered", g + "?mine=1", 30, []string{"Passés (31)",
			`<a class="more" href="` + g + `?past=all&amp;mine=1#c32">Afficher les 31 concerts passés</a>`}, true},
		{"banner on a hidden card", g + "?added=32", 32, []string{"Concert ajouté", `<details class="past" open>`, `id="c32"`}, false},
	} {
		_, body := b.get(t, tc.path)
		if n := len(cards.FindAllString(body, -1)); n != tc.cards {
			t.Errorf("%s: %d cards, want %d", tc.name, n, tc.cards)
		}
		assertContains(t, body, tc.want...)
		if got := strings.Contains(body, `class="more"`); got != tc.link {
			t.Errorf("%s: link to all = %v, want %v", tc.name, got, tc.link)
		}
	}

	b.postRedirect(t, g+"/concerts/32/delete", nil)
	_, body := b.postRedirect(t, g+"/concerts/32/restore", nil)
	assertContains(t, body, "Concert rétabli", `<details class="past" open>`, `id="c32"`)

	// Exactly pastShown past concerts: no link.
	b.postRedirect(t, g+"/concerts/2/status", url.Values{"status": {""}})
	if _, body = b.get(t, g+"?mine=1"); strings.Contains(body, `class="more"`) || !strings.Contains(body, "Passés (30)") {
		t.Errorf("30 past concerts: want no link to all, Passés (30):\n%s", body)
	}
}
