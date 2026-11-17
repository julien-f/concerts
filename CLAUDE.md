# CLAUDE.md

Design, threat model and later work: [docs/design.md](docs/design.md). Operations: [docs/operations.md](docs/operations.md). Decisions: [docs/decisions/](docs/decisions/).

## Stack

- Go standard library first: `net/http` (method + path-parameter routing), `html/template`, `embed`, `log/slog`.
- SQLite via `modernc.org/sqlite`, `CGO_ENABLED=0`; pragmas in the DSN (`internal/store/store.go`).
- htmx 2.0.11, vendored in `internal/web/static/`; hand-written, mobile-first CSS; no Node, no front-end build.
- Approved, deferred and rejected libraries and tools, with the slice that brings each: [ADR 0004](docs/decisions/0004-libraries-and-tools.md). Anything else needs approval.
- golangci-lint v2 and govulncheck, pinned in the `Makefile`; golangci-lint is installed into `bin/` (`.golangci.yml`).

## Structure

- `cmd/concerts/`: subcommands (`serve`, `new-group`, `backup`, `demo`…).
- `internal/store/`: SQLite, migrations (`migrations/NNNN_*.sql`, applied at startup), hand-written SQL.
- `internal/web/`: handlers, templates (parsed at startup), static files.
- `internal/prefill/`: ticketing-page parser (fixtures in `testdata/`) and SSRF-safe client.
- `deploy/`: templates of the systemd units and Caddy site (`@NAME@`, `@PROXY_USER@`, `@HOST@`: nothing server-specific), `install.sh`/`uninstall.sh` (root) with `lib.sh`, and `admin` (run on the server as the service user). `docs/`: design, operations and ADRs. `scripts/`: dev tooling (`verify-ui` and `measure-scroll`, sharing `ui-lib.sh`; `measure`, `og-image` for the preview PNGs).
- No interface without a second implementation, no speculative layers.

## Conventions

- Time comes from an injected clock (`now func() time.Time`), never `time.Now()` in `internal/`.
- The group secret never reaches logs or the database (only its SHA-256); request logs record the matched route pattern, never the raw path.
- Every `/g/{secret}` handler goes through `inGroup` (`internal/web/identity.go`), and a `/c/{token}` handler resolves the group cookie through `fromCookie`; user input goes through `parseForm` and `cleanText` (`internal/web/form.go`).
- Every slice's HTTP tests reuse `newTestServer(t)` (`internal/web/testserver_test.go`): temp DB, fake clock, real router, `t.Parallel()`; table tests for several cases.
- One vertical slice per branch (slices in `docs/design.md`), with its migration, handlers, tests and an update of `docs/design.md`; merged with `--no-ff`.
- New schema change = new migration file; never edit a committed one.
- Templates link static files with `{{static "name"}}`, which returns a content-hashed URL served with `Cache-Control: immutable`.
- htmx attributes are written `data-hx-*` (valid HTML for vnu); every htmx form also works as a plain POST with a 303. Shared fragments live in `templates/partials.html`.

## Commands

- `make fmt` after editing Go files. `make run`, `make test` (`go test -race ./...`), `make lint`, `make vuln` (needs network), `make check` (lint + vuln + test; the pre-commit hook runs it when `go.mod`/`go.sum` are staged, else lint + test; `make deploy` runs vuln), `make build`.
- Single test: `go test -race -run TestName ./internal/web`.
- `make demo`: fresh `demo.db` with sample data, then serves it. `make verify-ui`: axe-core and vnu on every demo screen, plus the group page's weight; run before merging a UI change ([ADR 0006](docs/decisions/0006-scripted-ui-verification.md)). `make measure-scroll [REF=main]`: scroll fps of `style.css` at `REF` vs the working tree, on a phone on the same network; for CSS that may be costly to paint (masks, shadows, filters). `make measure`: Go lines, binary size and `make check` duration, for a merge commit's body.

## Pitfalls

- `verify-ui` times out waiting for a swap that never came: `page.click("text=…")` is not strict and silently clicks the first match (e.g. a hint containing the button's word). Target the element: `button:has-text("…")`, `#id`.

- A heading inside `<summary>` is not announced as a heading (the summary's button role hides it), and neither axe nor vnu flags it: keep headings outside `<summary>`.

- A plain `go test -race` run takes 1 s longer than its tests: every `-race` binary sleeps 1 s at exit (`atexit_sleep_ms`) so goroutines still running can report races. `make test` and the children in `backup_fail_test.go` and `cmd/concerts/main_test.go` set `GORACE=atexit_sleep_ms=0`; the cost: a race in a goroutine that outlives every test goes unreported (one a test waits for is still caught).

- Test expects « Qui es-tu ? » but the body has it: French templates put U+202F (narrow no-break space) before `? : ;` and inside « »; write `\u202f` in test strings and Playwright selectors, or match the words only.
