# Plan: no server-specific value in deploy/

Branch `deploy/configurable-name`; this file is deleted before the merge.

Goal: the repo holds no value specific to one server. Today `concerts` (user, home, units, socket, sudoers) and the Caddy setup (`caddy` group and unit, `/etc/caddy/Caddyfile`, reload) are fixed. On the target server Caddy runs as a system unit under its own user, with its config in that user's home, managed by hand.

## Parameters of `make install`

- `NAME` (default `concerts`): user, `/home/$NAME`, units `$NAME.socket`, `$NAME.service`, `$NAME-backup.service`, `$NAME-backup.timer`, `/run/$NAME.sock`, `/etc/sudoers.d/$NAME`, drop-in `$NAME.service.d/base-url.conf`.
- `PROXY_GROUP` (required): the group the reverse proxy runs as; owns the socket (`0660`).
- `BASE_URL`, `DEPLOY_KEY`: unchanged.
- Both names checked against `^[a-z_][a-z0-9_-]{0,31}$` before any use: they end up in sed, sudoers and unit files written as root.

## Rendering

- `deploy/*.socket|service|timer` and the sudoers lines become templates with `@NAME@` and `@PROXY_GROUP@`; `install.sh` renders them with `sed` into `/etc/systemd/system/$NAME…`. `bin/concerts` (the app binary) keeps its name.
- `bin/admin` takes the name from `id -un` (it runs as that user) and its home from `$HOME`; no other change.
- `make deploy`/`rollback` unchanged: the name is in `DEPLOY_HOST=<name>@server`.

## Caddy: by hand

- `install.sh` no longer writes, validates or reloads anything of Caddy, and no longer checks `caddy`, its unit or `/etc/caddy`. It prints the site block to add (`deploy/site.caddy` rendered with the host and socket path).
- It checks `PROXY_GROUP` exists. `getent group`, nothing else about the proxy.
- `uninstall.sh NAME` drops the Caddyfile check; its confirmation asks to remove the site block first (the socket goes away). `make uninstall NAME=…`.
- Health check unchanged: through the proxy on loopback port 443 (or the base URL's port).

## Docs

README Deploy (parameters, Caddy by hand), ADR 0008 (configuration section), CLAUDE.md structure line if needed.

## Verification

Replay the container cycle with `NAME=gigs` and Caddy run as a system unit under a user `web` with its Caddyfile in `/home/web`: install, deploy twice, rollback, restore, uninstall leaving nothing.
