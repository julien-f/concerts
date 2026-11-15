# Design

Living document: each slice updates it.

## Need

A WhatsApp group of friends (5–30 people) shares concerts and sees who goes to what.

## Solution

- A web app opened through one secret link per group: `/g/<secret>`.
- No account: on the first visit, a member picks their first name, which the app remembers (cookie).
- Anyone holding the link belongs to the group; if the link leaks, the secret is rotated.
- Go, single binary under systemd on a personal server, behind an HTTPS reverse proxy.
- Local repository, no CI: the pre-commit hook (`make check`) is the safeguard.

## Out of scope

- WhatsApp bot: the official API is out of reach for this use, unofficial libraries risk a ban of the number ([ADR 0003](decisions/0003-no-whatsapp-bot.md)).
- Accounts and passwords ([ADR 0002](decisions/0002-secret-link-identity.md)).
- Push notifications.

## Secret link threat model

- **Secret**: 16 bytes from `crypto/rand` (128 bits), base64url without padding; guessing is infeasible.
- **Storage**: only the SHA-256 of the secret, under a unique index; lookup by hash. A database leak does not reveal working links.
- **Leaks through the browser**: `Referrer-Policy: no-referrer` keeps the link out of outbound `Referer`; `X-Robots-Tag: noindex` keeps it out of search engines.
- **Leaks through the server**: request logs redact `/g/<secret>`; the reverse proxy's access logs must do the same or be disabled for `/g/`.
- **Identity cookie** (slice 1): `HttpOnly`, `SameSite=Lax`, `Path=/g/<secret>`, `Secure` when the base URL is https, `Max-Age` 400 days.
- **Accepted risks**: anyone with the link can join and act as any first name; a leaked link (forwarded message, screenshot) is fixed by rotating the secret. Members are friends, so impersonation is a social, not technical, problem.

## Data model (target)

- `groups`: id, name, secret_hash, created_at. *(slice 0)*
- `members`: id, group_id, first name (unique per group), created_at.
- `concerts`: id, group_id, artist, date, venue, ticket URL, optional on-sale date, created_by, created_at.
- `statuses`: member_id, concert_id, status (`interested` | `going` | `ticket`), updated_at; one per member and concert.

## Roadmap

One slice per branch.

0. ✅ Skeleton: groups, `new-group`, `/g/<secret>` empty page, `/healthz`.
1. Members and first-name identity.
2. Concerts (artist, date, venue, ticket link, optional on-sale date; past concerts hidden).
3. Statuses: interested / going / ticket bought.
4. Views "who goes where" and "my concerts".
5. Prefill from a ticketing URL (Open Graph and JSON-LD `MusicEvent`; manual entry on failure).
6. `.ics` feed per group, subscribable via `webcal://`, filterable by member.
7. Open Graph preview of concert pages and a "Share on WhatsApp" button (`https://wa.me/?text=`).
