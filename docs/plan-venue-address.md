# Plan: venue address and OpenStreetMap link

Branch `venue-address` (worktree branch `worktree-venue-address`, renamed before merging), merged with `--no-ff`. This file is deleted before the merge.

## Decisions (agreed with the user)

- The OSM link appears only when an address is known; it searches « venue, address ».
- The `.ics` LOCATION becomes « venue, address » (venue alone when there is no address).
- Address and link on the concert page only, not in the list.

## Commit 1: prefill reads the address

- `prefill.Result` gets `Address`.
- `fromEvent`: `location.address` (first element if it is an array) is either a string (cleaned) or a `PostalAddress`: `streetAddress`, then `postalCode addressLocality`, joined with « , »; empty parts skipped; `addressCountry` ignored (French app).
- The street already holding the postcode or city: they are not repeated. An address equal to the venue is dropped.
- Fixture: extend an existing fixture with a full `PostalAddress` (shaped like La Belle Électrique) and one with a string address; the existing locality-only fixture yields « Paris ».

## Commit 2: stored, edited, shown

- Migration `0005_concert_address.sql`: `ALTER TABLE concerts ADD COLUMN address TEXT` (NULL when empty, like the other optional fields).
- `store.Concert.Address`; select, insert and update carry it with `nullif(?, '')`.
- Form: optional field « Adresse » after « Salle », `autocomplete="off"` (else browsers offer the user's home address); checked with `cleanText` and `maxTextRunes` (100) only when not blank; error « Adresse invalide (100 caractères au plus). ». A longer prefilled address is dropped, like any invalid prefilled value.
- `prefill` handler passes `res.Address` through the form validation like the other fields.
- Concert page: under the venue, the address on its own line, then the link « Voir sur la carte<span class="visually-hidden"> OpenStreetMap</span> », both only when the address is set. The URL is built in Go as a plain string (html/template still filters it): `https://www.openstreetmap.org/search?query=` + `url.QueryEscape(venue + ", " + address)`, `rel="noopener noreferrer"`; pages already send `Referrer-Policy: no-referrer`. Threat model: venue and address reach OSM, nothing else.
- `ics`: LOCATION = venue, or « venue, address », escaped once; the `timed.ics` case splits its venue into venue + address and its golden file stays byte-identical.
- Demo data: concert 1 (the one `verify-ui` checks) gets an address.
- Tests: store round-trip; form validation (too long, invisible character); prefill handler fills the address; concert page shows the link only with an address, with the escaped query; `.ics` golden.
- `docs/design.md`: slice 2 (fields), slice 5 (address from JSON-LD), slice 6 (LOCATION); `make verify-ui` before merging.
