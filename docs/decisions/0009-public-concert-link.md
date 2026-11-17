# 0009: Public concert link

Amends [0002](0002-secret-link-identity.md): the group link is the key to enter; the browser then keeps it for concert pages.

## Context

Members send a concert to friends outside the group by copying the address bar, sharing from the browser or forwarding « Prévenir le groupe ». At `/g/<secret>/concerts/<id>`, each of these handed out the whole group. A dedicated share button would not change the habit.

## Decision

- A concert page lives at `/c/<token>`, a random per-concert token stored in clear; every link to it, and `og:url`/canonical, uses that address.
- Opening any `/g/<secret>` page sets a cookie on `Path=/c` holding the secret and member id. With it, `/c/<token>` is the full member page; without it, a public card: the concert, its ticket and calendar links, a count of members going, nothing of the group.
- The old concert URL redirects there.

## Consequences

- Sharing a concert never shares the group, whatever the gesture.
- A member's browser that never opened the group link (in-app browser, new phone) sees the card until it does.
- A concert link cannot be revoked short of deleting the concert, and survives a secret rotation.
- The list and forms still show the secret in their address; moving them is a later step.

## Rejected

- **Share button with its own token**: members copy what they see.
- **Rewriting the address bar with `history.replaceState`**: needs JavaScript, and a reload or a copied URL needs the server route anyway.
- **Numeric ids in the public URL**: they would let anyone list every group's concerts.
