package web

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestPublicCard: a concert's public link shows outsiders the concert and a count, nothing of the group.
func TestPublicCard(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	lea, secret := ts.member(t, "Léa")
	g := "/g/" + secret
	v := concertValues("Zaz", "2026-07-01", "20:30", "Olympia", "https://tickets.example/z", "2026-06-05")
	v.Set("price", "20 €")
	lea.postRedirect(t, g+"/concerts", v)
	c := ts.concertURL(t, secret, 1)
	outsider := ts.browser(t)

	_, body := outsider.get(t, c)
	assertContains(t, body, "<title>Zaz · Concerts</title>", `<span class="brand">Concerts</span>`, "<h1>Zaz</h1>",
		"20 €", ">Écouter<",
		"Olympia", "Mise en vente : ", `href="https://tickets.example/z"`, `href="`+c+`/agenda.ics"`,
		"Tu fais partie du groupe ?")
	assertLacks(t, body, "Léa", "/g/", secret, "/logout", "personne", "Concerts · g")

	lea.postRedirect(t, g+"/concerts/1/status", url.Values{"status": {"going"}})
	_, body = outsider.get(t, c)
	assertContains(t, body, "1 personne du groupe y va")
	tom := ts.browser(t)
	tom.join(t, secret, "Tom")
	tom.postRedirect(t, g+"/concerts/1/status", url.Values{"status": {"going"}})
	sam := ts.browser(t)
	sam.join(t, secret, "Sam")
	sam.postRedirect(t, g+"/concerts/1/status", url.Values{"status": {"interested"}})
	resp, body := outsider.get(t, c+"?saved=1")
	assertContains(t, body, "2 personnes du groupe y vont")
	assertLacks(t, body, "Léa", "Tom", "Sam", "Modifications enregistrées")
	if got := resp.Header.Get("Vary"); got != "Cookie" {
		t.Errorf("Vary = %q, want Cookie", got)
	}
	if strings.Contains(ts.logs.String(), strings.TrimPrefix(c, "/c/")) {
		t.Errorf("the log holds the token:\n%s", ts.logs.String())
	}
}

// TestPublicLinkForMembers: the cookie a group visit leaves lets the same link show the full page.
func TestPublicLinkForMembers(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	lea, secret := ts.member(t, "Léa")
	g := "/g/" + secret
	lea.postRedirect(t, g+"/concerts", validConcert())
	c := ts.concertURL(t, secret, 1)

	resp, _ := lea.get(t, g+"/concerts/1?saved=1")
	if got := location(t, resp); got != c+"?saved=1" {
		t.Fatalf("group concert URL: Location = %q, want %q", got, c+"?saved=1")
	}
	_, body := lea.get(t, c)
	assertContains(t, body, "<h1>A</h1>", "Ce n'est pas moi", `id="c1-going"`, "Modifier", `href="`+c+`/agenda.ics"`)

	lea.postRedirect(t, g+"/logout", nil)
	_, body = lea.get(t, c)
	assertContains(t, body, `href="`+g+`?next=/concerts/1"`, "Choisis ton prénom")
	assertLacks(t, body, "Ce n'est pas moi")

	// A cookie holding a rotated secret, another group's or a malformed one gives the card.
	other := ts.newGroup(t, "other")
	for name, cookie := range map[string]string{
		"another group's secret": other + ".",
		"malformed":              secret + ".x",
		"no separator":           secret,
	} {
		t.Run(name, func(t *testing.T) {
			b := ts.browser(t)
			req, err := http.NewRequest(http.MethodGet, ts.URL+c, nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Cookie", "g1="+cookie)
			_, body := b.do(t, req)
			assertContains(t, body, `<span class="brand">Concerts</span>`)
		})
	}
	if err := ts.store.RotateSecret(context.Background(), 1, NewSecret()); err != nil {
		t.Fatal(err)
	}
	_, body = lea.get(t, c)
	assertContains(t, body, `<span class="brand">Concerts</span>`)
	assertLacks(t, body, secret)
}

// TestAnswerWithoutGroupCookie: a tab opened before the group cookie existed still lands on the member page.
func TestAnswerWithoutGroupCookie(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	lea, secret := ts.member(t, "Léa")
	g := "/g/" + secret
	lea.postRedirect(t, g+"/concerts", validConcert())

	old := ts.browser(t)
	u, err := url.Parse(ts.URL + g)
	if err != nil {
		t.Fatal(err)
	}
	old.client.Jar.SetCookies(u, []*http.Cookie{{Name: memberCookie, Value: "1", Path: g, HttpOnly: true, SameSite: http.SameSiteLaxMode}}) //nolint:gosec // G124: the test server is plain http
	_, body := old.postRedirect(t, g+"/concerts/1/status", url.Values{"status": {"going"}, "back": {"concert"}})
	assertContains(t, body, "Ce n'est pas moi", "Y va : Léa")
}

// TestPublicLinkNotFound: unknown and deleted tokens answer alike, cookie or not.
func TestPublicLinkNotFound(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	lea, secret := ts.member(t, "Léa")
	g := "/g/" + secret
	lea.postRedirect(t, g+"/concerts", validConcert())
	deleted := ts.concertURL(t, secret, 1)
	lea.postRedirect(t, g+"/concerts/1/delete", nil)

	_, want := ts.get(t, "/c/unknown")
	assertContains(t, want, "Concert introuvable : il a été supprimé")
	for name, get := range map[string]func() (*http.Response, string){
		"deleted, outsider": func() (*http.Response, string) { return ts.get(t, deleted) },
		"deleted, member":   func() (*http.Response, string) { return lea.get(t, deleted) },
		"unknown, member":   func() (*http.Response, string) { return lea.get(t, "/c/unknown") },
	} {
		resp, body := get()
		if resp.StatusCode != http.StatusNotFound || body != want || resp.Header.Get("Vary") != "Cookie" {
			t.Errorf("%s: status %d, Vary %q, body %q; want 404, Cookie, %q", name, resp.StatusCode, resp.Header.Get("Vary"), body, want)
		}
	}
}
