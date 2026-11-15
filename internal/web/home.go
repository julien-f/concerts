package web

import (
	"errors"
	"net/http"
	"strconv"

	"concerts/internal/store"
)

type homePage struct {
	*groupRequest
	Concerts []store.Concert
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
	p := &homePage{groupRequest: gr, Concerts: cs}
	// Banners come from the URL, so each is shown only while still true: added and
	// restored concerts must be in the live list just loaded, a deleted one still deleted.
	find := func(param string) *store.Concert {
		id, err := strconv.ParseInt(q.Get(param), 10, 64)
		for i := range cs {
			if err == nil && cs[i].ID == id {
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
