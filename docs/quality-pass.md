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
- [x] Base-URL command list stated twice (`main.go:38,86`); `demo` reads `time.Now` itself; usage says « empty database »; `backup` error unwrapped.

deploy + scripts
- [x] `verify-ui.mjs` still uses `text=` selectors (CLAUDE.md pitfall).
- [ ] `admin`: rollback/restore share the stop/swap/start sequence; journalctl args stated twice. (set aside: root scripts, untestable here)
- [x] `check_url` accepts an empty host or a path (with the base URL fix).
- [ ] `install.sh`/`uninstall.sh` SELinux lookup duplicated → `lib.sh`; `base-url.conf` not written via `.new` + `mv`. (set aside: root scripts, untestable here)

tests
- [x] Anonymous create/delete/status rows check only 303: also check the picker `Location`.
- [x] `TestRevisionsAtLimit` (1.8 s, slowest) duplicates `TestConcertLimit`.
- [x] `TestICSFoldInvalidUTF8` races a real 5 s timer.
- [x] Shared helpers (`assertContains`, `location`…) into `testserver_test.go`; bulk-insert SQL twice; overlapping 404 rows; `Cookies()[0]` panic; extra DB with `time.Now` in `identity_test.go`.
- [x] Ignored setup errors in store tests (`g, _ := s.CreateGroup`): `must*` helpers.
- [x] Missing tests: 500 page and `/healthz` 503, stale `?added=` banner, `demo` in-process (non-numeric `{rev}` was already covered).

docs
- [x] design.md: `token` missing from the data model; `Concert` does not filter deleted; `inGroup` ignores (not drops) a stale cookie; logout and old URL need no identity; « Prévenir le groupe » → « Partager »; icon list; palette names; « only orange button » for members; delete banner « Annuler la suppression »; facts stated twice (row cap, year rule, `next`, group cookie, pre-commit hook).
- [x] CLAUDE.md: « logged per slice » for `make measure` names no log; GORACE pitfall names one file of two.

## Set aside for the user

- `install.sh` adopts a leftover `/home/<name>` and writes through an unchecked `.ssh` on a first install (root).
- Refactors of `deploy/admin`, `install.sh`, `uninstall.sh` (dedup above): only a container run (ADR 0008) would verify them.

## Review rounds

- After bugs + store: /simplify (2 fixes: CLI test helper, needsURL check) and /code-review high (7 fixes: site-rule descriptions, braille blank as space, `~` in DEPLOY_KEY, base URL path/query, refresh test, invalid UTF-8 echoed, Deleted read only without error; dismissed: write before refusing at the limit (same lock), `Invisible` in prefill (stripping must precede `unglue`), usage text listing commands (operator doc)).

## Rejected

- `ConcertByToken` + `Concert` = 3 queries on `/c/`: small page, API churn.
- One cross-site test instead of per-route ones: the per-route rows guard a route mounted outside the wrapper.
- Exit code 2 for usage errors: behaviour change, nothing depends on it.
- design.md section lengths: taste.

- After all zones: /simplify (helpers used everywhere, misplaced doc comment, CleanName alias; dismissed: otherYear reparse via stubOf (cards would show the year), valid's four scans) and /code-review high (5 fixes: invalid UTF-8 in every sent-back field, `https://h#` and `https://h:`, check_url after the slash strip, invisible characters in every prefilled field, otherYear takes a parsed date; dismissed again: limit refusal after the write, the Deleted guard in both layers, needsURL; dismissed: plain turning no-break spaces into spaces, which the form's paragraphs already does).

## Round 2 (inventory on the new code)

- Bugs fixed: verify-ui weighed the picker, not the list (24884 B now, 22909 reported before); « Rien trouvé » checked before validation; invalid edit of a deleted concert gave a 422 form; base URL without host name / bad port (Go and check_url); JSON-LD offset without a colon.
- Tidy: no-op form margins and stale CSS comment; prose = clean; plain/Invisible comments; TestNewClientChecksDial; store « nothing listed » checks; AddedBy comment; tests for history dates and the word cut; stale doc lines.
- Its reviews: edit loads the concert only for an invalid form; prefill result compared without the link; word kept when the cut ends it; hours-only offset; ports 1–65535 and « no host name » (Go and check_url); verify-ui weighs in its own context.
- Kept below the bar: UpdateConcert/CreateMember returning values with errors (callers check err first); form.html's two repeated lines; render/fragment's shared buffering; pickerPage.MaxRunes.

## State

Round 2 and its reviews done. Next: round 3 inventory (same zones, bar: bugs or clear maintainability wins); stop when a round finds nothing worth a commit; then make check, verify-ui, measure, the user question (set-aside items), the merge commit body.
