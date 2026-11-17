# Operations

A system user `<name>` (default `concerts`; the examples below use it) holds everything under `/home/<name>` (`bin/`, `data/`, `backups/`); the reverse proxy reaches the app through `/run/<name>.sock`. Only install and uninstall need root; deploys and admin commands log in as `<name>`. Nothing in `deploy/` is specific to one server: the name and the proxy's user are install parameters. Design and rejected options: [ADR 0008](decisions/0008-deployment-under-a-home-user.md).

## Install (root, once and after any change to `deploy/`)

Requires on the server: systemd (257+ for every directive; older ones skip `PrivatePIDs=`), sudo, curl, and a reverse proxy (Caddy shown) running as its own user.

```sh
make install ADMIN_HOST=admin@server PROXY_USER=caddy BASE_URL=https://concerts.example.com DEPLOY_KEY=~/.ssh/id_ed25519.pub   # NAME=concerts by default
```

- `ADMIN_HOST` is an account with sudo. The script runs from a fresh `mktemp -d` and is idempotent: rerun it to apply changed units, a new base URL or a new deploy key.
- `PROXY_USER`, the user the proxy runs as, owns the socket (`0600`): no group to create or join.
- It refuses a name already used by a user or unit it did not install, so it never takes over an existing account or system unit; uninstall refuses the same way.
- It creates the user and its home, installs the deploy key (with `restrict`: no forwarding, no pty), the units rendered from `deploy/` (`<name>.socket` and the backup timer enabled) and `/etc/sudoers.d/<name>` (only the `systemctl` and `journalctl` lines `bin/admin` runs).
- It never touches the proxy: it prints the site block (`deploy/site.caddy`) to add by hand before the first deploy. No access log for that site: paths hold group secrets. With Caddy, check that no other `log` covers this host: `caddy adapt --config <Caddyfile> | jq '.apps.http.servers[].logs'`. Keep the original `Host` header (Caddy does by default): cross-origin protection compares it with `Origin`.
- If sshd has `AllowUsers`/`AllowGroups`, add the user (install warns).
- SELinux: install labels `bin/` `bin_t` (systemd refuses to run home content). Not tested with a confined proxy: if `/healthz` fails, look for denials with `sudo ausearch -m avc -ts recent`.

Then `make deploy` (below), create a group and send its link with the [member message](#member-message):

```sh
ssh concerts@server 'bin/admin new-group "Gig buddies"'   # prints the group link
```

`new-group` and `rotate-secret` print a working link in your terminal: paste it into the group chat only.

## Member message

To send with the link (in a code block here so the narrow no-break spaces paste intact):

```
Notre appli concerts : <lien>
Touche ton prénom ou tape-le, pas besoin de compte.
Ce lien est la clé : ne le transfère pas hors du groupe, même dans une autre messagerie (elle lit le lien pour en faire un aperçu). Avec lui, n'importe qui peut répondre sous ton prénom.
Tout le monde peut ajouter, modifier ou supprimer un concert. Pas de notif : pense à « Prévenir le groupe ».
Nouveau téléphone ou navigateur : rouvre le lien et touche ton prénom.
Lien partagé par erreur, prénom à corriger ou à retirer : écris à <opérateur>.
```

After `rotate-secret`, replace the first line with: `Nouveau lien, l'ancien ne marche plus : <lien> (rechoisis ton prénom).`

## Update and roll back

```sh
make deploy DEPLOY_HOST=concerts@server     # build, upload, back up with the old binary, swap, restart, check /healthz
make rollback DEPLOY_HOST=concerts@server   # previous binary and pre-deploy database back
```

- `deploy` keeps the previous binary (`bin/concerts.prev`) and a pre-deploy snapshot (`backups/pre-deploy.db`), taken by the old binary so the new one cannot migrate the live database first. Deploying again after a failed deploy (`/healthz` never passed, and still failing) keeps that pair, so `rollback` still reaches the last good version.
- Requests arriving during a deploy's restart wait in the socket. `/healthz` is checked through the proxy on loopback (port 443, or the base URL's); on failure, the service status and journal tail are printed.
- `rollback` stops everything that could open the database (socket, service, backup), moves the current database and its `-wal`/`-shm` aside (`data/concerts.db.pre-rollback-<time>`: writes made since the deploy are there), restores both, restarts and checks `/healthz`.

## Admin commands

```sh
ssh concerts@server 'bin/admin list-groups'   # any concerts subcommand, with the service's configuration
ssh concerts@server bin/admin logs            # last 200 lines, backup runs included; logs -f follows
```

- The outer quotes matter: ssh joins its arguments, so a quoted name would otherwise be split.
- `demo` and `serve` are for development: `demo` adds a sample group to the live database.
- A member asks to change their first name: `bin/admin rename-member <id> "<first-name>" "<new-first-name>"` (`list-groups` gives the id). Answers and « ajouté par » follow; the picker's rules apply and an existing name is refused.

## Monitoring

Nothing alerts on failure (automation: [design.md, Later](design.md#later)). Check by hand now and then:

```sh
curl -fsS https://concerts.example.com/healthz
ssh concerts@server 'ls -t backups | head -3'   # newest nightly snapshot should be under a day old
ssh concerts@server bin/admin logs              # errors, failed backups
```

## Leaked link

```sh
ssh concerts@server 'bin/admin list-groups'         # find the group id
ssh concerts@server 'bin/admin rotate-secret <id>'  # prints the new link; the old one returns 404 at once, data is kept
```

- Share the new link only where the leaker cannot see it; everyone picks their first name again. What the leaker already saw stays disclosed.
- Review every concert, ticket links included: the intruder may have added or edited them under any first name, an existing one too, and nothing tells such a change from the real person's. Fix or delete them in the app.
- Then remove a first name nobody in the group claims: `bin/admin delete-member <id> "<first-name>"`. Its answers go with it, whoever used it is sent back to the picker, and its concerts lose « ajouté par », so review those first.
- Restoring a backup or `make rollback` brings the old secret back: run `rotate-secret` again afterwards.
- Concert links (`/c/…`) survive the rotation and keep showing their public card; deleting a concert is the only way to cut its link.
- Once, after first deploying the public concert link ([ADR 0009](decisions/0009-public-concert-link.md)): rotate every group, since concert links posted before it carry the secret.

## Backup

`concerts-backup.timer` writes a consistent snapshot (`VACUUM INTO`) to `backups/concerts-<time>.db` every night and keeps 14 days; one that fails (e.g. full disk) removes its partial file. Copying them off the server is not set up yet: see [design.md, Later](design.md#later).

Before a risky step, take one by hand (the file must not exist):

```sh
ssh concerts@server 'bin/admin backup backups/manual-2026-11-17.db'
```

Only `concerts-*.db` files are pruned: delete manual ones once done, they hold names and answers.

## Restore a backup

```sh
ssh concerts@server 'ls backups'
ssh concerts@server 'bin/admin restore backups/concerts-2026-11-16T040012.db'
```

- Same steps as `rollback`, without touching the binary; the current database is kept as `data/concerts.db.pre-restore-<time>`. If a step fails, the service stays stopped and the message gives the command that starts it again.
- Never copy over `concerts.db` by hand while the service runs or next to a leftover `-wal`: SQLite writes the old WAL into the snapshot, corrupting it without any error.
- An older snapshot is migrated at startup; one from a newer binary is refused (`/healthz` fails, the reason is in `bin/admin logs`).
- A copy from off the server: only one you made yourself (a tampered database can hold a secret someone knows). Upload it under another prefix than `concerts-`, or the nightly pruning may delete it (`scp -p` keeps its old date): `scp copy.db concerts@server:backups/offsite-2026-11-16.db`, then restore it as above.
- A snapshot older than a `rotate-secret` brings the old link back: rotate again (see [Leaked link](#leaked-link)).

## Change the base URL

- Rerun `make install` with the new `BASE_URL` (https only) and the same `PROXY_USER` and `DEPLOY_KEY`; it stops and restarts the app.
- Add the site block it prints to the proxy, and remove the old host's.
- Group paths keep their secret: send each group its link on the new host. Everyone picks their first name again (the identity cookie belongs to the host).
- If the old domain is dropped and could be registered by someone else, run `rotate-secret` for every group instead: old links would send their secret there.

## Uninstall

```sh
make uninstall ADMIN_HOST=admin@server   # NAME=concerts by default
```

Remove the site from the proxy first (its socket goes away), and copy `backups/` off the server: the whole home goes.
