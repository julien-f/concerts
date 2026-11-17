# 0007: « Prévenir le groupe » is a plain wa.me link

Superseded by [0010](0010-share-sheet.md).

## Context

ADR 0004 planned the Web Share API with a `wa.me` fallback (about 10 lines of JS). The v1 spec asks for a « Prévenir le groupe » button using `https://wa.me/?text=`.

## Decision

- A plain link to `https://wa.me/?text=` with the artist, date, venue and the absolute concert link (spaces as `%20`: WhatsApp shows `+` literally).
- No JS beyond htmx; no Web Share.

## Consequences

- Works without JS and opens WhatsApp directly, the group's only channel.
- The text, link included, goes through Meta's `wa.me` servers: an accepted risk in the threat model, fixed by rotating the secret.
