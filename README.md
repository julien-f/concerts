# concerts

A small web app where a group of friends (5–30, typically a WhatsApp group) shares concerts and sees who goes to what. Each group is reached through a secret link (`/g/<secret>`); no accounts.

Design and roadmap: [docs/design.md](docs/design.md).

## Run

Requires Go (see `go.mod`) and gcc (for `go test -race` only).

```sh
make run                                                   # serves on :8080, database concerts.db
CONCERTS_BASE_URL=http://localhost:8080 go run ./cmd/concerts new-group "Gig buddies"   # prints the group link
make demo                                                  # or: a fresh demo.db with sample data, served on :8080
```

Environment: `CONCERTS_ADDR` (default `:8080`), `CONCERTS_DB` (default `concerts.db`), `CONCERTS_BASE_URL` (public URL; required by `serve`, `new-group`, `rotate-secret`). `concerts` without arguments lists the subcommands (`list-groups`, `rotate-secret`, `delete-member`, `backup`…).

## Test

```sh
make check   # golangci-lint, govulncheck (needs network), then go test -race ./...
```

The pre-commit hook runs `make check`; enable it once per clone with `git config core.hooksPath .githooks`.

## Deploy

1. `make build`, then copy `bin/concerts` to `/usr/local/bin/concerts` on the server.
2. `sudo useradd --system --no-create-home --shell /usr/sbin/nologin concerts`
3. Copy `deploy/concerts.service` to `/etc/systemd/system/`, set `CONCERTS_BASE_URL`, then `sudo systemctl enable --now concerts`.
4. Reverse proxy HTTPS to `127.0.0.1:8080`, e.g. Caddy:
   ```
   concerts.example.com {
       reverse_proxy 127.0.0.1:8080
   }
   ```
5. Create a group: `sudo -u concerts env CONCERTS_DB=/var/lib/concerts/concerts.db CONCERTS_BASE_URL=https://concerts.example.com concerts new-group "Gig buddies"`

Migrations run at startup; the binary refuses to start on a database newer than itself.

### Backup

`concerts backup <file>` writes a consistent snapshot while the app runs (`VACUUM INTO`). Nightly, keeping 14 days:

```sh
sudo cp deploy/concerts-backup.{service,timer} /etc/systemd/system/
sudo systemctl enable --now concerts-backup.timer
```

Snapshots land in `/var/lib/concerts/backups/`; copy them off the server (rsync, restic…).
