# 0008: Deployment under a dedicated home user

## Context

The app runs on a shared personal server whose Caddy already serves other sites. Goals, in order: security, minimal footprint on the machine, easy deployment. The operator has root but wants routine work (deploy, rollback, restore, admin commands) without it.

## Decision

- **Layout**: a system user `<name>` (install parameter, default `concerts`) whose home `/home/<name>` holds `bin/`, `data/` and `backups/`. The home and `.ssh/` are root-owned, the three directories owned by the user: the user cannot swap a path systemd bind-mounts, or that `install.sh` rewrites as root, for a symlink.
- **System unit**, sandboxed with `ProtectHome=tmpfs`: the service sees only `bin/` (read-only) and `data/`, so a compromised app can neither replace its binary nor reach `authorized_keys` or the backups. `PrivatePIDs=` hides the deployer's processes, which share its UID (systemd 257 then passes a wrong `LISTEN_PID`: `listen()` also accepts PID 1). `systemd-analyze security`: 1.1 for the service, 0.4 for the backup.
- **Socket activation**: `/run/<name>.sock`, owned `0600` by the proxy's user (install parameter; not a group, which the server may lack: Caddy's user there has only `nogroup`). Only the proxy connects; requests wait in the socket during a deploy's restart.
- **Narrow sudoers** for `<name>`: exact `systemctl` lines (restart; stop and start for rollback and restore; `reset-failed` after a crash loop) and two `journalctl` lines. Everything else runs as `<name>` through `bin/admin`, over SSH with a `restrict` key.
- **Configuration** lives in the root-owned unit and a drop-in written by `install.sh`; `bin/admin` reads it back with `systemctl show`.
- **Nothing server-specific in the repo**: units and sudoers are rendered from templates (`@NAME@`, `@PROXY_USER@`), and install never touches the proxy, whose setup varies (here a system unit under its own user, config in that user's home): it prints the site block to add by hand. A marker line in the rendered units tells ours apart, so install and uninstall refuse a name owned by another user or unit.

## Rejected

- **User unit with linger**: the mount sandbox needs unprivileged user namespaces (restricted on Ubuntu), the app could rewrite its own unit, `journalctl --user` is empty for a system user, and Caddy could not get a socket it may open without joining the user's group.
- **`EnvironmentFile=` in the home**: systemd reads it as root, so a symlink would hand a root-only `KEY=value` file to the service.
- **Restart without sudo** (the user killing its process with `Restart=always`, or a `.path` unit): no result for the deployer, and rollback cannot stop the service while it swaps the database.
- **polkit**: not always installed, and it gives no access to logs.
- **`IPAddressDeny=`**: the resolver can be on loopback, a private range or link-local (AWS), so it needs per-host exceptions; the prefill SSRF guard is tested.
- **A second user owning the binary**: stronger isolation, but the pre-deploy snapshot needs the database, and uninstall is no longer one home.

## Consequences

- Still root: unit and sudoers changes (rerun `make install`), the base URL, the deploy key, the proxy's site, uninstall.
- The deploy key is no longer a sudo key: stolen, it gives the service user, not root on a shared server.
- Rollback, restore, a crash loop and install still stop the socket: Caddy then logs the requested `/g/<secret>` paths to the journal (threat model).
- Verified in a Fedora 44 container (systemd 259): install twice, deploy, failed deploy and rollback, restore, a real prefill fetch, uninstall; then with a custom name and Caddy run under its own user: the same cycle, and refusals of an existing user, a system unit name, the root group. Not verified: SELinux enforcing with a confined Caddy, Debian/Ubuntu.
