# 0001: Go + SQLite + htmx on a personal server

## Context

A tiny app for one friend group or a few (5–30 users each), maintained alone, hosted on a personal server.

## Decision

- Go, standard library first, deployed as a single static binary under systemd behind an HTTPS reverse proxy.
- SQLite through `modernc.org/sqlite` (pure Go, `CGO_ENABLED=0`), WAL mode; hand-written migrations embedded in the binary.
- Server-rendered `html/template` pages with htmx for interactivity; no JS build.

## Consequences

- One file to deploy, one file to back up (`VACUUM INTO`).
- Single-server only: SQLite rules out horizontal scaling, which this load never needs.
- No SPA-style offline UI; every interaction is a server round trip.
