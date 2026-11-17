# Plan: desktop layout

Branch `desktop-layout`. On a computer the screens are sparse (3 concerts per 1280×800 screen) and the fixed « Ajouter » bar is a thumb pattern with no use for a mouse. Below 48 rem nothing changes visually.

## Changes

1. **Breakpoints** `@media (width < 48rem)` (mobile-only rules) and `@media (width >= 48rem)`: `.top, main` widen from 40 to 56 rem. A portrait iPad (768 px) gets the desktop layout: intended.
2. **« Ajouter un concert »**: one link `class="button add"`, moved from the `after` block (removed: only `group.html` defines it) to `<div class="toolbar">` after the « Filtre » nav, inside `<div class="add-bar">` (no nav: one link isn't navigation). Below 48 rem the fixed bar, its gradient, the big `.add` sizing, `scroll-padding-bottom` and main's bottom padding stay as today, in the mobile block; from 48 rem it is a plain `.button` on the right of the filters. Focus now reaches it right after the filters, before the cards, also on mobile, where it stays drawn at the bottom: accepted (a fixed bar is outside reading order; `scroll-padding-bottom` keeps focused cards visible).
3. **Compact cards** (≥ 48 rem):
   - `.card .body` grid `minmax(0, 1fr) auto`; `.card .statuses { display: contents }` (cards only: the concert page keeps its block; htmx still swaps it, `closest` walks the DOM). Explicit placement: everything in column 1, `.mine` in column 2, rows 1 / span 3, centred, no wrap. Spacing by margins, no `row-gap`, so missing rows take no height.
   - Status lines wrapped in `{{with .Lines}}<div class="who-list">` (partials `statuses`), one wrapping line from 48 rem, labels kept (« Billet : Marc »).
   - « ajouté par » in `<span class="by">` instead of after a `<br>`: own line below 48 rem, prefixed by `" · " / ""` from 48 rem.
   - Tighter card padding.
4. **Answer buttons for a mouse**: `@media not (any-pointer: coarse)` sets `.mine button` to 2.25 rem (36 px, above the 24 px of WCAG 2.5.8); any touch input keeps 2.75 rem.
5. **Concert page** (≥ 48 rem): `<div class="facts">` (date, venue, map, on-sale, added by) and `<div class="side">` (actions, statuses) in two columns; tighter paragraph margins. Modifier/Supprimer stay below the ticket: moving them up would change mobile.
6. **Concert form** (≥ 48 rem): label + input + error wrapped in `<div class="field">` (same gaps on mobile); the form (`stack` kept, plus `fields`) gets 2 columns: Artiste full width, then Date | Heure, Salle | Adresse, Lien | Mise en vente (the `concerts.go` order, so tab order matches); anything else spans both. « Enregistrer » and, on a duplicate, « Ajouter quand même » share a right-aligned row, « Enregistrer » first in the DOM. The prefill box widens with the column.

## Checks

- `verify-ui`: axe also at 768 px (hardest squeeze of the info column; full height, width parametrised), screens not re-run; no horizontal overflow at 390 and 768 px on every screen; on the group screen at 768 px, `.mine` on the same row as `.artist`. Run before merge.
- Go: `statuses_test.go` asserts `who-list` in the htmx fragment.
- Docs: design.md (journey 2, Screens intro, Main wireframe note, Visual direction: « Ajouter », targets « ≥ 44 px on touch », new slice entry), ADR 0006 and the `verify-ui.mjs` header (768 px).
- Group page weight before/after and `make measure` in the commit body.
