# Plan: rich link previews (WhatsApp and other messengers)

Branch: `link-previews` (one slice, merged with `--no-ff`). This plan is committed there and deleted before the merge.

## Problem

- Concert pages (`/g/<secret>/concerts/<id>`) carry text Open Graph tags (slice 7) but no `og:image`: WhatsApp shows a bare text card.
- The group link `/g/<secret>` has no Open Graph tag: previewers show only « Qui es-tu ? · <group> » or the bare URL.
- The only icon is `icon.svg`; iMessage and Signal need a PNG icon (they fall back to `apple-touch-icon` without `og:image`).

## How previewers fetch (checked)

- WhatsApp, iMessage, Signal: the sender's phone fetches the page with the app's own client (UA `WhatsApp/2…`; Signal also sends `WhatsApp/2`), no browser cookie, and the preview travels with the message: recipients never refetch, so a later change never reaches messages already sent.
- Telegram, Messenger, Discord, Slack: their servers fetch (Telegram unverified), no cookie, cache ~30 min.
- So a previewer always gets the picker on `/g/<secret>`, never the list. CSP, `Referrer-Policy`, `noindex`, `Cache-Control` don't matter to them; a disallow-all `robots.txt` might (Meta, Discord): never add one.
- Limits met by design: WhatsApp large preview needs an absolute https `og:image` under 600 kB, ≥ 300 px wide, ratio ≤ 4:1, `<head>` in the first 300 kB, non-empty title/description/url; iMessage wants ≥ 900 px wide, no text in the image, a square icon ≥ 108 px; Signal accepts PNG/JPEG/WebP over https only; Discord needs `twitter:card summary_large_image` for its large layout and reads `og:image:width/height`.
- Sources: developers.facebook.com/docs/whatsapp/link-previews, Apple TN3156, Signal-Desktop `linkPreviewFetch`, docs.discord.com link-previews.

## Changes

1. **Static images**, no text (fonts differ per machine, text is unreadable as a thumbnail, Apple advises against it), light palette hard-coded:
   - `internal/web/static/og.png`: 1200×630, ticket stub centred in the middle 630×630 (WhatsApp's square fallback crop). ~14 kB measured on a sample render.
   - `internal/web/static/apple-touch-icon.png`: 180×180, stub on paper background (iOS fills transparency with black).
   - Look: brick-filled stub, perforation in paper colour (picked on a mockup), also applied to `icon.svg`, so favicon, iOS icon and preview share one silhouette.
   - Sources `scripts/og-image.svg` and `scripts/apple-touch-icon.svg` (outside `static/`, which `loadStatic` embeds whole); `scripts/og-image.mjs` (~15 lines, header comment saying how to run it) screenshots them with the Playwright Chromium already in `bin/tools` (viewport set, `deviceScaleFactor: 1`). PNGs committed. No Makefile target: regenerated about once a year.
   - Same image for every page: a per-concert image would need `golang.org/x/image` and a font (new dependency) to repeat what `og:title` already prints.
2. **Template func `abs`**: `s.baseURL + path`. Image: `{{abs (static "og.png")}}` (hashed, `immutable`, no secret; a new image gets a new URL). `pageURL` reuses it, so "base URL + path" is written once.
3. **Partial `ogCommon`** in `templates/partials.html`: `og:type`, `og:site_name`, `og:image` + `:width` + `:height` + `:type` + `:alt`, `twitter:card summary_large_image`. Used by `concert.html` and `picker.html`. No `og:locale` (no previewer documents using it).
4. **Group link preview** in `picker.html`'s `meta` block: `og:title` = group name, `og:description` « Qui va à quel concert ? Ouvre le lien et choisis ton prénom. » (generic: no first names), `og:url` = `{{abs .Base}}` (works for both the `home` and the 422 `join` renders, no new field).
5. **`<link rel="apple-touch-icon">`** in `layout.html` (also the home-screen icon on iOS). Not counted by `verify-ui`'s page weight (browsers don't fetch it, nor `og:image`): `MAX_WEIGHT` unchanged; the binary grows ~20 kB (`make measure`).
6. **Tests** (`newTestServer`):
   - `TestOpenGraph` extended: concert page and picker carry the absolute hashed `og:image`, size/type tags and `twitter:card`; picker carries group title, description, URL, and its description holds no first name; the 422 picker (`POST /me` with empty name) carries the same tags; the list (with identity cookie) and not-found pages carry no `og:` at all (today the list check only looks for `og:title`).
   - Both PNGs decode with `image/png` at their expected sizes, `og.png` under 50 kB (catches a 2× or photo regeneration).
7. **Docs** in the same commit: `docs/design.md` slice 7 (drop « No `og:image`: weight for no information »; static image, group preview, who fetches and why a previewer always gets the picker); threat model: image URL holds no secret, server-side previewers (Telegram, Discord, Slack) are covered by the existing accepted risk; never a disallow-all `robots.txt`.

## Verification

- `make test`, `make lint`, `make verify-ui` (vnu validates the new tags).
- Locally: `curl -A WhatsApp/2` on the picker and a concert page; tags in `<head>`.
- After `make deploy` (asked separately; production base URL is https, required by WhatsApp and Signal): send the group link and a concert link in WhatsApp (chat with oneself), Signal, Telegram, iMessage if available.
