# 0002: Identity through a secret link, no accounts

## Context

Members join from a WhatsApp group on their phones; signing up would lose most of them. The data (who goes to which concert) is low-stakes.

## Decision

- Each group is reached at `/g/<secret>`; holding the link is membership.
- On first visit a member picks a first name, remembered by a cookie scoped to the group path.
- Only the secret's SHA-256 is stored; logs redact it; responses send `Referrer-Policy: no-referrer`.

## Consequences

- Zero-friction onboarding: tap the link, type a name.
- No protection against a member impersonating another; accepted between friends.
- A leaked link gives full access until the secret is rotated, which breaks the old link for everyone.
