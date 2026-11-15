package web

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"concerts/internal/store"
)

const (
	memberCookie = "member"
	maxNameRunes = 30
)

// groupRequest is what every /g/{secret} handler gets: the group, its base path
// and the member the identity cookie names (nil if none or stale).
type groupRequest struct {
	Group  store.Group
	Base   string // "/g/<secret>", prefix of every link in the group
	Me     *store.Member
	Notice string // <title> prefix (banner or « Erreur »), heard after a no-JS redirect
}

// inGroup resolves the secret (404 if unknown) and the identity cookie.
func (s *server) inGroup(h func(http.ResponseWriter, *http.Request, *groupRequest)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		secret := r.PathValue("secret")
		g, err := s.store.GroupBySecret(r.Context(), secret)
		if errors.Is(err, store.ErrNotFound) {
			notFound(w)
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
	Members  []store.Member
	MaxRunes int
	Name     string        // refilled first-name field
	Error    string        // validation error on Name
	Taken    *store.Member // existing member whose name was typed again
}

func (s *server) home(w http.ResponseWriter, r *http.Request, gr *groupRequest) {
	if gr.Me == nil {
		s.picker(w, r, http.StatusOK, &pickerPage{groupRequest: gr})
		return
	}
	s.render(w, r, http.StatusOK, "group", gr)
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
		http.Redirect(w, r, gr.Base, http.StatusSeeOther)
		return
	}

	p := &pickerPage{groupRequest: gr, Name: r.PostForm.Get("name")}
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
		s.picker(w, r, http.StatusUnprocessableEntity, p)
	case errors.Is(err, store.ErrLimit):
		p.Error = fmt.Sprintf("Le groupe est complet (%d prénoms au plus).", store.MaxMembers)
		s.picker(w, r, http.StatusUnprocessableEntity, p)
	case err != nil:
		s.fail(w, r, err)
	default:
		s.remember(w, gr, m.ID)
		http.Redirect(w, r, gr.Base, http.StatusSeeOther)
	}
}

// logout is « Ce n'est pas moi »: it forgets the identity, for shared phones.
func (s *server) logout(w http.ResponseWriter, r *http.Request, gr *groupRequest) {
	s.setMember(w, gr, "", -1)
	http.Redirect(w, r, gr.Base, http.StatusSeeOther)
}
