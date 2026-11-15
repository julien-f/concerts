# 0003: No WhatsApp bot

## Context

The group lives on WhatsApp, so a bot posting and reading concerts there was tempting.

## Decision

No bot. The app is shared as a link in the group; slice 7 adds a "Share on WhatsApp" button (`https://wa.me/?text=`).

## Consequences

- The official WhatsApp Business API is out of reach for a private friend group; unofficial libraries expose the phone number to a ban.
- Members open the app themselves; there are no in-chat updates or push notifications.
