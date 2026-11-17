package web

import (
	"net/http"
	"strings"
	"testing"

	"concerts/internal/store"
)

// location is the path and query a 303 points to.
func location(t *testing.T, resp *http.Response) string {
	t.Helper()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", resp.StatusCode)
	}
	return resp.Header.Get("Location")
}

func assertLacks(t *testing.T, body string, unwanted ...string) {
	t.Helper()
	for _, s := range unwanted {
		if strings.Contains(body, s) {
			t.Errorf("body has %q:\n%s", s, body)
		}
	}
}

// TestEditHistory walks edit, undo from the banner and going back from the history page.
func TestEditHistory(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t) // clock: Mon 1 June 2026, 22:00 in Paris
	lea, secret := ts.member(t, "Léa")
	tom := ts.browser(t)
	tom.join(t, secret, "Tom")
	g := "/g/" + secret
	c1 := g + "/concerts/1"
	revert := func(rev string) string { return c1 + "/revisions/" + rev + "/revert" }
	lea.postRedirect(t, g+"/concerts", concertValues("A", "2026-07-01", "", "V", "", ""))

	_, body := lea.follow(t, c1)
	assertLacks(t, body, "modifié", "/history")
	_, body = lea.follow(t, c1+"/history")
	assertContains(t, body, "<title>Historique · A · g</title>", "Aucune modification.")

	// An edit records who changed what; the banner offers to undo it.
	resp, _ := tom.post(t, c1, concertValues("A", "2026-07-01", "20:30", "W", "", ""))
	if got := location(t, resp); got != c1+"?saved=1" {
		t.Fatalf("after an edit: Location = %q, want ?saved=1", got)
	}
	_, body = tom.follow(t, c1+"?saved=1")
	assertContains(t, body, "Modifications enregistrées.", `action="`+revert("1")+`"><button class="button secondary">Annuler</button>`, "modifié par Tom · ", `href="`+c1+`/history"`)
	_, body = tom.follow(t, c1+"/history")
	assertContains(t, body, "Tom · lun. 1 juin à 22\u00a0h",
		"<li>Heure\u202f: (vide) → 20\u00a0h\u00a030</li>", "<li>Salle\u202f: V → W</li>",
		`action="`+revert("1")+`"`, "Revenir avant cette modification")
	assertLacks(t, body, "Artiste\u202f:")

	// An unchanged form records nothing and offers no undo.
	resp, _ = tom.post(t, c1, concertValues("A", "2026-07-01", "20:30", "W", "", ""))
	if got := location(t, resp); got != c1+"?saved" {
		t.Fatalf("after a no-op edit: Location = %q, want ?saved", got)
	}
	// The banner offers undo only for the concert's newest revision.
	for q, want := range map[string]bool{"?saved": false, "?saved=abc": false, "?saved=99": false, "?reverted=1": true} {
		_, body = tom.follow(t, c1+q)
		if got := strings.Contains(body, `action="`+revert("1")+`"`); got != want {
			t.Errorf("%s: undo form shown = %v, want %v", q, got, want)
		}
	}

	// Once someone else edits, the old banner no longer offers to undo.
	lea.postRedirect(t, c1, concertValues("B", "2026-07-01", "20:30", "W", "", ""))
	_, body = tom.follow(t, c1+"?saved=1")
	assertContains(t, body, "Modifications enregistrées.")
	assertLacks(t, body, "/revert")

	// Undo is an edit too, so it can be undone in turn.
	resp, _ = lea.post(t, revert("2"), nil)
	if got := location(t, resp); got != c1+"?reverted=3" {
		t.Fatalf("after undo: Location = %q, want ?reverted=3", got)
	}
	_, body = lea.follow(t, c1+"?reverted=3")
	assertContains(t, body, "<title>Version rétablie – ", "Version rétablie.", "<h1>A</h1>", `action="`+revert("3")+`"`, "modifié par Léa")

	// Going back before Tom's edit also undoes the newer ones.
	_, body = lea.postRedirect(t, revert("1"), nil)
	assertContains(t, body, "<h1>A</h1>", "<p>V</p>")
	assertLacks(t, body, "20\u00a0h\u00a030")

	// Without an identity: history readable, no buttons; going back needs a first name.
	anon := ts.browser(t)
	_, body = anon.follow(t, c1+"/history")
	assertContains(t, body, "Tom · ")
	assertLacks(t, body, "/revert")
	_, body = anon.follow(t, c1+"?saved=4")
	assertLacks(t, body, "/revert")
	resp, _ = anon.post(t, revert("1"), nil)
	if got := location(t, resp); got != g {
		t.Errorf("revert without identity: Location = %q, want the picker", got)
	}

	// A history entry stays once its author is deleted.
	if err := ts.store.DeleteMember(t.Context(), 1, "Tom"); err != nil {
		t.Fatal(err)
	}
	_, body = lea.follow(t, c1+"/history")
	assertContains(t, body, "Ancien membre · ")
}

func TestRevertErrors(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	b, secret := ts.member(t, "Léa")
	g := "/g/" + secret
	b.postRedirect(t, g+"/concerts", validConcert())
	b.postRedirect(t, g+"/concerts/1", concertValues("B", "2026-05-01", "", "V", "", "")) // revision 1: A, 2026-07-01

	other, otherSecret := ts.member(t, "Zoé")
	other.postRedirect(t, "/g/"+otherSecret+"/concerts", validConcert()) // concert 2

	for name, path := range map[string]string{
		"revision of another concert":    g + "/concerts/2/revisions/1/revert",
		"concert of another group":       "/g/" + otherSecret + "/concerts/1/revisions/1/revert",
		"revision through another group": "/g/" + otherSecret + "/concerts/2/revisions/1/revert",
		"unknown revision":               g + "/concerts/1/revisions/9/revert",
		"invalid revision":               g + "/concerts/1/revisions/x/revert",
	} {
		br := b
		if strings.HasPrefix(path, "/g/"+otherSecret) {
			br = other
		}
		if resp, _ := br.post(t, path, nil); resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", name, resp.StatusCode)
		}
	}

	// A stored value that today's validation refuses is never written back.
	ts.exec(t, "UPDATE concert_revisions SET ticket_url = 'javascript:alert(1)' WHERE id = 1")
	resp, body := b.post(t, g+"/concerts/1/revisions/1/revert", nil)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("revert to an invalid value: status = %d, want 422", resp.StatusCode)
	}
	assertContains(t, body, "plus valide")
	ts.exec(t, "UPDATE concert_revisions SET ticket_url = NULL WHERE id = 1")

	// Going back to a future date counts against the limit.
	ts.exec(t, `WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < ?)
		INSERT INTO concerts (group_id, token, artist, date, venue, created_at)
		SELECT 1, lower(hex(randomblob(12))), 'A', '2026-06-01', 'V', '2026-01-01T00:00:00Z' FROM n`, store.MaxConcerts)
	if resp, _ := b.post(t, g+"/concerts/1/revisions/1/revert", nil); resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("revert past the limit: status = %d, want 422", resp.StatusCode)
	}
	if _, body := b.follow(t, g+"/concerts/1"); !strings.Contains(body, "<h1>B</h1>") {
		t.Errorf("refused revert changed the concert")
	}

	// A deleted concert has no history.
	b.postRedirect(t, g+"/concerts/1/delete", nil)
	if resp, _ := b.get(t, g+"/concerts/1/history"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("history of a deleted concert: status = %d, want 404", resp.StatusCode)
	}
	if resp, _ := b.post(t, g+"/concerts/1/revisions/1/revert", nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("revert of a deleted concert: status = %d, want 404", resp.StatusCode)
	}
}
