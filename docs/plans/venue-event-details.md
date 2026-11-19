# Price, ticket link and address from event pages: plan

On `venue-event-details`, deleted before the merge; what lasts goes into `docs/design.md` (slice 12, Data model, Security) and `docs/operations.md`. Reviewed once (store, sync, prefill with live pages); fixes folded in.

## Need

An audit of 170 imports (2026-11-18) found:

- the « Billetterie » button opens the venue's info page, not the ticketing, on 152 imports: listings carry no ticket link (except L'ilyade's), and event pages, which do, are read only for a missing time;
- no price on 129 imports (every venue but MC2), although event pages show one;
- no address on Retour de Scène's imports, whose halls are not followed venues.

`details` (`internal/agenda/sync.go`) reads a page only when the listing has no time and keeps only the time; La Belle Électrique has no `Details`, so its pages are never read.

Scope: La Belle Électrique (price, ticket link), La Source (price, ticket link), Retour de Scène (price, address). Later if this pays off: L'Ampérage (no prefill rule), MC2 (link on the page only), Retour de Scène's ticket link (a Weezevent widget the form's hop would fail on: unverified).

## Changes

1. **Prefill rules** (`internal/prefill/sites.go`):
   - One price rule for the three venues, `textPrice`: every euro amount of the text through `widen`/`formatPrice` (« 24 € - 27 € - 29 € » → « 24–29 € », « Tarif D : 6 à 12 € » → « 6–12 € », as the form already shows billetterie offers); a text starting with « gratuit » (any case) → « Gratuit »; else none. RdS's amounts loop moves into it.
   - Billetterie links: `belleTicketHost`/`belleTicketLink` become `billetterieLink(p, host)` (share link rebuilt as `/agenda/<id>-<slug>?session=<id>`, which carries the Event; the share page is a meta refresh without one), used by La Belle and La Source. Two ids on a page: no ticket link, the other fields kept (today the whole result is dropped).
   - `belleElectrique`: date and time from the `date-xl` text (« 18.11.26 / 20h »); price: the first price-like text after it and before the first billetterie link or « Billetterie » (skips the hero « Gratuit » tag and related events).
   - `laSource`: price from the « Tarif » item; ticket link from its billetterie host. Effect on the form: a La Source link now hops to the billetterie like La Belle's (artist from its Event, « Aïtawa » not « Aïtawa + Captain XXI »; price « 6–12 € »): acceptable, same as La Belle.
   - `retourDeScene`: « GRATUIT – sur réservation » and micro-lives (no « Billetterie » button) priced, the hero date line as anchor.
2. **Cache**: migration 0014 adds `price`, `ticket_url`, `address` and `read_at` (TEXT, NULL) to `event_details`, keeping its rows (they hold La Source's and RdS's times: deleting them would blank times for several runs). `read_at` NULL marks a row read before 0014. A row keeps what the page gave, not merged with the listing; each field checked as the form would (`validate.Text`, length, price ≤ 30 runes; address clipped), a bad one dropped, never the event.
3. **Which pages, in order, within the 40-page budget**: events without a row whose listing lacks a time; events without a row; rows with `read_at` NULL; rows without a ticket link read more than 7 days ago (`now`), since ticketing often opens after the announcement. `Details` becomes « event pages are read with prefill »; set on La Belle.
4. **Merge**: each run, a cached row whose date matches fills what the listing left empty (time, price, ticket link, address): the listing, re-read every run, wins. The ticket link then goes through `ticketLink` (venue site or `Ticketing` domains, else the event page). `Ticketing`: La Belle `la-belle-electrique.com`, La Source `lasource-fontaine.fr` (suffix match covers their `billetterie.` hosts).
5. **Time limit**: La Belle's 8 listing pages take 80 s at its 10 s Crawl-delay; give it `timeout: 3 * time.Minute` (≈ 10 event pages a run; venues total 13 min of the unit's 15). A venue deadline reached while reading event pages stops the reading without failing the venue (the rest waits for the next run, logged as a count); the listing still applies. `TestVenues` sums each venue's own timeout.
6. **Undated pages**: unchanged rule (a venue fails when it read pages and none had a date); La Belle's pages now have one.
7. **Existing imports**: the sync's usual rule fills owned imports once (counted « updated » once); member-edited ones keep their values.

## Touchpoints

`prefill/sites.go` and `parse_test.go`, new fixtures (real La Belle page; La Belle free and two-id cases; La Source with « Tarif D : »; RdS free and micro-live), `fetch_test.go` (La Source hop); `store/venues.go` `EventDetail`, migration 0014, `venues_test.go`; `agenda/sync.go` (`details`, `read`, comments of `Details`, `maxDetails`, `read`, `details`), `venues.go`, `parsers_test.go` `TestVenues`, `sync_test.go`; docs: design.md (lines on 40 event pages « (times) », event_details schema, « Times missing from a listing », 6 × 2 min, out of scope « prices from event pages »), operations.md (2 min per venue, « no date on any event page »).

## Steps (one commit each, `make check` green)

1. Prefill: `textPrice`, `billetterieLink`, La Belle date and price, La Source price and link, RdS free and micro-live prices; fixture cases first, seen failing.
2. Store: migration 0014 and the wider `EventDetail`; round-trip test, pre-0014 row kept with its time.
3. Sync: page order and merge, field checks, La Belle `Details` and timeout, `Ticketing` domains, deadline stop. Sync tests: a La Belle listing and page give the billetterie link and price; a second run reads no page; a pre-0014 row keeps its time on the first run; a deadline after N pages (fake sleep) applies the listing without failing; a member-edited import untouched.
4. Docs; delete this file.
