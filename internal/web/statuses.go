package web

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"

	"concerts/internal/store"
)

// statusLabels: button order is interested, going, ticket; lines list them the other way round.
var statusLabels = []struct{ Status, Button, One, Many string }{
	{store.Interested, "Ça m'intéresse", "Intérêt", "Intérêt"},
	{store.Going, "J'y vais", "Y va", "Y vont"},
	{store.Ticket, "J'ai mon billet", "Billet", "Billet"},
}

type statusLine struct{ Status, Label, Names string }

type statusButton struct {
	Status, Label string
	Pressed       bool
}

// statusBlock is the names per status and, for an identified member, their three buttons;
// the card and the concert page share it, and htmx swaps it alone.
type statusBlock struct {
	Base      string
	ConcertID int64
	Artist    string
	Back      string // where a no-JS answer returns: "list" or "concert"
	Lines     []statusLine
	Buttons   []statusButton
}

func newStatusBlock(gr *groupRequest, c store.Concert, statuses []store.Status, back string) statusBlock {
	b := statusBlock{Base: gr.Base, ConcertID: c.ID, Artist: c.Artist, Back: back}
	mine := ""
	for i := len(statusLabels) - 1; i >= 0; i-- {
		l := statusLabels[i]
		var names []string
		for _, st := range statuses {
			if st.Status == l.Status {
				names = append(names, st.Member.Name)
			}
			if gr.Me != nil && st.Member.ID == gr.Me.ID {
				mine = st.Status
			}
		}
		label := l.One
		if len(names) > 1 {
			label = l.Many
		}
		if names != nil {
			b.Lines = append(b.Lines, statusLine{l.Status, label, strings.Join(names, ", ")})
		}
	}
	if gr.Me != nil {
		for _, l := range statusLabels {
			b.Buttons = append(b.Buttons, statusButton{l.Status, l.Button, l.Status == mine})
		}
	}
	return b
}

// byConcert groups statuses by concert id.
func byConcert(ss []store.Status) map[int64][]store.Status {
	m := map[int64][]store.Status{}
	for _, st := range ss {
		m[st.ConcertID] = append(m[st.ConcertID], st)
	}
	return m
}

func validStatus(v string) bool {
	if v == "" {
		return true
	}
	for _, l := range statusLabels {
		if v == l.Status {
			return true
		}
	}
	return false
}

// setStatus records my answer; htmx gets the new status block, a browser without JS a 303 back.
func (s *server) setStatus(w http.ResponseWriter, r *http.Request, gr *groupRequest) {
	id, ok := pathID(w, r)
	if !ok || !parseForm(w, r) {
		return
	}
	status, back := r.PostForm.Get("status"), r.PostForm.Get("back")
	if !validStatus(status) {
		http.Error(w, "Statut inconnu.", http.StatusBadRequest)
		return
	}
	target := fmt.Sprintf("%s#c%d", gr.Base, id)
	if back == "concert" {
		target = fmt.Sprintf("%s/concerts/%d", gr.Base, id)
	}
	err := s.store.SetStatus(r.Context(), gr.Group.ID, id, gr.Me.ID, status)
	if err != nil || r.Header.Get("HX-Request") != "true" {
		s.done(w, r, err, target)
		return
	}
	c, ok := s.concert(w, r, gr)
	if !ok {
		return
	}
	ss, err := s.store.Statuses(r.Context(), gr.Group.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if back != "concert" {
		back = "list"
	}
	s.fragment(w, r, "statuses", newStatusBlock(gr, c, byConcert(ss)[c.ID], back))
}

// fragment renders one shared template (from partials.html) for htmx to swap in.
func (s *server) fragment(w http.ResponseWriter, r *http.Request, name string, data any) {
	var buf bytes.Buffer
	if err := s.pages["group"].ExecuteTemplate(&buf, name, data); err != nil {
		s.fail(w, r, fmt.Errorf("render %s: %w", name, err))
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = buf.WriteTo(w)
}
