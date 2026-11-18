# Design

Living document: each slice updates it. Each section stays under 40 lines; what doesn't guide the code doesn't go here.

## Need

A WhatsApp group of friends (5–30 people) shares concerts and sees who goes to what.

## Solution

- A web app opened through one secret link per group: `/g/<secret>`.
- No account: on the first visit, a member picks their first name, which the app remembers (cookie).
- A daily job imports the agendas of the venues a group follows, shown « À l'affiche » until someone answers (slice 12).
- Anyone holding the link belongs to the group; if the link leaks, the secret is rotated.
- Go, single binary under systemd on a shared personal server, run by a dedicated user from its home, behind Caddy over a Unix socket ([ADR 0008](decisions/0008-deployment-under-a-home-user.md)).
- Local repository, no CI: the pre-commit hook is the safeguard (what it runs: [CLAUDE.md](../CLAUDE.md#commands)). `make deploy` runs govulncheck first, so a vulnerability published since, or newly reached by a code change, still blocks a deploy.
- UI in French (`lang="fr"`); code, docs and commits in English.

## Out of scope

- WhatsApp bot ([ADR 0003](decisions/0003-no-whatsapp-bot.md)); accounts and passwords ([ADR 0002](decisions/0002-secret-link-identity.md)); push notifications.
- Calendar subscription feeds: a feed URL would hand the secret to a calendar provider; one-concert `.ics` downloads instead.
- Images (posters, avatars), comments, analytics.

## Secret link threat model

- **Secret**: 16 bytes from `crypto/rand` (128 bits), base64url without padding; guessing is infeasible.
- **Storage**: only the SHA-256 of the secret, under a unique index; lookup by hash. A database leak does not reveal working links.
- **Leaks through the browser**: `Referrer-Policy: no-referrer`; `X-Robots-Tag: noindex`; group pages send `Cache-Control: private, no-cache`.
- **Leaks through the server**: request logs record the matched route pattern (`GET /g/{secret}/concerts/{id}`, `GET /c/{token}`), never the raw path, so no spelling (`//g/…`, `/./g/…`) can leak the secret; no IP addresses. The reverse proxy writes no access log for the site. Its error log does record the path when the app cannot answer: not on a deploy (requests wait in the socket), but during a rollback, a restore, a crash loop or an install; it goes to the journal, readable by root and, depending on the distro, the `adm`, `wheel` or `systemd-journal` groups.
- **Leaks through features**: the `.ics` holds no page URL; preview images are static files, outside `/g/`; the prefill fetcher sends no cookie or `Referer`; the « Voir sur la carte » link sends only venue and address to OpenStreetMap; « Écouter » sends only the artist to YouTube; absolute URLs come from `CONCERTS_BASE_URL` (`http(s)://host` only, checked by every command that prints a link), never from `Host`.
- **Identity cookie**: `HttpOnly`, `SameSite=Lax`, `Path=/g/<secret>`, `Secure` when the base URL is https (`serve` logs a warning for a plain-http one outside localhost), `Max-Age` 400 days. It holds the member id, checked against the group on every request.
- **Public concert link** ([ADR 0009](decisions/0009-public-concert-link.md)): a concert page's address is `/c/<token>`, so what members copy or share (address bar, share sheet, « Partager ») never holds the secret.
  - Token: 12 random bytes in hex (96 bits), per concert, stored in clear (it must be rendered into links); a database leak exposes only public cards. It never rotates: deleting the concert is the only revocation.
  - Without a valid group cookie, the page is the public card: concert facts and a count of members going, no group name, first names or link into the group. Unknown and deleted tokens give the same plain 404, cookie or not; the response varies on `Cookie`.
  - Group cookie `g<group id>`: `<secret>.<member id>`, `Path=/c`, other attributes as the identity cookie; refreshed by every GET under `/g/<secret>`. It is a bearer credential like the link; the proxy logs no cookies. On `/c` it is parsed strictly, resolved like the link and must name the concert's group, else the card. The member page it unlocks holds the secret in its links and forms, never in its address.
  - Links sent before this slice carry the secret: rotate once after deploying it.
- **CSRF**: every state change is a POST behind `http.CrossOriginProtection` (lets GET through; trusts `Sec-Fetch-Site`, else compares `Origin` with `Host`, so the proxy must keep `Host`). The prefill GET triggers an outbound fetch but needs the secret and is rate-limited.
- **Abuse with a leaked link**: per-group caps (see Data model) bound what it can store. At the row cap a create purges old concerts, so a leaked link can flush the undo history and the past, as deleting everything then creating and deleting already could; likewise 20 edits flush a concert's edit history. Backups are the remedy. Server read, write and idle timeouts; request bodies capped at 64 kB.
- **SSRF** (prefill): the resolved IP of every connection, redirects included, must be public unicast (IPv6 within `2000::/3`, special-purpose ranges excluded) on port 80 or 443; no proxy, no keep-alive, 3 redirects, 8 s, 1 MB of `text/html` or XHTML; visible text is kept only for a host with a site rule (exact match on the final host, after redirects), at most 5,000 nodes; fetch errors are logged with the host only (a pasted group link must not reach the logs); one fetch in flight and 10 a minute per group, 2 in flight overall.
- **Venue agenda import** (slice 12): once a day, 1 to 8 listing pages per followed venue plus at most 40 event pages a run (times), only to hosts compiled into the binary, carrying no group data. Same dial checks as prefill; https only, redirects (3) and pagination links only to the venue's site hosts; 30 s, 2 MB refused rather than cut, the venue's media type; no cookie or `Referer`; at least 3 s between requests to one host (more where robots.txt asks), a 429 or 503 stops the venue for the run.
  - A hostile or compromised venue site can plant plausible concerts « À l'affiche »: validated as the form, at most 200 per venue and run and 400 upcoming per group; a ticket link only to the venue's site or a ticketing site listed for it, else the event page. Members see « annoncé par <Source> » and must answer before it reaches the list.
  - A group's followed venues say a little about it: only the operator sets them, from the CLI; members see them in « À l'affiche », nothing outside the group does.
- **Accepted risks**: anyone with the link can join and act as any first name; members are friends, so impersonation is social, not technical. Link previews in other messengers hand the link to the messaging provider (Telegram, Discord, Slack fetch it from their servers): for the group link, the picker and its first names, fixed by rotating the secret; for a concert link, only the public card. Anyone given a concert link sees the card for as long as the concert exists.

## User journeys

1. **Join**: tap the link in WhatsApp → picker screen → tap « C'est moi » on one's name, or type a new first name → main screen. Later visits skip the picker (cookie). Opening a concert link after the group link but without identity, the concert page shows « Choisis ton prénom pour répondre », and the picker returns to that concert (`next`).
2. **Add**: tap « Ajouter un concert » (fixed at the bottom on a phone, beside the filters on a computer) → paste a ticket URL, tap « Pré-remplir » → check or type the fields → « Enregistrer » → main screen with a banner « Concert ajouté » and « Partager » (the share sheet, else WhatsApp, with artist, date and the concert link; [ADR 0010](decisions/0010-share-sheet.md)).
3. **Respond**: on a card, tap « Ça m'intéresse » or « J'y vais »; tapping the active one clears it. The status block updates in place (htmx) or after a full reload (no JS).
4. **Plan**: filter « Mes concerts »; open a concert → « Écouter » searches the artist on YouTube (one « Écouter <artiste> » per artist of a line-up written with a spaced « + » or « / », the separator prefill joins the performers with); « Ajouter à mon agenda » downloads its `.ics`; « Billetterie » opens the ticket link.
5. **Fix**: open a concert → « Modifier » (banner with « Annuler ») or « Supprimer » (banner « Concert supprimé » with « Annuler la suppression »). A bad edit noticed later: « Voir l'historique » → « Revenir avant cette modification ».
6. **Shared phone**: « Ce n'est pas moi » in every page header → picker again.
7. **Invite a friend to one concert**: copy the concert page's address (or share it) → the friend sees the public card: the concert, « Billetterie », « Ajouter à mon agenda » and how many members go, nothing of the group. A member whose browser never opened the group link sees the same card, with a hint to open it once.
8. **Spot**: filter « À l'affiche » (concerts announced by the venues the group follows) → « Ça m'intéresse » or « J'y vais » on a row → it joins « Tous » for everyone.
9. **Operator**: [operations.md](operations.md).

## Screens

Mobile first (390 px), one column, max 40 rem wide. From 48 rem (computers, and tablets: a portrait iPad is exactly 48 rem) the column is 56 rem and the main screen, concert page and form use the width (below). Landmarks `<header>`, `<main>`, `<nav aria-label="Filtre">`. Header on every group page: group name, « <Prénom> » and a « Ce n'est pas moi » button (POST form, 44 px target). `<title>` starts with the banner or « Erreur » when there is one, so no-JS screen-reader users hear it after a redirect.

### Picker (`/g/<secret>` without identity)

```
┌──────────────────────────────┐
│ Les copains du concert       │
│ Qui es-tu ?                  │  h1
│ Pas de compte ni de mot de   │  hint: the link is the key
│ passe : ce lien suffit. Ne   │  (members had no other way
│ le partage pas hors du groupe│  to learn it)
│ ┌──────────────────────────┐ │
│ │      C'est moi : Léa     │ │  one big button per member,
│ └──────────────────────────┘ │  alphabetical
│ ┌──────────────────────────┐ │
│ │      C'est moi : Marc    │ │
│ └──────────────────────────┘ │
│ ── Pas dans la liste ? ───── │
│ Ton prénom [______________]  │
│ Marc est déjà dans la liste :│  on duplicate: aria-invalid,
│ si c'est toi, [C'est moi] ;  │  aria-describedby, autofocus,
│ sinon ajoute une initiale    │  field refilled "Marc "
│ (ex. « Marc D. »)            │
│ [        Rejoindre         ] │
└──────────────────────────────┘
```

### Main (`/g/<secret>`)

```
┌──────────────────────────────┐
│ Les copains   Léa · Ce n'est │
│ du concert          pas moi  │
│ ✓ Concert ajouté [Partager]  │  banner after add / delete
│ or [Annuler la suppression]  │
│ (Tous) (Mes concerts) (À l'… │  filter links, aria-current
│ MARS 2027                    │  h2 per month (h1 = group)
│ ┌────┬─────────────────────┐ │
│ │sam.┆ Fontaines D.C.      │ │  h3 linking to the concert;
│ │ 14 ┆ 20 h · Zénith Paris │ │  stub aria-hidden, so <time>
│ │mars┆ · 29–32 €           │ │  keeps the date visually hidden;
│ │    ┆                     │ │  price if set, nowrap with « · »;
│ │    ┆                     │ │  « Complet » tag if sold out
│ │    ┆ ✓ Y vont : Léa, Marc│ │  SVG icon + text per status,
│ │    ┆ ★ Intérêt : Sam     │ │  names alphabetical
│ │    ┆ [★ Ça m'intéresse]  │ │  role=group "Ma réponse",
│ │    ┆ [✓ J'y vais]        │ │  aria-pressed, stable ids,
│ │    ┆                     │ │  flex-wrap
│ └────┴─────────────────────┘ │
│ PASSÉS (3)                   │  h2, then a <details>: summary
│ ▸ Afficher les concerts…     │  « Afficher les concerts passés », names only
│ [   + Ajouter un concert   ] │  fixed bar, main screen only
└──────────────────────────────┘
```

- Status lines: « Y va : » / « Y vont : », « Intérêt : » (no gender, count-aware).
- An import (from a venue's agenda) is in the list (« Tous », « Mes concerts », « Passés ») only while someone answered it, computed in Go from the statuses already loaded; the « À l'affiche » filter shows only for a group following a venue or holding upcoming imports nobody answered (edited ones outlive an unfollow).
- « Mes concerts » = concerts where I have a status, computed in Go from the statuses already loaded; « Passés » is filtered too; clearing a status under the filter keeps the card until reload.
- Past cards show names only, no buttons, newest first, under an `h2` « Passés (N) » followed by a `<details>` (« Afficher les concerts passés »). Only the 30 newest are rendered (cards are built for those only), then « Afficher les N concerts passés » (`?past=all`, keeping `mine=1`) lands on the first card not shown so far. N is the total. All are rendered, `open`, with `?past=all` or when an added or restored concert is past. A late answer posted to a past concert is still accepted.
- Empty filter with only past answers: « Aucun concert à venir parmi les tiens. »
- Empty list: « Aucun concert pour l'instant. Ajoute le premier ! »; empty filter: « Tu n'as encore réagi à aucun concert. »
- Banners come from the URL but show only while true: `?added=<id>` and `?restored=<id>` if that concert's card is in the current view, `?deleted=<id>` (« Annuler la suppression ») if it is still deleted. An edit returns to the concert page with `?saved=<rev>`, going back to a revision with `?reverted=<rev>` (no `=<rev>` when nothing changed); their « Annuler » shows only while `<rev>` is the concert's newest revision.

### À l'affiche (`/g/<secret>?affiche=1`)

```
┌──────────────────────────────┐
│ (Tous) (Mes concerts) (À l'… │  « À l'affiche » aria-current
│ Concerts annoncés par les    │  explanation, after a visually
│ salles suivies. Réponds…     │  hidden h2 « À l'affiche »
│ Annoncé par (MC2) (La Source)│  nav, &salle=<key>, aria-current
│ DÉCEMBRE 2026                │  h2 per month
│ ┌──────────────────────────┐ │  one paper strip, dashed rows;
│ │DIM.  Pomme               │ │  the link covers the row;
│ │ 6    20 h · La Source    │ │  artist on the full width;
│ │DÉC.  COMPLET             │ │  « Complet » if sold out;
│ │      [★ Ça m'intéresse]  │ │  role=group « Ma réponse pour
│ │      [✓ J'y vais]        │ │  <artiste> », 44 px, aria-pressed
│ └──────────────────────────┘ │
└──────────────────────────────┘
```

- Upcoming imports nobody answered (`affiche=1` wins over `mine=1`), as compact rows rather than tickets (≈ 250 items), each with the cards' two answers on a second line under date and venue (exclusive; tapping the pressed one clears it). No past section: a past unanswered import shows nowhere.
- `<title>` « À l'affiche · … »; empty: « Rien à l'affiche pour l'instant. », or « Rien à l'affiche pour <Source>. » under a followed venue; any other `salle` is ignored.
- The answers post `back=affiche` and the view's `salle`; htmx swaps the whole group (stable ids `c<id>-interested`, `c<id>-going`, so focus stays) and, out of band, writes « <Artiste> : J'y vais. Ajouté à la liste du groupe. » (or « Ça m'intéresse »), « Retiré de la liste du groupe. », or « <Artiste> : réponse retirée, toujours dans la liste du groupe. » when another member answered, into a visually hidden `role=status` region present before the swap. The row stays until reload, as under « Mes concerts ». Without JS the answered row leaves « À l'affiche »: back to `?affiche=1` (with `salle` if followed) at the next row of that view, no anchor after the last.
- Venue names come from `web.Config.VenueName` (the compiled-in list); a key no longer listed shows as is.

### Concert (`/c/<token>`, with the group cookie)

```
┌──────────────────────────────┐
│ ← Tous les concerts          │
│┌─────┐                       │  head: the artist's pattern;
││ SAM │ Fontaines D.C.        │  date stub (year if not this
││ 14  │                       │  one, time if set) on a paper
││MARS │                       │  label, h1
││20 h │                       │
│└─────┘                       │
│- - - - - - - - - - - - - - - │  perforation, notches
│ (▶ Écouter)                  │  pills: one per artist, named
│                              │  when several; YouTube search
│ Zénith Paris                 │
│ Prix : 29–32 € COMPLET       │  if set; tag if sold out
│ Mise en vente : ven. 3 oct.  │  if set
│ 📅 Ajouter à mon agenda      │  .ics, a link like the map's
│ ajouté par Léa               │
│ modifié par Tom · Voir l'his…│  if edited
│ [Billetterie ↗] [Partager]   │  the only orange button, if
│                              │  set; share sheet (ADR 0010)
│ statuses + my 2 buttons      │  same block as the card
│ ▸ En savoir plus             │  description if set: <details>,
│                              │  folded; one <p> per line, full
│                              │  width from 48 rem
│ [Modifier]   [Supprimer]     │
└──────────────────────────────┘
```

An import shows « annoncé par <Source> » where « ajouté par » goes; while upcoming and unanswered, its back link is « ← À l'affiche » (`…?affiche=1#c<id>`); past and unanswered, it is in no view, so « ← Tous les concerts » without anchor.

Without identity: statuses shown, buttons and edit/delete replaced by « Choisis ton prénom pour répondre » (picker with `next`, accepted only as `/concerts/<id>`), then the page's filled accent button.

### Public card (`/c/<token>`, without the group cookie)

```
┌──────────────────────────────┐
│ Concerts                     │  no group name
│ head and pills               │  same as the member page
│ Zénith Paris                 │
│ Mise en vente : ven. 3 oct.  │  if set
│ 📅 Ajouter à mon agenda      │
│ 3 personnes du groupe y vont │  « J'y vais », if any
│ [Billetterie ↗]              │
│ Tu fais partie du groupe ? … │  open the group link once
└──────────────────────────────┘
```

### Concert form (`/concerts/new`, `/concerts/<id>/edit`)

```
┌──────────────────────────────┐
│ ← Annuler                    │
│ Ajouter un concert           │
│ Lien de la billetterie       │  own GET form (new only),
│ [https://…        ][Pré-remplir]│ hx-sync drop, role=status
│ (Impossible de lire cette    │  « Lecture de la page… »;
│  page : essaie le lien de la │  failure/limit: described-by
│  salle ou de l'organisateur, │
│  ou remplis à la main.)      │
│ ─────────────────────────────│  main POST form below
│ Artiste*      [____________] │  autofocus after prefill
│ Date*  [jj/mm/aaaa] Heure [--:--]│
│ Salle*        [____________] │
│ Adresse       [____________] │  autocomplete=off
│ Lien de billetterie [_______]│  the prefilled URL (main form)
│ Prix          [____________] │  free text (« Prix libre »)
│ [✓] Complet (plus de billets │  checkbox inside its label
│     en vente)                │  (44 px target)
│ Mise en vente [jj/mm/aaaa]   │
│ Description   [____________] │  textarea, 6 rows
│               [____________] │
│ [        Enregistrer        ]│
│ [Relire la billetterie]      │  edit only, after Enregistrer
└──────────────────────────────┘
```

After « Relire la billetterie », a differing field (edit form):

```
│ 2 champs diffèrent : Heure,  │  summary above the form
│ Complet. Rien n'est enregis… │
│ Heure         [21:00       ] │  autofocus on the first, described
│ La billetterie indique :     │  by the summary and its line
│ 20 h 30 [Utiliser]           │  posts use=time, swaps <main>
```

- The ticket link field of the main form carries the prefilled URL, so Enter in any field saves instead of prefilling.
- Errors: listed above the form and tied to each field (`aria-describedby`, `aria-invalid`); the first invalid field gets `autofocus`; fields come back as typed, rejected characters included.
- « Utiliser » buttons come before « Enregistrer » in the form, so a visually hidden « Enregistrer » (`tabindex=-1`, `aria-hidden`) opens the form while one is shown: Enter still saves.

## Visual direction

- **Motif: the ticket stub**, chosen over a gig-poster direction (phase 0 mockup, removed once applied in slice 2): denser, warmer, the stub naturally carries the date. Implemented in `internal/web/static/style.css`.
- Card = stub (weekday, day, month in `ui-monospace`) + dashed perforation + body; half-circle notches cut into the card.
- Stub pattern, festive without images but soft, since a list stacks many: faint fine rings, stripes, dots or scallops on a pale tint in one of 12 `oklch` hues, both picked from a hash of the artist (`stubStyle`), unmasked (fading them with `mask-image` cut scrolling from 51 to 36 fps on a Galaxy S10 in Firefox), desaturated on past concerts; the perforation stays visible over the tint; the concert page's ticket opens on a head of the same pattern, holding a larger stub and the title, cut by a perforation with notches; the title there sits on the pattern, which axe does not check, so the tint stays pale (dark in dark mode). Classes `hue-N pat-N`, since the CSP forbids `style=""`. The date sits on an opaque paper label: axe skips text over gradients, so the label is what keeps its contrast checked.
- Palette: background `#f4efe6` / paper `#fffdf8` (cards, date labels) / ink `#1f1a17` / brick accent `#b4361d`; dark: `#17140f` / `#24201a` / `#f1ebe1` / `#ff8a65`. AA contrast checked by axe.
- A filled accent button marks a screen's main action (« Billetterie », « Ajouter un concert », form submits); banner buttons (« Partager », « Annuler ») are secondary.
- Month headings: small monospace caps in the accent color.
- Pressed status = filled ink button + icon + `aria-pressed`; never color alone.
- `system-ui` fonts, fields ≥ 16 px, targets ≥ 44 px on touch screens (36 px answer buttons on cards and the concert page without any touch input; « À l'affiche » rows keep 44 px), `:focus-visible` outline, `prefers-reduced-motion` respected.
- « Ajouter un concert »: one link right after the filters in the DOM (so the keyboard reaches it before the cards). Below 48 rem a fixed bar on the main screen only, above the cards (each a stacking context), padded by `env(safe-area-inset-bottom)`; `scroll-padding-bottom` and list padding keep focused and last cards visible. From 48 rem a plain button on the right of the filters.
- From 48 rem: tickets in one row (info left, answer buttons right, status lines on one wrapping line), so about twice as many concerts per screen; concert page in two columns (facts | actions and statuses); concert form in pairs (Date | Heure, Salle | Adresse, Prix | Complet, then Mise en vente alone; Artiste, Lien and Description full width), « Enregistrer » right-aligned. One column, not a grid of cards: dates keep reading top to bottom.
- Icons: inline Lucide SVG (`star`, `check`, `play`, `share`, `calendar`), `aria-hidden`; never emoji.
- App icon and preview images: the `ticket` stub filled brick, perforation in paper colour (`icon.svg`; PNGs rendered from `scripts/*.svg` by `scripts/og-image.mjs`).

## Data model

Timestamps (`created_at`…) are UTC RFC 3339. Concert date and time are Europe/Paris wall-clock strings ([ADR 0005](decisions/0005-concert-wall-clock-time.md)). All tables `STRICT`, foreign keys on.

```sql
groups   (id PK, name, secret_hash BLOB UNIQUE, created_at)
members  (id PK, group_id → groups ON DELETE CASCADE, name, name_key,
          created_at, UNIQUE (group_id, name_key))
concerts (id PK, token UNIQUE, group_id → groups ON DELETE CASCADE,
          artist, date 'YYYY-MM-DD', time 'HH:MM' NULL, venue,
          address NULL, ticket_url NULL, on_sale_date 'YYYY-MM-DD' NULL,
          price NULL, description NULL, sold_out 0|1 DEFAULT 0,
          created_by → members ON DELETE SET NULL, created_at,
          deleted_at NULL, source NULL, source_id NULL)  -- import: venue key, its event id
          INDEX (group_id, date)
          UNIQUE (group_id, source, source_id) WHERE source IS NOT NULL
group_venues  (group_id → groups ON DELETE CASCADE, venue, PRIMARY KEY (group_id, venue)) WITHOUT ROWID
event_details (source, source_id, date, time NULL, PRIMARY KEY (source, source_id)) WITHOUT ROWID
venue_events  (source, source_id, artist, date, time NULL, venue, address NULL,
          ticket_url NULL, price NULL, sold_out, seen_at,
          PRIMARY KEY (source, source_id)) WITHOUT ROWID  -- what the agenda last said
statuses (concert_id → concerts ON DELETE CASCADE,
          member_id → members ON DELETE CASCADE,
          status CHECK IN ('interested','going'),
          PRIMARY KEY (concert_id, member_id)) WITHOUT ROWID
concert_revisions (id PK, concert_id → concerts ON DELETE CASCADE,
          artist, date, time NULL, venue, address NULL, ticket_url NULL,
          on_sale_date NULL, price NULL, description NULL, sold_out,
          -- values before the edit
          edited_by → members ON DELETE SET NULL, edited_at)
          INDEX (concert_id, id)
```

- `name` is stored with spaces trimmed and collapsed; `name_key` = `strings.ToLower(name)`, the one rule for "same name" and for sorting (byte order: « Élodie » sorts after « Zoé », accepted). No accent folding or Unicode normalisation (would need `x/text`): « Elodie » and « Élodie » are two names.
- Main page = 3 queries per group (live concerts with creator name and source; their statuses with member names; the followed venues), assembled in Go: no N+1.
- The identity cookie holds the member id; `inGroup` (web) resolves the secret, sets `Cache-Control` and ignores a cookie naming a member of another group or a deleted one (no identity).
- One accessor `Concert(groupID, id)` filters `group_id` and returns a deleted concert flagged `Deleted`: every route answers 404 for it but restore, which updates only a deleted row of the group, and the deleted banner. Status writes also check the member belongs to the group.
- Required: artist, date, venue. Optional: time, address, ticket link, on-sale date (a link is often not known yet), price, description, « Complet » (sold out, unticked by default). At most 100 members and 500 upcoming concerts per group (422 beyond; edits and restores included; past ones never lock a group) and 1000 rows with past and deleted ones, so the disk cannot fill: at 1000 rows, a create purges, oldest first and with its statuses and revisions: a past row that is deleted or an unanswered import; another deleted row, except an upcoming import's; a past row; last, an upcoming import's tombstone (only abuse gets there; that import may then come back). Never a live upcoming row. At most 20 revisions per concert, older ones pruned on each edit: worst case ≈ 1000 × 20 × 11 kB (a 2000-rune description is up to 8 kB) ≈ 220 MB per group, bounded since only the operator creates groups. « Today » (Paris) is passed to the store by web.
- An edit that changes something first copies the current values into a revision, in one transaction (`INSERT … SELECT` with NULL-safe `IS NOT`, so a no-op records nothing). Revision lookups join the concert, checking group and liveness in the same statement.
- Validation (`internal/validate`, also applied to prefilled values; `CleanName` for first names; `parseForm` only caps the body): text trimmed, 1–100 runes (name 1–30), valid UTF-8, no invisible characters (control, format, variation selectors, default-ignorable, braille blank), first names starting with a letter or digit; dates `YYYY-MM-DD`, time `HH:MM`; ticket URL absolute `http`/`https` with an ASCII host and no user part (`fnac.com@evil.example`), ≤ 2000 bytes; price free text ≤ 30 runes; description ≤ 2000 runes, line feeds kept (blank-line runs folded into one).
- Prefill of price and description: schema.org `offers` give a euro range (« 29–32 € », currency inherited, absent = euros, 0 and ≥ 10,000 skipped); every JSON-LD text gets HTML entities decoded (WordPress encodes them); the event's `description` gets other blanks (tabs, braille blank) turned into spaces and invisible characters stripped (the form would reject the whole text; a site rule's description too) and words glued by stripped markup separated (« SNIPERGroupe », « énergie.KENZ »); one longer than 2000 runes is cut at a line or sentence end, else between words, in its second half (else at the limit) with « … ». Open Graph descriptions are never used (Retour de Scène serves its menu there); Retour de Scène's rule reads the price and artist blocks between « Billetterie » and « Lieu ».
- Writes use `_txlock=immediate` and `busy_timeout`; status writes are upserts (idempotent). `synchronous(NORMAL)` may lose the last commits on power loss: accepted.

## Routes

All under `/g/{secret}` (written `…`) but the first two and the last two; unknown secret → 404. Every POST: `CrossOriginProtection`, then 303 (Post/Redirect/Get) — htmx requests get a fragment instead where noted.

| Method | Path | Role | Response |
|---|---|---|---|
| GET | `/healthz` | DB ping | text |
| GET | `/static/{name}` | hashed static file | file |
| GET | `…` | picker (no identity, optional `next`; the group link's preview) or main list; `?mine=1` filter, `?affiche=1` (`&salle=<key>`) unanswered imports, `?past=all` every past concert, `?added=<id>` / `?restored=<id>` / `?deleted=<id>` banner | page |
| POST | `…/me` | pick `member_id` or create `name`; sets cookie | 303 `next` (a concert path) or `…`; 422 picker |
| POST | `…/logout` | « Ce n'est pas moi »: clears cookie | 303 `…` |
| GET | `…/concerts/new` | add form; `?url=` pre-fills from the ticket page | page; htmx swaps the form (`hx-select`) |
| POST | `…/concerts` | create; `anyway=1` skips the duplicate check | 303 `…?added=<id>`; 422 form (invalid or duplicate); 422 if the group is full |
| GET | `…/concerts/{id}` | old and internal concert URL; sets the group cookie | 303 `/c/<token>`, query kept |
| GET | `…/concerts/{id}/edit` | edit form | page |
| POST | `…/concerts/{id}/refresh` | « Relire la billetterie »: fills the posted form's empty fields and offers the differing ones from the venue's agenda (an import) or the ticket link; `use=<field>` takes the offered value posted back as `source_<field>`, reading nothing; saves nothing | form page (200, as htmx swaps no 4xx) |
| POST | `…/concerts/{id}` | update, recording a revision | 303 `…/concerts/{id}?saved=<rev>`; 422 form; 422 if the group is full |
| GET | `…/concerts/{id}/history` | edits, newest first, with the fields each changed | page |
| POST | `…/concerts/{id}/revisions/{rev}/revert` | go back to before that edit (an edit itself) | 303 `…/concerts/{id}?reverted=<rev>`; 422 if a value is no longer valid or the group is full |
| POST | `…/concerts/{id}/delete` | soft delete | 303 `…?deleted=<id>` |
| POST | `…/concerts/{id}/restore` | undo delete | 303 `…?restored=<id>#c<id>` (`…?affiche=1&restored=<id>#c<id>` for an upcoming import nobody answered, plain `…` for a past one); 422 if the group is full |
| POST | `…/concerts/{id}/status` | set `status` (empty clears); `back` = `list`, `mine`, `concert` or `affiche` | htmx: status block fragment (`affiche`: the row's answers and the status region; « Retourné à l'affiche. » in it when clearing the last answer of an upcoming import from the list); else 303 to `…#c<id>`, `…?mine=1#c<id>`, `…?affiche=1[&salle=<key>]#c<next row>` for `affiche`, `…?affiche=1#c<id>` (on clearing the last answer of an upcoming import from the list; `…` or `…?mine=1` for a past one) or the concert page |
| GET | `/c/{token}` | concert page with the group cookie (`?saved`, `?reverted` banners), else public card | page; 404 plain |
| GET | `/c/{token}/agenda.ics` | one-event calendar, public | `text/calendar` |

- Every GET under `…` refreshes the group cookie (threat model, public concert link).
- Routes needing an identity (all but the picker, `/me`, logout, the old concert URL, the concert page and history) redirect to `…` (303) without a valid member cookie.
- htmx requests get `HX-Redirect` instead of a 303 (htmx would swap the target page into the fragment) and `HX-Refresh` with a 404 (htmx swaps no 4xx).
- Errors: 404 for unknown concert or a concert of another group: inside a group, a page « Concert introuvable » with the group header and « ← Tous les concerts » (htmx gets `HX-Refresh`, then that page); an unknown secret (rotated, truncated or never issued) gets the same plain « Page introuvable. », which reveals nothing, plus a hint to ask whoever runs the group (not the members: a rotation cuts off the leaker); other unmatched paths get Go's default 404; 500 is a generic page, details only in logs. Pages render into a buffer first, so a template error never sends a truncated 200.

## Edge cases

- **Duplicate first name** (same `name_key`): 422 picker offering « C'est moi : Marc » (likely a lost cookie) or an initial, field refilled with the name + space. Two concurrent creates: the unique index decides, the loser sees the same message.
- **Lost cookie** (new phone, cleared browser): the picker lists existing names; tapping one restores identity. No data is tied to the cookie itself.
- **Stale cookie** (member deleted, or from another group): treated as no identity → picker.
- **Secret rotation**: the old link 404s; the cookie path changes with the secret, so everyone re-picks their name from the list; data is untouched. Concert links keep working: the old group cookie no longer resolves, so they show the public card until the new link is opened.
- **Concert without time**: card and page show the date only; `.ics` becomes an all-day event; sorting puts it before timed concerts the same day.
- **Past concerts**: a concert moves to « Passés » the day after its date (Europe/Paris), newest first.
- **Delete and undo**: delete sets `deleted_at`; the main page banner offers « Annuler la suppression » (POST restore). Deleted concerts are hidden everywhere; their pages, edits and status writes 404 (stale tab); statuses kept for undo, until the row cap purges them (Data model).
- **Deleted member**: their statuses go (cascade); their concerts stay, shown without « ajouté par ».
- **Ticket URL**: only `http`/`https`; rendered with `rel="noopener noreferrer"`.
- **Edit history**: going back to a revision is an ordinary edit with its values, so it is recorded and can be undone; they go through today's validation first (a value accepted then but refused now → 422, nothing written). Concurrent edits: last write wins, each recorded. A deleted member's edits stay, as « Ancien membre ». The history page is readable without identity (like the concert page), its buttons are not.
- **Double submit**: status writes are idempotent (the form sends the target state, not a toggle); a second create asks first, as for any duplicate concert.
- **Duplicate concert** (add only): a live concert of the group with the same date and artist (case ignored, as first names) re-renders the form with 422, fields kept: « <artist>, <date>, est déjà dans la liste (<venue>, ajouté par X). Voir ce concert », focus on that link; « Ajouter quand même » (`anyway=1`) comes after « Enregistrer », which stays the button Enter submits. Against an upcoming import nobody answered: « <artist>, <date>, est à l'affiche (annoncé par <Source>), pas encore dans la liste. Réponds-y pour l'ajouter. », without « Ajouter quand même »; a past one is in no view, so it gets the plain warning. A warning, not a constraint: an artist may play twice a day, and two concurrent adds both pass. An edit is not checked.
- **Venue imports**: a deleted import stays a tombstone and never comes back; a member's edit (even reverted) makes the row the members', no longer updated by the sync, whose own updates leave no revision (the history may say « Aucune modification. » while the venue moved the date). An event gone from a later listing (cancelled) stays until a member deletes it. Answering moves an import to « Tous »; clearing its last answer sends it back « À l'affiche » (htmx: « Retourné à l'affiche. » in its status block); editing does not. A past unanswered import shows nowhere. One concert spelled differently by two sources gives two rows, deleted by hand.

## Slices

One slice per branch, merged with `--no-ff`. 0–11 shipped as v1.

0. Skeleton: groups, `new-group`, `/g/<secret>`, `/healthz`.
1. **Members and identity**: first names as « C'est moi » buttons plus a « Ton prénom » field; a taken name offers « C'est moi » or an initial; « Ce n'est pas moi » on every page; `list-groups`, `rotate-secret <group-id>` (prints the new link; everyone picks their name again), `delete-member <group-id> <first-name>`, `rename-member <group-id> <first-name> <new-first-name>`.
2. **Concerts**: artist, date, optional time, venue, optional address (concert page only, with « Voir sur la carte »: an OpenStreetMap search for « venue, address »), optional http(s) ticket link, optional on-sale date; editable; soft delete with « Annuler la suppression »; « ajouté par X » on the concert page; « Prévenir le groupe » (`wa.me`, [ADR 0007](decisions/0007-plain-whatsapp-link.md)) right after adding, since slice 11 « Partager » ([ADR 0010](decisions/0010-share-sheet.md)).
3. **Statuses**: interested / going; tapping the active one clears it. A third one, « J'ai mon billet », was merged into « J'y vais » after v1 (migration 0007): members rarely came back to set it once they had bought.
   - One status block (names per status, my buttons) shared by card and concert page; htmx swaps that block only, and buttons keep stable ids so focus survives.
   - The active button submits an empty status, so every POST states the target state: a double tap is harmless.
4. **Single main screen**: upcoming concerts grouped by month; « Mes concerts » filters the same list; « Passés » (collapsed, newest first, no buttons) from the day after the date; « sam. 14 mars · 20 h », Europe/Paris. « Today » is the Paris date of the injected clock.
5. **Prefill from a ticketing URL** (`internal/prefill`), manual entry on failure.
   - `parse` uses the tokenizer (meta tags, `<title>`, every `ld+json` block, visible text for site rules); fixtures shaped like Dice, Olympia, Zénith, Fnac, Accor Arena, La Belle Électrique, L'ilyade, Retour de Scène and La Source pages. Most other French ticketing sites block plain clients (401/403/429 or a challenge), so manual entry is the common path.
   - Shotgun answers 429 with a Vercel Security Checkpoint (JS challenge) on every URL, oEmbed and API host included; its only API is organiser-only with a key, and `robots.txt` allowing all does not authorise passing a challenge. No browser User-Agent, challenge solving or proxy: manual entry.
   - JSON-LD: first `MusicEvent`, else any `*Event`/`Festival`, wherever nested; artist = first performer, else event name; `startDate` converted to Paris; venue = location name; address = `location.address`, a string or a PostalAddress as « street, postcode city » (country left out, city not repeated if the street holds it, dropped if equal to the venue); on-sale = earliest `validFrom`/`availabilityStarts` from today on. Fallback artist: og:title (`property` or `name`), twitter:title, `<title>`, cut at ` | `, ` - `, ` @ `…; never a date from a title.
   - Site rules (`sites.go`) for pages without an Event in JSON-LD, chosen by the final host (lowercased, trailing dot and `www.` trimmed, exact key). They run only when JSON-LD gave no date (no mixing of two events) and fill only empty fields. Visible text = text nodes outside `script`, `style`, `noscript`, `svg`, `template`…, with the class of the innermost open element (cleared by any end tag) and a heading flag.
     - `lilyade.fr`: Elementor items `elementor-post-info__item--type-custom` before the heading « À découvrir également » (related events): first French date, then, up to the next date (the desktop copy), time, venue (trailing comma trimmed), city, the last two only if a time was read. Address = city: the footer's is the hall's. If that heading is reworded and the main date is missing, a related event's date would be read: accepted, the user checks the form.
     - `lasource-fontaine.fr`: « Date de début » item (« 12 mai 2027 20 h 30 min » → date and time; « Date de fin » ignored); venue « La Source » (the « Salle » item is the room); address from the footer (`ls-ico-pin`), a comma put before its postcode. No start date: nothing, so a non-event page gets no venue.
     - `retourdescene.net`: « Lieu » block (venue; street, trailing comma trimmed; city line found by its postcode, « City, Region NNNNN France » → « NNNNN City ») and « Horaire » block (date, then the first time of the next line: « 20h > 01h (portes 20h…) » → 20:00).
     - Artist from the title as served, all caps included (Retour de Scène): converting would mangle acronyms.
   - Client and limits: see the threat model (SSRF). Fetched values lose invisible characters (other blanks become spaces), then go through the form's validation; invalid ones are dropped. The user sees one generic message, never the error.
   - `GET …/concerts/new?url=`: a separate GET form above the main one (Enter in the main form saves); htmx swaps `<main>` and pushes the URL. The outcome is tied to the focused field (`aria-describedby`); « Lecture de la page… » sits in a live region present before the request.
   - « Relire la billetterie » (edit form): a button inside the main form, after « Enregistrer » so Enter saves; it posts the fields as typed (`formaction`, `formnovalidate`) and fills only the empty ones, nothing saved until « Enregistrer ». Only the link is checked then; focus goes to the first filled field, else stays on the button (« Rien de nouveau sur la billetterie. »).
     - Since slice 12 it also offers, under each filled field the source gives another value for, « La billetterie indique : <value> » and « Utiliser » (a submit button posting `use=<field>` to the same route, the form's values and the offered ones as `source_<field>` hidden fields: nothing is read again). Never the description, never an empty value (a field is never cleared), « complet / pas complet » for the checkbox only when the source tells. A summary above the form (« N champs diffèrent : … Rien n'est enregistré avant « Enregistrer ». ») takes the focus's description: autofocus on the first differing field, described by the summary and its line; after « Utiliser », on the replaced field.
     - Source: for an import whose venue event the sync kept (`venue_events`), that row, so no network and venues whose event pages prefill can't read work too; else the ticket link's page, as before (limiter, errors).
6. **« Ajouter à mon agenda »**: one concert's `.ics`, no feed, so the secret never reaches a calendar provider.
   - Hand-written RFC 5545: no METHOD (Outlook would see an invitation); UID `concert-<group>-<id>` (no host, RFC 7986); timed concerts in UTC with a 3 h end (no VTIMEZONE), date-only ones `VALUE=DATE` with an exclusive next-day end; SUMMARY, LOCATION (« venue, address »), DESCRIPTION « Billets : <link> » only.
   - TEXT escaping (a field cannot start a property line), folding at 75 octets on rune boundaries, CRLF; golden files. `attachment; filename="concert.ics"`, `private, no-store`. An edited concert re-imported on iOS is not updated (known limit).
7. **Link previews** (WhatsApp, Signal, iMessage, Telegram…): the previewer (the sender's phone for WhatsApp, Signal and iMessage, the provider's servers for the others) never has the identity cookie, so it gets the picker for the group link and, since slice 10, the public card for a concert link, never the list.
   - Concert page: `og:title` (artist), `og:description` (date · venue); picker: `og:title` (group name) and a generic `og:description`, no first name. Both: `og:url` (from `CONCERTS_BASE_URL`), `og:site_name`, `og:type`, `twitter:card` (Discord's large layout), shared in partial `ogCommon`.
   - One static `og:image` for every page (1200×630, no text, stub in the central square that WhatsApp's thumbnail keeps) and a 180×180 `apple-touch-icon` (iMessage and Signal fall back to it; no SVG support). A per-concert image would need a font and a new dependency to repeat `og:title`.
   - Previewers need the https base URL and must not be blocked: never add a disallow-all `robots.txt`. A preview travels with the message, so a change never reaches links already sent.
8. **Desktop layout**: the 48 rem layout of « Visual direction »; `verify-ui` also runs at 768 px (overflow, axe, buttons beside the info).
9. **Edit history**: every edit is traceable and reversible, without accounts or roles (ADR 0002 kept).
   - « modifié par X · Voir l'historique » on the concert page; the banner after an edit offers « Annuler ».
   - History page: one entry per edit (« Tom · mar. 3 mars à 18 h 05 », Paris time), only the fields it changed as « Salle : V → W » (reads aloud, no `<mark>`), and « Revenir avant cette modification » (also undoes newer edits; « Rétablir » names undelete); « Aucune modification. » if never edited.
   - Out of scope: detecting concurrent edits, history of statuses and deletes, history on the list's cards.
10. **Public concert link** ([ADR 0009](decisions/0009-public-concert-link.md)): members share what they see, so the concert page moved to a secret-free `/c/<token>` (see the threat model) instead of adding a share button.
   - Old concert URLs, the picker's `next` and post-edit redirects reach it through a 303. Cards, `og:url`, `<link rel="canonical">` (Chrome on Android shares it), « Prévenir le groupe » and the history page link to it directly; the `.ics` is served under it (`/c/<token>/agenda.ics`), and its old address under `/g/` is gone (404).
   - Out of scope: secret-free list and forms (their addresses still hold the secret), revoking one concert's link.
11. **Lighter concert page**: one orange button (« Billetterie »); the date and title move into the patterned head; « Écouter » becomes one pill per artist, « Ajouter à mon agenda » a link among the facts, « Prévenir le groupe », here and in the « Concert ajouté » banner, a secondary « Partager » opening the share sheet ([ADR 0010](decisions/0010-share-sheet.md)).
12. **Venue agenda import** ([ADR 0011](decisions/0011-venue-imports.md)): the daily `concerts sync` (systemd timer) copies the upcoming concerts of the venues a group follows into its concerts; screens, routes and threat model above.
   - `internal/agenda`: the compiled-in venues (MC2, La Belle Électrique, L'ilyade, L'Ampérage, La Source, Retour de Scène: listing URLs, site and ticketing hosts, crawl delay, robots.txt noted), one parser per venue walking an `html.Parse` tree or MC2's JSON (fixtures in `testdata/`), the fetcher and `Sync`. Music events only (club nights count); every event needs the venue's stable id (`source_id`, ≤ 256 bytes).
   - Cleaning: status prefixes and suffixes stripped from titles (« COMPLET • », « REPORTÉ • », « (COMPLET) », « \ COMPLET / »), « Complet » sets `sold_out`, the price left as listed; a date without year is its next occurrence, skipped if more than 10 months ahead (a past item still listed); then `validate.Concert`, as the form.
   - Sync: followed venues only, halls before promoters (Retour de Scène lists L'ilyade's concerts: the hall's copy wins), each read within 2 min (6 × 2 min stay under the unit's 15 min) then applied at once, so a slow or failing venue costs the others nothing; ≤ 200 events per venue and run; `store.ApplyImports`, one transaction per group and venue (`created_by` NULL, `source` and `source_id` set), applies nothing if the group stopped following the venue since the run began. Before that, each venue's events read (validated, as imported) are kept once for every group in `venue_events`, one transaction a run, pruned once past or unseen for 30 days (not on missing one run): what « Relire la billetterie » compares an edited import with. Not `event_details`, whose date is a cache key.
   - Per event: inserted unless the group has the same bill that day from elsewhere, live or deleted (artist key equal, or followed by ` + `, ` • `, ` / `, `, ` or ` & `, either way round); a row the import owns (live, never edited) updated in place; imports stop at 400 upcoming concerts, 100 kept for members.
   - Times missing from a listing (La Source, Retour de Scène): each event's page read once ever through `prefill`, kept in `event_details`, pruned of past dates; ≤ 40 pages a run. A page gone (404, 410) is kept without time; one unread or where `prefill` finds no date is read again next run, and the venue fails when none of the pages it read had a date.
   - Fails (non-zero exit) when a venue failed or listed no upcoming concert, its parser's breakage sign. Logs: venue key, host, group id, counts; never a URL or a concert.
   - CLI: `list-venues`, `follow-venue`, `unfollow-venue` (deletes the venue's rows nobody answered or edited), `sync`.
   - Out of scope: « Nouveau » badges, on-sale reminders, members choosing venues, following artists, aggregator APIs (ADR 0011), arenas behind bot protection (Summum, Zénith: prefill by hand), Drak-Art (Facebook only), detecting cancellations, a « mis à jour par la salle » history.

Also in v1: `make install`, `make deploy`, `make rollback` ([operations.md](operations.md)).

## Later

Off-server backups and failure alerts, nothing set up yet. Leads:
- First check whether the server is already backed up (host snapshots, a whole-server backup) and covers `/home/<name>/backups`, not the live database.
- Best fit: another machine pulls nightly with `rsync` and a key limited to `rrsync -ro /home/<name>/backups`; the server holds no credential, so a compromise cannot delete the copies. `make install` rewrites `authorized_keys` with the deploy key only, so it must learn to keep that second key first. Retention on the puller: about 14 daily, 8 weekly, 12 monthly.
- Without an always-on machine: restic to BorgBase or a storage box in append-only mode, pruned from a trusted machine.
- A periodic restore test (`PRAGMA integrity_check`, `list-groups`) and an alert when the newest snapshot is over 36 h old.

A « Je cherche un billet » status, if the group asks for it: it flags the exception (a sold-out concert, a resale), which members have a reason to set, unlike the dropped « J'ai mon billet ».

A window on « À l'affiche » (the next months, then « Afficher la suite »), if the compact list proves too long.

Carpooling, if the group asks for it: once « J'y vais » is set, an optional « Je propose / Je cherche un covoiturage » on the concert page only (`statuses.carpool CHECK IN ('offer','seek') NULL`), shown as a mark by the first name; no seats or departure city, the arranging stays on WhatsApp.
