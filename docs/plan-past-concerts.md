# Plan: past concerts (branch `past-concerts`)

Deleted before the merge.

## Problem

- Every past concert is rendered: 465 past → 247 kB, 20 ms per `GET /g/<secret>` (2 ms with none, 5 ms with 50).
- Past concerts count in the 500 live-concert limit: an active group ends up with a 422 on every add.

## Decision (user, option c)

Render the 30 newest past concerts, with a link to all of them; count only upcoming concerts in the 500 limit, and at the 1000-row cap purge the oldest past concert when no deleted one is left.

## Commit 1: limit on upcoming concerts (`internal/store`)

- "Upcoming" = `date >= today`, `today` the Paris date computed by web (`s.today()`), passed as a parameter, so one definition of today. `CreateConcert`, `UpdateConcert`, `RestoreConcert` take `today string`.
- `checkRoom(ctx, tx, groupID, date, today)`:
  - `date >= today` and upcoming live ≥ 500 → `ErrLimit`; a past-dated create is never refused by the limit.
  - rows ≥ 1000 → one `DELETE` that can only pick a deleted or past row: `WHERE group_id = ? AND (deleted_at IS NOT NULL OR date < ?) ORDER BY deleted_at IS NULL, deleted_at, date, time, id LIMIT 1`; statuses go by cascade. Nothing qualifying (clock skew at midnight) deletes nothing: one extra row, harmless.
- `UpdateConcert` (now a transaction) and `RestoreConcert`: update, then upcoming > 500 → `ErrLimit` (a past concert is not in the count, so moving one to the future at 500 is refused, anything else passes).
- Web: `done` already maps `ErrLimit` to 422 for every write; only its message changes (« … 500 concerts à venir … »).
- `MaxConcerts` comment rewritten.
- Tests (`concerts_test.go`, table where it fits): past create at 500 upcoming accepted; upcoming create refused; edit past → future at 500 refused, future → future accepted; restore past at 500 accepted, upcoming refused; a concert dated today counts as upcoming; edit future → past at 500 accepted; at 1000 rows the deleted concert goes before the past one, and with no deleted concert the oldest past one and its statuses go, upcoming ones never. Test dates relative to a constant `today` (the current test hard-codes 2027). Web: 422 on update at the limit.

## Commit 2: 30 newest past concerts rendered (`internal/web`)

- `home.go`: `pastShown = 30`; `p.PastOpen = ?past=all || a banner (added/restored) targets a past concert`; `p.PastTotal = len(p.Past)`; truncate to 30 only when not `PastOpen` (a banner's card is always there; adding old concerts one after another renders the full list each time, accepted).
- `group.html`: heading « Passés ({{.PastTotal}}) »; when truncated, after the list inside `<details>`: `<a href="{{.Base}}?past=all{{if .Mine}}&mine=1{{end}}#c<id of the 31st>">Afficher les {{.PastTotal}} concerts passés</a>`, so reading resumes at the first hidden card; 44 px target (`.past > a`, like `.past summary`). Filter links drop `past=all`. Plain link: works without JS.
- Tests (`home_test.go`): 31 past → 30 cards, heading total 31, link to `#c<31st>` (with `mine=1` under the filter, heading = filtered total); `?past=all` → 31 cards, `open`, no link; exactly 30 → no link; `?added=` and `?restored=` of the 31st → its card, `open`.
- Demo: 29 generated past concerts created after the 5 fixed ones (verify-ui relies on ids 1–5), some with statuses, so 31 past: `make verify-ui` sees the link, and the weight budget is measured at 30 cards. No `past-all` screen (`past-open` covers `open`).
- Re-measure with `ab` (465 past): expected close to the 50-past figure (~5 ms).

## Docs (`docs/design.md`, with each commit)

- Threat model « Abuse with a leaked link »: 500 upcoming concerts; live and deleted rows capped at 1000 (so up to ~1000 live, which bounds `?past=all`); at the cap, oldest deleted then oldest past purged. History wipe by an intruder was already possible (delete all, then create-delete); backups are the remedy.
- Data model limits line, Screens « Passés (N) » bullet (30 newest, link to all), Routes table (`?past=all`, missing `?restored`), Edge cases: a group at the cap silently loses its oldest past concert, with its answers, on each add.
