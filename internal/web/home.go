package web

import (
	"errors"
	"net/http"
	"slices"
	"strconv"

	"concerts/internal/store"
)

// card is a concert in the list with its status block.
type card struct {
	store.Concert
	Base     string
	Statuses statusBlock
}

type month struct {
	Label string // « mars 2027 »
	Cards []card
}

// pastShown bounds the past cards rendered by default: a group's history grows without end.
const pastShown = 30

type homePage struct {
	*groupRequest
	Mine      bool    // « Mes concerts »: only concerts I answered
	Months    []month // upcoming, chronological
	Past      []card  // newest first, in a collapsed section; the pastShown newest unless PastOpen
	PastTotal int
	PastMore  int64          // first past concert not rendered (link to all of them), 0 if none
	PastOpen  bool           // ?past=all, or a banner targets a past concert: all of them, section open
	Added     *store.Concert // banner « Concert ajouté » with « Prévenir le groupe »
	Share     string
	Restored  *store.Concert // banner « Concert rétabli »
	Deleted   *store.Concert // banner « Concert supprimé » with « Annuler la suppression »
}

// home is the picker without identity, else the concert list.
func (s *server) home(w http.ResponseWriter, r *http.Request, gr *groupRequest) {
	q := r.URL.Query()
	if gr.Me == nil {
		s.picker(w, r, http.StatusOK, &pickerPage{groupRequest: gr, Next: validNext(q.Get("next"))})
		return
	}
	cs, err := s.store.Concerts(r.Context(), gr.Group.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	ss, err := s.store.Statuses(r.Context(), gr.Group.ID, 0)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	statuses := map[int64][]store.Status{} // by concert id
	for _, st := range ss {
		statuses[st.ConcertID] = append(statuses[st.ConcertID], st)
	}
	p := &homePage{groupRequest: gr, Mine: q.Get("mine") == "1"}
	back := "list"
	if p.Mine {
		back = "mine"
	}
	today := s.today() // once, so every card of the page agrees
	lastMonth := ""
	var past []store.Concert
	for _, c := range cs {
		if p.Mine && !answered(statuses[c.ID], gr.Me.ID) {
			continue
		}
		if c.Date < today {
			past = append(past, c)
			continue
		}
		cd := card{c, gr.Base, newStatusBlock(gr, c, statuses[c.ID], back, true)}
		if c.Date[:7] == lastMonth {
			p.Months[len(p.Months)-1].Cards = append(p.Months[len(p.Months)-1].Cards, cd)
		} else {
			lastMonth = c.Date[:7]
			p.Months = append(p.Months, month{monthOf(c.Date), []card{cd}})
		}
	}
	slices.Reverse(past) // newest first

	// Banners come from the URL, so each is shown only while still true: added and
	// restored concerts must be in the live list just loaded, a deleted one still deleted.
	find := func(param string) *store.Concert {
		id, err := strconv.ParseInt(q.Get(param), 10, 64)
		if err != nil {
			return nil
		}
		for i := range cs {
			if cs[i].ID == id {
				return &cs[i]
			}
		}
		return nil
	}
	if p.Added = find("added"); p.Added != nil {
		p.Share, p.Notice = s.shareURL(*p.Added), "Concert ajouté"
	}
	if p.Restored = find("restored"); p.Restored != nil {
		p.Notice = "Concert rétabli"
	}
	p.PastOpen = q.Get("past") == "all"
	for _, c := range []*store.Concert{p.Added, p.Restored} {
		if c != nil && c.Date < today {
			p.PastOpen = true
		}
	}
	// Cards are built for the shown concerts only: most of the cost of a long history.
	if p.PastTotal = len(past); !p.PastOpen && p.PastTotal > pastShown {
		p.PastMore, past = past[pastShown].ID, past[:pastShown]
	}
	for _, c := range past {
		p.Past = append(p.Past, card{c, gr.Base, newStatusBlock(gr, c, statuses[c.ID], back, false)})
	}
	if id, err := strconv.ParseInt(q.Get("deleted"), 10, 64); err == nil {
		c, err := s.store.Concert(r.Context(), gr.Group.ID, id)
		switch {
		case err == nil && c.Deleted:
			p.Deleted, p.Notice = &c, "Concert supprimé"
		case err != nil && !errors.Is(err, store.ErrNotFound):
			s.fail(w, r, err)
			return
		}
	}
	s.render(w, r, http.StatusOK, "group", p)
}

func answered(statuses []store.Status, memberID int64) bool {
	for _, st := range statuses {
		if st.Member.ID == memberID {
			return true
		}
	}
	return false
}
