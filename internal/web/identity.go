package web

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"concerts/internal/store"
	"concerts/internal/validate"
)

const (
	memberCookie = "member"
	maxNameRunes = 30
)

// CleanGroupName applies the concert form's text rules to the operator's new-group.
func CleanGroupName(v string) (string, bool) { return validate.Text(v, validate.MaxTextRunes) }

// groupRequest is what every /g/{secret} handler gets: the group, its base path
// and the member the identity cookie names (nil if none or stale).
type groupRequest struct {
	Group  store.Group
	Base   string // "/g/<secret>", prefix of every link in the group
	Me     *store.Member
	Notice string // <title> prefix (banner or « Erreur »), heard after a no-JS redirect
}

// resolve finds the group of secret (store.ErrNotFound if none) and the member named by
// memberID, ignored unless it is a member of that group.
func (s *server) resolve(ctx context.Context, secret, memberID string) (*groupRequest, error) {
	g, err := s.store.GroupBySecret(ctx, secret)
	if err != nil {
		return nil, err
	}
	// Base only ever holds a secret that matched a group: a plain base64url segment.
	gr := &groupRequest{Group: g, Base: "/g/" + secret}
	if id, err := strconv.ParseInt(memberID, 10, 64); err == nil {
		m, err := s.store.Member(ctx, g.ID, id)
		switch {
		case err == nil:
			gr.Me = &m
		case !errors.Is(err, store.ErrNotFound):
			return nil, err
		}
	}
	return gr, nil
}

// inGroup resolves the secret (404 if unknown) and the identity cookie.
func (s *server) inGroup(h groupHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var memberID string
		if c, err := r.Cookie(memberCookie); err == nil {
			memberID = c.Value
		}
		gr, err := s.resolve(r.Context(), r.PathValue("secret"), memberID)
		if errors.Is(err, store.ErrNotFound) {
			s.notFound(w, r, nil)
			return
		}
		if err != nil {
			s.fail(w, r, err)
			return
		}
		w.Header().Set("Cache-Control", "private, no-cache")
		w.Header().Set("Vary", "Cookie")
		// GET only: every write ends in one, which then records the member it left.
		if r.Method == http.MethodGet {
			s.setGroupCookie(w, gr)
		}
		h(w, r, gr)
	}
}

// setGroupCookie lets /c pages, whose URL lacks the secret, recognise a member, and other groups'
// pages list this one: "<secret>.<member id or nothing>".
func (s *server) setGroupCookie(w http.ResponseWriter, gr *groupRequest) {
	v := strings.TrimPrefix(gr.Base, "/g/") + "."
	if gr.Me != nil {
		v += strconv.FormatInt(gr.Me.ID, 10)
	}
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: Secure follows the base URL scheme (plain http in development)
		Name: groupCookie(gr.Group.ID), Value: v, Path: "/",
		MaxAge: 400 * 24 * 3600, HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteLaxMode,
	})
	// Expires the former Path=/c copy, which /c would read first; remove 400 days (its Max-Age) after this deploys.
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: as above
		Name: groupCookie(gr.Group.ID), Path: "/c",
		MaxAge: -1, HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteLaxMode,
	})
}

func groupCookie(groupID int64) string { return "g" + strconv.FormatInt(groupID, 10) }

// groupCookieName matches a group cookie's name, capturing the group id.
var groupCookieName = regexp.MustCompile(`^g([0-9]+)$`)

// maxOtherGroups bounds the queries the client-controlled cookies of one request can cause.
const maxOtherGroups = 10

// otherGroup links to another group this browser opened, in « Autres groupes ».
type otherGroup struct {
	ID         int64
	Name, Base string
}

// otherGroups lists the groups whose cookie the browser sends, except gr's, in id order: names and links
// only, never an identity. The first maxOtherGroups cookies are looked at, invalid ones included.
func (s *server) otherGroups(r *http.Request, gr *groupRequest) ([]otherGroup, error) {
	var gs []otherGroup
	seen := map[string]bool{groupCookie(gr.Group.ID): true}
	looked := 0
	for _, c := range r.Cookies() {
		n := groupCookieName.FindStringSubmatch(c.Name)
		if n == nil || seen[c.Name] {
			continue
		}
		seen[c.Name] = true
		if looked++; looked > maxOtherGroups {
			break
		}
		id, err := strconv.ParseInt(n[1], 10, 64)
		v := groupCookieValue.FindStringSubmatch(c.Value)
		if err != nil || v == nil {
			continue
		}
		g, err := s.store.GroupBySecret(r.Context(), v[1])
		switch {
		case errors.Is(err, store.ErrNotFound):
		case err != nil:
			return nil, err
		case g.ID == id:
			gs = append(gs, otherGroup{g.ID, g.Name, "/g/" + v[1]})
		}
	}
	slices.SortFunc(gs, func(a, b otherGroup) int { return cmp.Compare(a.ID, b.ID) })
	return gs, nil
}

// groupCookieValue matches a well-formed group cookie: a secret as NewSecret makes it, then an optional member id.
var groupCookieValue = regexp.MustCompile(`^([A-Za-z0-9_-]{22})\.([0-9]*)$`)

// fromCookie resolves the group cookie of groupID; nil if absent, malformed, rotated or another group's.
func (s *server) fromCookie(r *http.Request, groupID int64) (*groupRequest, error) {
	c, err := r.Cookie(groupCookie(groupID))
	if err != nil {
		return nil, nil
	}
	m := groupCookieValue.FindStringSubmatch(c.Value)
	if m == nil {
		return nil, nil
	}
	gr, err := s.resolve(r.Context(), m[1], m[2])
	if errors.Is(err, store.ErrNotFound) || (err == nil && gr.Group.ID != groupID) {
		return nil, nil
	}
	return gr, err
}

type groupHandler = func(http.ResponseWriter, *http.Request, *groupRequest)

// requireMe sends a request without identity to the picker, which returns to the page asked for (GET only).
func requireMe(h groupHandler) groupHandler {
	return func(w http.ResponseWriter, r *http.Request, gr *groupRequest) {
		if gr.Me == nil {
			target := gr.Base
			if next := validNext(strings.TrimPrefix(r.URL.Path, gr.Base)); r.Method == http.MethodGet && next != "" {
				target += "?next=" + next
			}
			seeOther(w, r, target)
			return
		}
		h(w, r, gr)
	}
}

// setMember sets the identity cookie; logout passes "" and -1 to clear it.
func (s *server) setMember(w http.ResponseWriter, gr *groupRequest, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: Secure follows the base URL scheme (plain http in development)
		Name: memberCookie, Value: value, Path: gr.Base,
		MaxAge: maxAge, HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteLaxMode,
	})
}

func (s *server) remember(w http.ResponseWriter, gr *groupRequest, id int64) {
	s.setMember(w, gr, strconv.FormatInt(id, 10), 400*24*3600)
}

type pickerPage struct {
	*groupRequest
	Next     string // group-relative page to return to after picking
	Members  []store.Member
	MaxRunes int
	Name     string        // refilled first-name field
	Error    string        // validation error on Name
	Taken    *store.Member // existing member whose name was typed again
}

// nextPage keeps only a group page that can be opened by a link as the picker's return target, so it cannot redirect elsewhere.
var nextPage = regexp.MustCompile(`^/concerts/(new|[0-9]+(/edit)?)$`)

func validNext(v string) string {
	if nextPage.MatchString(v) {
		return v
	}
	return ""
}

func (s *server) picker(w http.ResponseWriter, r *http.Request, status int, p *pickerPage) {
	ms, err := s.store.Members(r.Context(), p.Group.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	p.Members, p.MaxRunes = ms, maxNameRunes
	if p.Error != "" || p.Taken != nil {
		p.Notice = "Erreur"
	}
	s.render(w, r, status, "picker", p)
}

// join handles the picker: « C'est moi » buttons send member_id, the name form sends name.
func (s *server) join(w http.ResponseWriter, r *http.Request, gr *groupRequest) {
	if !parseForm(w, r) {
		return
	}
	next := validNext(r.PostForm.Get("next"))
	if v := r.PostForm.Get("member_id"); v != "" {
		id, _ := strconv.ParseInt(v, 10, 64)
		m, err := s.store.Member(r.Context(), gr.Group.ID, id)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			s.fail(w, r, err)
			return
		}
		if err == nil {
			s.remember(w, gr, m.ID)
		}
		seeOther(w, r, gr.Base+next)
		return
	}

	p := &pickerPage{groupRequest: gr, Next: next, Name: r.PostForm.Get("name")}
	name, ok := CleanName(p.Name)
	if !ok {
		p.Error = fmt.Sprintf("Indique un nom de 1 à %d caractères, commençant par une lettre ou un chiffre.", maxNameRunes)
		s.picker(w, r, http.StatusUnprocessableEntity, p)
		return
	}
	m, err := s.store.CreateMember(r.Context(), gr.Group.ID, name)
	switch {
	case errors.Is(err, store.ErrDuplicate):
		p.Taken = &m
		if p.Name = m.Name; utf8.RuneCountInString(p.Name) < maxNameRunes {
			p.Name += " " // ready for an initial
		}
		s.picker(w, r, http.StatusUnprocessableEntity, p)
	case errors.Is(err, store.ErrLimit):
		p.Error = fmt.Sprintf("Le groupe est complet (%d membres au plus).", store.MaxMembers)
		s.picker(w, r, http.StatusUnprocessableEntity, p)
	case err != nil:
		s.fail(w, r, err)
	default:
		s.remember(w, gr, m.ID)
		seeOther(w, r, gr.Base+next)
	}
}

// logout is « Ce n'est pas moi »: it forgets the identity, for shared phones.
func (s *server) logout(w http.ResponseWriter, r *http.Request, gr *groupRequest) {
	s.setMember(w, gr, "", -1)
	seeOther(w, r, gr.Base)
}

// seeOther answers 303 (Post/Redirect/Get, or to the picker). Every target is built from
// groupRequest.Base, the path of a secret that matched a group, never from raw input.
// An htmx request gets HX-Redirect instead: it would follow a 303 silently and
// swap the whole target page into the fragment.
func seeOther(w http.ResponseWriter, r *http.Request, target string) {
	if isHTMX(r) {
		w.Header().Set("HX-Redirect", target)
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther) //nolint:gosec // G710: see above
}

func isHTMX(r *http.Request) bool { return r.Header.Get("HX-Request") == "true" }
