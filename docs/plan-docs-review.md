# Plan: documentation review

Branch `docs/review`, three commits, merged with `--no-ff`; this file is deleted before the merge.

## 1. Fix what is wrong

- README: "the proxy's group" → "the proxy's user" (install parameter is `PROXY_USER` since 306d3cc); env list: `CONCERTS_BASE_URL` also required by `demo` (main.go:83).
- ADR 0008: sudoers and admin user `concerts` → `<name>`. The "Verified" line stays: it records what was tested then.
- ADR 0004: `parseXForm` → `parseForm`; Web Share row and "checks that stay out of the repo" marked superseded by ADR 0007 and ADR 0006; `x/time/rate` moved from Deferred to Rejected: slice 5 shipped a hand-written limiter (`internal/web/prefill.go`).
- CLAUDE.md: add `@HOST@` to the deploy template placeholders (`deploy/site.caddy`).
- design.md "Later": the `rrsync` pull key would be removed by the next `make install`, which rewrites `authorized_keys` with the deploy key only; say install must learn a second key first.

## 2. Tell members what the UI doesn't

- Picker (`picker.html`), one line under « Qui es-tu ? »: « Ton prénom et tes réponses sont visibles par tout le groupe. Pas de mot de passe : garde ce lien entre vous. » (U+202F before `:`).
- Unknown-secret 404 (`web.go` `notFound`, plain text, same for any unknown path so it reveals nothing): « Page introuvable. Si c'est le lien d'un groupe, il a peut-être changé : demande le nouveau au groupe. » Update `notfound_test.go`.
- Operator doc (section 3): a ready-to-paste WhatsApp message to send with the link after `new-group` and `rotate-secret`: the link is the key (don't forward), new phone = open the link and tap your name, anyone can edit or delete a concert, no notifications (« Prévenir le groupe »), ask <operator> for a leak or to remove your name.
- design.md: picker mockup and the 404 line in Routes.
- `make test`, `make verify-ui`.

## 3. Operator doc in `docs/operations.md`

- Move README "Deploy" through "Uninstall" there unchanged; README keeps the pitch, Run, Test and a link. Fix links to `README.md#deploy` (design.md).
- Add, one short section or bullet each:
  - Member message (section 2).
  - Monitoring: nothing alerts; what to check by hand (`/healthz`, newest file in `backups/`, `bin/admin logs`). Automation stays in design.md "Later".
  - Manual snapshot before a risky step: `bin/admin backup backups/manual-<date>.db`.
  - Restoring an off-server copy: copy it into `backups/`, then `restore`.
  - Changing the base URL: rerun install with the new `BASE_URL`, update the proxy site, every link changes host: resend them.
  - `bin/admin` runs any subcommand, but `demo` and `serve` are for development: `demo` adds a sample group to the live database.
- Deduplicate:
  - Pre-commit hook: what it runs stays in CLAUDE.md (Commands); README Test keeps only how to enable it; design.md Solution keeps the why (no CI) without the list.
  - Operator journey (design.md journey 7) → one line linking operations.md.
  - Group caps: stated in design.md Data model; threat model and Edge cases keep their reasoning and link there.
- design.md: "Roadmap" → "Slices (v1, shipped)"; "Later" becomes its own section.
- CLAUDE.md Commands: add `make measure`; README Run: `make demo` listens on `:8080`, so on the LAN.

## Out of scope (proposed separately)

- UI wording changes (« Mes concerts » vs « Mes réponses », on-sale date without reminder, tap again to clear).
- Code: `demo` refusing a non-empty database, a version stamp in the binary, backup/health alerts, off-server backups.
