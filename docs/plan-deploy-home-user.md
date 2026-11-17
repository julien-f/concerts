# Plan: deploy under a dedicated home user

Branch `deploy/home-user`; this file is deleted before the merge.

Goals, in order: security, minimal footprint on a shared server (Caddy already serving other sites), routine operations without root. Nothing is deployed yet: no migration from the `/var/lib/concerts` layout.

## Server layout

`useradd -r -U -d /home/concerts -s /bin/sh concerts` (no `-m`), password `*` (no password login; `!` would make OpenSSH without PAM refuse the key).

```
/home/concerts              root:concerts 0750
├── bin/                    concerts 0700: concerts, concerts.prev (hard link), admin
├── data/                   concerts 0700: concerts.db
├── backups/                concerts 0700: nightly concerts-*.db (14 days), pre-deploy.db
└── .ssh/authorized_keys    root, 0755/0644: the deploy key with `restrict`
```

The home is root-owned so `concerts` cannot swap `bin`, `data`, `backups` or `.ssh` for symlinks: systemd bind-mounts those paths as root, and install reruns as root. Root never writes inside a `concerts`-owned directory, and nothing uses `-R`. Cost: `concerts` cannot write dotfiles (shell history), harmless here.

Root-owned, outside the home (all removed by uninstall):
- `/etc/systemd/system/concerts.socket`, `concerts.service`, `concerts-backup.service`, `concerts-backup.timer`.
- `/etc/systemd/system/concerts.service.d/base-url.conf`: `Environment=CONCERTS_BASE_URL=…`, written by install. Not an `EnvironmentFile=` in the home: systemd reads it as root.
- `/etc/sudoers.d/concerts`.
- `/etc/caddy/concerts.caddy`; install prints the `import` line if the shared Caddyfile lacks it and never edits that file.
- SELinux, when enabled: `bin_t` file context on `/home/concerts/bin`.

## Units

- `concerts.socket`: `ListenStream=/run/concerts.sock`, `SocketGroup=caddy`, `SocketMode=0660`, `WantedBy=sockets.target`. Only Caddy can connect; requests queue during a deploy's restart.
- `concerts.service`, no `[Install]` (the socket starts it):
  - `Type=exec`, `User=concerts`, `ExecStart=/home/concerts/bin/concerts serve`, `Environment=CONCERTS_DB=/home/concerts/data/concerts.db GOMEMLIMIT=160MiB`, `WorkingDirectory=/home/concerts/data`, `Restart=on-failure`, `RestartSec=1s`.
  - `ProtectHome=tmpfs`, `BindReadOnlyPaths=/home/concerts/bin`, `BindPaths=/home/concerts/data`: the service cannot write its binary, `authorized_keys` or the backups. The directory is bound, not the file, so a rename-swapped binary is seen.
  - Existing hardening kept (minus `StateDirectory=`), plus `ProtectProc=invisible`, `ProcSubset=pid`, `PrivateUsers=yes`, `PrivateIPC=yes`, `SystemCallFilter=~@privileged @resources` with `SystemCallErrorNumber=EPERM`, `RestrictAddressFamilies=AF_INET AF_INET6` (the inherited socket is not created by the service), `LimitCORE=0`.
  - `PrivatePIDs=yes` if the container test shows `LISTEN_PID` still matches: it hides the deployer's SSH processes (same UID) from a compromised app.
  - Limits: `MemoryMax=256M` with `GOMEMLIMIT`, `TasksMax=128`.
  - No `IPAddressDeny=`: the resolver can sit in loopback, private or link-local space (AWS: 169.254.169.253), so it needs per-host exceptions; the SSRF guard is tested.
- `concerts-backup.service`: same home sandbox (`BindPaths=` data and backups; WAL readers write `-shm`), keeps `PrivateNetwork=yes`, adds `RestrictAddressFamilies=none`, `Nice=10`, `IOSchedulingClass=idle`.
- Tested with systemd 259; older versions ignore unknown directives with a warning.

## App changes (done)

`serve` listens on the socket systemd passes, else TCP; `CONCERTS_ADDR` defaults to `127.0.0.1:8080`. Test: a child test process serves `/healthz` on an inherited Unix socket and leaves the socket file.

## Root grants after install

`/etc/sudoers.d/concerts`, `NOPASSWD`, exact command lines, called with `sudo -n` and absolute paths:
- `/usr/bin/systemctl restart concerts.service`
- `/usr/bin/systemctl stop concerts.socket concerts.service concerts-backup.timer concerts-backup.service` (a running backup would delete the restored database's `-wal` by name on close) and `start concerts.socket concerts-backup.timer`.
- `/usr/bin/systemctl reset-failed concerts.service concerts.socket`: a crash loop hits the start limit and fails the socket; `admin` resets before starting.
- `/usr/bin/journalctl -u concerts.service -u concerts-backup.service -n 200 --no-pager` and `-f … --no-pager`.

Still needs root: unit, sudoers or Caddy changes (rerun install), the base URL, the deploy key, uninstall.

## Scripts

- `deploy/admin` (POSIX sh, on the server as `concerts`, from its home; `umask 077`; `set -e` with an EXIT trap printing the recovery command):
  - Environment: `eval "export $(systemctl show -P Environment concerts.service)"` (root-written units; quoting checked on systemd 259); fails if `CONCERTS_BASE_URL` is empty.
  - `admin deploy`: expects `bin/concerts.new`; snapshot by the old binary, `ln -f` to `.prev`, the deploy-failed marker keeping the last good pair (as today), `mv` the new binary, `reset-failed`, `restart`, health check.
  - `admin rollback`, `admin restore <backups/…>`: stop, move the database and `-wal`/`-shm` aside, `cp` the snapshot to `.new` then `mv`, binary via `ln` + `mv` (rollback), `reset-failed`, start, health check. The snapshot must be a file under `backups/`.
  - `admin logs [-f]`.
  - Anything else runs the `concerts` CLI with that environment: `admin new-group "…"`, `admin rotate-secret 3`.
  - Health check: `curl -fsS --max-time 5 --resolve host:port:127.0.0.1 $CONCERTS_BASE_URL/healthz` (through Caddy on loopback, no hairpin NAT), 20 tries; on failure `systemctl status` and the journal tail.
- `deploy/install.sh` (root, idempotent):
  - Checks: `sudo`, `caddy` group and `systemctl show -P Group caddy`, `noexec` on `/home`, `semanage` when SELinux is enabled; warns if `sshd -T -C user=concerts,…` has `allowusers`/`allowgroups` without `concerts`.
  - User, home (`install -d` for `root:concerts 0750`, then the three `concerts` dirs in it), `.ssh` and `authorized_keys` replaced with the given key, units, drop-in, sudoers (`visudo -cf` on a temp file, then `mv`), SELinux label, Caddy site file, `caddy validate` then reload.
  - Units: `daemon-reload`, then stop socket and service and `enable --now concerts.socket concerts-backup.timer`, so a rerun applies changed units.
- `deploy/uninstall.sh` (root): refuses while the Caddyfile imports `concerts.caddy` (a missing import breaks every site's reload); asks for confirmation (backups go too); stops and disables the units, `loginctl terminate-user`, `userdel`, `rm -rf --one-file-system /home/concerts`, removes what install wrote, the socket file and the SELinux rule; `daemon-reload`, `reset-failed`, reloads Caddy.

## Makefile

- `make install ADMIN_HOST=… BASE_URL=https://… DEPLOY_KEY=~/.ssh/id_ed25519.pub`: `mktemp -d` on the server (no predictable path in `/tmp` for root to run), `scp` `deploy/` and the key into it, `ssh -t` to run `install.sh` under sudo, remove the dir.
- `make uninstall ADMIN_HOST=…`: same transport.
- `make deploy DEPLOY_HOST=concerts@host`: `vuln build`, `scp` to `bin/concerts.new` and `bin/admin.new`, then `ssh … 'mv bin/admin.new bin/admin && bin/admin deploy'`.
- `make rollback DEPLOY_HOST=concerts@host`: `ssh … bin/admin rollback`.

## Docs

- README Deploy rewritten around these commands; restore becomes `bin/admin restore`.
- `docs/design.md`: deployment line; threat model: no 502 on a deploy, but rollback, restore, a crash loop or install still make Caddy log `/g/<secret>` paths to the journal, readable by root and by the `adm`/`wheel`/`systemd-journal` groups depending on the distro; backup leads in the roadmap.
- ADR 0008: root-owned home + system unit + narrow sudoers + socket activation; rejected: user unit with linger, `EnvironmentFile` in the home, `.path`/self-kill restarts, polkit, `IPAddressDeny`, a second user for the binary.
- CLAUDE.md structure line for `deploy/`.

## Verification

- Go: the socket-activation test; `make check`.
- Ad hoc, not committed (podman is not an approved tool): a Fedora container booting systemd with sshd, sudo and Caddy: install, rerun install, deploy, `/healthz` through Caddy, rollback, restore, `admin new-group`, a prefill fetch, `PrivatePIDs`, writes through `BindPaths` under `ProtectSystem=strict`, `systemd-analyze security`, uninstall leaving nothing.
- Needs a real host, listed as unverified: SELinux (Fedora's Caddy may be confined and denied the socket), Debian/Ubuntu specifics.
