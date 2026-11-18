package agenda

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	neturl "net/url"
	"slices"
	"strings"
	"time"

	"concerts/internal/prefill"
	"concerts/internal/store"
	"concerts/internal/validate"
)

const (
	maxEvents  = 200 // per venue and run
	maxDetails = 40  // event pages per run: about 30 on the first, then one or two a day
	// venueTimeout bounds a venue's reading: len(Venues) of them stay under the sync unit's TimeoutStartSec (TestVenues).
	venueTimeout = 2 * time.Minute
)

// Sync imports the upcoming concerts of the followed venues into the groups following them,
// venue by venue in list order (promoters last), each read within venueTimeout, and keeps what
// each venue said once for every group (store.PutVenueEvents).
// It returns an error when a venue failed or listed no upcoming concert (its parser broke),
// after applying whatever the others gave. Logs name venue keys, hosts and group ids, never
// a URL or a concert.
func Sync(ctx context.Context, st *store.Store, client *http.Client, venues []Venue,
	now func() time.Time, sleep func(context.Context, time.Duration) error, log *slog.Logger,
) error {
	today := validate.Today(now())
	f := &fetcher{client: client, sleep: sleep, seen: map[string]bool{}}
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
		vctx, cancel := context.WithTimeout(ctx, cmp.Or(v.timeout, venueTimeout))
		events, err := f.read(vctx, st, v, today, now, &details, log)
		cancel()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err == nil && len(events) == 0 {
			err = errors.New("no upcoming concert: has the page changed?")
		}
		// Once for every group, before they apply: what « Relire la billetterie » compares an import with.
		if perr := st.PutVenueEvents(ctx, v.Key, events); perr != nil {
			err = errors.Join(err, perr)
		}
		if err != nil {
			log.ErrorContext(ctx, "venue failed", "venue", v.Key, "host", v.host(), "err", err)
			failed = append(failed, v.Key)
		}
		for _, g := range groups {
			n, err := st.ApplyImports(ctx, g, v.Key, events, today)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err != nil {
				log.ErrorContext(ctx, "import failed", "venue", v.Key, "group", g, "err", err)
				if !slices.Contains(failed, v.Key) {
					failed = append(failed, v.Key)
				}
				continue
			}
			log.InfoContext(ctx, "imported", "venue", v.Key, "group", g, "inserted", n.Inserted, "updated", n.Updated,
				"moved", n.Moved, "duplicate", n.Duplicate, "kept", n.Kept, "full", n.Full, "past", n.Past, "bad_id", n.BadID,
				"unfollowed", n.Unfollowed)
		}
	}
	if _, err := st.PruneEventDetails(ctx, today); err != nil {
		return err
	}
	if _, err := st.PruneVenueEvents(ctx, today); err != nil {
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
	fetched, undated := 0, 0
	if v.Details && !errors.Is(err, errBusy) && ctx.Err() == nil {
		var derr error
		fetched, undated, derr = f.details(ctx, st, v, upcoming, now, budget, log)
		err = errors.Join(err, derr)
	}
	var events []store.Event
	for _, l := range upcoming {
		if e, ok := v.event(l); ok {
			events = append(events, e)
		}
	}
	log.InfoContext(ctx, "venue read", "venue", v.Key, "host", v.host(), "pages", pages, "listed", len(items),
		"past", past, "over_cap", len(items)-past-len(upcoming), "invalid", len(upcoming)-len(events), "event_pages", fetched, "undated", undated)
	return events, err
}

// details sets the time of the events without one from what their page gave, reading the
// pages never read for that date while the budget lasts; it returns how many pages it read
// and how many of them had no date. A page without a date, or unread, is read again next
// run, but one gone (404, 410) is kept without time. It stops at a 429 or 503, and fails
// when it read pages and none had a date: prefill no longer reads them.
func (f *fetcher) details(ctx context.Context, st *store.Store, v *Venue, events []listed, now func() time.Time,
	budget *int, log *slog.Logger,
) (fetched, undated int, err error) {
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
			return fetched, undated, err
		case *budget == 0:
			continue // read on a later run
		}
		u, err := neturl.Parse(l.page)
		if err != nil {
			continue
		}
		*budget--
		body, final, err := f.get(ctx, v, u, "text/html")
		switch {
		case errors.Is(err, errBusy) || ctx.Err() != nil:
			return fetched, undated, err
		case errors.Is(err, errGone):
			if err := st.PutEventDetail(ctx, v.Key, l.id, store.EventDetail{Date: l.date}); err != nil {
				return fetched, undated, err
			}
			continue
		case err != nil:
			log.WarnContext(ctx, "event page unread", "venue", v.Key, "host", hostname(u), "err", err)
			continue
		}
		fetched++
		res := prefill.Parse(bytes.NewReader(body), hostname(final), now().In(validate.Paris))
		if res.Date == "" {
			undated++
			log.WarnContext(ctx, "event page without date", "venue", v.Key, "host", hostname(final))
			continue
		}
		if res.Date == l.date { // a time read beside another date is not this event's
			l.time = res.Time
		}
		if err := st.PutEventDetail(ctx, v.Key, l.id, store.EventDetail{Date: l.date, Time: l.time}); err != nil {
			return fetched, undated, err
		}
	}
	if fetched > 0 && undated == fetched {
		return fetched, undated, errors.New("no date on any event page: has the page changed?")
	}
	return fetched, undated, nil
}
