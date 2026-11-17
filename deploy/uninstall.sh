#!/bin/sh
# Removes everything install.sh created for <name>, backups included; run as root by make uninstall.
set -eu
cd "$(dirname "$0")"
. ./lib.sh

[ $# = 1 ] || die "usage: uninstall.sh <name>"
NAME=$1
H=/home/$NAME

[ "$(id -u)" = 0 ] || die "run as root"
check_name name "$NAME"
# Before any action: a wrong name must not kill or delete anything of another user.
installed "$NAME" || die "no service $NAME installed by concerts"
[ "$(stat -c %U:%G "$H" 2>/dev/null)" = "root:$NAME" ] || die "$H is not the root:$NAME home install.sh creates"

printf 'Remove the %s site from the reverse proxy first (its socket goes away).\nThis deletes %s, backups included. Type "%s" to confirm: ' "$NAME" "$H" "$NAME"
read -r answer
[ "$answer" = "$NAME" ] || die "aborted"

systemctl disable --now "$NAME.socket" "$NAME-backup.timer" 2>/dev/null || true
systemctl stop "$NAME.service" "$NAME-backup.service" 2>/dev/null || true
loginctl terminate-user "$NAME" 2>/dev/null || true
pkill -KILL -u "$NAME" 2>/dev/null || true
userdel "$NAME"
# userdel -r refuses a home its user does not own.
rm -rf --one-file-system "$H"

for s in $UNITS; do rm -f "/etc/systemd/system/$NAME$s"; done
rm -rf "/etc/systemd/system/$NAME.service.d"
rm -f "/etc/sudoers.d/$NAME" "/run/$NAME.sock"
if command -v selinuxenabled >/dev/null && selinuxenabled && semanage fcontext -l | grep -q "^$H/bin(/\.\*)?"; then
	semanage fcontext -d "$H/bin(/.*)?"
fi
systemctl daemon-reload
for s in $UNITS; do systemctl reset-failed "$NAME$s" 2>/dev/null || true; done
echo "uninstalled $NAME; journal entries stay until journald rotates them"
