# Plan: deploy under a dedicated home user

Branch `deploy/home-user`; this file is deleted before the merge.

Goals, in order: security, minimal footprint on a shared server (Caddy already serving other sites), routine operations without root. Nothing is deployed yet: no migration from the `/var/lib/concerts` layout.

## Server layout

`useradd -m -r -s /bin/sh concerts`, password `*` (no password login, but not "locked": OpenSSH without PAM refuses key login to `!` accounts).

```
/home/concerts              0700 concerts
├── bin/concerts            binary; bin/concerts.prev is a hard link to the previous one
├── bin/admin               deploy/admin, copied by every deploy
├── data/concerts.db
├── backups/                nightly concerts-*.db (14 days), pre-deploy.db
└── .ssh/authorized_keys    deploy key, `restrict,pty`
```

Root-owned, outside the home (all removed by uninstall):
- `/etc/systemd/system/concerts.socket`, `concerts.service`, `concerts-backup.service`, `concerts-backup.timer`.
- `/etc/systemd/system/concerts.service.d/base-url.conf`: `Environment=CONCERTS_BASE_URL=…`, written by install. Not an `EnvironmentFile=` in the home: systemd reads it as root, so a symlink there would leak a root-only `KEY=value` file into the service.
- `/etc/sudoers.d/concerts` (below).
- `/etc/caddy/concerts.caddy`; install prints the `import` line if the shared Caddyfile lacks it, never edits that file.
- SELinux, when enabled: `bin_t` file context on `/home/concerts/bin` (systemd refuses to exec `user_home_t`).

## Units

- `concerts.socket`: `ListenStream=/run/concerts.sock`, `SocketUser=root`, `SocketGroup=caddy`, `SocketMode=0660`. Only Caddy can connect; connections queue during a restart (no 502, so no `/g/<secret>` in Caddy's error log on deploys).
- `concerts.service` (activated by the socket, not enabled itself):
  - `Type=exec`, `User=concerts`, `ExecStart=/home/concerts/bin/concerts serve`, `Environment=CONCERTS_DB=/home/concerts/data/concerts.db`, `WorkingDirectory=/home/concerts/data`, `Restart=on-failure`.
  - `ProtectHome=tmpfs`, `BindReadOnlyPaths=/home/concerts/bin`, `BindPaths=/home/concerts/data`: the service cannot write its binary, `authorized_keys` or the backups. The binary directory is bound, not the file, so a rename-swapped binary is seen.
  - Existing hardening kept, plus `ProtectProc=invisible`, `ProcSubset=pid`, `PrivateUsers=yes`, `PrivateIPC=yes`, `SystemCallFilter=~@privileged @resources` with `SystemCallErrorNumber=EPERM`, `RestrictAddressFamilies=AF_INET AF_INET6` (the inherited Unix socket is not created by the service).
  - `IPAddressDeny=localhost link-local multicast 10.0.0.0/8 172.16.0.0/12 192.168.0.0/16 100.64.0.0/10 fc00::/7`, `IPAddressAllow=127.0.0.53`: defense in depth behind the prefill SSRF guard; a host whose resolver is elsewhere adds a drop-in (README).
  - Limits: `MemoryHigh=192M`, `MemoryMax=256M`, `GOMEMLIMIT=160MiB`, `TasksMax=64`, `CPUQuota=50%`, `CPUWeight=50`, `IOWeight=50`.
  - Not `PrivatePIDs=`: in a PID namespace the process is PID 1, unlike the `LISTEN_PID` systemd sets (unverified, not worth the risk).
- `concerts-backup.service`: same home sandbox (`BindPaths=` data and backups), keeps `PrivateNetwork=yes`, adds `RestrictAddressFamilies=none`, `Nice=10`, `IOSchedulingClass=idle`.
- `concerts-backup.timer`: unchanged.

## App changes

1. `serve` listens on the socket systemd passes (fd 3) when `LISTEN_PID` is its PID and `LISTEN_FDS=1`, else on `CONCERTS_ADDR` over TCP (development, `make demo`, verify-ui). No library: about 10 lines. Test: a child test process gets a Unix listener as fd 3 through `sh -c 'LISTEN_PID=$$ exec …'`, answers `/healthz` on it.
2. `CONCERTS_ADDR` defaults to `127.0.0.1:8080` instead of `:8080` (every interface): a missing variable no longer serves secret links over plain HTTP to the network.

## Root grants after install

`/etc/sudoers.d/concerts`, exact command lines only, called with `sudo -n` and absolute paths:
- `/usr/bin/systemctl restart concerts.service`
- `/usr/bin/systemctl stop concerts.socket concerts.service concerts-backup.timer` and the matching `start`: rollback and restore swap the database while nothing can open it (a request would socket-activate the service; a backup would create an empty database).
- `/usr/bin/journalctl -u concerts.service -u concerts-backup.service -n 200 --no-pager` and the `-f` variant: the system journal is otherwise root-only, and the `systemd-journal` group would expose every service's logs.

Still needs root: unit, sudoers or Caddy changes (rerun install), the base URL, uninstall.

## Scripts

- `deploy/admin` (POSIX sh, runs on the server as `concerts`, in its home):
  - `admin deploy`: expects `bin/concerts.new`; same safety as today's `make deploy` (snapshot by the old binary, `.prev`, the deploy-failed marker keeping the last good pair), then `restart` and the health check.
  - `admin rollback`, `admin restore <snapshot>`: stop, move the database and its `-wal`/`-shm` aside, install the snapshot, start, health check. Replaces the README's restore subshell.
  - `admin logs [-f]`.
  - Anything else runs the `concerts` CLI with the service's environment (`systemctl show -P Environment concerts.service`): `admin new-group "…"`, `admin rotate-secret 3`, `admin backup backups/x.db`. One source for the configuration: the unit.
  - Health check: `curl -fsS $CONCERTS_BASE_URL/healthz` through Caddy (the deployer cannot open the socket), 20 tries; on failure, the journal tail.
  - `umask 077` (SSH sessions run with 022).
- `deploy/install.sh` (root, idempotent): user, directories, deploy key, units, drop-in, sudoers (`visudo -cf` before moving it in place), SELinux label, Caddy site file, `caddy validate` then reload, `enable --now concerts.socket concerts-backup.timer`. Checks: `noexec` on `/home`, the `caddy` group exists; warns if `sshd -T` shows `AllowUsers`/`AllowGroups` without `concerts`.
- `deploy/uninstall.sh` (root): asks for confirmation (backups are deleted with the home), disables and removes everything install created, `userdel -r`.

## Makefile

- `make install ADMIN_HOST=root-or-sudoer@host BASE_URL=https://… DEPLOY_KEY=~/.ssh/id_ed25519.pub`: copies `deploy/` to a temp dir, runs `install.sh` under sudo.
- `make deploy DEPLOY_HOST=concerts@host`: `vuln build`, `scp` the binary to `bin/concerts.new` (same directory: atomic rename, inherited SELinux label) and `deploy/admin`, then `ssh … bin/admin deploy`.
- `make rollback DEPLOY_HOST=concerts@host`: `ssh … bin/admin rollback`.
- `make uninstall ADMIN_HOST=…`.

## Docs

- README Deploy rewritten around these commands; restore becomes `bin/admin restore`.
- `docs/design.md`: deployment line and threat model (socket, the proxy's error log).
- ADR 0008: home user + system unit + narrow sudoers + socket activation; rejected: user unit with linger, `EnvironmentFile` in the home, `.path`/self-kill restarts, polkit.
- CLAUDE.md structure line for `deploy/`.
- Backup leads (pull with `rrsync -ro`, restic append-only, restore test, staleness alert) in the design roadmap, not implemented.

## Verification

- Go: the socket-activation test; `make check`.
- Ad hoc, not committed (podman is not an approved tool; proposed separately): a Fedora container booting systemd, with sshd, sudo and Caddy: install, deploy, `/healthz` through Caddy, rollback, restore, `admin new-group`, prefill fetch under `IPAddressDeny`, `systemd-analyze security`, uninstall leaving nothing behind.
