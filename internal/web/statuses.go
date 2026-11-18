package web

import (
	"bytes"
	"fmt"
	"net/http"
	"net/url"
	"slices"
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

// statusBlock is the names per status and, for an identified member, their buttons;
// the card and the concert page share it, and htmx swaps it alone.
type statusBlock struct {
	Base      string
	ConcertID int64
	Artist    string
	Back      string // where a no-JS answer returns: "list", "mine" or "concert"
	Lines     []statusLine
	Buttons   []statusButton
	Note      string // after a swap that sent the import back to « À l'affiche »
}

// afficheAnswer is the two answers of a row of « À l'affiche »; htmx swaps them alone, and the
// status region out of band.
type afficheAnswer struct {
	Base      string
	ConcertID int64
	Artist    string
	Status    string // my answer, "" if none
	Salle     string // the view's venue filter, kept by a no-JS answer
	Said      string // status region text after an answer, "" on the page
}

// Buttons are the row's answer buttons, as on the cards.
func (a afficheAnswer) Buttons() []statusButton { return answerButtons(a.Status) }

// answerButtons are the answer buttons of a member whose answer is mine ("" if none).
func answerButtons(mine string) []statusButton {
	var bs []statusButton
	for _, l := range statusLabels {
		bs = append(bs, statusButton{l.Status, l.Button, l.Status == mine})
	}
	return bs
}

// statusOf is memberID's answer among statuses, "" if none.
func statusOf(statuses []store.Status, memberID int64) string {
	for _, st := range statuses {
		if st.Member.ID == memberID {
			return st.Status
		}
	}
	return ""
}

// newStatusBlock builds the block; buttons only for an identified member and an upcoming concert.
func newStatusBlock(gr *groupRequest, c store.Concert, statuses []store.Status, back string, upcoming bool) statusBlock {
	b := statusBlock{Base: gr.Base, ConcertID: c.ID, Artist: c.Artist, Back: back}
	names := map[string][]string{}
	for _, st := range statuses {
		names[st.Status] = append(names[st.Status], st.Member.Name)
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
		b.Buttons = answerButtons(statusOf(statuses, gr.Me.ID))
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

// setStatus records my answer; htmx gets the new status block (the answers under « À l'affiche »),
// a browser without JS a 303 back.
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
		back, target = "concert", fmt.Sprintf("%s/concerts/%d", gr.Base, c.ID) // sets the group cookie on its way to /c
	case "mine":
		back, target = "mine", fmt.Sprintf("%s?mine=1#c%d", gr.Base, c.ID)
	case "affiche":
		back = "affiche"
	}
	// Past concerts show no buttons, but a late answer (a tab left open past midnight)
	// is harmless, so the server still takes it.
	err := s.store.SetStatus(r.Context(), gr.Group.ID, c.ID, gr.Me.ID, status)
	// Without JS, clearing an import's answer in the list may send it back to « À l'affiche ».
	leaving := c.Source != "" && status == "" && (back == "list" || back == "mine")
	if err == nil && back == "affiche" && !isHTMX(r) {
		target, err = s.nextRow(r, gr, c, r.PostForm.Get("salle"))
	}
	if err != nil || (!isHTMX(r) && !leaving) {
		s.done(w, r, gr, err, target)
		return
	}
	ss, err := s.store.Statuses(r.Context(), gr.Group.ID, c.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	switch {
	case !isHTMX(r):
		switch {
		case visible(c, ss):
		case s.upcoming(c):
			target = afficheURL(gr, c.ID) // its card left the list: follow it
		case back == "mine":
			target = gr.Base + "?mine=1" // past, it is in no view
		default:
			target = gr.Base
		}
		seeOther(w, r, target)
	case back == "affiche":
		a := afficheAnswer{Base: gr.Base, ConcertID: c.ID, Artist: c.Artist, Status: statusOf(ss, gr.Me.ID),
			Salle: r.PostForm.Get("salle"), Said: "Retiré de la liste du groupe."}
		for _, l := range statusLabels {
			if l.Status == a.Status {
				a.Said = c.Artist + "\u202f: " + l.Button + ". Ajouté à la liste du groupe."
			}
		}
		if a.Status == "" && len(ss) > 0 {
			a.Said = c.Artist + "\u202f: réponse retirée, toujours dans la liste du groupe."
		}
		s.fragment(w, r, "answers", a)
	default:
		b := newStatusBlock(gr, c, ss, back, s.upcoming(c))
		if leaving && !visible(c, ss) && s.upcoming(c) {
			b.Note = "Retourné à l'affiche."
		}
		s.fragment(w, r, "statuses", b)
	}
}

// nextRow is « À l'affiche », under the venue filter salle if the group follows it, at the
// first row from c on: the next one once an answer took c out, none past the last.
func (s *server) nextRow(r *http.Request, gr *groupRequest, c store.Concert, salle string) (string, error) {
	cs, err := s.store.Concerts(r.Context(), gr.Group.ID)
	if err != nil {
		return "", err
	}
	ss, err := s.store.Statuses(r.Context(), gr.Group.ID, 0)
	if err != nil {
		return "", err
	}
	followed, err := s.store.Followed(r.Context(), gr.Group.ID)
	if err != nil {
		return "", err
	}
	target := gr.Base + "?affiche=1"
	if !slices.Contains(followed, salle) {
		salle = ""
	} else {
		target += "&salle=" + url.QueryEscape(salle)
	}
	answered := map[int64]bool{}
	for _, st := range ss {
		answered[st.ConcertID] = true
	}
	today, from := s.today(), false
	for _, o := range cs {
		from = from || o.ID == c.ID
		if from && o.Source != "" && !answered[o.ID] && o.Date >= today && (salle == "" || o.Source == salle) {
			return fmt.Sprintf("%s#c%d", target, o.ID), nil
		}
	}
	return target, nil
}

// afficheURL is « À l'affiche » at concert id.
func afficheURL(gr *groupRequest, id int64) string {
	return fmt.Sprintf("%s?affiche=1#c%d", gr.Base, id)
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
