package web

import (
	"errors"
	"net/http"
	"slices"
	"strconv"

	"concerts/internal/store"
)

// card is a concert in the list with its status block, or in « À l'affiche » with its answers.
type card struct {
	store.Concert
	Statuses statusBlock
	Answer   afficheAnswer
}

type month struct {
	Label string // « mars 2027 »
	Cards []card
}

// venueLink is a followed venue in the « Annoncé par » nav.
type venueLink struct{ Key, Name string }

// pastShown bounds the past cards rendered by default: a group's history grows without end.
const pastShown = 30

type homePage struct {
	*groupRequest
	Mine      bool        // « Mes concerts »: only concerts I answered
	Affiche   bool        // « À l'affiche »: upcoming imports nobody answered, as compact rows
	Salle     string      // under Affiche, the venue key the rows are limited to, or ""
	Venues    []venueLink // followed venues, the « Annoncé par » filter
	Months    []month     // upcoming, chronological
	Past      []card      // newest first, in a collapsed section; the pastShown newest unless PastOpen
	PastTotal int
	PastMore  int64          // first past concert not rendered (link to all of them), 0 if none
	PastOpen  bool           // ?past=all, or a banner targets a past concert: all of them, section open
	Added     *store.Concert // banner « Concert ajouté » with « Partager »
	Share     shareLink
	Restored  *store.Concert // banner « Concert rétabli »
	Deleted   *store.Concert // banner « Concert supprimé » with « Annuler la suppression »

	// AfficheLink: the group follows a venue or has upcoming imports nobody answered (some kept after an unfollow).
	AfficheLink bool
}

// visible reports whether a concert is in the list (« Tous », « Mes concerts », « Passés »):
// added by a member, or imported from a venue and answered by someone.
func visible(c store.Concert, statuses []store.Status) bool {
	return c.Source == "" || len(statuses) > 0
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
	followed, err := s.store.Followed(r.Context(), gr.Group.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	statuses := map[int64][]store.Status{} // by concert id
	for _, st := range ss {
		statuses[st.ConcertID] = append(statuses[st.ConcertID], st)
	}
	p := &homePage{groupRequest: gr, Affiche: q.Get("affiche") == "1"}
	p.Mine = !p.Affiche && q.Get("mine") == "1"
	if salle := q.Get("salle"); p.Affiche && slices.Contains(followed, salle) {
		p.Salle = salle
	}
	p.AfficheLink = len(followed) > 0
	for _, v := range followed {
		p.Venues = append(p.Venues, venueLink{v, s.venue(v)})
	}
	back := "list"
	if p.Mine {
		back = "mine"
	}
	today := s.today() // once, so every card of the page agrees
	var upcoming, past []store.Concert
	for _, c := range cs {
		st := statuses[c.ID]
		if !visible(c, st) && c.Date >= today {
			p.AfficheLink = true
		}
		switch {
		case p.Affiche:
			// Past imports nobody answered show nowhere.
			if visible(c, st) || c.Date < today || (p.Salle != "" && c.Source != p.Salle) {
				continue
			}
		case !visible(c, st), p.Mine && statusOf(st, gr.Me.ID) == "":
			continue
		case c.Date < today:
			past = append(past, c)
			continue
		}
		upcoming = append(upcoming, c)
	}
	lastMonth := ""
	for _, c := range upcoming {
		cd := card{Concert: c}
		if p.Affiche {
			cd.Answer = afficheAnswer{Base: gr.Base, ConcertID: c.ID, Artist: c.Artist, Salle: p.Salle}
		} else {
			cd.Statuses = newStatusBlock(gr, c, statuses[c.ID], back, true)
		}
		if c.Date[:7] == lastMonth {
			p.Months[len(p.Months)-1].Cards = append(p.Months[len(p.Months)-1].Cards, cd)
		} else {
			lastMonth = c.Date[:7]
			p.Months = append(p.Months, month{monthOf(c.Date), []card{cd}})
		}
	}
	slices.Reverse(past) // newest first

	// Banners come from the URL, so each is shown only while still true: added and
	// restored concerts must have their card in this view, a deleted one be still deleted.
	find := func(param string) *store.Concert {
		id, err := strconv.ParseInt(q.Get(param), 10, 64)
		if err != nil {
			return nil
		}
		for _, l := range [][]store.Concert{upcoming, past} {
			for i := range l {
				if l[i].ID == id {
					return &l[i]
				}
			}
		}
		return nil
	}
	if p.Added = find("added"); p.Added != nil {
		p.Share, p.Notice = s.shareLink(*p.Added), "Concert ajouté"
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
		p.Past = append(p.Past, card{Concert: c, Statuses: newStatusBlock(gr, c, statuses[c.ID], back, false)})
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
