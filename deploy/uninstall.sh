#!/bin/sh
# Removes everything install.sh created, backups included; run as root by make uninstall.
set -eu
H=/home/concerts

die() { echo "uninstall: $*" >&2; exit 1; }

[ "$(id -u)" = 0 ] || die "run as root"
# A missing import fails every site's reload.
if grep -Eq '^[[:space:]]*import[[:space:]]+(/etc/caddy/)?concerts\.caddy' /etc/caddy/Caddyfile 2>/dev/null; then
	die "remove the concerts.caddy import from /etc/caddy/Caddyfile first"
fi
printf 'This deletes %s, backups included. Type "concerts" to confirm: ' $H
read -r answer
[ "$answer" = concerts ] || die "aborted"

systemctl disable --now concerts.socket concerts-backup.timer 2>/dev/null || true
systemctl stop concerts.service concerts-backup.service 2>/dev/null || true
loginctl terminate-user concerts 2>/dev/null || true
pkill -KILL -u concerts 2>/dev/null || true
if id concerts >/dev/null 2>&1; then userdel concerts; fi
# userdel -r refuses a home its user does not own.
rm -rf --one-file-system $H

rm -f /etc/systemd/system/concerts.socket /etc/systemd/system/concerts.service \
	/etc/systemd/system/concerts-backup.service /etc/systemd/system/concerts-backup.timer \
	/etc/sudoers.d/concerts /etc/caddy/concerts.caddy /run/concerts.sock
rm -rf /etc/systemd/system/concerts.service.d
if command -v selinuxenabled >/dev/null && selinuxenabled && semanage fcontext -l | grep -q "^$H/bin(/\.\*)?"; then
	semanage fcontext -d "$H/bin(/.*)?"
fi
systemctl daemon-reload
systemctl reset-failed concerts.service concerts.socket concerts-backup.service concerts-backup.timer 2>/dev/null || true
systemctl reload caddy
echo "uninstalled; journal entries stay until journald rotates them"
