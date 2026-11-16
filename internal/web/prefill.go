package web

import (
	"net/http"
	"net/url"
	"sync"
	"time"

	"concerts/internal/prefill"
	"concerts/internal/store"
)

// fetchLimiter keeps prefill fetches cheap for the server and polite to ticketing
// sites: one in flight per group, 10 per minute per group, 2 in flight in total.
// Its maps hold one entry per group that ever prefilled: a handful.
type fetchLimiter struct {
	mu       sync.Mutex
	inFlight map[int64]bool
	recent   map[int64][]time.Time
	total    int
}

const (
	fetchesPerMinute = 10
	fetchesInFlight  = 2
)

// acquire reserves a fetch for group at now; call release when done.
func (l *fetchLimiter) acquire(group int64, now time.Time) (release func(), ok bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.inFlight == nil {
		l.inFlight, l.recent = map[int64]bool{}, map[int64][]time.Time{}
	}
	var keep []time.Time
	for _, t := range l.recent[group] {
		if now.Sub(t) < time.Minute {
			keep = append(keep, t)
		}
	}
	l.recent[group] = keep
	if l.inFlight[group] || len(keep) >= fetchesPerMinute || l.total >= fetchesInFlight {
		return nil, false
	}
	l.inFlight[group], l.recent[group] = true, append(keep, now)
	l.total++
	return func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		delete(l.inFlight, group)
		l.total--
	}, true
}

// prefill reads the ticketing page u; it returns the concert to show in the
// form (only values the form itself accepts) and a message for the user.
func (s *server) prefill(r *http.Request, gr *groupRequest, u string) (store.Concert, string) {
	if !validTicketURL(u) {
		return store.Concert{}, "Lien invalide : il doit commencer par http:// ou https://."
	}
	release, ok := s.fetches.acquire(gr.Group.ID, s.now())
	if !ok {
		return store.Concert{TicketURL: u}, "Trop de lectures en cours : réessaie dans une minute, ou remplis à la main."
	}
	defer release()
	res, err := prefill.Fetch(r.Context(), s.fetch, u, s.now().In(paris))
	if err != nil {
		s.log.InfoContext(r.Context(), "prefill failed", "err", err)
		return store.Concert{TicketURL: u}, "Impossible de lire cette page : remplis à la main."
	}
	if res == (prefill.Result{}) {
		return store.Concert{TicketURL: u}, "Rien trouvé sur cette page : remplis à la main."
	}
	// The page is untrusted input: run it through the form's validation and drop what fails.
	c, errs := parseConcert(url.Values{"artist": {res.Artist}, "date": {res.Date}, "time": {res.Time},
		"venue": {res.Venue}, "ticket_url": {u}, "on_sale_date": {res.OnSaleDate}})
	for name := range errs {
		switch name {
		case "artist":
			c.Artist = ""
		case "date":
			c.Date = ""
		case "time":
			c.Time = ""
		case "venue":
			c.Venue = ""
		case "on_sale_date":
			c.OnSaleDate = ""
		}
	}
	return c, "Vérifie les champs pré-remplis avant d'enregistrer."
}
