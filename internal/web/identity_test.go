package web

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"concerts/internal/store"
)

func TestJoin(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	secret := ts.newGroup(t, "g")
	b := ts.browser(t)
	b.join(t, secret, "Marc")
	long := strings.Repeat("é", 30)
	ts.browser(t).join(t, secret, long)

	tests := []struct {
		name     string
		form     url.Values
		status   int
		contains []string
	}{
		{"new name, extra spaces collapsed", url.Values{"name": {"  Léa \u00a0 B. "}}, http.StatusSeeOther, nil},
		{"existing member", url.Values{"member_id": {"1"}}, http.StatusSeeOther, nil},
		{"duplicate offers C'est moi and an initial", url.Values{"name": {"marc"}}, http.StatusUnprocessableEntity,
			[]string{"<title>Erreur – ", "Marc est déjà dans la liste", `name="member_id" value="1"`, `value="Marc "`, `aria-invalid="true"`, "autofocus"}},
		{"duplicate at the length limit gets no space", url.Values{"name": {long}}, http.StatusUnprocessableEntity,
			[]string{`value="` + long + `"`}},
		{"empty", url.Values{"name": {"   "}}, http.StatusUnprocessableEntity, []string{"de 1 à 30 caractères"}},
		{"punctuation first", url.Values{"name": {"-Marc"}}, http.StatusUnprocessableEntity, []string{"commençant par une lettre ou un chiffre"}},
		{"a number that is not a digit first", url.Values{"name": {"½Marc"}}, http.StatusUnprocessableEntity, []string{"commençant par une lettre ou un chiffre"}},
		{"too long", url.Values{"name": {strings.Repeat("é", 31)}}, http.StatusUnprocessableEntity, []string{"de 1 à 30 caractères"}},
		{"zero-width character", url.Values{"name": {"Ma\u200brc"}}, http.StatusUnprocessableEntity, []string{"de 1 à 30 caractères"}},
		{"bidi override", url.Values{"name": {"\u202eMarc"}}, http.StatusUnprocessableEntity, []string{"de 1 à 30 caractères"}},
		{"invalid UTF-8", url.Values{"name": {"Ma\xffrc"}}, http.StatusUnprocessableEntity, []string{"de 1 à 30 caractères"}},
		{"variation selector", url.Values{"name": {"Anna\ufe0f"}}, http.StatusUnprocessableEntity, []string{"de 1 à 30 caractères"}},
		{"Hangul filler", url.Values{"name": {"\u3164"}}, http.StatusUnprocessableEntity, []string{"de 1 à 30 caractères"}},
		{"braille blank", url.Values{"name": {"\u2800"}}, http.StatusUnprocessableEntity, []string{"de 1 à 30 caractères"}},
		{"leading combining mark", url.Values{"name": {"\u0301Anna"}}, http.StatusUnprocessableEntity, []string{"de 1 à 30 caractères"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			resp, body := ts.browser(t).post(t, "/g/"+secret+"/me", tt.form)
			if resp.StatusCode != tt.status {
				t.Fatalf("status = %d, want %d; body:\n%s", resp.StatusCode, tt.status, body)
			}
			assertContains(t, body, tt.contains...)
		})
	}
}

// TestJoinWithoutJS walks the picker as a browser without JS: 303, then full pages.
func TestJoinWithoutJS(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	secret := ts.newGroup(t, "g")

	body := ts.browser(t).join(t, secret, "Léa")
	for _, s := range []string{"<!doctype html>", "Léa", "Ce n'est pas moi", "Aucun concert"} {
		if !strings.Contains(body, s) {
			t.Errorf("page after joining lacks %q", s)
		}
	}

	// Another phone: Léa is listed, and picking her restores the identity.
	b := ts.browser(t)
	_, picker := b.get(t, "/g/"+secret)
	if !strings.Contains(picker, "C'est moi\u202f: Léa") {
		t.Fatalf("picker lacks Léa:\n%s", picker)
	}
	_, body = b.postRedirect(t, "/g/"+secret+"/me", url.Values{"member_id": {"1"}})
	if !strings.Contains(body, "Ce n'est pas moi") {
		t.Fatalf("picking Léa did not restore the identity:\n%s", body)
	}

	// « Ce n'est pas moi » forgets it.
	_, body = b.postRedirect(t, "/g/"+secret+"/logout", nil)
	if !strings.Contains(body, "Qui es-tu") {
		t.Fatalf("logout did not lead to the picker:\n%s", body)
	}
}

func TestIdentityCookie(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	secret := ts.newGroup(t, "g")
	other := ts.newGroup(t, "other")
	ts.browser(t).join(t, other, "Intrus") // member 1, in another group
	ts.browser(t).join(t, secret, "Léa")   // member 2

	tests := []struct {
		name, cookie string
		identified   bool
	}{
		{"own member", "2", true},
		{"member of another group", "1", false},
		{"unknown member", "99", false},
		{"garbage", "x", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req, _ := http.NewRequest(http.MethodGet, ts.URL+"/g/"+secret, nil)
			req.AddCookie(&http.Cookie{Name: memberCookie, Value: tt.cookie}) //nolint:gosec // G124: request cookie, attributes unused
			resp, body := ts.browser(t).do(t, req)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", resp.StatusCode)
			}
			// The header names the member only with an identity; the picker shows otherwise.
			if got := strings.Contains(body, "Ce n'est pas moi"); got != tt.identified {
				t.Fatalf("identified = %v, want %v", got, tt.identified)
			}
			if picker := strings.Contains(body, "Qui es-tu"); picker == tt.identified {
				t.Fatalf("picker shown = %v, want %v", picker, !tt.identified)
			}
			if cc := resp.Header.Get("Cache-Control"); cc != "private, no-cache" {
				t.Errorf("Cache-Control = %q", cc)
			}
		})
	}

	resp, _ := ts.browser(t).post(t, "/g/"+secret+"/me", url.Values{"name": {"Sam"}})
	cs := resp.Cookies()
	if len(cs) != 1 || cs[0].Name != memberCookie {
		t.Fatalf("cookies = %+v, want the member cookie", cs)
	}
	if c := cs[0]; c.Path != "/g/"+secret || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Secure || c.MaxAge != 400*24*3600 {
		t.Fatalf("cookie = %+v", c)
	}
}

func TestBaseURL(t *testing.T) {
	t.Parallel()
	ts := newTestServerWith(t, "HTTPS://concerts.example")
	resp, _ := ts.browser(t).post(t, "/g/"+ts.newGroup(t, "g")+"/me", url.Values{"name": {"Léa"}})
	if c := resp.Cookies(); len(c) != 1 || !c[0].Secure {
		t.Fatalf("cookies %+v: want a Secure cookie for an https base URL in any case", c)
	}

	// Links append /g/<secret> to it, and the proxy sends the whole host to the app.
	for _, bad := range []string{"", "concerts.example", "ftp://concerts.example", "https://", "https://h/sub",
		"https://h/?x", "https://h#x", "https://h#", "https://h:", "https://:443", "https://h:0", "https://h:99999", "https://u@h"} {
		if _, err := New(ts.store, slog.New(slog.DiscardHandler), Config{BaseURL: bad, Now: ts.clock.Now}); err == nil {
			t.Errorf("New(%q): want an error", bad)
		}
	}
}

// TestDeletedMemberCookie checks a deleted member's id is never reused by a newcomer.
func TestDeletedMemberCookie(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	secret := ts.newGroup(t, "g")
	troll := ts.browser(t)
	troll.join(t, secret, "Troll")
	g, _ := ts.store.GroupBySecret(context.Background(), secret)
	if err := ts.store.DeleteMember(context.Background(), g.ID, "Troll"); err != nil {
		t.Fatal(err)
	}
	ts.browser(t).join(t, secret, "Léa")
	if _, body := troll.get(t, "/g/"+secret); !strings.Contains(body, "Qui es-tu") {
		t.Fatalf("the deleted member's cookie names someone:\n%s", body)
	}
}

func TestGroupPostsRejected(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	secret := ts.newGroup(t, "g")
	for _, path := range []string{"/me", "/logout"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			b := ts.browser(t)
			form := url.Values{"name": {"Léa"}}
			req := b.postRequest(t, "/g/"+secret+path, form)
			req.Header.Set("Sec-Fetch-Site", "cross-site")
			if resp, _ := b.do(t, req); resp.StatusCode != http.StatusForbidden {
				t.Errorf("cross-site POST: status = %d, want 403", resp.StatusCode)
			}
			if resp, _ := b.post(t, "/g/"+NewSecret()+path, form); resp.StatusCode != http.StatusNotFound {
				t.Errorf("unknown secret: status = %d, want 404", resp.StatusCode)
			}
		})
	}
	if resp, _ := ts.browser(t).post(t, "/g/"+secret+"/me", url.Values{"name": {strings.Repeat("x", maxFormBytes)}}); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("oversized body: status = %d, want 400", resp.StatusCode)
	}
}

func TestMemberLimitMessage(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	secret := ts.newGroup(t, "g")
	g, _ := ts.store.GroupBySecret(context.Background(), secret)
	for i := range store.MaxMembers {
		if _, err := ts.store.CreateMember(context.Background(), g.ID, fmt.Sprint("m", i)); err != nil {
			t.Fatal(err)
		}
	}
	resp, body := ts.browser(t).post(t, "/g/"+secret+"/me", url.Values{"name": {"Léa"}})
	if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, "complet") {
		t.Fatalf("status = %d, body:\n%s", resp.StatusCode, body)
	}
}

func TestOtherGroups(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	ctx := context.Background()
	a := ts.newGroup(t, "Les copains")
	b := ts.newGroup(t, "Les collègues")
	c := ts.newGroup(t, "Escalade")
	link := func(secret, name string) string { return `<a href="/g/` + secret + `">` + name + `</a>` }
	id := func(secret string) int64 {
		g, err := ts.store.GroupBySecret(ctx, secret)
		if err != nil {
			t.Fatal(err)
		}
		return g.ID
	}

	// Léa joined B, only saw C's picker, then joined A: A lists B then C (group id order), B lists A and C.
	lea := ts.browser(t)
	lea.join(t, b, "Léa")
	lea.get(t, "/g/"+c)
	body := lea.join(t, a, "Léa")
	assertContains(t, body, "Autres groupes", link(b, "Les collègues")+"\n"+link(c, "Escalade"))
	assertLacks(t, body, link(a, "Les copains"))
	_, body = lea.get(t, "/g/"+b)
	assertContains(t, body, link(a, "Les copains"), link(c, "Escalade"))
	assertLacks(t, body, link(b, "Les collègues"))

	// The picker lists none; a browser that knows one group sees no switcher.
	sam := ts.browser(t)
	sam.get(t, "/g/"+b)
	_, body = sam.get(t, "/g/"+a)
	assertLacks(t, body, "Autres groupes")
	_, body = ts.browser(t).follow(t, "/g/"+a)
	assertLacks(t, body, "Autres groupes")

	// A GET moves the group cookie to Path=/ and expires its former Path=/c copy.
	resp, _ := lea.get(t, "/g/"+a)
	var paths []string
	for _, ck := range resp.Cookies() {
		if ck.Name == groupCookie(id(a)) {
			paths = append(paths, fmt.Sprintf("%s %d", ck.Path, ck.MaxAge))
		}
	}
	if want := []string{fmt.Sprintf("/ %d", 400*24*3600), "/c -1"}; strings.Join(paths, ",") != strings.Join(want, ",") {
		t.Errorf("group cookies = %v, want %v", paths, want)
	}
	if v := resp.Header.Get("Vary"); v != "Cookie" {
		t.Errorf("Vary = %q, want Cookie", v)
	}

	// Raw cookies, sent with Léa's identity in A.
	ms, err := ts.store.Members(ctx, id(a))
	if err != nil {
		t.Fatal(err)
	}
	me := fmt.Sprintf("%s=%d", memberCookie, ms[0].ID)
	rotated := ts.newGroup(t, "Rotated")
	rotatedID := id(rotated)
	if err := ts.store.RotateSecret(ctx, rotatedID, NewSecret()); err != nil {
		t.Fatal(err)
	}
	var many []string
	for i := range 11 {
		s := ts.newGroup(t, fmt.Sprintf("Many %d", i))
		many = append(many, fmt.Sprintf("g%d=%s.", id(s), s))
	}
	tests := []struct {
		name, cookies  string
		listed, absent []string
	}{
		{"rotated secret", fmt.Sprintf("g%d=%s.", rotatedID, rotated), nil, nil},
		{"another group's value", fmt.Sprintf("g%d=%s.", id(c), b), nil, nil},
		{"malformed", fmt.Sprintf("g%d=%s", id(b), b), nil, nil},
		{"own group only", fmt.Sprintf("g%d=%s.", id(a), a), nil, nil},
		{"valid", fmt.Sprintf("g%d=%s.", id(b), b), []string{link(b, "Les collègues")}, nil},
		{"first 10 cookies only", strings.Join(many, "; "), []string{"Many 0</a>", "Many 9</a>"}, []string{"Many 10</a>"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req, _ := http.NewRequest(http.MethodGet, ts.URL+"/g/"+a, nil)
			req.Header.Set("Cookie", me+"; "+tt.cookies)
			_, body := ts.browser(t).do(t, req)
			if tt.listed == nil {
				assertLacks(t, body, "Autres groupes")
			}
			assertContains(t, body, tt.listed...)
			assertLacks(t, body, tt.absent...)
		})
	}

	// Another group's cookie is never an identity here: without A's member cookie, A shows its picker.
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/g/"+a, nil)
	req.Header.Set("Cookie", fmt.Sprintf("g%d=%s.%d", id(b), b, ms[0].ID))
	if _, body := ts.browser(t).do(t, req); !strings.Contains(body, "Qui es-tu") {
		t.Fatalf("another group's cookie gave an identity:\n%s", body)
	}
}
