package web

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
			if got := !strings.Contains(body, "Qui es-tu"); got != tt.identified {
				t.Fatalf("identified = %v, want %v", got, tt.identified)
			}
			if cc := resp.Header.Get("Cache-Control"); cc != "private, no-cache" {
				t.Errorf("Cache-Control = %q", cc)
			}
		})
	}

	resp, _ := ts.browser(t).post(t, "/g/"+secret+"/me", url.Values{"name": {"Sam"}})
	c := resp.Cookies()[0]
	if c.Path != "/g/"+secret || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Secure || c.MaxAge != 400*24*3600 {
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

	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "t.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	// Links append /g/<secret> to it, and the proxy sends the whole host to the app.
	for _, bad := range []string{"", "concerts.example", "ftp://concerts.example", "https://", "https://h/sub",
		"https://h/?x", "https://h#x", "https://u@h"} {
		if _, err := New(st, slog.New(slog.DiscardHandler), Config{BaseURL: bad, Now: time.Now}); err == nil {
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
