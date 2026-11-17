# Plan: per-site prefill rules (branch `ticketing-sites`)

Deleted before the merge; the outcome lands in `docs/design.md` (slice 5).

## Findings (real client, 2026-11-17)

| Site | Today | Where the data is |
|---|---|---|
| L'ilyade (`www.lilyade.fr`, WordPress + Elementor) | artist only (og:title) | JSON-LD: Yoast `WebPage`/`BreadcrumbList`/`Organization`, no Event. Visible: `<span class="… elementor-post-info__item--type-custom">` items: « Musique », then « Samedi 21 novembre 2026 », « 20h00 », « L'ilyade », « Seyssinet-Pariset » (mobile and desktop copies), then related events after the heading « À découvrir également ». |
| Retour de Scène (`www.retourdescene.net`, WordPress + Divi) | artist only, all caps | JSON-LD: Rank Math `BlogPosting`, no Event. Visible: `<h3>Lieu</h3>` then « L'Ilyade<br>32 Rue de la Fauconnière<br>Seyssinet-Pariset, Isère 38170 France », « Accès : Tram C… »; `<h3>Horaire</h3>` then « Samedi 21 novembre 2026<br>20h > 01h (portes 20h, début de concert 20h30) ». |
| Shotgun (`shotgun.live`) | 429 | Vercel Security Checkpoint (JS challenge) on every URL tried: `/fr/events/…`, `/events/…`, `/oembed?url=…`, `api.shotgun.live`. `robots.txt` allows all, which does not authorise passing a challenge. The only API found is organiser-only with a key; undocumented internal endpoints are not a documented API. `sitemap.xml` sits behind the same checkpoint and would mean a second fetch. Documented, not worked around. |
| La Belle Électrique | complete | Top-level JSON-LD array, `Event`, local `startDate` without offset, `validFrom: null`, performer object, PostalAddress. Kept as a regression fixture. |

## Change

- Host: `Fetch` passes `resp.Request.URL.Hostname()` (final page, port and userinfo dropped); lowercased, one trailing `.` and one leading `www.` trimmed; exact map lookup (never suffix/contains). Nothing new is logged.
- `Parse(r, host, now)`; `scan` collects visible text nodes only when the host has a rule, at most 5,000 non-empty ones, each with the `class` of its innermost open start tag (cleared on every end tag) and a heading flag (h1–h6 depth counter). Text inside `script`, `style`, `noscript`, `template`, `svg` is skipped (depth counter for the last two).
- Precedence: if the generic result has a Date, no rule runs (no mixing of sources); else the rule fills only the empty fields (artist from og:title kept).
- `sites.go`, one small function per host:
  - `lilyade.fr`: texts before the heading « À découvrir également »; among those with class `elementor-post-info__item--type-custom`, the first whole-text French date; time, venue, city taken only if the next three such texts are a time and two non-empty texts. Address = city (the footer address is the hall's, wrong for an event held elsewhere).
  - `retourdescene.net`: texts between heading « Lieu » and the next heading: venue = first; city line = the one with a 5-digit postcode; « City, Region NNNNN France » → « NNNNN City »; street = the single text between venue and city line, if any. Between « Horaire » and the next heading: first whole-text French date, then the first time in the following text (« 20h > 01h (portes 20h, début de concert 20h30) » → 20:00, same convention as L'ilyade's « 20h00 », i.e. the advertised start, not the doors-vs-show nuance).
  - Helpers: `frenchDate` (whole text, case- and accent-insensitive months, optional weekday, « 1er », year required, round-trip checked); `frenchTime` (`\b([01]?\d|2[0-3]) ?h ?([0-5]\d)?\b`).
- Values still go through `clean`, then `parseConcert` in `internal/web/prefill.go` (unchanged): a value failing validation (e.g. address > 100 runes) is dropped.
- `maxPage` comment updated.

## Tests (first, seen failing)

- Fixtures, anonymised, keeping the real quirks (tabs in spans, `&#039;`, « Musique » item, a Production `h3`, inline `svg` with `style`, `noscript`, `&gt;`, `<br />`, a plain script holding a French date, the RdS header line): `lilyade.html`, `retourdescene.html`, `belle-electrique.html`.
- `TestParseFixtures` with a host per case; lilyade fixture under another host → artist only; lilyade without its main date → related event's date not taken.
- Table tests: host normalisation (`WWW.LILYADE.FR`, trailing dot, `lilyade.fr.evil.com` misses), `frenchDate`/`frenchTime`, JSON-LD on a ruled host wins, RdS block without street, overlong address dropped by the form (web test).
- `fetch_test.go`: stub `Transport` redirecting `a.example` → `www.lilyade.fr`, serving the fixture: the rule applies to the final host. No network.

## Docs

- `docs/design.md`: SSRF entry (body read, text collected only for ruled hosts and capped, exact final host), slice 5 (rules, precedence, Shotgun finding, fixtures).
- ADR 0004: `x/net/html` purpose adds per-site visible-text rules.

## Decided

- All-caps artist (Retour de Scène): kept as served; converting would mangle acronyms (IAM, AC/DC) and the user can fix it in the form.
