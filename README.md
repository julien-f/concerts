# concerts

A small web app where a group of friends (5–30, typically a WhatsApp group) shares concerts and sees who goes to what. Each group is reached through a secret link (`/g/<secret>`); no accounts.

Design and roadmap: [docs/design.md](docs/design.md).

## Run

Requires Go (see `go.mod`) and gcc (for `go test -race` only).

```sh
make run                                                   # serves on :8080, database concerts.db
CONCERTS_BASE_URL=http://localhost:8080 go run ./cmd/concerts new-group "Gig buddies"   # prints the group link
```

Environment: `CONCERTS_ADDR` (default `:8080`), `CONCERTS_DB` (default `concerts.db`), `CONCERTS_BASE_URL` (required by `new-group`).

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

Safe while the app runs (WAL mode):

```sh
sqlite3 /var/lib/concerts/concerts.db "VACUUM INTO '/backups/concerts-$(date +%F).db'"
```
