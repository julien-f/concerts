# Design

Living document: each slice updates it. Each section stays under 40 lines; what doesn't guide the code doesn't go here.

## Need

A WhatsApp group of friends (5–30 people) shares concerts and sees who goes to what.

## Solution

- A web app opened through one secret link per group: `/g/<secret>`.
- No account: on the first visit, a member picks their first name, which the app remembers (cookie).
- Anyone holding the link belongs to the group; if the link leaks, the secret is rotated.
- Go, single binary under systemd on a personal server, behind an HTTPS reverse proxy.
- Local repository, no CI: the pre-commit hook (`make check`) is the safeguard.
- UI in French (`lang="fr"`); code, docs and commits in English.

## Out of scope

- WhatsApp bot ([ADR 0003](decisions/0003-no-whatsapp-bot.md)); accounts and passwords ([ADR 0002](decisions/0002-secret-link-identity.md)); push notifications.
- Calendar subscription feeds: a feed URL would hand the secret to a calendar provider; one-concert `.ics` downloads instead.
- Images (posters, avatars), comments, analytics.

## Secret link threat model

- **Secret**: 16 bytes from `crypto/rand` (128 bits), base64url without padding; guessing is infeasible.
- **Storage**: only the SHA-256 of the secret, under a unique index; lookup by hash. A database leak does not reveal working links.
- **Leaks through the browser**: `Referrer-Policy: no-referrer`; `X-Robots-Tag: noindex`; group pages send `Cache-Control: private, no-cache`.
- **Leaks through the server**: request logs record the matched route pattern (`GET /g/{secret}/concerts/{id}`), never the raw path, so no spelling (`//g/…`, `/./g/…`) can leak the secret; no IP addresses. The reverse proxy's access logs must redact `/g/` or be disabled.
- **Leaks through features**: the `.ics` holds no page URL; the prefill fetcher sends no cookie or `Referer`; absolute URLs come from `CONCERTS_BASE_URL` (required by `serve`), never from `Host`.
- **Identity cookie**: `HttpOnly`, `SameSite=Lax`, `Path=/g/<secret>`, `Secure` when the base URL is https, `Max-Age` 400 days. It holds the member id, checked against the group on every request.
- **CSRF**: every state change is a POST behind `http.CrossOriginProtection` (lets GET through; trusts `Sec-Fetch-Site`, else compares `Origin` with `Host`, so the proxy must keep `Host`). The prefill GET triggers an outbound fetch but needs the secret and is rate-limited.
- **Abuse with a leaked link**: at most 100 members and 500 live concerts per group (422 beyond); server read, write and idle timeouts; request bodies capped at 64 kB.
- **SSRF** (prefill): see slice 5.
- **Accepted risks**: anyone with the link can join and act as any first name; members are friends, so impersonation is social, not technical. « Prévenir le groupe » (`wa.me`) and link previews in other messengers hand the link to the messaging provider's servers. Both are fixed by rotating the secret.

## User journeys

1. **Join**: tap the link in WhatsApp → picker screen → tap « C'est moi » on one's name, or type a new first name → main screen. Later visits skip the picker (cookie). From a shared concert link without identity, the concert page shows « Choisis ton prénom pour répondre », and the picker returns to that concert (`next`).
2. **Add**: tap « Ajouter un concert » (fixed at the bottom) → paste a ticket URL, tap « Pré-remplir » → check or type the fields → « Enregistrer » → main screen with a banner « Concert ajouté » and « Prévenir le groupe » (opens WhatsApp with artist, date and the concert link).
3. **Respond**: on a card, tap « Ça m'intéresse », « J'y vais » or « J'ai mon billet »; tapping the active one clears it. The status block updates in place (htmx) or after a full reload (no JS).
4. **Plan**: filter « Mes concerts »; open a concert → « Ajouter à mon agenda » downloads its `.ics`; « Billetterie » opens the ticket link.
5. **Fix**: open a concert → « Modifier » or « Supprimer » → banner « Concert supprimé » with « Annuler ».
6. **Shared phone**: « Ce n'est pas moi » in every page header → picker again.
7. **Operator** (SSH): `new-group`, `list-groups`, `rotate-secret`, `delete-member`, `backup`; `make deploy`, `make rollback`.

## Screens

Mobile first (390 px), one column, max 40 rem wide on larger screens. Landmarks `<header>`, `<main>`, `<nav aria-label="Filtre">`. Header on every group page: group name, « <Prénom> » and a « Ce n'est pas moi » button (POST form, 44 px target). `<title>` starts with the banner or « Erreur » when there is one, so no-JS screen-reader users hear it after a redirect.

### Picker (`/g/<secret>` without identity)

```
┌──────────────────────────────┐
│ Les copains du concert       │
│ Qui es-tu ?                  │  h1
│ ┌──────────────────────────┐ │
│ │      C'est moi : Léa     │ │  one big button per member,
│ └──────────────────────────┘ │  alphabetical
│ ┌──────────────────────────┐ │
│ │      C'est moi : Marc    │ │
│ └──────────────────────────┘ │
│ ── Pas dans la liste ? ───── │
│ Nouveau prénom [__________]  │
│ Marc est déjà dans la liste :│  on duplicate: aria-invalid,
│ si c'est toi, [C'est moi :   │  aria-describedby, autofocus,
│ Marc] ; sinon ajoute une     │  field refilled "Marc "
│ initiale (ex. « Marc D. »)   │
│ [        Rejoindre         ] │
└──────────────────────────────┘
```

### Main (`/g/<secret>`)

```
┌──────────────────────────────┐
│ Les copains   Léa · Ce n'est │
│ du concert          pas moi  │
│ ✓ Concert ajouté [Prévenir le│  banner after add / delete
│   groupe]  (or [Annuler])    │
│ (Tous) ( Mes concerts )      │  filter links, aria-current
│ MARS 2027                    │  h2 per month (h1 = group)
│ ┌────┬─────────────────────┐ │
│ │sam.┆ Fontaines D.C.      │ │  h3 linking to the concert;
│ │ 14 ┆ sam. 14 mars · 20 h │ │  stub aria-hidden, <time>
│ │mars┆ · Zénith Paris      │ │  in the body
│ │    ┆ ✓ Y vont : Léa, Marc│ │  SVG icon + text per status,
│ │    ┆ ★ Intérêt : Sam     │ │  names alphabetical
│ │    ┆ [★ Ça m'intéresse]  │ │  role=group "Ma réponse",
│ │    ┆ [✓ J'y vais][🎟 J'ai │ │  aria-pressed, stable ids,
│ │    ┆  mon billet]        │ │  flex-wrap
│ └────┴─────────────────────┘ │
│ ▸ Passés (3)                 │  <details>, names only
│ [   + Ajouter un concert   ] │  fixed <a>, main screen only
└──────────────────────────────┘
```

- Status lines: « Billet : », « Y va : » / « Y vont : », « Intérêt : » (no gender, count-aware).
- « Mes concerts » = concerts where I have a status, computed in Go from the statuses already loaded; « Passés » is filtered too; clearing a status under the filter keeps the card until reload.
- Past cards show names only, no buttons, newest first, under an `h2` « Passés (N) » followed by a `<details>` (« Afficher les concerts passés »), rendered `open` when an added or restored concert is past. A late answer posted to a past concert is still accepted.
- Empty filter with only past answers: « Aucun concert à venir parmi les tiens. »
- Empty list: « Aucun concert pour l'instant. Ajoute le premier ! »; empty filter: « Tu n'as encore réagi à aucun concert. »
- Banners come from the URL but show only while true: `?added=<id>` and `?restored=<id>` if that concert is in the list, `?deleted=<id>` (« Annuler la suppression ») if it is still deleted; an edit returns to the concert page with `?saved`.

### Concert (`/g/<secret>/concerts/<id>`)

```
┌──────────────────────────────┐
│ ← Tous les concerts          │
│ Fontaines D.C.               │  h1
│ sam. 14 mars · 20 h          │
│ Zénith Paris                 │
│ Mise en vente : ven. 3 oct.  │  if set
│ ajouté par Léa               │
│ [Billetterie ↗] [Agenda]     │  ticket link if set; .ics
│ [Prévenir le groupe]         │
│ statuses + my 3 buttons      │  same block as the card
│ [Modifier]   [Supprimer]     │
└──────────────────────────────┘
```

Without identity: statuses shown, buttons and edit/delete replaced by « Choisis ton prénom pour répondre » (picker with `next`, accepted only as `/concerts/<id>`). The year is shown when it differs from the current one.

### Concert form (`/concerts/new`, `/concerts/<id>/edit`)

```
┌──────────────────────────────┐
│ ← Annuler                    │
│ Ajouter un concert           │
│ Lien de billetterie          │  own GET form (new only),
│ [https://…        ][Pré-remplir]│ hx-disabled-elt, indicator
│ (Impossible de lire cette    │  « Lecture de la page… »;
│  page : remplis à la main.)  │  failure/limit, role=status
│ ─────────────────────────────│  main POST form below
│ Artiste*      [____________] │  autofocus after prefill
│ Date*  [jj/mm/aaaa] Heure [--:--]│
│ Salle*        [____________] │
│ Mise en vente [jj/mm/aaaa]   │
│ [        Enregistrer        ]│
└──────────────────────────────┘
```

- The ticket link field of the main form carries the prefilled URL, so Enter in any field saves instead of prefilling.
- Errors: listed above the form and tied to each field (`aria-describedby`, `aria-invalid`); the first invalid field gets `autofocus`.

## Visual direction

- **Motif: the ticket stub**, chosen over a gig-poster direction (phase 0 mockup, removed once applied in slice 2): denser, warmer, the stub naturally carries the date. Implemented in `internal/web/static/style.css`.
- Card = stub (weekday, day, month in `ui-monospace`) + dashed perforation + body; half-circle notches cut into the card.
- Palette: paper `#f4efe6` / card `#fffdf8` / ink `#1f1a17` / brick accent `#b4361d`; dark: `#17140f` / `#24201a` / `#f1ebe1` / `#ff8a65`. AA contrast checked by axe.
- Month headings: small monospace caps in the accent color.
- Pressed status = filled ink button + icon + `aria-pressed`; never color alone.
- `system-ui` fonts, fields ≥ 16 px, targets ≥ 44 px, `:focus-visible` outline, `prefers-reduced-motion` respected.
- Fixed « Ajouter » bar on the main screen only, padded by `env(safe-area-inset-bottom)`; `scroll-padding-bottom` and list padding keep focused and last cards visible.
- Icons: inline Lucide SVG (`star`, `check`, `ticket`), `aria-hidden`; never emoji.

## Data model

Timestamps (`created_at`…) are UTC RFC 3339. Concert date and time are Europe/Paris wall-clock strings ([ADR 0005](decisions/0005-concert-wall-clock-time.md)). All tables `STRICT`, foreign keys on.

```sql
groups   (id PK, name, secret_hash BLOB UNIQUE, created_at)
members  (id PK, group_id → groups ON DELETE CASCADE, name, name_key,
          created_at, UNIQUE (group_id, name_key))
concerts (id PK, group_id → groups ON DELETE CASCADE,
          artist, date 'YYYY-MM-DD', time 'HH:MM' NULL, venue,
          ticket_url NULL, on_sale_date 'YYYY-MM-DD' NULL,
          created_by → members ON DELETE SET NULL, created_at,
          deleted_at NULL)
          INDEX (group_id, date)
statuses (concert_id → concerts ON DELETE CASCADE,
          member_id → members ON DELETE CASCADE,
          status CHECK IN ('interested','going','ticket'),
          PRIMARY KEY (concert_id, member_id)) WITHOUT ROWID
```

- `name` is stored with spaces trimmed and collapsed; `name_key` = `strings.ToLower(name)`, the one rule for "same name" and for sorting (byte order: « Élodie » sorts after « Zoé », accepted). No accent folding or Unicode normalisation (would need `x/text`): « Elodie » and « Élodie » are two names.
- Main page = 2 queries per group (live concerts with creator name; their statuses with member names), assembled in Go: no N+1.
- The identity cookie holds the member id; `inGroup` (web) resolves the secret, sets `Cache-Control` and drops a cookie naming a member of another group or a deleted one.
- One accessor `Concert(groupID, id)` filters `group_id` and `deleted_at IS NULL` for every route but restore, which updates only a deleted row of the group; anything else is 404. Status writes also check the member belongs to the group.
- Required: artist, date, venue. Optional: time, ticket link, on-sale date (a link is often not known yet). At most 500 live concerts per group (restores included) and 1000 rows with deleted ones, so create-then-delete cannot fill the disk.
- Validation (`parseForm`, also applied to prefilled values): text trimmed, 1–100 runes (name 1–30), valid UTF-8, no invisible characters (control, format, variation selectors, default-ignorable, braille blank), first names starting with a letter or digit; dates `YYYY-MM-DD`, time `HH:MM`; ticket URL absolute `http`/`https`, ≤ 2000 bytes.
- Writes use `_txlock=immediate` and `busy_timeout`; status writes are upserts (idempotent). `synchronous(NORMAL)` may lose the last commits on power loss: accepted.

## Routes

All under `/g/{secret}` (written `…`); unknown secret → 404. Every POST: `CrossOriginProtection`, then 303 (Post/Redirect/Get) — htmx requests get a fragment instead where noted.

| Method | Path | Role | Response |
|---|---|---|---|
| GET | `/healthz` | DB ping | text |
| GET | `/static/{name}` | hashed static file | file |
| GET | `…` | picker (no identity, optional `next`) or main list; `?mine=1` filter, `?added=<id>` / `?deleted=<id>` banner | page |
| POST | `…/me` | pick `member_id` or create `name`; sets cookie | 303 `next` (a path inside the group) or `…`; 422 picker |
| POST | `…/logout` | « Ce n'est pas moi »: clears cookie | 303 `…` |
| GET | `…/concerts/new` | add form; `?url=` pre-fills from the ticket page | page; htmx swaps the form (`hx-select`) |
| POST | `…/concerts` | create | 303 `…?added=<id>`; 422 form |
| GET | `…/concerts/{id}` | concert page, Open Graph tags | page |
| GET | `…/concerts/{id}/edit` | edit form | page |
| POST | `…/concerts/{id}` | update | 303 `…/concerts/{id}?saved`; 422 form |
| POST | `…/concerts/{id}/delete` | soft delete | 303 `…?deleted=<id>` |
| POST | `…/concerts/{id}/restore` | undo delete | 303 `…?restored=<id>#c<id>`; 422 if the group is full |
| POST | `…/concerts/{id}/status` | set `status` (empty clears); `back` = `list`, `mine` or `concert` | htmx: status block fragment; else 303 to `…#c<id>`, `…?mine=1#c<id>` or the concert page |
| GET | `…/concerts/{id}/agenda.ics` | one-event calendar | `text/calendar` |

- Routes needing an identity (all but the picker, `/me`, the concert page and `agenda.ics`) redirect to `…` (303) without a valid member cookie.
- htmx requests get `HX-Redirect` instead of a 303 (htmx would swap the target page into the fragment) and `HX-Refresh` with a 404 (htmx swaps no 4xx).
- Errors: 404 for unknown concert or a concert of another group; 500 is a generic page, details only in logs. Pages render into a buffer first, so a template error never sends a truncated 200.

## Edge cases

- **Duplicate first name** (same `name_key`): 422 picker offering « C'est moi : Marc » (likely a lost cookie) or an initial, field refilled with the name + space. Two concurrent creates: the unique index decides, the loser sees the same message.
- **Lost cookie** (new phone, cleared browser): the picker lists existing names; tapping one restores identity. No data is tied to the cookie itself.
- **Stale cookie** (member deleted, or from another group): treated as no identity → picker.
- **Secret rotation**: the old link 404s; the cookie path changes with the secret, so everyone re-picks their name from the list; data is untouched.
- **Concert without time**: card and page show the date only; `.ics` becomes an all-day event; sorting puts it before timed concerts the same day.
- **Past concerts**: a concert moves to « Passés » the day after its date (Europe/Paris), newest first.
- **Delete and undo**: delete sets `deleted_at`; the main page banner offers « Annuler » (POST restore). Deleted concerts are hidden everywhere; their pages, edits and status writes 404 (stale tab); statuses kept for undo. No purge in v1.
- **Deleted member**: their statuses go (cascade); their concerts stay, shown without « ajouté par ».
- **Ticket URL**: only `http`/`https`; rendered with `rel="noopener noreferrer"`.
- **Double submit**: status writes are idempotent (the form sends the target state, not a toggle); creating twice creates two concerts (accepted; delete fixes it).

## Roadmap

One slice per branch, merged with `--no-ff`. ✅ = shipped.

0. ✅ Skeleton: groups, `new-group`, `/g/<secret>` empty page, `/healthz`.
1. ✅ **Members and identity.**
   - First visit: existing first names as big « C'est moi » buttons, plus a « Nouveau prénom » field.
   - If the first name is taken, suggest adding an initial.
   - « Ce n'est pas moi » link on every page, for shared phones.
   - Subcommands: `concerts list-groups`; `concerts rotate-secret <group-id>`, printing the new link (after a rotation everyone picks their name again from the list); `concerts delete-member <group-id> <first-name>`.
2. ✅ **Concerts** (ticket cards in one chronological list, with the French date format; slice 4 adds month groups, « Passés » and the filter).
   - Fields: artist, date, optional time, venue, ticket link (http/https only), optional on-sale date.
   - Editable.
   - Soft delete (`deleted_at`) with an « Annuler » banner.
   - « ajouté par X » on each concert.
   - « Prévenir le groupe » button (`https://wa.me/?text=`), shown right after adding.
3. ✅ **Statuses**: interested / going / ticket bought. Tapping the active status again clears it.
   - One status block (names per status, my three buttons) shared by the card and the concert page; htmx swaps that block only (`hx-target="closest .statuses"`), and buttons keep stable ids so focus survives the swap.
   - The active button submits an empty status (clear), so every POST states the target state: a double tap is harmless.
4. ✅ **Single main screen.**
   - Chronological list grouped by month.
   - Each card shows first names per status and my three status buttons.
   - « Mes concerts » is a filter of this list, not another screen.
   - A concert moves to « Passés » (collapsible section) the day after its date.
   - Date format « sam. 14 mars · 20 h », Europe/Paris time zone.
   - « Today » is the Europe/Paris date of the injected clock; a concert dated today is still upcoming. The card is one partial for both sections; past cards get no status buttons (on the concert page either).
5. ✅ **Prefill from a ticketing URL** (Open Graph and JSON-LD `MusicEvent`).
   - Manual entry if fetching fails.
   - SSRF protection per ADR 0004.
   - Limit: one fetch in flight per group, 10 per minute.
   - `internal/prefill`: `Parse` (tokenizer, no tree: meta tags, `<title>`, every `ld+json` block) and an SSRF-safe client; tested on fictional fixtures shaped like Dice, Olympia, Zénith, Fnac and Accor Arena pages (most other French ticketing sites block plain clients: 401/403/429 or a challenge page, so manual entry is the common path).
   - JSON-LD: first `MusicEvent`, else any `*Event`/`Festival` (also in arrays, `@graph`, `ItemList`); artist = first performer, else event name; `startDate` with or without offset, converted to Paris; venue = location name; on-sale = earliest future `validFrom`/`availabilityStarts` in offers (a past one is a listing date). Fallback artist: og:title (`property` or `name`), twitter:title, `<title>`, cut at ` | `, ` - `, ` @ `…; never a date from a title.
   - Client: `Dialer.Control` allows only public unicast IPs (after `Unmap`; no private, loopback, link-local, CGNAT, NAT64, 6to4, Teredo, benchmark, reserved) on ports 80/443; no proxy; 3 redirects max, http(s) only; 8 s total; `text/html` only; 1 MB read. The user sees one generic message, never the dial error.
   - Values go through the form's own validation; invalid ones are dropped. A second fetch while one runs, beyond 10 per minute per group, or beyond 2 in flight in total, gets « Trop de lectures en cours » and the manual form.
   - `GET …/concerts/new?url=` (a separate GET form above the main one; htmx swaps `<main>`).
6. **« Ajouter à mon agenda »**: a single concert's `.ics`, no subscription, so the secret never reaches a calendar provider.
7. **Open Graph preview** of concert pages (in WhatsApp).

Also in v1: `make deploy` / `make rollback` (see [Operations](#operations)).

## Operations

- `make demo`: fresh `demo.db` with a group, 3 members, past and future concerts, then serves it.
- `make verify-ui` (`scripts/verify-ui.sh`): every screen of `make demo` at 390 px, light and dark: 0 axe-core violations, 0 vnu errors, `/g/<secret>` page weight (gzip -9 of HTML, CSS, JS, icons) < 40 kB. Pinned tools in the main checkout's `bin/tools/`.
- `make deploy DEPLOY_HOST=…` (no default): build, copy; the **installed (old) binary** writes the pre-deploy backup as the `concerts` user (the new one would migrate the live database first); previous binary kept; atomic swap (`mv`); restart; `/healthz` checked.
- `make rollback DEPLOY_HOST=…`: stop; move the current db, `-wal` and `-shm` aside; install the pre-deploy snapshot owned by `concerts`; restore the previous binary; start; `/healthz`. Writes since the deploy are lost (kept in the moved-aside files).
