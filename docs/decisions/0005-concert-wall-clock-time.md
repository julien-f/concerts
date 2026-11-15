# 0005: Concert dates as Europe/Paris wall-clock strings

## Context

ADR 0004 stores times as UTC. A concert has a date and an optional start time; "no time" cannot be converted to UTC, and a concert is "on the 14th at 20 h in Paris" whatever the offset.

## Decision

- `concerts.date` is `YYYY-MM-DD` and `concerts.time` is `HH:MM` or NULL, both Europe/Paris wall clock; same for `on_sale_date`.
- Row timestamps (`created_at`, `updated_at`, `deleted_at`) stay UTC RFC 3339.
- Conversion to an instant happens only where needed (the `.ics`, in UTC), with `time/tzdata` embedded.

## Consequences

- Sorting and "past" checks compare strings against today's date in Europe/Paris.
- One time zone for the whole app; a group abroad would see Paris times.
