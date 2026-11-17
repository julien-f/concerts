package web

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"concerts/internal/prefill"
	"concerts/internal/store"
)

// fetchLimiter keeps prefill fetches cheap for the server and polite to ticketing
// sites: one in flight per group, 10 per minute per group, 2 in flight in total.
type fetchLimiter struct {
	mu       sync.Mutex
	inFlight map[int64]bool
	recent   map[int64][]time.Time // emptied entries are deleted
	total    int
}

const (
	fetchesPerMinute = 10
	fetchesInFlight  = 2
)

func newFetchLimiter() *fetchLimiter {
	return &fetchLimiter{inFlight: map[int64]bool{}, recent: map[int64][]time.Time{}}
}

// acquire reserves a fetch for group at now; call release when done.
func (l *fetchLimiter) acquire(group int64, now time.Time) (release func(), ok bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	keep := slices.DeleteFunc(l.recent[group], func(t time.Time) bool { return now.Sub(t) >= time.Minute })
	if len(keep) == 0 {
		delete(l.recent, group)
	} else {
		l.recent[group] = keep
	}
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

// prefill reads the ticketing page u. It returns the concert found (only values the
// form itself accepts), and when nothing was found, why and whether the link itself is wrong.
func (s *server) prefill(r *http.Request, gr *groupRequest, u string) (c store.Concert, problem string, badLink bool) {
	if _, errs := parseConcert(url.Values{"ticket_url": {u}}); errs["ticket_url"] != "" {
		return store.Concert{}, errs["ticket_url"], true
	}
	c.TicketURL = u
	release, ok := s.fetches.acquire(gr.Group.ID, s.now())
	if !ok {
		return c, "Trop de lectures en cours : réessaie dans une minute, ou remplis à la main.", false
	}
	defer release()
	res, err := prefill.Fetch(r.Context(), s.fetch, u, s.now().In(paris))
	if err != nil {
		s.log.InfoContext(r.Context(), "prefill failed", "err", err) // host only, see prefill.Fetch
		return c, "Impossible de lire cette page : essaie le lien de la salle ou de l'organisateur, ou remplis à la main.", false
	}
	// The page is untrusted input: run it through the form's validation and drop what fails.
	v := url.Values{"artist": {res.Artist}, "date": {res.Date}, "time": {res.Time},
		"venue": {res.Venue}, "address": {res.Address}, "ticket_url": {u}, "on_sale_date": {res.OnSaleDate},
		"price": {res.Price}, "description": {fitParagraphs(res.Description, maxDescriptionRunes)}}
	_, errs := parseConcert(v)
	for name := range errs {
		v.Del(name)
	}
	if c, _ = parseConcert(v); c == (store.Concert{TicketURL: u}) {
		return c, "Rien trouvé sur cette page : remplis à la main.", false
	}
	return c, "", false
}

// fitParagraphs normalizes v as cleanParagraphs does and, if longer than maxRunes, cuts it
// at the last line or sentence end (else word) that fits with « … ».
func fitParagraphs(v string, maxRunes int) string {
	v = paragraphs(v)
	if utf8.RuneCountInString(v) <= maxRunes {
		return v
	}
	cut := string([]rune(v)[:maxRunes-1])
	if i := max(strings.LastIndex(cut, "\n"), strings.LastIndex(cut, ". ")+1); i > 0 {
		cut = cut[:i]
	} else if i := strings.LastIndex(cut, " "); i > 0 {
		cut = cut[:i]
	}
	return strings.TrimSpace(cut) + "…"
}
