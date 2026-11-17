# Quality pass (branch `quality-pass`, deleted before the merge)

Observable behaviour (routes, HTML, schema, CLI) unchanged except for bugs. Each item: one commit.

## Baseline on `main` (409f408)

- `make check`: 0 lint issues, no vulnerabilities, all 4 packages pass.
- `make measure`: 3561 Go lines (non-test), binary 14.2 MB, `make check` 7.8 s.
- `make verify-ui`: 20 screens × 390/768 px × light/dark, 0 axe violations, vnu 0 errors in 20 pages, page weight 22968 B (limit 40000).

## Bugs (test first, seen failing, separate commit)

- [x] web: an invalid text field (artist, venue, address, price, description) comes back empty on the 422 form, so what was typed is lost; « Relire la billetterie » then overwrites it (`parseConcert`, `concerts.go:77`).
- [x] prefill: `prose` strips a different invisible set than the form (misses U+2800), so one braille blank drops the whole description; share one predicate (`parse.go:275`, `web/form.go:60`).
- [x] prefill: `prose` drops tabs, gluing words (« Un\tduo » → « Unduo »).
- [x] prefill: 2 redirects followed where the threat model says 3 (`fetch.go:73`, `via` includes the first request); test named « fourth » tests the third.
- [x] prefill: an inline SVG `<title>` wins over the page `<title>` (`parse.go:113`).
- [x] cmd: `new-group` stores an uncleaned name (blank, tabs break `list-groups`).
- [x] cmd: base URL only validated by `serve`; `rotate-secret` with a bad one kills the old link and prints a broken one.
- [x] store: `Concert` returns a half-filled struct with an error, so a DB error on a deleted concert gives 404, not 500 (`concerts.go:61`).
- [x] Makefile: `install` guard passes with an empty `DEPLOY_KEY` (`test … -a -f` with no operand).
- [x] scripts: `measure-scroll.sh` exits silently under `set -e` before « no default route ».

## Code, by zone (highest gain/risk first)

store
- [x] Upcoming limit checked twice (`checkRoom` before insert, `commitWithin` after): keep `commitWithin` only, `checkRoom` → purge only.
- [x] Sentinel errors together in `store.go`; `ErrLimit` comment covers concerts; `MaxConcerts` comment « at most twice ».

web
- [x] `refreshConcert` restates the 8 form fields: walk the form built from the page instead.
- [x] `publicPage`/`concertPage` compute URL, Map, Listen twice: one helper.
- [x] Year-if-not-current written twice (`frDate`, `ticketDate`).
- [x] `cleanName` `maxRunes` never varies; `card.Base` unused.
- [x] `showConcert` only redirects: rename `redirectToConcertPage`.
- [x] Stale comments: « three buttons » (`statuses.go:25`), `done` and ErrLimit.

templates + CSS
- [x] Dead `.htmx-indicator` rules; unused `class="edit"` in history (kept: removing it changes the HTML).
- [x] Duplicated facts/about/meta markup between `concert.html` and `public.html`: partials.
- [x] CSS duplicates: notches, perforation border, repeated selectors, `.inline` min-height, pressed look.

prefill
- [x] Price range widening written twice (`sites.go:121`, `parse.go:296`); dead `cmp.Or(res.Time, …)`; inner `walk` shadows package `walk`; `Parse` unexported; stale comments (`sites.go:14`, package doc).

cmd
- [ ] Base-URL command list stated twice (`main.go:38,86`); `demo` reads `time.Now` itself; usage says « empty database »; `backup` error unwrapped.

deploy + scripts
- [ ] `verify-ui.mjs` still uses `text=` selectors (CLAUDE.md pitfall).
- [ ] `admin`: rollback/restore share the stop/swap/start sequence; journalctl args stated twice.
- [ ] `install.sh`/`uninstall.sh` SELinux lookup duplicated → `lib.sh`; `base-url.conf` not written via `.new` + `mv`; `check_url` accepts an empty host.

tests
- [ ] Anonymous create/delete/status rows check only 303: also check the picker `Location`.
- [ ] `TestRevisionsAtLimit` (1.8 s, slowest) duplicates `TestConcertLimit`.
- [ ] `TestICSFoldInvalidUTF8` races a real 5 s timer.
- [ ] Shared helpers (`assertContains`, `location`…) into `testserver_test.go`; bulk-insert SQL twice; overlapping 404 rows; `Cookies()[0]` panic; extra DB with `time.Now` in `identity_test.go`.
- [ ] Ignored setup errors in store tests (`g, _ := s.CreateGroup`): `must*` helpers.
- [ ] Missing tests: 500 page and `/healthz` 503, stale `?added=` banner, non-numeric `{rev}`, `demo` in-process.

docs
- [ ] design.md: `token` missing from the data model; `Concert` does not filter deleted; `inGroup` ignores (not drops) a stale cookie; logout and old URL need no identity; « Prévenir le groupe » → « Partager »; icon list; palette names; « only orange button » for members; delete banner « Annuler la suppression »; facts stated twice (row cap, year rule, `next`, group cookie, pre-commit hook).
- [ ] CLAUDE.md: « logged per slice » for `make measure` names no log; GORACE pitfall names one file of two.

## Set aside for the user

- `install.sh` adopts a leftover `/home/<name>` and writes through an unchecked `.ssh` on a first install (root).

## Rejected

- `ConcertByToken` + `Concert` = 3 queries on `/c/`: small page, API churn.
- One cross-site test instead of per-route ones: the per-route rows guard a route mounted outside the wrapper.
- Exit code 2 for usage errors: behaviour change, nothing depends on it.
- design.md section lengths: taste.

## State

Bugs done. Next: zones in the order above, store first.
