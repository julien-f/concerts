package web

import (
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

// headings returns the h2 texts of a page, in order.
func headings(body string) []string {
	var hs []string
	for _, m := range regexp.MustCompile(`<h2[^>]*>([^<]*)</h2>`).FindAllStringSubmatch(body, -1) {
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
	order := regexp.MustCompile(`id="c(\d)"`).FindAllStringSubmatch(body, -1)
	var ids string
	for _, m := range order {
		ids += m[1]
	}
	if ids != "25134" { // today and June, July, then past newest first
		t.Errorf("card order = %s, want 25134", ids)
	}
	past := body[strings.Index(body, `<details class="past">`):]
	if strings.Contains(past, `name="status"`) {
		t.Errorf("past concerts offer status buttons")
	}
	if _, page := b.get(t, g+"/concerts/3"); strings.Contains(page, `name="status"`) {
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
	b.postRedirect(t, g+"/concerts/1/status", url.Values{"status": {"interested"}})
	if err := ts.store.SetStatus(t.Context(), 1, 3, 1, "ticket"); err != nil { // past: no buttons left to answer
		t.Fatal(err)
	}

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
