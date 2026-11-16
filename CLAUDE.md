# CLAUDE.md

Design, threat model and roadmap: [docs/design.md](docs/design.md). Decisions: [docs/decisions/](docs/decisions/).

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
- `deploy/`: systemd units. `docs/`: design and ADRs. `scripts/`: dev tooling (`verify-ui`).
- No interface without a second implementation, no speculative layers.

## Conventions

- Time comes from an injected clock (`now func() time.Time`), never `time.Now()` in `internal/`.
- The group secret never reaches logs or the database (only its SHA-256); request logs record the matched route pattern, never the raw path.
- Every `/g/{secret}` handler goes through `inGroup` (`internal/web/identity.go`); user input goes through `parseForm` and `cleanText` (`internal/web/form.go`).
- Every slice's HTTP tests reuse `newTestServer(t)` (`internal/web/testserver_test.go`): temp DB, fake clock, real router, `t.Parallel()`; table tests for several cases.
- One vertical slice per branch (roadmap in `docs/design.md`), with its migration, handlers, tests and an update of `docs/design.md`; merged with `--no-ff`.
- New schema change = new migration file; never edit a committed one.
- Templates link static files with `{{static "name"}}`, which returns a content-hashed URL served with `Cache-Control: immutable`.
- htmx attributes are written `data-hx-*` (valid HTML for vnu); every htmx form also works as a plain POST with a 303. Shared fragments live in `templates/partials.html`.

## Commands

- `make fmt` after editing Go files. `make run`, `make test` (`go test -race ./...`), `make lint`, `make vuln` (needs network), `make check` (lint + vuln + test, run by the pre-commit hook), `make build`.
- Single test: `go test -race -run TestName ./internal/web`.
- `make demo`: fresh `demo.db` with sample data, then serves it. `make verify-ui`: axe-core, vnu and page weight on every demo screen; run before merging a UI change ([ADR 0006](docs/decisions/0006-scripted-ui-verification.md)).

## Pitfalls

- A heading inside `<summary>` is not announced as a heading (the summary's button role hides it), and neither axe nor vnu flags it: keep headings outside `<summary>`.

- Test expects « Qui es-tu ? » but the body has it: French templates put U+202F (narrow no-break space) before `? : ;` and inside « »; write `\u202f` in test strings and Playwright selectors, or match the words only.
