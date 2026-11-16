# Improvement log (temporary, deleted in the last commit)

Cycles merged: 10. Consecutive audits without a kept finding: 0.

## Remaining (ranked)

(empty: second audit next)

## Proposals (not delivered autonomously)

- Hook runs `make vuln` only when go.mod/go.sum change (offline commits; vuln is 95 % of make check): relaxes a hook check.
- Group-aware 404 page (header + link back) for a deleted concert reached from a shared link, instead of plain « Page introuvable. » (web.go notFound): a new screen.

## Discarded

- delete-member leaves the name in free pages and in backups (store DSN): score 1; secure_delete would be a pragma change.
- Cross-origin 403 logged as "POST (unmatched)" without reason (web.go): score 1.
- /healthz stays 200 on a full disk: score 1.
- Nightly backup failure only in the journal: needs an alert channel (proposal-level).
- README:17 omits `demo` among BASE_URL users: harmless.
- pre-deploy.db is 0644 (Makefile:68, sudo umask 022) in a 0755 dir: score 1.
- Limit-reached 422 is bare text (concerts.go:139): score 1.
- systemd exposure 1.7/2.2: remaining items have no observable benefit.
- Venue/banner without overflow-wrap: constructed (28+ char unbroken word).
- Dead `concertForm.FormError`, unused `.htmx-indicator` CSS: gain 0, few lines.
- « Lien de la billetterie » vs « Lien de billetterie », picker hint omits digits: gain 0.
- Statuses single-concert query uses group index: ≤ 1000 rows, negligible.

- docs/design.md:180 says `Concert(groupID, id)` filters `deleted_at IS NULL`; it returns deleted rows too (concerts.go:57): score 1, misleads only a reader of the code who reads it anyway.

- ics.go:63 « Billets : » with an ASCII space: no layout effect in calendar apps, golden-file churn only.
- new-group/rotate-secret print an http:// link without a warning: the scheme is visible in the printed link.

## Blocked

(none)
