# Later

Ideas not scheduled, each with what would bring it on. Current design: [design.md](design.md).

## Off-server backups and failure alerts

Nothing set up yet. Leads:
- First check whether the server is already backed up (host snapshots, a whole-server backup) and covers `/home/<name>/backups`, not the live database.
- Best fit: another machine pulls nightly with `rsync` and a key limited to `rrsync -ro /home/<name>/backups`; the server holds no credential, so a compromise cannot delete the copies. `make install` rewrites `authorized_keys` with the deploy key only, so it must learn to keep that second key first. Retention on the puller: about 14 daily, 8 weekly, 12 monthly.
- Without an always-on machine: restic to BorgBase or a storage box in append-only mode, pruned from a trusted machine.
- A periodic restore test (`PRAGMA integrity_check`, `list-groups`) and an alert when the newest snapshot is over 36 h old.

## « Je cherche un billet »

A status, if the group asks for it: it flags the exception (a sold-out concert, a resale), which members have a reason to set, unlike the dropped « J'ai mon billet ».

## A window on « À l'affiche »

The next 3 months, then « Afficher la suite », as `?past=all`, if its cards prove too long a list: at 180 cards over 12 months, desktop Chromium throttled 4× scrolled it at 58 fps (59.6 with the former compact rows); a phone may differ (`make measure-scroll PAGE=affiche`).

## Carpooling

If the group asks for it: once « J'y vais » is set, an optional « Je propose / Je cherche un covoiturage » on the concert page only (`statuses.carpool CHECK IN ('offer','seek') NULL`), shown as a mark by the name; no seats or departure city, the arranging stays on WhatsApp.

## Several groups per person

Friends, colleagues: each link gives an independent member, possibly under another name, and no group sees anything of another. Beyond switching between them ([design.md, slice 13](design.md#slices)):
- A « Tous mes concerts » view across one's groups, as a personal agenda: read-only, since answering needs a group.
- Answering once for a concert present in several groups (venue imports give one row per group): needs a person linking members, against [ADR 0002](decisions/0002-secret-link-identity.md), and a cross-group match of concerts (the import's same-bill rule works within one group); a wrong propagation would show colleagues a concert one goes to with friends.
