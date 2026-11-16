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

1. `make build`, then copy `bin/concerts` to `/usr/local/bin/concerts` on the server (later updates: `make deploy`, below).
2. `sudo useradd --system --no-create-home --shell /usr/sbin/nologin concerts`
3. Copy `deploy/concerts.service` to `/etc/systemd/system/`, set `CONCERTS_BASE_URL`, then `sudo systemctl enable --now concerts`.
4. Reverse proxy HTTPS to `127.0.0.1:8080`, compressing responses (the app does not), e.g. Caddy:
   ```
   concerts.example.com {
       reverse_proxy 127.0.0.1:8080
       encode zstd gzip
   }
   ```
   No proxy log may hold a `/g/…` path (it contains the group secret): no access log (Caddy writes none without a `log` directive; nginx needs `access_log off;` in this site's `server` block). Both still write the request path to their error log when the app is down (restart, deploy): keep that log readable by root only.
5. Create a group: `sudo -u concerts env CONCERTS_DB=/var/lib/concerts/concerts.db CONCERTS_BASE_URL=https://concerts.example.com concerts new-group "Gig buddies"`

Migrations run at startup; the binary refuses to start on a database newer than itself.

### Update and roll back

```sh
make deploy DEPLOY_HOST=admin@server     # build, back up with the old binary, swap, restart, check /healthz
make rollback DEPLOY_HOST=admin@server   # previous binary and pre-deploy database back
```

- Needs SSH with `sudo` on the server; `DEPLOY_HOST` has no default.
- `deploy` keeps the previous binary (`/usr/local/bin/concerts.prev`) and a pre-deploy snapshot (`/var/lib/concerts/backups/pre-deploy.db`), taken by the old binary so the new one cannot migrate the live database first.
- `rollback` stops the service, moves the current database and its `-wal`/`-shm` aside (`*.pre-rollback-<time>`: writes made since the deploy are there), restores both, restarts and checks `/healthz`.
- The reverse proxy must keep the `Host` header (Caddy does by default): cross-origin protection compares it with `Origin`.

### Backup

`concerts backup <file>` writes a consistent snapshot while the app runs (`VACUUM INTO`). Nightly, keeping 14 days:

```sh
sudo cp deploy/concerts-backup.{service,timer} /etc/systemd/system/
sudo systemctl enable --now concerts-backup.timer
```

Snapshots land in `/var/lib/concerts/backups/`; copy them off the server (rsync, restic…).

### Restore a backup

```sh
ls /var/lib/concerts/backups/            # pick a snapshot, e.g. concerts-2026-11-16T040012.db
snap=/var/lib/concerts/backups/concerts-2026-11-16T040012.db
sudo systemctl stop concerts-backup.timer concerts
ts=$(date +%Y%m%dT%H%M%S)
for f in /var/lib/concerts/concerts.db /var/lib/concerts/concerts.db-wal /var/lib/concerts/concerts.db-shm; do
    if sudo test -e "$f"; then sudo mv "$f" "$f.pre-restore-$ts"; fi
done
sudo install -o concerts -g concerts -m 600 "$snap" /var/lib/concerts/concerts.db
sudo systemctl start concerts concerts-backup.timer
for i in $(seq 20); do curl -fsS http://127.0.0.1:8080/healthz && break; sleep 1; done   # else: journalctl -u concerts
```

- Never copy over `concerts.db` while the service runs or next to a leftover `-wal`: SQLite writes the old WAL into the snapshot, corrupting it without any error.
- Writes made since the snapshot stay in the `*.pre-restore-<time>` files.
- An older snapshot is migrated at startup; one from a newer binary is refused (healthz fails, the reason is in the journal).
