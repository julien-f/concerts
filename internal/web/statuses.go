package web

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"

	"concerts/internal/store"
)

// statusLabels: button order is interested, going; lines list them the other way round.
var statusLabels = []struct{ Status, Button, One, Many string }{
	{store.Interested, "Ça m'intéresse", "Intérêt", "Intérêt"},
	{store.Going, "J'y vais", "Y va", "Y vont"},
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
	Back      string // where a no-JS answer returns: "list", "mine" or "concert"
	Lines     []statusLine
	Buttons   []statusButton
}

// newStatusBlock builds the block; buttons only for an identified member and an upcoming concert.
func newStatusBlock(gr *groupRequest, c store.Concert, statuses []store.Status, back string, upcoming bool) statusBlock {
	b := statusBlock{Base: gr.Base, ConcertID: c.ID, Artist: c.Artist, Back: back}
	names := map[string][]string{}
	mine := ""
	for _, st := range statuses {
		names[st.Status] = append(names[st.Status], st.Member.Name)
		if gr.Me != nil && st.Member.ID == gr.Me.ID {
			mine = st.Status
		}
	}
	for i := len(statusLabels) - 1; i >= 0; i-- {
		l := statusLabels[i]
		switch n := names[l.Status]; len(n) {
		case 0:
		case 1:
			b.Lines = append(b.Lines, statusLine{l.Status, l.One, n[0]})
		default:
			b.Lines = append(b.Lines, statusLine{l.Status, l.Many, strings.Join(n, ", ")})
		}
	}
	if gr.Me != nil && upcoming {
		for _, l := range statusLabels {
			b.Buttons = append(b.Buttons, statusButton{l.Status, l.Button, l.Status == mine})
		}
	}
	return b
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
	c, ok := s.concert(w, r, gr)
	if !ok || !parseForm(w, r) {
		return
	}
	status := r.PostForm.Get("status")
	if !validStatus(status) {
		http.Error(w, "Statut inconnu.", http.StatusBadRequest)
		return
	}
	back, target := "list", fmt.Sprintf("%s#c%d", gr.Base, c.ID)
	switch r.PostForm.Get("back") {
	case "concert":
		back, target = "concert", fmt.Sprintf("%s/concerts/%d", gr.Base, c.ID)
	case "mine":
		back, target = "mine", fmt.Sprintf("%s?mine=1#c%d", gr.Base, c.ID)
	}
	// Past concerts show no buttons, but a late answer (a tab left open past midnight)
	// is harmless, so the server still takes it.
	err := s.store.SetStatus(r.Context(), gr.Group.ID, c.ID, gr.Me.ID, status)
	if err != nil || !isHTMX(r) {
		s.done(w, r, gr, err, target)
		return
	}
	ss, err := s.store.Statuses(r.Context(), gr.Group.ID, c.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.fragment(w, r, "statuses", newStatusBlock(gr, c, ss, back, s.upcoming(c)))
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
