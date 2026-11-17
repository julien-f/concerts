# Plan: quiet banner buttons, one date stub

Branch `banner-stub`, two commits, merged with `--no-ff`; this file is deleted before the merge.

## 1. Banner buttons are secondary

Problem: since « Partager » became `.button.secondary` in the « Concert ajouté » banner, the other banner buttons are still orange; after an edit, the concert page shows two orange buttons (« Annuler », « Billetterie »).

- `concert.html`: « Annuler » of the « Modifications enregistrées » / « Version rétablie » banner → `class="button secondary"`.
- `group.html`: « Annuler la suppression » → `class="button secondary"`.
- Tests: the existing banner tests assert `class="button secondary"` on both.
- `docs/design.md`: one line in the visual rules: banner buttons are secondary; orange marks the page's main action only.

## 2. One date stub partial for the card and the concert page

Problem: `card` and `ticketHead` each write weekday / day / month; the head adds year and time.

- `dates.go`: `stub` gains `Year, Time string`; `stubOf` leaves them empty (the card shows the time in its meta line, the year in the month heading). `ticketDate` returns a `stub` (the separate `ticketDate` type goes).
- `partials.html`: `{{define "stubLabel"}}<span class="stub-label"><span>{{.Weekday}}</span><b>{{.Day}}</b><span>{{.Month}}</span>{{with .Year}}…{{end}}{{with .Time}}<span class="hour">…</span>{{end}}</span>{{end}}`, used by `card` (inside `.stub`, `aria-hidden` stays on `.stub`) and `ticketHead` (`aria-hidden` on a wrapper, full date visually hidden as now).
- `style.css`: one `.stub-label` rule (paper label, column, mono uppercase muted text, `b` in ink); size differences only under `.stub` and `.head`. Replaces `.stub .date`, `.head .label`.
- Tests: update the asserted markup; no behaviour change. `make verify-ui` and a look at a card and a concert page (phone, light/dark) to confirm nothing moved.
