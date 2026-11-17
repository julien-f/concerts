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
	Autocomplete             string // "off" where browsers would offer the user's own data
	Max                      int    // maxlength, 0 for none
	Optional                 bool
	Error                    string
	Autofocus                bool   // first invalid field, or the artist after a prefill
	DescribedBy              string // id of an extra description (the prefill outcome)
}

// concertForm is the add (ID 0) and edit page.
type concertForm struct {
	*groupRequest
	ID           int64
	Fields       []field
	FormError    string         // error not tied to a field
	PrefillURL   string         // ticketing page last read (add form only)
	PrefillMsg   string         // outcome of that read, or of « Relire la billetterie » (edit form)
	BadLink      bool           // the prefill link itself is invalid: focus goes there
	RefreshFocus bool           // nothing filled by « Relire la billetterie »: focus stays on it
	Duplicate    *store.Concert // live concert with the same artist and date (add form only)
}

func newConcertForm(gr *groupRequest, c store.Concert, errs map[string]string) *concertForm {
	f := &concertForm{groupRequest: gr, ID: c.ID, FormError: errs[""], Fields: []field{
		{Name: "artist", Label: "Artiste", Type: "text", Value: c.Artist, Max: maxTextRunes},
		{Name: "date", Label: "Date", Type: "date", Value: c.Date},
		{Name: "time", Label: "Heure", Type: "time", Value: c.Time, Optional: true},
		{Name: "venue", Label: "Salle", Type: "text", Value: c.Venue, Max: maxTextRunes},
		{Name: "address", Label: "Adresse", Type: "text", Value: c.Address, Max: maxTextRunes, Optional: true, Autocomplete: "off"},
		{Name: "ticket_url", Label: "Lien de billetterie", Type: "url", Value: c.TicketURL, Max: maxURLBytes, Optional: true},
		{Name: "on_sale_date", Label: "Mise en vente", Type: "date", Value: c.OnSaleDate, Optional: true},
		{Name: "price", Label: "Prix", Type: "text", Value: c.Price, Max: maxPriceRunes, Optional: true},
		{Name: "description", Label: "Description", Type: "textarea", Value: c.Description, Max: maxDescriptionRunes, Optional: true},
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
	maxTextRunes        = 100
	maxURLBytes         = 2000
	maxPriceRunes       = 30
	maxDescriptionRunes = 2000
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
	if a := f.Get("address"); strings.TrimSpace(a) != "" {
		if c.Address, ok = cleanText(a, maxTextRunes); !ok {
			errs["address"] = fmt.Sprintf("Adresse invalide (%d caractères au plus).", maxTextRunes)
		}
	}
	if c.TicketURL = strings.TrimSpace(f.Get("ticket_url")); c.TicketURL != "" && !validTicketURL(c.TicketURL) {
		errs["ticket_url"] = "Lien invalide : il doit commencer par http:// ou https://."
	}
	if c.OnSaleDate = f.Get("on_sale_date"); c.OnSaleDate != "" && !validDate(c.OnSaleDate) {
		errs["on_sale_date"] = "Date de mise en vente invalide."
	}
	if p := f.Get("price"); strings.TrimSpace(p) != "" {
		if c.Price, ok = cleanText(p, maxPriceRunes); !ok {
			errs["price"] = fmt.Sprintf("Prix invalide (%d caractères au plus).", maxPriceRunes)
		}
	}
	if d := f.Get("description"); strings.TrimSpace(d) != "" {
		if c.Description, ok = cleanParagraphs(d, maxDescriptionRunes); !ok {
			errs["description"] = fmt.Sprintf("Description invalide (%d caractères au plus).", maxDescriptionRunes)
		}
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
	if msg == "" {
		msg = "Vérifie les champs pré-remplis avant d'enregistrer."
	}
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
	if r.PostForm.Get("anyway") == "" {
		d, err := s.store.SameConcert(r.Context(), gr.Group.ID, c.Artist, c.Date)
		switch {
		case err == nil:
			f := newConcertForm(gr, c, nil)
			f.Duplicate, f.Notice = &d, "Déjà dans la liste"
			s.render(w, r, http.StatusUnprocessableEntity, "form", f)
			return
		case !errors.Is(err, store.ErrNotFound):
			s.fail(w, r, err)
			return
		}
	}
	id, err := s.store.CreateConcert(r.Context(), gr.Group.ID, gr.Me.ID, c, s.today())
	s.done(w, r, gr, err, fmt.Sprintf("%s?added=%d", gr.Base, id))
}

type concertPage struct {
	*groupRequest
	store.Concert
	Statuses statusBlock
	URL      string // absolute page URL, for Open Graph
	Share    shareLink
	Map      string // OpenStreetMap search for the venue, "" without an address
	Listen   []listenLink
	Saved    bool  // banner after an edit
	Reverted bool  // banner after going back to a revision
	UndoRev  int64 // revision the banner offers to go back to, 0 for none
}

// listenLink is an « Écouter » YouTube search; Name is "" when the concert has a single artist.
type listenLink struct{ Name, URL string }

// listenLinks gives one link per artist of a line-up split on spaced « + » or « / » (« AC/DC » stays whole).
func listenLinks(artist string) []listenLink {
	var links []listenLink
	for _, a := range strings.Split(strings.ReplaceAll(" "+artist+" ", " / ", " + "), " + ") {
		if a = strings.TrimSpace(a); a != "" {
			links = append(links, listenLink{a, "https://www.youtube.com/results?search_query=" + url.QueryEscape(a)})
		}
	}
	if len(links) == 1 {
		links[0].Name = ""
	}
	return links
}

// showConcert redirects to the public link, banner query kept, so the address bar never shows the secret.
func (s *server) showConcert(w http.ResponseWriter, r *http.Request, gr *groupRequest) {
	if c, ok := s.concert(w, r, gr); ok {
		target := "/c/" + c.Token
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		// The target is a token from the database: the query cannot change the host.
		http.Redirect(w, r, target, http.StatusSeeOther) //nolint:gosec // G710: see above
	}
}

// byToken loads the live concert named by the {token} path value, else answers the same plain 404 for every miss.
func (s *server) byToken(w http.ResponseWriter, r *http.Request) (groupID int64, c store.Concert, ok bool) {
	groupID, id, err := s.store.ConcertByToken(r.Context(), r.PathValue("token"))
	if err == nil {
		c, err = s.store.Concert(r.Context(), groupID, id)
	}
	if errors.Is(err, store.ErrNotFound) || c.Deleted {
		http.Error(w, "Concert introuvable : il a été supprimé ou ce lien est incomplet.", http.StatusNotFound)
		return 0, c, false
	}
	if err != nil {
		s.fail(w, r, err)
		return 0, c, false
	}
	return groupID, c, true
}

// publicPage is a concert as anyone holding its public link sees it: no group, no first names.
type publicPage struct {
	*groupRequest // empty: the layout reads its fields
	store.Concert
	URL    string
	Map    string
	Listen []listenLink
	Going  int // members answering « J'y vais »
}

// publicConcert is /c/{token}: the full concert page for a member of its group, else the public card.
func (s *server) publicConcert(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("Vary", "Cookie")
	groupID, c, ok := s.byToken(w, r)
	if !ok {
		return
	}
	gr, err := s.fromCookie(r, groupID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if gr != nil {
		s.showConcertPage(w, r, gr, c)
		return
	}
	ss, err := s.store.Statuses(r.Context(), groupID, c.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	p := &publicPage{groupRequest: &groupRequest{}, Concert: c, URL: s.pageURL(c), Map: mapURL(c), Listen: listenLinks(c.Artist)}
	for _, st := range ss {
		if st.Status == store.Going {
			p.Going++
		}
	}
	s.render(w, r, http.StatusOK, "public", p)
}

// mapURL is the OpenStreetMap search for the venue, "" without an address.
func mapURL(c store.Concert) string {
	if c.Address == "" {
		return ""
	}
	return "https://www.openstreetmap.org/search?query=" + url.QueryEscape(c.Venue+", "+c.Address)
}

func (s *server) showConcertPage(w http.ResponseWriter, r *http.Request, gr *groupRequest, c store.Concert) {
	ss, err := s.store.Statuses(r.Context(), gr.Group.ID, c.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	p := &concertPage{groupRequest: gr, Concert: c, Statuses: newStatusBlock(gr, c, ss, "concert", s.upcoming(c)),
		URL: s.pageURL(c), Share: s.shareLink(c), Map: mapURL(c), Listen: listenLinks(c.Artist)}
	q := r.URL.Query()
	var rev string
	switch {
	case q.Has("saved"):
		p.Saved, p.Notice, rev = true, "Concert modifié", q.Get("saved")
	case q.Has("reverted"):
		p.Reverted, p.Notice, rev = true, "Version rétablie", q.Get("reverted")
	}
	// Undo only while that edit is the newest, as banners show only while true.
	if id, err := strconv.ParseInt(rev, 10, 64); err == nil && id != 0 && id == c.LastRevID {
		p.UndoRev = id
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
	rev, err := s.store.UpdateConcert(r.Context(), gr.Group.ID, gr.Me.ID, id, c, s.today())
	s.done(w, r, gr, err, revisedURL(gr, id, "saved", rev))
}

// revisedURL is the concert page after a write, with its banner naming revision rev (none if 0: nothing changed).
func revisedURL(gr *groupRequest, id int64, banner string, rev int64) string {
	u := fmt.Sprintf("%s/concerts/%d?%s", gr.Base, id, banner)
	if rev != 0 {
		u += "=" + strconv.FormatInt(rev, 10)
	}
	return u
}

// refreshConcert is « Relire la billetterie »: it fills the empty fields of the edit form,
// as posted, from its ticket link, and shows the form again without saving.
func (s *server) refreshConcert(w http.ResponseWriter, r *http.Request, gr *groupRequest) {
	saved, ok := s.concert(w, r, gr)
	if !ok || !parseForm(w, r) {
		return
	}
	c, errs := parseConcert(r.PostForm)
	c.ID = saved.ID
	if errs["ticket_url"] == "" && c.TicketURL == "" {
		errs["ticket_url"] = "Indique d'abord le lien de billetterie."
	}
	// Other errors wait for « Enregistrer »; 200 like the add form's prefill, as htmx swaps no 4xx.
	if errs["ticket_url"] != "" {
		s.render(w, r, http.StatusOK, "form", newConcertForm(gr, c, map[string]string{"ticket_url": errs["ticket_url"]}))
		return
	}
	page, problem, _ := s.prefill(r, gr, c.TicketURL)
	filled := map[string]bool{}
	if problem == "" {
		for _, x := range []struct {
			name string
			dst  *string
			src  string
		}{{"artist", &c.Artist, page.Artist}, {"date", &c.Date, page.Date}, {"time", &c.Time, page.Time},
			{"venue", &c.Venue, page.Venue}, {"address", &c.Address, page.Address}, {"on_sale_date", &c.OnSaleDate, page.OnSaleDate},
			{"price", &c.Price, page.Price}, {"description", &c.Description, page.Description}} {
			if *x.dst == "" && x.src != "" {
				*x.dst, filled[x.name] = x.src, true
			}
		}
	}
	f := newConcertForm(gr, c, nil)
	var labels []string
	for i := range f.Fields {
		if fd := &f.Fields[i]; filled[fd.Name] {
			if labels == nil {
				fd.Autofocus, fd.DescribedBy = true, "prefill-msg"
			}
			labels = append(labels, fd.Label)
		}
	}
	switch {
	case problem != "":
		f.PrefillMsg = problem
	case labels == nil:
		f.PrefillMsg = "Rien de plus sur la billetterie."
	default:
		f.PrefillMsg = "Complété depuis la billetterie : " + strings.Join(labels, ", ") + ". Vérifie puis enregistre."
	}
	f.RefreshFocus = labels == nil
	s.render(w, r, http.StatusOK, "form", f)
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

// pageURL is the absolute public link of a concert page, safe to hand outside the group.
func (s *server) pageURL(c store.Concert) string {
	return s.abs("/c/" + c.Token)
}

// shareURL is « Partager » without the share sheet: WhatsApp with the concert and its page.
func (s *server) shareURL(c store.Concert) string {
	text := s.shareText(c) + "\n" + s.pageURL(c)
	// %20, not +: WhatsApp shows a literal + otherwise.
	return "https://wa.me/?text=" + strings.ReplaceAll(url.QueryEscape(text), "+", "%20")
}

func (s *server) shareText(c store.Concert) string {
	return fmt.Sprintf("%s, %s, %s", c.Artist, s.when(c), c.Venue)
}

// shareLink is « Partager »: the device's share sheet (share.js) with Text and URL, else WhatsApp (ADR 0010).
type shareLink struct{ WhatsApp, Text, URL string }

func (s *server) shareLink(c store.Concert) shareLink {
	return shareLink{s.shareURL(c), s.shareText(c), s.pageURL(c)}
}
