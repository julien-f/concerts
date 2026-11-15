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

type homePage struct {
	*groupRequest
	Mine     bool           // « Mes concerts »: only concerts I answered
	Months   []month        // upcoming, chronological
	Past     []card         // newest first, in a collapsed section
	PastOpen bool           // a banner targets a past concert
	Added    *store.Concert // banner « Concert ajouté » with « Prévenir le groupe »
	Share    string
	Restored *store.Concert // banner « Concert rétabli »
	Deleted  *store.Concert // banner « Concert supprimé » with « Annuler la suppression »
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
	statuses := byConcert(ss)
	p := &homePage{groupRequest: gr, Mine: q.Get("mine") == "1"}
	back := "list"
	if p.Mine {
		back = "mine"
	}
	today := s.today() // once, so every card of the page agrees
	lastMonth := ""
	for _, c := range cs {
		if p.Mine && !answered(statuses[c.ID], gr.Me.ID) {
			continue
		}
		upcoming := c.Date >= today
		cd := card{c, gr.Base, newStatusBlock(gr, c, statuses[c.ID], back, upcoming)}
		switch n := len(p.Months); {
		case !upcoming:
			p.Past = append(p.Past, cd)
		case c.Date[:7] == lastMonth:
			p.Months[n-1].Cards = append(p.Months[n-1].Cards, cd)
		default:
			lastMonth = c.Date[:7]
			p.Months = append(p.Months, month{monthOf(c.Date), []card{cd}})
		}
	}
	slices.Reverse(p.Past) // newest first

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
		p.Share, p.Notice = s.shareURL(gr, *p.Added), "Concert ajouté"
	}
	if p.Restored = find("restored"); p.Restored != nil {
		p.Notice = "Concert rétabli"
	}
	for _, c := range []*store.Concert{p.Added, p.Restored} {
		if c != nil && c.Date < today {
			p.PastOpen = true
		}
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
