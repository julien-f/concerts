# 0004: Libraries and tools

## Context

Goal: develop fast and ship a polished app (clean, accessible, fast on mobile) while keeping the stdlib-first, minimal-dependency stance of [ADR 0001](0001-go-sqlite-htmx-self-hosted.md). Candidates were reviewed in November 2026 from four angles: Go backend, front end and accessibility, ops and performance, security.

## Decision

Adopted (the slice is when the tool first gets used):

| What | When | Notes |
|---|---|---|
| `govulncheck` v1.9.0 | now | Pinned in `make check` (`make vuln`). Reports only reachable vulnerabilities. Needs network. |
| `http.CrossOriginProtection` (stdlib) | slice 1 | Wraps the mux. CSRF protection without tokens. Every state change is a POST. |
| `http.MaxBytesReader`, length/UTF-8 checks, server timeouts (stdlib) | slice 1 | Hand-written `parseForm` instead of validation libraries. |
| `time/tzdata` + `Europe/Paris` (stdlib) | slice 2 | Times stored as UTC, displayed in local time. |
| Lucide icons, inline SVG | slice 3 | Copy only the icons used. Decorative icons get `aria-hidden`. |
| `golang.org/x/net/html` + `encoding/json` | slice 5 | Open Graph and JSON-LD prefill, tested on saved HTML fixtures. |
| SSRF-safe client (stdlib) | slice 5 | `net.Dialer.Control` rejects non-global and private IPs. http/https only, capped redirects, body and time limits. |
| Hand-written `.ics` | slice 6 | CRLF, escaping, 75-octet folding, stable UID, golden-file test. |
| ~~Web Share API with `wa.me` fallback~~ | slice 7 | Superseded by [ADR 0007](0007-plain-whatsapp-link.md): a plain `wa.me` link. |

Front-end practices:
- Hand-written CSS using native features (`:focus-visible`, `prefers-reduced-motion`, `color-scheme`, view transitions).
- `system-ui` fonts.
- Plain forms with Post/Redirect/Get, enhanced with `hx-*`. Native elements (`fieldset`, `details`, `dialog`) instead of JS widgets.

Ad hoc checks that stay out of the repo: axe-core through Playwright (390px wide, light and dark), and the Nu HTML Checker (vnu). Superseded by [ADR 0006](0006-scripted-ui-verification.md): scripted in `make verify-ui`.

Deferred:
- `sqlc`: reconsider past about 20 queries.
- Litestream: only if losing up to a day of data (nightly `VACUUM INTO`) becomes unacceptable.
- `go-cmp`: only if test diffs become painful.

Rejected:
- `templ`: codegen, still pre-1.0. `html/template` parsed at startup plus a test that renders every page covers it.
- ORMs, sqlx, query builders, migration libraries, `mattn/go-sqlite3` (needs CGO).
- Routers (chi, gin, echo) and gorilla/csrf.
- testify, validator, gorilla/schema.
- Compression inside the app: the reverse proxy compresses.
- CSS frameworks:
  - Pico.css: generic look, defaults to fight, no release since 2025-03.
  - Open Props, Simple.css.
- Tailwind, Alpine.js, _hyperscript, icon fonts, web fonts.
- `hx-boost` on everything: it disturbs focus and scroll for screen readers.
- JSON-LD processors (json-gold), goquery, OG libraries.
- Rarely updated `.ics` or SSRF libraries.
- `golang.org/x/time/rate` (was deferred): slice 5 hand-writes its limiter (about 40 lines in `internal/web/prefill.go`), which also caps fetches in flight.

## Consequences

- Runtime dependencies stay at `modernc.org/sqlite`, plus `golang.org/x/net` from slice 5. Every other addition needs approval.
- About 60 lines each are hand-written for `.ics` and the SSRF guard, so their edge cases are ours to test.
- `make check` needs network access (about 3 s warm).
