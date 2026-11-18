# Plan: prefill from a La Belle Électrique venue page via its billetterie

Branch `prefill-belle-electrique`; this file is deleted before the merge.

## Problem

- Members paste the venue page (`www.la-belle-electrique.com/fr/agenda/<slug>`), the one the venue's agenda, search and social media link to. It has no schema.org Event: prefill finds the artist only.
- The ticketing page (`billetterie.la-belle-electrique.com/agenda/<id>-<slug>?session=<id>`) has a full Event that the parser already reads (fixture `belle-electrique.html`), and it is the page the stored link is for (« Billetterie » button, `.ics`, public card).

## Findings (Nov. 2026, 42 venue pages)

- 22 link « Billetterie » to `https://billetterie.la-belle-electrique.com/share/event/<id>-<slug>`: a 200 page that meta-refreshes to `/agenda/<id>-<other slug>?session=<id>`. 2 link straight to `/agenda/<id>-<slug>?session=<id>`.
- Sold-out concerts link to a resale site (`reelax-tickets.com`); workshops to Weezevent or HelloAsso; some events have no link.
- `/agenda/<id>-<any slug>?session=<id>` serves the Event for all 22 share ids; only the id matters (`/agenda/<id>?session=<id>` does not work).

## Change

1. **Site rule finds the ticketing link** (`internal/prefill/sites.go`, `parse.go`)
   - `scan` also collects `a[href]` values when the host has a site rule (same `maxTexts` cap); site rules take the `page` instead of `[]text`.
   - `Result` gets `TicketURL`: the page to read instead, and to store as the ticket link.
   - Rule `la-belle-electrique.com`, applied like the other rules only when the page has no Event date: hrefs matching `^https://billetterie\.la-belle-electrique\.com/(?:share/event|agenda)/(\d{1,9})-([A-Za-z0-9-]{0,200})` (not anchored at the end: an accented slug is cut short, harmless since only the id counts). Exactly one distinct id on the page, else no link: a sold-out page (resale button) must not pick another concert's link. Rebuilt as `https://billetterie.la-belle-electrique.com/agenda/<id>-<slug>?session=<id>` from the captures only. Resale, Weezevent and other hosts are not followed.
   - No share-page fetch: rewriting saves a request; if the venue changes its URLs, the fallback below applies.
2. **One hop, pinned** (`internal/prefill/fetch.go`)
   - `Fetch` puts one 10 s deadline on the whole read (today 8 s per request; a slow first page leaves less to the hop).
   - If the first page gives a `TicketURL`, fetch it once with the same client (same dial and redirect checks). Use its result only if it has a date and its final host, after redirects, is the constant `billetterie.la-belle-electrique.com`; the stored link stays the rebuilt https URL. Else keep the first result with `TicketURL` cleared. The second page's own `TicketURL` is ignored: at most two pages per prefill (each up to 3 redirects).
   - `Fetch` returns the hop's error apart from the page's (`Result, hopErr, err`): the handler logs it with the host only, so a change of the venue's URLs shows in the logs.
3. **Handler** (`internal/web/prefill.go`, `concerts.go`)
   - The form's ticket link is `TicketURL` when set, else the pasted URL (also if the found one fails validation).
   - Add form message when the link changed: « Lien de billetterie trouvé sur la page de la salle. Vérifie les champs pré-remplis avant d'enregistrer. »
   - « Relire la billetterie »: when the page gives another ticket link, it replaces the posted one and « Lien de billetterie » is listed in « Complété depuis la billetterie : … » (an exception to « empty fields only »: the same concert's buy link), so a concert saved with the venue page gets its billetterie link.
4. **Tests**
   - `parse_test.go`: anonymized venue fixture `belle-electrique-site.html` (og:title, a mailto « billetterie », a look-alike host, then the share link) → `Result{Artist, TicketURL}`; the same file on another host → artist only. Table test of the link rule: share and agenda links, two ids, resale only, `http://`, look-alike host, userinfo, port, protocol-relative, uppercase host, query or fragment injection.
   - `fetch_test.go` with `roundTripFunc`: venue → billetterie gives the full result and `TicketURL`; billetterie 404, no date, or redirect to another host → venue result, `TicketURL` "", a hop error.
   - `web`: the test server's fetch client routes the two La Belle Électrique hosts to `testdata` files (no network, `http.DefaultClient` untouched); cases: link replaced and message, hop failed → pasted link kept, « Relire » replaces a venue link.
5. **Docs**: `docs/design.md` prefill and SSRF bullets (one pinned hop, 10 s total, one limiter slot covers two pages).

## Not done (proposed separately)

- Venue-page visible-text rule as a fallback when the hop fails: second parser for one site; the venue's price list mixes in « 32 € » (presumably door price, unverified).
- Generic meta-refresh or canonical following: lets any page choose the stored link.
- Title-casing all-caps cities (« GRENOBLE ») in addresses.
- Rewording the prefill hint; a « billetterie illisible » message when the hop fails.
