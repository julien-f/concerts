package agenda

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	neturl "net/url"
	"strings"
	"time"

	"concerts/internal/prefill"
	"concerts/internal/store"
	"concerts/internal/validate"
)

const (
	maxEvents  = 200 // per venue and run
	maxDetails = 40  // event pages per run: about 30 on the first, then one or two a day
)

// Sync imports the upcoming concerts of the followed venues into the groups following them:
// every venue is read first, in list order (promoters last), then applied group by group.
// It returns an error when a venue failed or listed no upcoming concert (its parser broke),
// after applying whatever the others gave. Logs name venue keys, hosts and group ids, never
// a URL or a concert.
func Sync(ctx context.Context, st *store.Store, client *http.Client, venues []Venue,
	now func() time.Time, sleep func(context.Context, time.Duration) error, log *slog.Logger,
) error {
	today := validate.Today(now())
	f := &fetcher{client: client, sleep: sleep, seen: map[string]bool{}}
	type venueRead struct {
		v      *Venue
		groups []int64
		events []store.Event
	}
	var reads []venueRead
	var failed []string
	details := maxDetails
	for i := range venues {
		v := &venues[i]
		groups, err := st.Followers(ctx, v.Key)
		if err != nil {
			return err
		}
		if len(groups) == 0 {
			continue
		}
		events, err := f.read(ctx, st, v, today, now, &details, log)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err == nil && len(events) == 0 {
			err = errors.New("no upcoming concert: has the page changed?")
		}
		if err != nil {
			log.ErrorContext(ctx, "venue failed", "venue", v.Key, "host", v.host(), "err", err)
			failed = append(failed, v.Key)
		}
		reads = append(reads, venueRead{v, groups, events})
	}
	for _, r := range reads {
		for _, g := range r.groups {
			n, err := st.ApplyImports(ctx, g, r.v.Key, r.events, today)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err != nil {
				log.ErrorContext(ctx, "import failed", "venue", r.v.Key, "group", g, "err", err)
				failed = append(failed, r.v.Key)
				continue
			}
			log.InfoContext(ctx, "imported", "venue", r.v.Key, "group", g, "inserted", n.Inserted, "updated", n.Updated,
				"moved", n.Moved, "duplicate", n.Duplicate, "kept", n.Kept, "full", n.Full, "past", n.Past, "bad_id", n.BadID)
		}
	}
	if _, err := st.PruneEventDetails(ctx, today); err != nil {
		return err
	}
	if len(failed) > 0 {
		return fmt.Errorf("venues failed: %s", strings.Join(failed, ", "))
	}
	return nil
}

// host names a venue in logs: its first listing's host.
func (v *Venue) host() string {
	if u, err := neturl.Parse(v.Listings[0]); err == nil {
		return hostname(u)
	}
	return "?"
}

// read returns v's upcoming events, at most maxEvents, cleaned and validated; times missing
// from the listing come from the event pages, read once ever (store.EventDetail) within
// the run's budget. The events read are returned even with an error.
func (f *fetcher) read(ctx context.Context, st *store.Store, v *Venue, today string, now func() time.Time,
	budget *int, log *slog.Logger,
) ([]store.Event, error) {
	items, pages, err := f.listing(ctx, v, today)
	var upcoming []listed
	for _, l := range items {
		if l.date == "" || l.date >= today { // no date: counted invalid below
			upcoming = append(upcoming, l)
		}
	}
	past := len(items) - len(upcoming)
	upcoming = upcoming[:min(len(upcoming), maxEvents)]
	fetched := 0
	if v.Details && !errors.Is(err, errBusy) && ctx.Err() == nil {
		var derr error
		fetched, derr = f.details(ctx, st, v, upcoming, now, budget, log)
		err = errors.Join(err, derr)
	}
	var events []store.Event
	for _, l := range upcoming {
		if e, ok := v.event(l); ok {
			events = append(events, e)
		}
	}
	log.InfoContext(ctx, "venue read", "venue", v.Key, "host", v.host(), "pages", pages, "listed", len(items),
		"past", past, "over_cap", len(items)-past-len(upcoming), "invalid", len(upcoming)-len(events), "event_pages", fetched)
	return events, err
}

// details sets the time of the events without one from what their page gave, reading the
// pages never read for that date while the budget lasts. It stops at a 429 or 503.
func (f *fetcher) details(ctx context.Context, st *store.Store, v *Venue, events []listed, now func() time.Time,
	budget *int, log *slog.Logger,
) (int, error) {
	fetched := 0
	for i := range events {
		l := &events[i]
		if l.time != "" || l.date == "" {
			continue
		}
		d, err := st.EventDetail(ctx, v.Key, l.id)
		switch {
		case err == nil && d.Date == l.date:
			l.time = d.Time
			continue
		case errors.Is(err, store.ErrSourceID):
			continue // event() refuses it too
		case err != nil && !errors.Is(err, store.ErrNotFound):
			return fetched, err
		case *budget == 0:
			continue // read on a later run
		}
		u, err := neturl.Parse(l.page)
		if err != nil {
			continue
		}
		*budget--
		body, final, err := f.get(ctx, v, u, "text/html")
		if errors.Is(err, errBusy) || ctx.Err() != nil {
			return fetched, err
		}
		if err != nil {
			log.WarnContext(ctx, "event page unread", "venue", v.Key, "host", hostname(u), "err", err)
			continue // read again next run
		}
		fetched++
		res := prefill.Parse(bytes.NewReader(body), hostname(final), now().In(validate.Paris))
		if res.Date == l.date { // a time read beside another date is not this event's
			l.time = res.Time
		}
		if err := st.PutEventDetail(ctx, v.Key, l.id, store.EventDetail{Date: l.date, Time: l.time}); err != nil {
			return fetched, err
		}
	}
	return fetched, nil
}
