# 0011: Venue agendas imported as group concerts

## Context

Members add a concert only after seeing it elsewhere: concerts announced at the halls the group goes to are missed, or noticed after the on-sale date. The list must still mean « someone here cares about this one ».

## Decision

- A daily job (`concerts sync`, systemd timer) imports the upcoming concerts of the venues a group follows into that group's concerts, as ordinary rows: members edit, delete and answer them like any other.
- « Tous », « Mes concerts » and « Passés » show an import only once a member answered it. A third filter, « À l'affiche », lists the upcoming imports nobody answered; answering one moves it to the list.
- One copy per group: an edit or delete in one group never reaches another, and a leaked link reaches one group's data only. A deleted import stays a tombstone; an edited one is the members' and no longer updated.
- Venues are compiled into the binary (URLs, hosts, parser, crawl delay, robots.txt checked by hand); the operator makes a group follow one from the CLI.

## Consequences

- The list stays the group's choice; the venues' announcements wait one tap away.
- Imports reuse the concert's rows, statuses, history and pages; visibility costs no query (statuses are already loaded). Members keep precedence: imports stop at 400 of the 500 upcoming concerts, and the row-cap purge takes an import's tombstone last.
- An event fetched once per run is stored once per following group; at a few groups, negligible.
- A venue's site change breaks its parser: the sync fails until a fixture and the parser are updated and deployed (operations.md). Each venue is read within its own deadline and applied at once, so one broken or slow site costs the others nothing.
- Adding a venue means a release; members ask the operator.
- The sync's updates leave no history, and a cancelled concert stays until a member deletes it.
- An edited import no longer follows its venue, so the sync also keeps what each venue last said of an event once for every group (`venue_events`); « Relire la billetterie » on an import shows what differs from it, field by field, for the member to take or leave.

## Rejected

- **A suggestions table, promoted to a concert on answer**: a second concert-like model for every screen, route and edit path, and a promotion step to keep consistent.
- **One global collection shared by every group**: an edit or delete in one group would reach the others, and per-group state (statuses, tombstones, edits) would need overlay tables.
- **Aggregator APIs**: Songkick refuses hobby projects, Bandsintown is queried by artist only, Ticketmaster's terms target periodic jobs.
- **Members entering agenda URLs**: the server would fetch member-chosen URLs every day, for as long as the group lasts; a compiled-in list keeps the hosts, parsers and politeness reviewed.
