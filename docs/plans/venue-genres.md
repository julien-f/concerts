# Genre and « Nouveau » on imported concerts: plan

On `venue-import`, deleted before the merge; what lasts goes into `docs/design.md` (slice 12) and ADR 0011. Reviewed once (UX, backend); fixes folded in.

## Need

« À l'affiche » holds ≈ 180 concerts. Members can't tell a rap night from a jazz concert without opening it, and can't see what appeared since their last visit.

## Changes

1. **Genre**: a new optional concert field `genre` (free text, ≤ 60 runes, cleaned like other text), editable in the form (« Genre », beside Prix from 48 rem), recorded in revisions and history like every field. The sync sets it on imports while no member edited them (the one existing rule; editing it takes the row out of the sync, like any field).
   - Display: its own `<p class="meta">` after the time · venue · price line (and before « annoncé par »), on tickets and the concert page; parts joined with « , » so they never mix with the meta line's « · ».
   - Built by each parser from the listing, in the listing's order, duplicates dropped, so an unchanged listing never counts as « updated »; the type appended only when it is not a plain concert; values entirely in capitals are lowercased then capitalised, accents from a small table in the parser (« ELECTRO CUIVRé » → « Électro cuivré »), mixed case kept as written (« DJ set »); clipped to 60 runes like the artist, never dropping the event:
     - `belle-electrique`: genre line + tag (« Pop », « Électro, DJ set »; « Gratuit » dropped);
     - `amperage`: `.style` + `.type` club (« Électro cuivré, Club »);
     - `la-source`: `.ls-tag` labels (« Chanson, Rock »);
     - `mc2`: music categories other than « Musique » (« Jazz », « Ciné-concert »), else empty;
     - `retour-de-scene`: « Micro-live » for that category, else empty;
     - `lilyade`: empty.
   - Prefill: JSON-LD `genre`, string or array (each item through `first`/`str`), joined with « , »; invalid → dropped by validation. Fixture per shape.
   - `venue_events` gains `genre`, so « Relire la billetterie » proposes it like the other fields.
2. **« Nouveau »**: on « À l'affiche », a « Nouveau » tag right after the artist in the card title (text, styled like « Complet », contrast checked in both themes) on imports created in the last 7 days (UTC `created_at` vs the injected clock), except those created within 24 h of the group's earliest import from the same source: the first sync of a followed venue is not news. No schema change (`created_at` joins `concertSelect`).
   - A line under the explanation, « N nouveaux cette semaine », linking to `?affiche=1&nouveau=1` (kept with `salle`, like it), which lists only those; hidden when N = 0.
3. Out: a genre filter (vocabularies differ per venue), a concerts/club filter (later if asked), descriptions and prices from event pages, per-member « seen » state.

## Data

Migration 0013: `genre TEXT` on `concerts`, `concert_revisions` and `venue_events`. The first sync after it updates every owned import once (genre filled): expected.

## Touchpoints (as `sold_out`, commits a73dd97 and 3d7ded4)

`Concert`, `concertSelect`/`concertFields`, `setEditable`, `differs`, `editable()`, the INSERTs of `CreateConcert` and `insertImport`, the revision INSERT…SELECT of `UpdateConcert`, `revisionSelect`/`revisionFields`, `validate.Concert` + `MaxGenreRunes`, `newConcertForm`, `parseConcert`, `web/prefill.go` values, `PutVenueEvents`/`SourceEvent`, `partials.html` cards, the concert page, `demo.go`, `parsers_test.go` `row()`. History and revert go through the form fields.

## Steps (one commit each, `make check` green)

1. Genre field: store, validation, form, refresh suggestions, display; tests (edit, history, revert, refresh « La billetterie indique », long genre wraps).
2. Parsers and prefill set genre (fixture expectations, all-caps and accent cases, clip case in `TestEvent`, string/array JSON-LD), sync and `venue_events` carry it; a sync test where the same input twice gives 0 updated; demo data with genres.
3. « Nouveau » tag, count line and `nouveau=1` filter; tests (first-sync rule, 7-day edge with the fake clock, filter with `salle`); `verify-ui` screens and checks (tag contrast via axe, no overflow with a long genre at 320 px); docs (design.md slice 12, data model, screens, routes; ADR 0011 if relevant); delete this file.
