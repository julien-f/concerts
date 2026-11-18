# Venue agenda import: spec

Branch `venue-import`. Deleted before the merge; what lasts moves into `docs/design.md` (slice 12), `docs/operations.md` and ADR 0011. Reviewed once (UX, data, security/ops); their fixes are folded in.

## Need

Members add a concert only after seeing it elsewhere. Concerts announced at the halls the group goes to are missed, or noticed late (after the on-sale date). The list must still mean « someone here cares about this one ».

## Decision (agreed with the owner)

- A daily job imports the upcoming concerts of the venues a group follows **into that group's concerts**, as ordinary rows: members edit, delete and answer them like any other.
- The main list (« Tous », « Mes concerts », « Passés ») shows an imported concert only while at least one member has answered it. Concerts added by hand always show, as today. Past unanswered imports show nowhere.
- A third filter, « À l'affiche », lists the upcoming imported concerts nobody answered yet; answering one moves it to « Tous ». When its last answer is cleared, it goes back to « À l'affiche ». Under htmx the card stays until reload, as clearing a status under « Mes concerts » does.
- One copy per group: an edit or delete in one group never reaches another, and a leaked link reaches one group's data only.
- Venues come from a list compiled into the binary; the operator links a group to a venue from the CLI. Members never enter a URL the server would fetch again and again.

## First venues

MC2, La Belle Électrique, L'ilyade, Retour de Scène (organiser), La Source (Fontaine), L'Ampérage, La Bifurk, Drak-Art. Per-venue source and extraction: « Sources ». Multi-discipline venues keep only music events when the source says so.

## Packages

- `internal/validate`: the concert form's cleaning and validation (`parseConcert` and helpers, moved out of `internal/web`) on a `store.Concert`, and Paris `today`. `web` wraps it for form values; the sync calls it directly. Moved first, in its own commit, behaviour unchanged.
- `internal/agenda`: the venue list (key, name, listing URLs, allowed hosts, crawl delay, robots.txt note), one parser per venue with fixtures in `testdata/`, the fetcher, and `Sync(ctx, st, client, venues, now, sleep, log)`. Everything is injected, so tests use an `httptest` server, a fixed clock and a no-op sleep; only `cmd` passes `time.Now`, the real client and the compiled-in list.
- `internal/store`: `venues.go` (follow, unfollow, followers) and `imports.go` (`ApplyImports(group, source, events, today)`: one transaction per group and venue, purge and headroom included).

## Data model

Migration `0010_concert_source.sql`:

```sql
ALTER TABLE concerts ADD COLUMN source TEXT;     -- venue key, NULL when added by a member
ALTER TABLE concerts ADD COLUMN source_id TEXT;  -- the venue's stable id for the event
CREATE UNIQUE INDEX concerts_source ON concerts (group_id, source, source_id) WHERE source IS NOT NULL;
CREATE TABLE group_venues (group_id INTEGER NOT NULL REFERENCES groups ON DELETE CASCADE,
  venue TEXT NOT NULL, PRIMARY KEY (group_id, venue)) STRICT, WITHOUT ROWID;
CREATE TABLE event_details (source TEXT NOT NULL, source_id TEXT NOT NULL,
  date TEXT NOT NULL, time TEXT, PRIMARY KEY (source, source_id)) STRICT, WITHOUT ROWID;
```

- Imported rows: `created_by` NULL, `source` and `source_id` both set (the store refuses one without the other). Shown as « annoncé par <Source> » where « ajouté par » goes; a key no longer compiled in shows the key.
- `source_id`: a stable id from the venue (event URL, iCal `UID`, JSON id), ≤ 256 bytes, cleaned. Every venue must have one: a fallback built from date or title would change with them and duplicate the concert.
- Visibility costs no query: `source` joins `concertSelect`, and `visible = source == "" || len(statuses) > 0` uses the statuses already loaded.

## Sync (`concerts sync`)

- Fetches each followed venue once (venues no group follows are not fetched), all before any transaction, then applies its events to every group following it.
- Per event dated today (Paris) or later, at most 200 per venue and run:
  - Title normalised by the venue parser: « COMPLET • », « ANNULÉ », « REPORTÉ » prefixes stripped (« Complet » may go into the price).
  - New `(group, source, source_id)`: inserted (`ON CONFLICT (group_id, source, source_id) WHERE source IS NOT NULL DO NOTHING`), unless the group already has a concert that day, live or deleted, whose artist key equals the event's or starts with it followed by a separator (` + `, ` • `, ` / `, `, `, ` & `), either way round (`store.ConcertsOn`), rows of the same source excepted (an early and a late show; the unique index covers those). That covers a concert added by hand, one listed by two sources (Retour de Scène lists L'ilyade's) and one a member deleted. Venues sync before promoters, so the venue's copy wins within a run; a promoter announcing first keeps its copy for good (accepted). The form's duplicate check is unchanged.
  - Existing row the sync owns: updated in place in one statement, `WHERE … deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM concert_revisions r WHERE r.concert_id = concerts.id)`, without a revision. A member's edit (even reverted) makes the row the members'. A changed date is logged.
  - Deleted row: left as a tombstone, so it never comes back.
- An event failing validation is skipped (counted in the log); its row, if any, is left as is. Events dated before today are skipped, rows untouched. Events missing from a later listing are left as they are: a cancelled concert stays until a member deletes it.
- Caps:
  - Inserts and updates moving a row into the future stop at `MaxConcerts - 100` upcoming concerts in the group, keeping 100 for members (counted once per transaction; `_txlock=immediate` serialises with the web process). `commitWithin` stays the hard 500.
  - Inserts go through `purgeIfFull`, whose order becomes: past rows that are deleted or unanswered imports; other deleted rows except upcoming imports; past rows; upcoming import tombstones, oldest first (only abuse reaches this, and one may then come back).
- Ticket link: kept only if its host is in the venue's allowed hosts (its site and named ticketing sites), else the venue's event page, so a compromised venue site cannot point members anywhere.
- Unfollowing hard-deletes the venue's live rows nobody answered or edited, past ones included; tombstones and the others stay.
- Exit status non-zero when any venue failed or gave 0 upcoming events, so the unit shows in `systemctl --failed`. A venue always has upcoming events: 0 means its parser broke. Logs name the venue key, host and group id only, never a URL or a row.
- SIGTERM cancels the context; transactions stay short (one group and venue, milliseconds).

## Fetching

- `prefill`'s dialer and `dialControl` kept (public addresses, ports 80/443): redirects can go anywhere and a venue domain can lapse. Redirects: https only, to the venue's allowed hosts, 3 at most.
- Own limits: 30 s per request, 4 MB body (read limit+1, refuse over it rather than parse a cut page), `text/html`, `text/calendar` or `application/json` per venue, a few requests per venue, sequential, the venue's crawl delay between requests to one host (injected sleep). A 429 or 503 stops that venue for the run.
- User-Agent `concerts (agenda des salles, une fois par jour)`; no cookies, no Referer. robots.txt is checked by hand when a venue joins the list and noted beside it; no runtime parser.

## Running it

- `<name>-sync.service`: oneshot, sandbox copied from `concerts.service` (network: no `PrivateNetwork`, `RestrictAddressFamilies=AF_INET AF_INET6`, no `IPAddressDeny`, per ADR 0008), plus `TimeoutStartSec=15min`, `MemoryMax=256M`, `TasksMax=64`, `GOMEMLIMIT=160MiB`. `<name>-sync.timer`: daily, `RandomizedDelaySec`, `Persistent=true`. Not inside `serve`: the service is socket-activated and may not run for days.
- `lib.sh` (`UNITS`), `install.sh` (units, enable, sudoers), `uninstall.sh` (one `disable` per unit, so a missing one does not leave the others) and `admin`: `stop_all`/`start_all` and the trap message include the sync timer and service (a sync left running during a restore would delete the new `-wal` on close); `logs` includes the sync unit; `admin` refuses with « rerun make install » when the sync timer is missing, so a deploy before the install fails clearly instead of during a rollback.
- CLI: `list-venues` (key, name, groups following), `follow-venue <group-id> <venue-key>`, `unfollow-venue <group-id> <venue-key>`, `sync`. A manual `admin sync` may overlap the timer's run: the unique index and immediate transactions keep it correct.

## Screens

- Filter nav « Tous » · « Mes concerts » · « À l'affiche » (`?affiche=1`, which wins over `mine=1`); « À l'affiche » only for a group following a venue; one `aria-current="page"`.
- « À l'affiche »:
  - First line: « Concerts annoncés par les salles suivies. Réponds pour les ajouter à la liste du groupe. »; `<title>` « À l'affiche · … »; an `h2` before the month headings.
  - Compact rows, not full tickets (≈ 250 items): date · artist · venue linking to the concert page, and one « Ça m'intéresse » toggle (44 px, `aria-pressed`, labelled with the artist). « J'y vais » from the concert page.
  - Second nav labelled « Annoncé par » (visible and `aria-label`): one link per followed venue (`&salle=<key>`), `aria-current`.
  - A « Complet » mark in the row when the price says so; the parser writes « 29 € · Complet » when the venue gives a price too (30-rune cap).
  - Empty: « Rien à l'affiche pour l'instant. », or « Rien à l'affiche pour <Source>. » under a venue filter.
  - The htmx answer returns a compact-toggle fragment (stable id `c<id>-interested`, so focus stays), not the full status block; a `role=status` region present before the swap says « Ajouté à la liste du groupe » or « Retiré de la liste du groupe ».
- Status answers carry `back=affiche` (no-JS: back to `?affiche=1#c<id>`). Clearing the last answer of an import under « Tous » or « Mes concerts » without JS returns to « À l'affiche » at that concert.
- Concert page of an unanswered import: back link « ← À l'affiche » (`?affiche=1#c<id>`); « annoncé par <Source> »; « Billetterie » points at the venue's event page.
- Banners: shown only when their card is in the current view. Restoring an unanswered import redirects to `?affiche=1&restored=<id>#c<id>`.
- Add form duplicate warning when the match is an unanswered import: « <artist>, <date>, est à l'affiche (annoncé par <Source>), pas encore dans la liste. Réponds-y pour l'ajouter. » with « Voir ce concert », and no « Ajouter quand même ».
- Editing an unanswered import does not show it in « Tous »: only an answer does.
- The sync's changes leave no history: the history page may say « Aucune modification. » while the venue moved the date. Documented, accepted.
- The public card is unchanged.

## Security and privacy deltas (threat model)

- New outbound traffic: about one request per venue per day, to hosts compiled into the binary; no group data in it.
- A hostile or compromised venue site can plant plausible concerts in « À l'affiche » (bounded by the caps, validated, ticket link restricted to its allowed hosts); members see the source and must answer for it to reach the list.
- The followed venues of a group reveal a little about it: only the operator sees them.

## Docs

- `design.md`: slice 12 (journeys, screens, edge cases, data model, routes), threat model bullet.
- `operations.md`: the sync units, venue commands, daily check of the sync, what a failed or « 0 events » run means and how to fix a venue rule, install before deploy for this release.
- ADR 0011: imports as group rows shown on answer, vs a suggestions table with promotion, vs a global shared collection.
- `CLAUDE.md`: the new packages in Structure.

## Out of scope (v1)

« Nouveau » badges, on-sale reminders, members choosing venues, following artists, aggregator APIs (Songkick refuses hobby projects; Ticketmaster's terms target periodic jobs), arenas behind bot protection (Summum, Zénith: prefill by hand), detecting cancellations, a « mis à jour par la salle » history, a window on « À l'affiche » (next months, then « Afficher la suite ») unless the compact list proves too long.

## Sources

Probed 2026-11-18 with a plain client: no challenge anywhere, no Event JSON-LD on any listing. Drak-Art (no website, Facebook only) and La Bifurk (no upcoming concert, its agenda holds a market) are left out. Club and DJ nights count as concerts.

| Key | Listing | Requests | Id | Keep | Notes |
|---|---|---|---|---|---|
| `mc2` | `www.mc2grenoble.fr/wp-json/tribe/events/v1/events?categories=musique&per_page=50` (JSON; follow `next_rest_url`) | 1 | `id` + `url` (a recurring show has one per date) | category `musique` | `start_date` local wall clock; titles HTML-encoded (« &#8211; »); `venue.venue` is the room; `cost`; no ticket URL: the event `url`. iCal capped at 30 events: not used. robots: all allowed. |
| `belle-electrique` | `www.la-belle-electrique.com/fr/programmation`, then its « Suivant » link (`?from=YYYY-MM`) until none | ≈ 4, 10 s apart (`Crawl-delay: 10`) | slug of `/fr/agenda/<slug>` | `p.tag` Concert, DJ sets, Soirées…; not Formation, Visite | `article.event-card`; title `h3` (first card `h2`); date « 19.11.26 / 20h », ranges « Du … au … » (start kept); prefixes « COMPLET • », « REPORTÉ • / · », and series names (« Face B Live • », « Les Femmes S'en Mêlent • ») stripped so « Écouter » and the dedup see the artist. Ticket link only on the detail page: use the event page. |
| `lilyade` | `www.lilyade.fr/programmation/`, `/programmation/2/`… up to `data-max-page` | ≈ 5 | `post-<id>` class | class `genre-musique` | Elementor loop items; title `h3 a`; info items date (« Jeudi 19 novembre 2026 », often without year: next occurrence from today), time, place (« L'ilyade, Seyssinet-Pariset » or another hall); external ticket links in the item. |
| `la-source` | `lasource-fontaine.fr/programme/` | 1, + 1 per new event (time) | slug of `/event/<slug>/` | concerts (`.ls-tag`), not jeune public | `article.ls-event-card`; date « 19 novembre 2026 »; room line; « \ COMPLET / » suffix. Time from the event page (« Date de début 12 mai 2027 20 h 30 min »), via a new prefill site rule. |
| `amperage` | `www.amperage.fr/la-programmation/` | 1 | `data-event-id` | `.type` concert, club; not conférence | `div.item`; date « MER. 18<br>NOVEMBRE », year from the preceding month block; `.heure` « 19h30 – 23h30 » (start kept); title `.artistes`; genre upper-cased with odd accents. |
| `retour-de-scene` | `www.retourdescene.net/agenda/` (robots: `/feed` disallowed) | 1, + 1 per new event (time) | `post-<id>` class | categorie Concert, Micro-Live; not Formation | Promoter: events at other halls, often L'ilyade's or La Belle Électrique's, so it syncs last and the venue's copy wins the dedup. Title all caps, « (COMPLET) »; date « 19/11/2026 »; place line. Time from the event page via the existing prefill rule. |

- **Times read once**: for a listing without times (`la-source`, `retour-de-scene`), the sync fetches an event's page once ever, parsing it with `prefill`, and keeps the result (time, possibly none) in `event_details (source, source_id, date, time, PRIMARY KEY (source, source_id))`, venue data shared by every group, pruned of past dates each run. Fetched before the dedup and whatever the groups hold, so a dropped duplicate or a group following later never causes a refetch. At most 40 per run (≈ 30 on the first, then one or two a day). A time changed later by the venue is not seen; members can fix it. Test: a second run makes 0 detail requests.
- **Years**: a date without year takes its next occurrence from today; one more than 10 months ahead is a past item still listed, and skipped (fixture with yesterday's item).
- **MC2 ids**: the occurrence `id` is the `source_id` (a recurring show shares one `url` across dates); the `url` is only the link. Test with two dates of one show.
- **Pagination links** come from the venue's pages (`next_rest_url`, « Suivant », `data-max-page`): followed only if https, on the venue's site hosts, within its request cap; fixture where the next page points back to the first.
- **Delay**: at least 3 s between requests to one host, more where robots.txt says (10 s for `belle-electrique`). Redirects only to the venue's site hosts; ticketing hosts serve only to accept ticket links.
- Parsers walk an `html.Parse` tree (`golang.org/x/net/html`, already a dependency) with a small find-by-class helper; no new dependency.
- Fixtures: each listing trimmed to a few items, as in `internal/prefill/testdata`, keeping the edge cases above (no year, ranges, prefixes, filtered categories, pagination link).

## Plan

Commits on `venue-import`, each passing `make check` alone; merged with `--no-ff`, this file deleted first.

1. **`internal/validate`**: move `parseConcert`, its helpers and their unit tests out of `internal/web` (validation of a `store.Concert`, error map by field), plus Paris `today`; `web` wraps it for form values; the revert handler validates the revision directly. No behaviour change: the web HTTP tests pass unchanged.
2. **prefill rule `lasource-fontaine.fr`**: « Date de début 12 mai 2027 20 h 30 min » → date and time, venue « La Source », address Fontaine; trimmed fixture. Improves prefill of La Source links on its own.
3. **Store**: migration 0010; `Concert.Source`; `ConcertsOn(group, date)` (live and deleted); `ApplyImports(group, source, events, today)` (insert with the sync's dedup, update owned rows, headroom, `purgeIfFull`), returning counts (inserted, updated, skipped by reason); new purge order; `Follow`, `Unfollow` (with its hard delete), `Followers(venue)`, `Followed(group)`; `event_details` get/put/prune; `source_id` cleaned and capped at 256 bytes. Table tests with the fake clock: tombstone never revived, edit (and edit then revert) stops updates, dedup separators both ways and against deleted rows, headroom, purge order at the row cap, unfollow keeps answered and edited rows.
4. **`internal/agenda` fetcher and venue list**: client from `prefill`'s dialer, 30 s, 4 MB (limit+1), content types, redirects https within allowed hosts, crawl delay via injected sleep, 429/503 stop. Tests against `httptest` with a test dialer: body over 4 MB refused (not cut), wrong content type, User-Agent without cookie or Referer, redirect off the venue's hosts refused, pagination link off-host or looping, minimum delay, context cancelled during the delay stops the run; plus one test that the production client refuses loopback.
5. **`internal/agenda` parsers**, one commit per venue or two: `mc2`, `belle-electrique`, `lilyade`, `amperage`, `la-source`, `retour-de-scene`; shared title normalisation (prefixes, HTML entities, « (COMPLET) »/« \ COMPLET / » → price « Complet »), year inference, ticket-link host check. Fixture tests: expected events for a fixed « today ».
6. **Sync and CLI**: `agenda.Sync` (followed venues only, venues before promoters, fetch all then apply per group, per-event detail fetch for new ids, 200-event cap, error exit on a failure or 0 events, logs without URLs); `cmd/concerts`: `sync`, `list-venues`, `follow-venue`, `unfollow-venue`, usage text. End-to-end `Sync` tests in `internal/agenda` (injected client and venue list, store on a temp DB); `cmd` tests cover follow, unfollow, list-venues and usage.
7. **Web**: `source` in the list; visibility rule on « Tous », « Mes concerts », « Passés »; `?affiche=1` (and `&salle=`) view with compact rows, explanation line, `h2`, title, venue nav, status region; filter link only when following; `back=affiche` and the no-JS returns; banners only for cards in view, restore redirect; concert page back link and « annoncé par »; duplicate warning for an unanswered import without « Ajouter quand même »; demo data with a followed venue and imports (one without time, one « Complet »). HTTP tests via `newTestServer`. `verify-ui` gains the screens `affiche`, `affiche&salle=`, an unanswered import's concert page and the duplicate warning against an import, and checks: toggle target ≥ 44 px at 390 px, no overflow with a long artist or venue, focus kept on the toggle and status text set after the swap, no « Ajouter quand même » in that warning.
8. **Deploy**: `<name>-sync.service`/`.timer`, `lib.sh`, `install.sh` (units, enable, sudoers), `uninstall.sh` (one disable per unit), `admin` (stop/start/trap, logs, refuse without the sync timer, `reset-failed` includes the sync service, so a run of the old binary between install and deploy leaves no failed unit). Checked with `shellcheck`, `systemd-analyze verify` on the rendered units, and the ADR 0008 container cycle if podman is available: install over an existing install, admin refusal before install, deploy, one real `sync` in its sandbox (DNS, CA certificates), rollback and restore with the timer active, uninstall.
9. **Docs**: `design.md` slice 12 and threat model, `operations.md`, ADR 0011, `CLAUDE.md` Structure; delete this file.

Not run here: the real sync against the venues (once by hand at the end, sequential, with the crawl delay, output summarised) and the server install (the owner's: `make install` then `make deploy`).
