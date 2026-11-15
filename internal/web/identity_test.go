package web

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
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

	tests := []struct {
		name     string
		form     url.Values
		status   int
		contains []string
	}{
		{"new name, extra spaces collapsed", url.Values{"name": {"  Léa \u00a0 B. "}}, http.StatusSeeOther, nil},
		{"existing member", url.Values{"member_id": {"1"}}, http.StatusSeeOther, nil},
		{"duplicate offers C'est moi and an initial", url.Values{"name": {"marc"}}, http.StatusUnprocessableEntity,
			[]string{"<title>Erreur – ", "Marc est déjà dans la liste", `name="member_id" value="1"`, `value="marc "`, `aria-invalid="true"`, "autofocus"}},
		{"empty", url.Values{"name": {"   "}}, http.StatusUnprocessableEntity, []string{"de 1 à 30 caractères"}},
		{"too long", url.Values{"name": {strings.Repeat("é", 31)}}, http.StatusUnprocessableEntity, []string{"de 1 à 30 caractères"}},
		{"zero-width character", url.Values{"name": {"Ma\u200brc"}}, http.StatusUnprocessableEntity, []string{"de 1 à 30 caractères"}},
		{"bidi override", url.Values{"name": {"\u202eMarc"}}, http.StatusUnprocessableEntity, []string{"de 1 à 30 caractères"}},
		{"invalid UTF-8", url.Values{"name": {"Ma\xffrc"}}, http.StatusUnprocessableEntity, []string{"de 1 à 30 caractères"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			resp, body := ts.browser(t).post(t, "/g/"+secret+"/me", tt.form)
			if resp.StatusCode != tt.status {
				t.Fatalf("status = %d, want %d; body:\n%s", resp.StatusCode, tt.status, body)
			}
			for _, s := range tt.contains {
				if !strings.Contains(body, s) {
					t.Errorf("body lacks %q:\n%s", s, body)
				}
			}
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
	if !strings.Contains(picker, "C'est moi : Léa") {
		t.Fatalf("picker lacks Léa:\n%s", picker)
	}
	_, body = b.postRedirect(t, "/g/"+secret+"/me", url.Values{"member_id": {"1"}})
	if !strings.Contains(body, "Ce n'est pas moi") {
		t.Fatalf("picking Léa did not restore the identity:\n%s", body)
	}

	// « Ce n'est pas moi » forgets it.
	_, body = b.postRedirect(t, "/g/"+secret+"/logout", nil)
	if !strings.Contains(body, "Qui es-tu ?") {
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
			if got := !strings.Contains(body, "Qui es-tu ?"); got != tt.identified {
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

func TestSecureCookieWithHTTPSBaseURL(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"), (&fakeClock{}).Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.CreateGroup(ctx, "g", "s"); err != nil {
		t.Fatal(err)
	}
	h, err := New(st, slog.New(slog.NewTextHandler(io.Discard, nil)), "https://concerts.example")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/g/s/me", strings.NewReader("name=L%C3%A9a"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if c := rec.Result().Cookies(); rec.Code != http.StatusSeeOther || len(c) != 1 || !c[0].Secure {
		t.Fatalf("status %d, cookies %+v: want a Secure cookie", rec.Code, c)
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
			if resp, _ := b.post(t, "/g/"+secret+path, form, "Sec-Fetch-Site", "cross-site"); resp.StatusCode != http.StatusForbidden {
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
