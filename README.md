# concerts

A small web app where a group of friends (5–30, typically a WhatsApp group) shares concerts and sees who goes to what. Each group is reached through a secret link (`/g/<secret>`); no accounts.

- Design: [docs/design.md](docs/design.md); decisions: [docs/decisions/](docs/decisions/).
- Install, deploy, admin, backups, member message: [docs/operations.md](docs/operations.md).

## Run

Requires Go (see `go.mod`) and gcc (for `go test -race` only).

```sh
make run                                                   # serves on 127.0.0.1:8080, database concerts.db
CONCERTS_BASE_URL=http://localhost:8080 go run ./cmd/concerts new-group "Gig buddies"   # prints the group link
make demo                                                  # or: a fresh demo.db with sample data, served on :8080 (all interfaces, so the LAN too)
```

Environment: `CONCERTS_ADDR` (default `127.0.0.1:8080`; ignored when systemd passes a socket), `CONCERTS_DB` (default `concerts.db`), `CONCERTS_BASE_URL` (public URL; required by `serve`, `new-group`, `rotate-secret`, `demo`). `concerts` without arguments lists the subcommands (`list-groups`, `rotate-secret`, `delete-member`, `backup`…).

## Test

```sh
make check
```

Enable the pre-commit hook once per clone: `git config core.hooksPath .githooks`. What each target and the hook run: [CLAUDE.md, Commands](CLAUDE.md#commands).
