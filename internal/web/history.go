package web

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"concerts/internal/store"
)

// change is one field an edit changed, formatted for reading.
type change struct{ Label, Before, After string }

// edit is one entry of a concert's history.
type edit struct {
	RevID   int64
	By, At  string // « Tom », « mar. 3 mars à 18 h 05 »
	Changes []change
}

type historyPage struct {
	*groupRequest
	store.Concert
	Edits []edit
}

// showHistory lists a concert's edits, newest first, each with the fields it changed.
func (s *server) showHistory(w http.ResponseWriter, r *http.Request, gr *groupRequest) {
	c, ok := s.concert(w, r, gr)
	if !ok {
		return
	}
	rs, err := s.store.Revisions(r.Context(), gr.Group.ID, c.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	p := &historyPage{groupRequest: gr, Concert: c}
	after := c // values each edit produced: the next newer revision's, or the current ones
	for _, rev := range rs {
		at := rev.EditedAt.In(paris)
		p.Edits = append(p.Edits, edit{RevID: rev.ID, By: rev.EditedBy,
			At:      s.frDate(at.Format(time.DateOnly)) + " à " + frTime(at.Format("15:04")),
			Changes: s.changes(rev.Concert, after)})
		after = rev.Concert
	}
	s.render(w, r, http.StatusOK, "history", p)
}

// changes lists the form fields that differ between before and after, labelled as in the form.
func (s *server) changes(before, after store.Concert) []change {
	b, a := newConcertForm(nil, before, nil).Fields, newConcertForm(nil, after, nil).Fields
	var cs []change
	for i := range b {
		if b[i].Value != a[i].Value {
			cs = append(cs, change{b[i].Label, s.fieldText(b[i].Name, b[i].Value), s.fieldText(a[i].Name, a[i].Value)})
		}
	}
	return cs
}

// fieldText is a form value as the concert page shows it.
func (s *server) fieldText(name, v string) string {
	switch {
	case v == "":
		return "(vide)"
	case name == "date" || name == "on_sale_date":
		return s.frDate(v)
	case name == "time":
		return frTime(v)
	}
	return v
}

// revertConcert goes back to a revision: an ordinary edit with its values, which today's
// validation must still accept, so it is recorded and can be undone in turn.
func (s *server) revertConcert(w http.ResponseWriter, r *http.Request, gr *groupRequest) {
	id, ok := s.pathID(w, r, gr)
	if !ok {
		return
	}
	revID, err := strconv.ParseInt(r.PathValue("rev"), 10, 64)
	if err != nil {
		s.notFound(w, r, gr)
		return
	}
	rev, err := s.store.Revision(r.Context(), gr.Group.ID, id, revID)
	if errors.Is(err, store.ErrNotFound) {
		s.notFound(w, r, gr)
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	vals := url.Values{}
	for _, f := range newConcertForm(nil, rev.Concert, nil).Fields {
		vals.Set(f.Name, f.Value)
	}
	c, errs := parseConcert(vals)
	if len(errs) > 0 {
		http.Error(w, "Cette version n'est plus valide : modifie le concert à la main.", http.StatusUnprocessableEntity)
		return
	}
	newRev, err := s.store.UpdateConcert(r.Context(), gr.Group.ID, gr.Me.ID, id, c, s.today())
	s.done(w, r, gr, err, revisedURL(gr, id, "reverted", newRev))
}
