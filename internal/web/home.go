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
	Deleted  *store.Concert // banner « Concert supprimé » with « Annuler »
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
	// Banners come from the URL, so each is shown only while still true.
	if id, err := strconv.ParseInt(q.Get("added"), 10, 64); err == nil {
		for i := range cs {
			if cs[i].ID == id {
				p.Added, p.Share, p.Notice = &cs[i], s.shareURL(gr, cs[i]), "Concert ajouté"
			}
		}
	}
	if id, err := strconv.ParseInt(q.Get("deleted"), 10, 64); err == nil {
		c, err := s.store.DeletedConcert(r.Context(), gr.Group.ID, id)
		switch {
		case err == nil:
			p.Deleted, p.Notice = &c, "Concert supprimé"
		case !errors.Is(err, store.ErrNotFound):
			s.fail(w, r, err)
			return
		}
	}
	s.render(w, r, http.StatusOK, "group", p)
}
