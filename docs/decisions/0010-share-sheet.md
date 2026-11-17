# 0010: « Partager » opens the share sheet

Supersedes [0007](0007-plain-whatsapp-link.md) on the concert page; the banner after adding keeps its plain « Prévenir le groupe » link.

## Context

The concert page had a row of six orange buttons. Lightening it, « Prévenir le groupe » became a secondary « Partager »: members also send concerts outside WhatsApp (Signal, SMS, a friend outside the group, ADR 0009).

## Decision

- « Partager » is the `wa.me` link of ADR 0007, carrying `data-share-text` and `data-share-url`.
- `share.js` (8 lines, loaded by the concert page only) calls `navigator.share` on click where the browser has it, else lets the link open WhatsApp.

## Consequences

- Any app on the phone; the text no longer goes through Meta's `wa.me` servers when the share sheet opens.
- WhatsApp takes one more tap.
- The first JavaScript beyond htmx; without JS, or without Web Share (Firefox on a computer), nothing changes.
