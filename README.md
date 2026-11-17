# concerts

A small web app where a group of friends (5–30, typically a WhatsApp group) shares concerts and sees who goes to what. Each group is reached through a secret link (`/g/<secret>`); no accounts.

Design and roadmap: [docs/design.md](docs/design.md).

## Run

Requires Go (see `go.mod`) and gcc (for `go test -race` only).

```sh
make run                                                   # serves on 127.0.0.1:8080, database concerts.db
CONCERTS_BASE_URL=http://localhost:8080 go run ./cmd/concerts new-group "Gig buddies"   # prints the group link
make demo                                                  # or: a fresh demo.db with sample data, served on :8080
```

Environment: `CONCERTS_ADDR` (default `127.0.0.1:8080`; ignored when systemd passes a socket), `CONCERTS_DB` (default `concerts.db`), `CONCERTS_BASE_URL` (public URL; required by `serve`, `new-group`, `rotate-secret`). `concerts` without arguments lists the subcommands (`list-groups`, `rotate-secret`, `delete-member`, `backup`…).

## Test

```sh
make check   # golangci-lint, govulncheck (needs network), then go test -race ./...
```

The pre-commit hook runs lint and tests, plus govulncheck when `go.mod` or `go.sum` is staged (`make deploy` always runs it); enable it once per clone with `git config core.hooksPath .githooks`.

## Deploy

A system user `<name>` (default `concerts`; the examples below use it) holds everything under `/home/<name>` (`bin/`, `data/`, `backups/`); the reverse proxy reaches the app through `/run/<name>.sock`. Only install and uninstall need root; deploys and admin commands log in as `<name>`. Nothing in `deploy/` is specific to one server: the name and the proxy's group are install parameters. Design and rejected options: [ADR 0008](docs/decisions/0008-deployment-under-a-home-user.md).

### Install (root, once and after any change to `deploy/`)

Requires on the server: systemd (257+ for every directive; older ones skip `PrivatePIDs=`), sudo, curl, and a reverse proxy (Caddy shown) whose process can join a group.

```sh
make install ADMIN_HOST=admin@server PROXY_GROUP=caddy BASE_URL=https://concerts.example.com DEPLOY_KEY=~/.ssh/id_ed25519.pub   # NAME=concerts by default
```

- `ADMIN_HOST` is an account with sudo. The script runs from a fresh `mktemp -d` and is idempotent: rerun it to apply changed units, a new base URL or a new deploy key.
- `PROXY_GROUP` owns the socket (`0660`): the proxy's process must be in it, as primary or supplementary group (restart the proxy after adding it).
- It refuses a name already used by a user or unit it did not install, so it never takes over an existing account or system unit; uninstall refuses the same way.
- It creates the user and its home, installs the deploy key (with `restrict`: no forwarding, no pty), the units rendered from `deploy/` (`<name>.socket` and the backup timer enabled) and `/etc/sudoers.d/<name>` (only the `systemctl` and `journalctl` lines `bin/admin` runs).
- It never touches the proxy: it prints the site block (`deploy/site.caddy`) to add by hand before the first deploy. No access log for that site: paths hold group secrets. With Caddy, check that no other `log` covers this host: `caddy adapt --config <Caddyfile> | jq '.apps.http.servers[].logs'`. Keep the original `Host` header (Caddy does by default): cross-origin protection compares it with `Origin`.
- If sshd has `AllowUsers`/`AllowGroups`, add the user (install warns).
- SELinux: install labels `bin/` `bin_t` (systemd refuses to run home content). Not tested with a confined proxy: if `/healthz` fails, look for denials with `sudo ausearch -m avc -ts recent`.

Then `make deploy` (below) and create a group:

```sh
ssh concerts@server 'bin/admin new-group "Gig buddies"'   # prints the group link
```

### Update and roll back

```sh
make deploy DEPLOY_HOST=concerts@server     # build, upload, back up with the old binary, swap, restart, check /healthz
make rollback DEPLOY_HOST=concerts@server   # previous binary and pre-deploy database back
```

- `deploy` keeps the previous binary (`bin/concerts.prev`) and a pre-deploy snapshot (`backups/pre-deploy.db`), taken by the old binary so the new one cannot migrate the live database first. Deploying again after a failed deploy (`/healthz` never passed, and still failing) keeps that pair, so `rollback` still reaches the last good version.
- Requests arriving during a deploy's restart wait in the socket. `/healthz` is checked through the proxy on loopback (port 443, or the base URL's); on failure, the service status and journal tail are printed.
- `rollback` stops everything that could open the database (socket, service, backup), moves the current database and its `-wal`/`-shm` aside (`data/concerts.db.pre-rollback-<time>`: writes made since the deploy are there), restores both, restarts and checks `/healthz`.

### Admin commands

```sh
ssh concerts@server 'bin/admin list-groups'   # any concerts subcommand, with the service's configuration
ssh concerts@server bin/admin logs            # last 200 lines; logs -f follows
```

The outer quotes matter: ssh joins its arguments, so a quoted name would otherwise be split.

### Leaked link

```sh
ssh concerts@server 'bin/admin list-groups'         # find the group id
ssh concerts@server 'bin/admin rotate-secret <id>'  # prints the new link; the old one returns 404 at once, data is kept
```

- Share the new link only where the leaker cannot see it; everyone picks their first name again. What the leaker already saw stays disclosed.
- Review every concert, ticket links included: the intruder may have added or edited them under any first name, an existing one too, and nothing tells such a change from the real person's. Fix or delete them in the app.
- Then remove a first name nobody in the group claims: `bin/admin delete-member <id> "<first-name>"`. Its answers go with it, whoever used it is sent back to the picker, and its concerts lose « ajouté par », so review those first.
- Restoring a backup or `make rollback` brings the old secret back: run `rotate-secret` again afterwards.

### Backup

`concerts-backup.timer` writes a consistent snapshot (`VACUUM INTO`) to `backups/concerts-<time>.db` every night and keeps 14 days; one that fails (e.g. full disk) removes its partial file. Copying them off the server is not set up yet: see the roadmap in [docs/design.md](docs/design.md#roadmap).

### Restore a backup

```sh
ssh concerts@server 'ls backups'
ssh concerts@server 'bin/admin restore backups/concerts-2026-11-16T040012.db'
```

- Same steps as `rollback`, without touching the binary; the current database is kept as `data/concerts.db.pre-restore-<time>`. If a step fails, the service stays stopped and the message gives the command that starts it again.
- Never copy over `concerts.db` by hand while the service runs or next to a leftover `-wal`: SQLite writes the old WAL into the snapshot, corrupting it without any error.
- An older snapshot is migrated at startup; one from a newer binary is refused (`/healthz` fails, the reason is in `bin/admin logs`).

### Uninstall

```sh
make uninstall ADMIN_HOST=admin@server   # NAME=concerts by default
```

Remove the site from the proxy first (its socket goes away), and copy `backups/` off the server: the whole home goes.
