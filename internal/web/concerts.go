package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"concerts/internal/store"
)

// concertForm is the add and edit page; Errors and Focus are keyed by field name ("" for the form).
type concertForm struct {
	*groupRequest
	Title  string // page heading
	Action string // POST target
	Cancel string // link back
	store.Concert
	Errors map[string]string // field name -> message
	Focus  string            // first invalid field, focused on load
}

// parseConcert validates the concert form; errs is keyed by field name.
func parseConcert(f url.Values) (c store.Concert, errs map[string]string) {
	errs = map[string]string{}
	var ok bool
	if c.Artist, ok = cleanText(f.Get("artist"), 100); !ok {
		errs["artist"] = "Indique l'artiste (100 caractères au plus)."
	}
	if c.Date = f.Get("date"); !validDate(c.Date) {
		errs["date"] = "Indique une date valide."
	}
	if c.Time = f.Get("time"); c.Time != "" {
		if _, err := time.Parse("15:04", c.Time); err != nil {
			errs["time"] = "Heure invalide (ex. 20:30)."
		}
	}
	if c.Venue, ok = cleanText(f.Get("venue"), 100); !ok {
		errs["venue"] = "Indique la salle (100 caractères au plus)."
	}
	if c.TicketURL = strings.TrimSpace(f.Get("ticket_url")); c.TicketURL != "" && !validTicketURL(c.TicketURL) {
		errs["ticket_url"] = "Lien invalide : il doit commencer par http:// ou https://."
	}
	if c.OnSaleDate = f.Get("on_sale_date"); c.OnSaleDate != "" && !validDate(c.OnSaleDate) {
		errs["on_sale_date"] = "Date de mise en vente invalide."
	}
	return c, errs
}

func validDate(s string) bool {
	d, err := time.Parse(time.DateOnly, s)
	return err == nil && d.Year() >= 2000 && d.Year() < 2100
}

// validTicketURL accepts only absolute http(s) links, so no javascript: or data: URL is ever rendered.
func validTicketURL(s string) bool {
	if len(s) > 2000 || strings.ContainsFunc(s, func(r rune) bool { return r <= ' ' || r == 0x7f }) {
		return false
	}
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func (s *server) renderForm(w http.ResponseWriter, r *http.Request, status int, f *concertForm) {
	for _, name := range []string{"artist", "date", "time", "venue", "ticket_url", "on_sale_date", ""} {
		if _, bad := f.Errors[name]; bad {
			f.Focus, f.Notice = name, "Erreur"
			break
		}
	}
	s.render(w, r, status, "form", f)
}

func (s *server) newConcert(w http.ResponseWriter, r *http.Request, gr *groupRequest) {
	s.renderForm(w, r, http.StatusOK, &concertForm{groupRequest: gr, Title: "Ajouter un concert",
		Action: gr.Base + "/concerts", Cancel: gr.Base})
}

func (s *server) createConcert(w http.ResponseWriter, r *http.Request, gr *groupRequest) {
	if !parseForm(w, r) {
		return
	}
	c, errs := parseConcert(r.PostForm)
	f := &concertForm{groupRequest: gr, Title: "Ajouter un concert", Action: gr.Base + "/concerts", Cancel: gr.Base, Concert: c, Errors: errs}
	if len(errs) > 0 {
		s.renderForm(w, r, http.StatusUnprocessableEntity, f)
		return
	}
	id, err := s.store.CreateConcert(r.Context(), gr.Group.ID, gr.Me.ID, c)
	switch {
	case errors.Is(err, store.ErrLimit):
		f.Errors[""] = fmt.Sprintf("Le groupe a atteint %d concerts : supprime les anciens d'abord.", store.MaxConcerts)
		s.renderForm(w, r, http.StatusUnprocessableEntity, f)
	case err != nil:
		s.fail(w, r, err)
	default:
		seeOther(w, r, fmt.Sprintf("%s?added=%d", gr.Base, id))
	}
}

// concert loads the live concert named by the {id} path value; it answers 404 and returns false otherwise.
func (s *server) concert(w http.ResponseWriter, r *http.Request, gr *groupRequest) (store.Concert, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		notFound(w)
		return store.Concert{}, false
	}
	c, err := s.store.Concert(r.Context(), gr.Group.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		notFound(w)
		return c, false
	}
	if err != nil {
		s.fail(w, r, err)
		return c, false
	}
	return c, true
}

type concertPage struct {
	*groupRequest
	store.Concert
	Share string // « Prévenir le groupe » link
}

func (s *server) showConcert(w http.ResponseWriter, r *http.Request, gr *groupRequest) {
	c, ok := s.concert(w, r, gr)
	if !ok {
		return
	}
	s.render(w, r, http.StatusOK, "concert", &concertPage{groupRequest: gr, Concert: c, Share: s.shareURL(gr, c)})
}

func (s *server) editConcert(w http.ResponseWriter, r *http.Request, gr *groupRequest) {
	c, ok := s.concert(w, r, gr)
	if !ok {
		return
	}
	page := fmt.Sprintf("%s/concerts/%d", gr.Base, c.ID)
	s.renderForm(w, r, http.StatusOK, &concertForm{groupRequest: gr, Title: "Modifier le concert", Action: page, Cancel: page, Concert: c})
}

func (s *server) updateConcert(w http.ResponseWriter, r *http.Request, gr *groupRequest) {
	old, ok := s.concert(w, r, gr)
	if !ok || !parseForm(w, r) {
		return
	}
	page := fmt.Sprintf("%s/concerts/%d", gr.Base, old.ID)
	c, errs := parseConcert(r.PostForm)
	if len(errs) > 0 {
		c.ID = old.ID
		s.renderForm(w, r, http.StatusUnprocessableEntity, &concertForm{groupRequest: gr, Title: "Modifier le concert",
			Action: page, Cancel: page, Concert: c, Errors: errs})
		return
	}
	if err := s.store.UpdateConcert(r.Context(), gr.Group.ID, old.ID, c); err != nil {
		s.fail(w, r, err) // ErrNotFound only if deleted meanwhile: rare enough for a 500
		return
	}
	seeOther(w, r, page)
}

func (s *server) deleteConcert(w http.ResponseWriter, r *http.Request, gr *groupRequest) {
	c, ok := s.concert(w, r, gr)
	if !ok {
		return
	}
	if err := s.store.DeleteConcert(r.Context(), gr.Group.ID, c.ID); err != nil {
		s.fail(w, r, err)
		return
	}
	seeOther(w, r, fmt.Sprintf("%s?deleted=%d", gr.Base, c.ID))
}

func (s *server) restoreConcert(w http.ResponseWriter, r *http.Request, gr *groupRequest) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err == nil {
		err = s.store.RestoreConcert(r.Context(), gr.Group.ID, id)
	}
	switch {
	case errors.Is(err, store.ErrNotFound) || errors.Is(err, strconv.ErrSyntax) || errors.Is(err, strconv.ErrRange):
		notFound(w)
	case err != nil:
		s.fail(w, r, err)
	default:
		seeOther(w, r, fmt.Sprintf("%s#c%d", gr.Base, id))
	}
}

// shareURL is the « Prévenir le groupe » link: WhatsApp with the concert and its page.
func (s *server) shareURL(gr *groupRequest, c store.Concert) string {
	page := fmt.Sprintf("%s%s/concerts/%d", s.baseURL, gr.Base, c.ID)
	text := fmt.Sprintf("%s, %s, %s\n%s", c.Artist, when(c, s.now()), c.Venue, page)
	// %20, not +: WhatsApp shows a literal + otherwise.
	return "https://wa.me/?text=" + strings.ReplaceAll(url.QueryEscape(text), "+", "%20")
}
