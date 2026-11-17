# Plan: concert description and price

Branch `description-price` (based on `worktree-listen-link`, which touches the same template and doc). Deleted before the merge.

## Why

Friends decide whether to go from what the artist is and what it costs. Ticketing pages already say both; the prefill drops them.

## What the two reference pages give

- La Belle Électrique (schema.org `Event`): `description` (~1,700 chars, `\r\n` between paragraphs, some words glued where the source had markup: « SNIPERGroupe »); `offers[]` with `price` (number), `priceCurrency` `EUR`, `name` per tier (32, 29, 29). Its `og:description`/`meta description` repeat the description without line breaks.
- Retour de Scène (no `Event`, existing site rule): price as the visible text « 20€/23€ » right after the « Billetterie » button; description = the artist blocks (an `h1` per artist, then paragraphs, « Suivre » social links) between the price and the « Lieu » heading. Its `og:description` is menu text: never use Open Graph descriptions.

## Data

- Migration `0006_concert_description_price.sql`: `description TEXT` and `price TEXT`, NULL when unknown.
- `store.Concert` gains `Description`, `Price`; select, insert, update follow the `address` pattern (`nullif(?, '')`).

## Validation (`internal/web/form.go`)

- `price`: optional, `cleanText`, 1–30 runes. Free text: « 29–32 € », « Prix libre », « Gratuit ».
- `description`: optional, new `cleanParagraphs(v, 2000)`: same character rules as `cleanText`, but keeps line breaks: `\r\n` → `\n`, each line's spaces collapsed and trimmed, runs of blank lines folded into one, whole value trimmed. Line feed is the only control character allowed.

## Prefill (`internal/prefill`)

- `Result` gains `Description`, `Price`.
- JSON-LD event: `Description` = `description`; `Price` = range over `offers` (walked, so `AggregateOffer.lowPrice/highPrice` count too) where `priceCurrency` is `EUR` or absent, `price` a number or numeric string (`.` or `,` decimal), > 0.
- Retour de Scène rule: `Price` from the first text matching only euro amounts (`20€/23€`, `20 €`) before « Lieu »; `Description` = the texts after it up to « Lieu », minus « Suivre », joined with `\n`, artist headings included.
- Formatting (one function): min = max → « 32 € »; else « 29–32 € » (en dash); whole euros without decimals, else `29,50`; plain space before € (`cleanText` turns U+00A0 into a space anyway, `strings.Fields` splitting on it), and the price is rendered in a `white-space: nowrap` span.
- In `web/prefill.go`, a description longer than 2,000 runes is cut at the last line or sentence end before the limit, with « … », rather than dropped like other invalid fields. « Relire la billetterie » also fills empty description and price.

## Display

- Card: price after the venue on the meta line (« sam. 14 mars · 20 h · Zénith · 29–32 € »).
- Concert page facts: « Prix : 29–32 € »; below the facts, `<details class="about"><summary>En savoir plus</summary>` with the text in `white-space: pre-line` (no heading inside `summary`). Not in `og:description`, not in the `.ics`.
- Form: « Prix » (text, after « Mise en vente », paired with it from 48 rem) and « Description » (`<textarea>`, full width, last field). `field` gains a textarea case in `form.html`.

## Tests

- store: round trip of both fields, NULL for "".
- web: validation table (too long, invisible chars, blank lines folded, CR LF); card and page rendering; form refill on error; refresh fills empty ones.
- prefill: Belle Électrique fixture extended with `offers` → « 29–32 € » and its description; Retour de Scène fixture → « 20–23 € » and the artist text; price edge cases (single tier, string price, other currency, 0, `AggregateOffer`).
- `make verify-ui` with demo data carrying both fields.

## Docs

`docs/design.md`: data model, screens (card, concert page, form), validation; `make demo` sample data.

## Open questions

- Description cut at 2,000 runes: enough? Belle Électrique is ~1,700 for two artists.
- Strip HTML tags some sites put in JSON-LD descriptions? Not seen on the two pages; escaped on display anyway.
