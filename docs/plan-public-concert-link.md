# Plan: public concert link

Branch `public-concert-link`. Members share concerts by copying the address bar (or the browser's share sheet, or forwarding « Prévenir le groupe »), and today that URL, `/g/<secret>/concerts/<id>`, gives the whole group. Goal: the concert page lives at `/c/<token>`, which shows an outsider a read-only card and a member the full page. The list, forms and the links they hold (« Annuler », `?next=`) stay under `/g/<secret>` (a later slice may move them).

## Data

- Migration `0007_concert_token.sql`: `concerts.token TEXT` (nullable: SQLite refuses a non-constant default on ADD COLUMN), backfilled with `lower(hex(randomblob(12)))`, then `CREATE UNIQUE INDEX concerts_token`.
- `CreateConcert` sets a token from `crypto/rand` (12 bytes, hex): the same source as the group secret.
- Stored in clear, unlike the group secret: it must be rendered into links, and it unlocks only what the public card shows.
- `store.Concert.Token`, selected by `concertSelect`; test inserts of raw rows gain a token. `Store.ConcertByToken(ctx, token) (groupID, id int64, err error)`; the caller then uses `Concert`.

## Membership on `/c`

- `inGroup`'s body becomes `resolve(ctx, secret, memberID string) (*groupRequest, error)`, shared with `/c`.
- On GET only, `inGroup` sets cookie `g<groupID>` = `<secret>.<memberID>` (member part empty without identity), `Path=/c`, `HttpOnly`, `SameSite=Lax` (a tap in WhatsApp must send it), `Secure` as today, 400 days. Every write ends in a GET, so join, logout and rotation converge with no other writer.
- `/c/{token}` parses `g<groupID>` strictly (22-character base64url secret, empty or decimal member; anything else counts as no cookie), resolves it, and checks the group matches the concert's. Then `Base = /g/<secret>`: links and forms in the HTML keep the secret, the address bar does not.
- Missing, malformed or rotated cookie (new phone, in-app browser) gives the public card.

## Routes

- `GET /c/{token}`: member → today's concert page (banners included); else the public card, which ignores banner queries. Unknown or deleted → `notFoundText` 404, identical for every case, cookie or not.
- `GET /c/{token}/agenda.ics`: public, replaces `GET /g/{secret}/concerts/{id}/agenda.ics`.
- `GET /g/{secret}/concerts/{id}`: loads the concert in that group (in-group 404 otherwise, so ids from other groups yield no token), then 303 to `/c/<token>` with the query string kept. Old links, the picker's `next`, `revisedURL`, `setStatus`'s no-JS return and `HX-Redirect` all land there through that hop.
- Both `/c` routes set `Cache-Control: private, no-cache` and `Vary: Cookie` before the lookup. Logs keep the pattern `/c/{token}`.

## Links

- `pageURL` (`og:url`, canonical, « Prévenir le groupe »), list cards and the history page's back link → `/c/<token>`.

## Public card (`public.html`)

- Shown: artist (title « Artiste · Concerts », `og:title`), date and time, venue, address with « Voir sur la carte », « Mise en vente : », « Billetterie », « Ajouter à mon agenda », and « 1 personne du groupe y va » / « N personnes du groupe y vont » (going plus ticket: one status per member) when non-zero.
- Not shown: group name (brand « Concerts », `og:site_name` « Concerts »), first names, « ajouté par », history, edit links, « ← Tous les concerts », « Prévenir le groupe », « Ce n'est pas moi ».
- Footer: « Tu fais partie du groupe ? Ouvre une fois le lien du groupe dans WhatsApp, puis reviens sur cette page. »
- `<link rel="canonical">` on both renderings; `noindex` kept.

## Tests (`newTestServer`)

- Token set on create; the upgrade test asserts the backfill.
- Card without cookie: no first names, group name, `/g/`, `/logout`, nor banner; unknown and deleted tokens give the same 404 with and without cookie.
- Cookie from a `/g` visit: member page, statuses work; after a rotation, the old cookie gives the card; a cookie `g<A>` holding group B's secret gives the card; malformed cookies give the card.
- `/g/<secret>/concerts/<id>?saved=N` → 303 to `/c/<token>?saved=N`; another group's id → 404. Logout then `/c`: page without identity, « Choisis ton prénom ».
- `.ics` without cookie; `Vary: Cookie`; the log never contains the token.
- `verify-ui`: a last « public » screen: href from a card, clear cookies, open it; no first name, no group link.

## Docs

- `design.md`: threat model (rewrite the previews risk; cookie holds the raw secret on `/c`, never logged by the proxy; tokens in clear and never rotated: deleting the concert is the only revocation; old links carry the secret until a post-deploy rotation), journeys, Concert screen, routes, slice 10.
- ADR 0009 « Public concert link », amending ADR 0002.
- `CLAUDE.md` Conventions: `/c/{token}` handlers resolve the group through `resolve`.
- `operations.md`: rotate once after deploy.
