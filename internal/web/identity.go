package web

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"unicode/utf8"

	"concerts/internal/store"
)

const (
	memberCookie = "member"
	maxNameRunes = 30
)

// CleanName applies the picker's first-name rules, for the operator's rename-member.
func CleanName(v string) (string, bool) { return cleanName(v, maxNameRunes) }

// groupRequest is what every /g/{secret} handler gets: the group, its base path
// and the member the identity cookie names (nil if none or stale).
type groupRequest struct {
	Group  store.Group
	Base   string // "/g/<secret>", prefix of every link in the group
	Me     *store.Member
	Notice string // <title> prefix (banner or « Erreur »), heard after a no-JS redirect
}

// inGroup resolves the secret (404 if unknown) and the identity cookie.
func (s *server) inGroup(h groupHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		secret := r.PathValue("secret")
		g, err := s.store.GroupBySecret(r.Context(), secret)
		if errors.Is(err, store.ErrNotFound) {
			s.notFound(w, r, nil)
			return
		}
		if err != nil {
			s.fail(w, r, err)
			return
		}
		w.Header().Set("Cache-Control", "private, no-cache")
		// Base only ever holds a secret that matched a group: a plain base64url segment.
		gr := &groupRequest{Group: g, Base: "/g/" + secret}
		if c, err := r.Cookie(memberCookie); err == nil {
			if id, err := strconv.ParseInt(c.Value, 10, 64); err == nil {
				m, err := s.store.Member(r.Context(), g.ID, id)
				switch {
				case err == nil:
					gr.Me = &m
				case !errors.Is(err, store.ErrNotFound):
					s.fail(w, r, err)
					return
				}
			}
		}
		h(w, r, gr)
	}
}

type groupHandler = func(http.ResponseWriter, *http.Request, *groupRequest)

// requireMe sends a request without identity to the picker.
func requireMe(h groupHandler) groupHandler {
	return func(w http.ResponseWriter, r *http.Request, gr *groupRequest) {
		if gr.Me == nil {
			seeOther(w, r, gr.Base)
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

// nextPage keeps only a concert page as the picker's return target, so it cannot redirect elsewhere.
var nextPage = regexp.MustCompile(`^/concerts/[0-9]+$`)

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
	name, ok := cleanName(p.Name, maxNameRunes)
	if !ok {
		p.Error = fmt.Sprintf("Indique un prénom de 1 à %d caractères, commençant par une lettre.", maxNameRunes)
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
		p.Error = fmt.Sprintf("Le groupe est complet (%d prénoms au plus).", store.MaxMembers)
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
