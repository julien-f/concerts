# v1 progress (working memory, deleted in the last v1 commit)

Re-read at the start of each slice and after each context compaction.

## Autonomy rules (summary)

- Never stop before the End: no questions, no waiting for approval, no `/clear` between slices. Pick the simplest option that fits the principles; non-obvious decisions get a short ADR.
- Each slice: own worktree (`.worktrees/<branch>`) and branch from `main`; once done, `git merge --no-ff` from the main checkout (stays on `main`), then `git worktree remove` + `git branch -d`. No push.
- Approved: ADR 0004 dependencies at their slice, repo tooling (Makefile, scripts, hook, `.claude/skills/`), web for research and verification tools.
- Forbidden: runtime dependency outside ADR 0004 (write an ADR, flag it), touching anything outside the repo and scratchpad (`~/.claude` included), editing `.claude/settings.json`, `reset --hard`, `--force`, `--no-verify`, rewriting `main`.
- Blocked twice on the same problem: log it here, continue with the next independent slice, else go to the End.
- Reviews before each merge, one pass: slices 1, 2, 5 → 3 agents (security; accessibility and UI; simplicity and size); others → 1 agent (security and simplicity). Give them the diff and `verify-ui` output, not my conclusion; they never drive Playwright. At most 3 agents at once.
- Design: update the slice's section of `docs/design.md` before coding, fix it after if the code taught better; each section ≤ 40 lines.

## Definition of done (each slice)

- `make check` passes.
- Moderate tests through `newTestServer(t)`: nominal case plus errors and security cases per handler (unknown secret, CSRF, invalid input); per form, a no-JS POST (no `HX-Request`) expecting 303 then the full page; one test applies all migrations to a database from the previous slice; no trivial tests.
- `scripts/verify-ui.sh` on `make demo`: each screen at 390 px, light and dark, 0 axe-core violations, 0 vnu errors; `/g/<secret>` page weight (gzip -9 of HTML + CSS + JS + icons) < 40 kB.
- No third-party resources; no JS beyond htmx except a few justified lines.
- Measures logged below: Go lines (non-test), binary size, page weight, `make check` duration.
- `docs/design.md` describes what shipped; `CLAUDE.md` updated if a convention changes.

## Log

| Slice | State | Next step | Measures |
|---|---|---|---|
| 0 design-v1 | done | — | baseline `make check` 3.2 s; verify-ui 1.8 s warm (11.7 s first run, tools download); page weight 17.7 kB |
| 1 members | done | — | 923 Go lines, binary 12.9 MB, page 18.4 kB, `make check` 3.1 s |
| 2 concerts | done | — | 1500 Go lines, binary 13.4 MB (+0.5 MB, mostly `time/tzdata`), page 20.0 kB, `make check` 3.4 s |
| 3 statuses | done | — | 1740 Go lines, binary 13.4 MB, page 21.0 kB, `make check` 3.7 s |
| 4 main-screen | done | — | 1803 Go lines, binary 13.4 MB, page 21.2 kB, `make check` 6.3 s (cold) |
| 5 prefill | done | — | 2303 Go lines, binary 14.0 MB (+0.6 MB, x/net/html and the client), page 21.3 kB, `make check` 3.0 s |
| 6 agenda | review | 1 review, then merge | 2382 Go lines, binary 14.1 MB, page 21.3 kB, `make check` 8.4 s (lint cache rebuilt after the config change) |

### Discarded review findings

- Phase 0, security #11: move prefill to POST. Kept GET: the route needs the secret, is rate-limited, and a GET form is simpler without JS.
- Phase 0, security #5: replace `wa.me` with Web Share or `whatsapp://`. The owner's spec names `wa.me`; recorded as an accepted risk instead.
- Phase 0, data #6: one global fetch limiter instead of per group. The owner's spec says "one fetch in flight per group, 10 per minute"; a global cap of 2 concurrent fetches is added on top (security #12).

- Slice 1, a11y #6: cursor may land before the refilled name on Firefox Android. Unverified; the field now holds exactly what was typed.
- Slice 1, security #1 (partial): no cap on stacked combining marks (cosmetic) and no NFC normalisation (needs `x/text`, outside ADR 0004; risk recorded in the design).
- Slice 1, security #4: unique-constraint mapping independent of `_txlock=immediate`; not a live bug.

- Slice 2, a11y #7: full weekday names for screen readers. The owner's spec fixes the format « sam. 14 mars · 20 h ».

- Slice 3, #5: SetStatus as one statement. Clearing a missing answer would then affect 0 rows and 404, breaking idempotent clears.

- Slice 5, a11y #4 (complete fix): keep typed fields across a prefill. Rewording the label (« à coller en premier ») is enough for now.
- Slice 5, a11y #7: swap htmx error responses. The prefill route only fails with a rare 500; 404s already trigger HX-Refresh.

## Research notes kept for later slices

- Slice 5 (JSON-LD, fetched 2026-11): Fnac, Ticketmaster, Shotgun, See Tickets/Digitick, AXS, Infoconcert block plain clients (403/401/429 or challenge) → manual entry. Dice, Olympia, Zénith serve JSON-LD; Accor Arena OG only (`<meta name="og:title">`). Shapes: `@type` Event/MusicEvent/ComedyEvent; performer object or array; `startDate` with or without offset, seconds or ms; location `Place`/`EventVenue`; offers object or array, `validFrom` only if in the future (Fnac's is a past listing date); og:title cut at ` | `, `｜`, ` - `, ` @ `, ` Tickets`; treat "null" as missing; trim U+00A0.
- Slice 6 (RFC 5545): VERSION, PRODID, no METHOD/CALSCALE; UID without host or secret (RFC 7986); DTSTAMP from clock; timed = UTC `Z` with DTEND = start + 3 h (never DURATION too); date-only = `VALUE=DATE` with exclusive next-day DTEND; escape `\ ; , newline` in that order; fold at 75 octets on rune boundaries; CRLF everywhere; no URL property, ticket URL in DESCRIPTION; `text/calendar; charset=utf-8`, `attachment; filename="concert.ics"`, `Cache-Control: private, no-store`.
