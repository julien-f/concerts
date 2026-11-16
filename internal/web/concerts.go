package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"concerts/internal/store"
)

// field is one input of the concert form.
type field struct {
	Name, Label, Type, Value string
	Max                      int // maxlength, 0 for none
	Optional                 bool
	Error                    string
	Autofocus                bool   // first invalid field, or the artist after a prefill
	DescribedBy              string // id of an extra description (the prefill outcome)
}

// concertForm is the add (ID 0) and edit page.
type concertForm struct {
	*groupRequest
	ID         int64
	Fields     []field
	FormError  string // error not tied to a field
	PrefillURL string // ticketing page last read (add form only)
	PrefillMsg string // outcome of that read
	BadLink    bool   // the prefill link itself is invalid: focus goes there
}

func newConcertForm(gr *groupRequest, c store.Concert, errs map[string]string) *concertForm {
	f := &concertForm{groupRequest: gr, ID: c.ID, FormError: errs[""], Fields: []field{
		{Name: "artist", Label: "Artiste", Type: "text", Value: c.Artist, Max: maxTextRunes},
		{Name: "date", Label: "Date", Type: "date", Value: c.Date},
		{Name: "time", Label: "Heure", Type: "time", Value: c.Time, Optional: true},
		{Name: "venue", Label: "Salle", Type: "text", Value: c.Venue, Max: maxTextRunes},
		{Name: "ticket_url", Label: "Lien de billetterie", Type: "url", Value: c.TicketURL, Max: maxURLBytes, Optional: true},
		{Name: "on_sale_date", Label: "Mise en vente", Type: "date", Value: c.OnSaleDate, Optional: true},
	}}
	focused := false
	for i := range f.Fields {
		fd := &f.Fields[i]
		fd.Error = errs[fd.Name]
		if fd.Error != "" && !focused {
			fd.Autofocus, focused = true, true
		}
	}
	if len(errs) > 0 {
		f.Notice = "Erreur"
	}
	return f
}

const (
	maxTextRunes = 100
	maxURLBytes  = 2000
)

// parseConcert validates the concert form; errs is keyed by field name.
func parseConcert(f url.Values) (c store.Concert, errs map[string]string) {
	errs = map[string]string{}
	var ok bool
	if c.Artist, ok = cleanText(f.Get("artist"), maxTextRunes); !ok {
		errs["artist"] = fmt.Sprintf("Indique l'artiste (%d caractères au plus).", maxTextRunes)
	}
	if c.Date = f.Get("date"); !validDate(c.Date) {
		errs["date"] = "Indique une date valide."
	}
	if c.Time = f.Get("time"); c.Time != "" {
		// Stored as HH:MM so text order is time order: "9:30" would sort after "20:00".
		if t, err := time.Parse("15:04", c.Time); err != nil {
			errs["time"] = "Heure invalide (ex. 20:30)."
		} else {
			c.Time = t.Format("15:04")
		}
	}
	if c.Venue, ok = cleanText(f.Get("venue"), maxTextRunes); !ok {
		errs["venue"] = fmt.Sprintf("Indique la salle (%d caractères au plus).", maxTextRunes)
	}
	if c.TicketURL = strings.TrimSpace(f.Get("ticket_url")); c.TicketURL != "" && !validTicketURL(c.TicketURL) {
		errs["ticket_url"] = "Lien invalide : il doit commencer par http:// ou https://."
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

// validTicketURL accepts only absolute http(s) links with an ASCII host, so no
// javascript: or data: URL, and no look-alike host, is ever rendered.
func validTicketURL(s string) bool {
	if len(s) > maxURLBytes || !utf8.ValidString(s) || strings.ContainsFunc(s, func(r rune) bool { return r <= ' ' || r == 0x7f }) {
		return false
	}
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" &&
		!strings.ContainsFunc(u.Host, func(r rune) bool { return r > '~' })
}

// pathID parses the {id} path value; it answers 404 and returns false if invalid.
func (s *server) pathID(w http.ResponseWriter, r *http.Request, gr *groupRequest) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r, gr)
	}
	return id, err == nil
}

// concert loads the live concert named by the {id} path value; it answers 404 and returns false otherwise.
func (s *server) concert(w http.ResponseWriter, r *http.Request, gr *groupRequest) (store.Concert, bool) {
	id, ok := s.pathID(w, r, gr)
	if !ok {
		return store.Concert{}, false
	}
	c, err := s.store.Concert(r.Context(), gr.Group.ID, id)
	if errors.Is(err, store.ErrNotFound) || c.Deleted {
		s.notFound(w, r, gr)
		return c, false
	}
	if err != nil {
		s.fail(w, r, err)
		return c, false
	}
	return c, true
}

// done ends a write: 404 for ErrNotFound, 500 for other errors, else a 303 to target.
func (s *server) done(w http.ResponseWriter, r *http.Request, gr *groupRequest, err error, target string) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		s.notFound(w, r, gr)
	case errors.Is(err, store.ErrLimit):
		http.Error(w, fmt.Sprintf("Le groupe a atteint %d concerts à venir : supprimes-en d'abord.", store.MaxConcerts), http.StatusUnprocessableEntity)
	case err != nil:
		s.fail(w, r, err)
	default:
		seeOther(w, r, target)
	}
}

// newConcert is the add form; with ?url= it first reads that ticketing page.
func (s *server) newConcert(w http.ResponseWriter, r *http.Request, gr *groupRequest) {
	u := strings.TrimSpace(r.URL.Query().Get("url"))
	if u == "" {
		s.render(w, r, http.StatusOK, "form", newConcertForm(gr, store.Concert{}, nil))
		return
	}
	c, msg, bad := s.prefill(r, gr, u)
	f := newConcertForm(gr, c, nil)
	f.PrefillURL, f.PrefillMsg, f.BadLink = u, msg, bad
	if !bad {
		// htmx honours autofocus in swapped content; the outcome is read with the field.
		for i := range f.Fields {
			if f.Fields[i].Name == "artist" {
				f.Fields[i].Autofocus, f.Fields[i].DescribedBy = true, "prefill-msg"
			}
		}
	}
	s.render(w, r, http.StatusOK, "form", f)
}

func (s *server) createConcert(w http.ResponseWriter, r *http.Request, gr *groupRequest) {
	if !parseForm(w, r) {
		return
	}
	c, errs := parseConcert(r.PostForm)
	if len(errs) > 0 {
		s.render(w, r, http.StatusUnprocessableEntity, "form", newConcertForm(gr, c, errs))
		return
	}
	id, err := s.store.CreateConcert(r.Context(), gr.Group.ID, gr.Me.ID, c, s.today())
	s.done(w, r, gr, err, fmt.Sprintf("%s?added=%d", gr.Base, id))
}

type concertPage struct {
	*groupRequest
	store.Concert
	Statuses statusBlock
	URL      string // absolute page URL, for Open Graph
	Share    string // « Prévenir le groupe » link
	Saved    bool   // banner after an edit
}

func (s *server) showConcert(w http.ResponseWriter, r *http.Request, gr *groupRequest) {
	c, ok := s.concert(w, r, gr)
	if !ok {
		return
	}
	ss, err := s.store.Statuses(r.Context(), gr.Group.ID, c.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	p := &concertPage{groupRequest: gr, Concert: c, Statuses: newStatusBlock(gr, c, ss, "concert", s.upcoming(c)),
		URL: s.pageURL(gr, c), Share: s.shareURL(gr, c), Saved: r.URL.Query().Has("saved")}
	if p.Saved {
		p.Notice = "Concert modifié"
	}
	s.render(w, r, http.StatusOK, "concert", p)
}

func (s *server) editConcert(w http.ResponseWriter, r *http.Request, gr *groupRequest) {
	if c, ok := s.concert(w, r, gr); ok {
		s.render(w, r, http.StatusOK, "form", newConcertForm(gr, c, nil))
	}
}

func (s *server) updateConcert(w http.ResponseWriter, r *http.Request, gr *groupRequest) {
	id, ok := s.pathID(w, r, gr)
	if !ok || !parseForm(w, r) {
		return
	}
	c, errs := parseConcert(r.PostForm)
	if len(errs) > 0 {
		c.ID = id
		s.render(w, r, http.StatusUnprocessableEntity, "form", newConcertForm(gr, c, errs))
		return
	}
	err := s.store.UpdateConcert(r.Context(), gr.Group.ID, id, c, s.today())
	s.done(w, r, gr, err, fmt.Sprintf("%s/concerts/%d?saved", gr.Base, id))
}

func (s *server) deleteConcert(w http.ResponseWriter, r *http.Request, gr *groupRequest) {
	if id, ok := s.pathID(w, r, gr); ok {
		err := s.store.DeleteConcert(r.Context(), gr.Group.ID, id)
		s.done(w, r, gr, err, fmt.Sprintf("%s?deleted=%d", gr.Base, id))
	}
}

func (s *server) restoreConcert(w http.ResponseWriter, r *http.Request, gr *groupRequest) {
	if id, ok := s.pathID(w, r, gr); ok {
		err := s.store.RestoreConcert(r.Context(), gr.Group.ID, id, s.today())
		s.done(w, r, gr, err, fmt.Sprintf("%s?restored=%d#c%d", gr.Base, id, id))
	}
}

// pageURL is the absolute link of a concert page.
func (s *server) pageURL(gr *groupRequest, c store.Concert) string {
	return fmt.Sprintf("%s%s/concerts/%d", s.baseURL, gr.Base, c.ID)
}

// shareURL is the « Prévenir le groupe » link: WhatsApp with the concert and its page.
func (s *server) shareURL(gr *groupRequest, c store.Concert) string {
	text := fmt.Sprintf("%s, %s, %s\n%s", c.Artist, s.when(c), c.Venue, s.pageURL(gr, c))
	// %20, not +: WhatsApp shows a literal + otherwise.
	return "https://wa.me/?text=" + strings.ReplaceAll(url.QueryEscape(text), "+", "%20")
}
