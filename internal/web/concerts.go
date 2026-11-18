package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"concerts/internal/store"
	"concerts/internal/validate"
)

// field is one input of the concert form; a checkbox's Value is "1" when ticked, else "".
type field struct {
	Name, Label, Type, Value string
	Hint                     string // after the label, in parentheses
	Autocomplete             string // "off" where browsers would offer the user's own data
	Max                      int    // maxlength, 0 for none
	Optional                 bool
	Error                    string
	Autofocus                bool   // first invalid field, or the artist after a prefill
	DescribedBy              string // ids of extra descriptions (the prefill outcome, the source's value)
	// After « Relire la billetterie »: the source's value, differing from Value, offered with « Utiliser ».
	Offered            bool
	Source, SourceText string // as the form posts it, as the page shows it
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
	Summary      string         // above the form, once « Relire la billetterie » offered values
	Duplicate    *store.Concert // live concert with the same artist and date (add form only)
	OnAffiche    bool           // Duplicate is an import nobody answered: answering it adds it, not « Ajouter quand même »
}

// Offers reports whether a field offers the source's value.
func (f *concertForm) Offers() bool {
	return slices.ContainsFunc(f.Fields, func(fd field) bool { return fd.Offered })
}

func newConcertForm(gr *groupRequest, c store.Concert, errs map[string]string) *concertForm {
	f := &concertForm{groupRequest: gr, ID: c.ID, FormError: errs[""], Fields: []field{
		{Name: "artist", Label: "Artiste", Type: "text", Value: c.Artist, Max: validate.MaxTextRunes},
		{Name: "date", Label: "Date", Type: "date", Value: c.Date},
		{Name: "time", Label: "Heure", Type: "time", Value: c.Time, Optional: true},
		{Name: "venue", Label: "Salle", Type: "text", Value: c.Venue, Max: validate.MaxTextRunes},
		{Name: "address", Label: "Adresse", Type: "text", Value: c.Address, Max: validate.MaxTextRunes, Optional: true, Autocomplete: "off"},
		{Name: "ticket_url", Label: "Lien de billetterie", Type: "url", Value: c.TicketURL, Max: validate.MaxURLBytes, Optional: true},
		{Name: "price", Label: "Prix", Type: "text", Value: c.Price, Max: validate.MaxPriceRunes, Optional: true},
		{Name: "genre", Label: "Genre", Type: "text", Value: c.Genre, Max: validate.MaxGenreRunes, Optional: true},
		{Name: "sold_out", Label: "Complet", Hint: "plus de billets en vente", Type: "checkbox", Value: checked(c.SoldOut), Optional: true},
		{Name: "on_sale_date", Label: "Mise en vente", Type: "date", Value: c.OnSaleDate, Optional: true},
		{Name: "description", Label: "Description", Type: "textarea", Value: c.Description, Max: validate.MaxDescriptionRunes, Optional: true},
	}}
	focused := false
	for i := range f.Fields {
		fd := &f.Fields[i]
		fd.Value = strings.ToValidUTF8(fd.Value, "\uFFFD") // a rejected value comes back as typed, the page stays UTF-8
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

// checked is a checkbox field's Value.
func checked(b bool) string {
	if b {
		return "1"
	}
	return ""
}

// parseConcert validates the concert form; errs is keyed by field name.
func parseConcert(f url.Values) (store.Concert, map[string]string) {
	return validate.Concert(store.Concert{Artist: f.Get("artist"), Date: f.Get("date"), Time: f.Get("time"),
		Venue: f.Get("venue"), Address: f.Get("address"), TicketURL: f.Get("ticket_url"), OnSaleDate: f.Get("on_sale_date"),
		Price: f.Get("price"), Description: f.Get("description"), Genre: f.Get("genre"), SoldOut: f.Get("sold_out") != ""})
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
	if errors.Is(err, store.ErrNotFound) || (err == nil && c.Deleted) {
		s.notFound(w, r, gr)
		return c, false
	}
	if err != nil {
		s.fail(w, r, err)
		return c, false
	}
	return c, true
}

// done ends a write: 404 for ErrNotFound, a plain 422 for ErrLimit, 500 for other errors, else a 303 to target.
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
		var ss []store.Status
		if err == nil {
			ss, err = s.store.Statuses(r.Context(), gr.Group.ID, d.ID)
		}
		switch {
		case err == nil:
			f := newConcertForm(gr, c, nil)
			f.Duplicate, f.Notice = &d, "Déjà dans la liste"
			if !visible(d, ss) && s.upcoming(d) { // a past one is in no view: the member may add it anyway
				f.OnAffiche, f.Notice = true, "Déjà à l'affiche"
			}
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
	concertFacts
	Statuses statusBlock
	Share    shareLink
	Affiche  bool  // an upcoming import nobody answered: back to « À l'affiche », not the list
	Listed   bool  // in the list: the back link aims at its card
	Saved    bool  // banner after an edit
	Reverted bool  // banner after going back to a revision
	UndoRev  int64 // revision the banner offers to go back to, 0 for none
}

// concertFacts is what the member page and the public card both show of a concert.
type concertFacts struct {
	store.Concert
	URL    string // absolute page URL, for Open Graph
	Map    string // OpenStreetMap search for the venue, "" without an address
	Listen []listenLink
}

func (s *server) facts(c store.Concert) concertFacts {
	return concertFacts{Concert: c, URL: s.pageURL(c), Map: mapURL(c), Listen: listenLinks(c.Artist)}
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

// redirectToConcert redirects to the public link, banner query kept, so the address bar never shows the secret.
func (s *server) redirectToConcert(w http.ResponseWriter, r *http.Request, gr *groupRequest) {
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
	if errors.Is(err, store.ErrNotFound) || (err == nil && c.Deleted) {
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
	concertFacts
	Going int // members answering « J'y vais »
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
	p := &publicPage{groupRequest: &groupRequest{}, concertFacts: s.facts(c)}
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
	p := &concertPage{groupRequest: gr, concertFacts: s.facts(c), Statuses: newStatusBlock(gr, c, ss, "concert", s.upcoming(c)),
		Share: s.shareLink(c), Affiche: !visible(c, ss) && s.upcoming(c), Listed: visible(c, ss)}
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
		// The store answers a valid edit of a deleted concert with ErrNotFound; an invalid one gets the same 404.
		if _, ok := s.concert(w, r, gr); !ok {
			return
		}
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

// refreshConcert is « Relire la billetterie »: it fills the empty fields of the edit form, as
// posted, from what the ticketing source says, offers its other differing values under their
// field, and shows the form again without saving. The source is the venue's agenda for an
// import the sync kept it of, else the ticket link's page. With use=<field>, « Utiliser »
// takes that field's offered value, posted back with the form, and reads nothing.
func (s *server) refreshConcert(w http.ResponseWriter, r *http.Request, gr *groupRequest) {
	saved, ok := s.concert(w, r, gr)
	if !ok || !parseForm(w, r) {
		return
	}
	c, errs := parseConcert(r.PostForm)
	c.ID = saved.ID
	if use := r.PostForm.Get("use"); use != "" {
		f := newConcertForm(gr, c, nil)
		src := map[string]string{}
		for _, fd := range f.Fields {
			if k := "source_" + fd.Name; r.PostForm.Has(k) {
				src[fd.Name] = r.PostForm.Get(k)
			}
		}
		used := ""
		for i := range f.Fields {
			if fd := &f.Fields[i]; fd.Name == use && r.PostForm.Has("source_"+use) && src[use] != fd.Value {
				fd.Value, fd.Autofocus, fd.DescribedBy, used = src[use], true, "refresh-summary", fd.Label
			}
		}
		diffs := s.offer(f, src, used == "")
		if used != "" {
			f.Summary = used + " : valeur de la billetterie reprise. "
		}
		f.Summary += differ(diffs)
		s.render(w, r, http.StatusOK, "form", f)
		return
	}
	src, err := s.store.SourceEvent(r.Context(), gr.Group.ID, saved.ID)
	soldOutKnown := err == nil
	switch {
	case errors.Is(err, store.ErrNotFound):
		if errs["ticket_url"] == "" && c.TicketURL == "" {
			errs["ticket_url"] = "Indique d'abord le lien de billetterie."
		}
		// Other errors wait for « Enregistrer »; 200 like the add form's prefill, as htmx swaps no 4xx.
		if errs["ticket_url"] != "" {
			s.render(w, r, http.StatusOK, "form", newConcertForm(gr, c, map[string]string{"ticket_url": errs["ticket_url"]}))
			return
		}
		var problem string
		if src, problem, _ = s.prefill(r, gr, c.TicketURL); problem != "" {
			f := newConcertForm(gr, c, nil)
			f.PrefillMsg, f.RefreshFocus = problem, true
			s.render(w, r, http.StatusOK, "form", f)
			return
		}
	case err != nil:
		s.fail(w, r, err)
		return
	}
	f := newConcertForm(gr, c, nil)
	values := map[string]string{} // by field name; sold_out only when the source tells
	for _, fd := range newConcertForm(nil, src, nil).Fields {
		if fd.Type != "checkbox" || soldOutKnown {
			values[fd.Name] = fd.Value
		}
	}
	var filled []string
	firstFilled := 0
	for i := range f.Fields {
		if fd := &f.Fields[i]; fd.Type != "checkbox" && fd.Value == "" && values[fd.Name] != "" {
			fd.Value = values[fd.Name]
			if filled == nil {
				firstFilled = i
			}
			filled = append(filled, fd.Label)
		}
	}
	diffs := s.offer(f, values, true) // focus on the first difference, if any
	switch {
	case diffs != nil:
		if filled != nil {
			f.Summary = "Complété depuis la billetterie : " + strings.Join(filled, ", ") + ". "
		}
		f.Summary += differ(diffs)
	case filled == nil:
		f.PrefillMsg, f.RefreshFocus = "Rien de nouveau sur la billetterie.", true
	default:
		f.PrefillMsg = "Complété depuis la billetterie : " + strings.Join(filled, ", ") + ". Vérifie puis enregistre."
		f.Fields[firstFilled].Autofocus, f.Fields[firstFilled].DescribedBy = true, "prefill-msg"
	}
	s.render(w, r, http.StatusOK, "form", f)
}

// offer marks the fields whose source value differs from the form's, and returns their labels.
// The description is never compared, and an empty source value is offered only for the
// checkbox: a value the source leaves out is never cleared. With focus, the first one gets it.
func (s *server) offer(f *concertForm, src map[string]string, focus bool) (labels []string) {
	for i := range f.Fields {
		fd := &f.Fields[i]
		v, ok := src[fd.Name]
		if !ok || v == fd.Value || fd.Name == "description" || (v == "" && fd.Type != "checkbox") {
			continue
		}
		fd.Offered, fd.Source, fd.SourceText = true, v, s.fieldText(fd.Name, v)
		if fd.Name == "sold_out" {
			fd.SourceText = map[string]string{"": "pas complet", "1": "complet"}[v]
		}
		fd.DescribedBy = fd.Name + "-source"
		if focus && labels == nil {
			fd.Autofocus, fd.DescribedBy = true, "refresh-summary "+fd.DescribedBy
		}
		labels = append(labels, fd.Label)
	}
	return labels
}

// differ is the summary's sentence on the differing fields, then the reminder that nothing is saved.
func differ(labels []string) string {
	var b strings.Builder
	switch len(labels) {
	case 0:
	case 1:
		b.WriteString("1 champ diffère : " + labels[0] + ". ")
	default:
		fmt.Fprintf(&b, "%d champs diffèrent : %s. ", len(labels), strings.Join(labels, ", "))
	}
	b.WriteString("Rien n'est enregistré avant « Enregistrer ».")
	return b.String()
}

func (s *server) deleteConcert(w http.ResponseWriter, r *http.Request, gr *groupRequest) {
	if id, ok := s.pathID(w, r, gr); ok {
		err := s.store.DeleteConcert(r.Context(), gr.Group.ID, id)
		s.done(w, r, gr, err, fmt.Sprintf("%s?deleted=%d", gr.Base, id))
	}
}

func (s *server) restoreConcert(w http.ResponseWriter, r *http.Request, gr *groupRequest) {
	id, ok := s.pathID(w, r, gr)
	if !ok {
		return
	}
	err := s.store.RestoreConcert(r.Context(), gr.Group.ID, id, s.today())
	target := fmt.Sprintf("%s?restored=%d#c%d", gr.Base, id, id)
	if err == nil {
		// An import nobody answered is back in « À l'affiche », not in the list: the banner goes with
		// its card; once past, it is in no view.
		var c store.Concert
		var ss []store.Status
		if c, err = s.store.Concert(r.Context(), gr.Group.ID, id); err == nil {
			ss, err = s.store.Statuses(r.Context(), gr.Group.ID, id)
		}
		switch {
		case err != nil || visible(c, ss):
		case s.upcoming(c):
			target = fmt.Sprintf("%s?affiche=1&restored=%d#c%d", gr.Base, id, id)
		default:
			target = gr.Base
		}
	}
	s.done(w, r, gr, err, target)
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
