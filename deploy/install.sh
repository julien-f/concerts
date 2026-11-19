#!/bin/sh
# Installs or updates the service; run as root by make install, rerun after any change to deploy/.
# Usage: install.sh <name> <proxy-user> <base-url> <deploy-public-key-file>
# Never writes inside a directory the service user owns: it could have planted symlinks there.
set -eu
umask 022
cd "$(dirname "$0")"
. ./lib.sh

[ $# = 4 ] || die "usage: install.sh <name> <proxy-user> <base-url> <deploy-public-key-file>"
NAME=$1
PROXY_USER=$2
base_url=${3%/}
key=$4
H=/home/$NAME

[ "$(id -u)" = 0 ] || die "run as root"
check_name name "$NAME"
check_name proxy-user "$PROXY_USER"
check_url "$base_url"
[ "$NAME" != "$PROXY_USER" ] || die "the name and the proxy user must differ"
uid=$(id -u "$PROXY_USER" 2>/dev/null) || die "no user $PROXY_USER"
[ "$uid" != 0 ] || die "the proxy user cannot be root"
host=${base_url#https://}
host=${host%%/*}
grep -Eq '^(ssh-|ecdsa-|sk-)' "$key" || die "$key: not an SSH public key"
[ "$(grep -c . "$key")" = 1 ] || die "$key: one key expected"
command -v sudo >/dev/null || die "sudo is required (the deploy user restarts the service through it)"
command -v curl >/dev/null || die "curl is required (health check)"
if findmnt -no OPTIONS -T /home | tr , '\n' | grep -qx noexec; then die "/home is mounted noexec"; fi
selinux=false
if command -v selinuxenabled >/dev/null && selinuxenabled; then
	selinux=true
	command -v semanage >/dev/null || die "SELinux is enabled: install semanage (policycoreutils-python-utils)"
fi
# A rerun finds its own units; a first run must not take over an existing user or unit.
if ! installed "$NAME" && id "$NAME" >/dev/null 2>&1; then
	die "user $NAME already exists and is not ours: pick another name"
fi

# Units first: they carry the marker, so a run interrupted after this point can be rerun.
for s in $UNITS; do
	sed -e "s/@NAME@/$NAME/g" -e "s/@PROXY_USER@/$PROXY_USER/g" "concerts$s" >"/etc/systemd/system/$NAME$s.new"
	chmod 0644 "/etc/systemd/system/$NAME$s.new"
	mv "/etc/systemd/system/$NAME$s.new" "/etc/systemd/system/$NAME$s"
done
install -d -m 0755 "/etc/systemd/system/$NAME.service.d"
printf '[Service]\nEnvironment=CONCERTS_BASE_URL=%s\n' "$base_url" >"/etc/systemd/system/$NAME.service.d/base-url.conf"
systemctl daemon-reload

# User and home. The home is root-owned so the user cannot swap bin/data/backups/.ssh for symlinks.
id "$NAME" >/dev/null 2>&1 || useradd -r -U -d "$H" -s /bin/sh "$NAME"
usermod -p '*' "$NAME" # no password; not "!", which OpenSSH without PAM treats as locked
[ ! -L "$H" ] || die "$H is a symlink"
# A leftover home (a deleted user's, a reused UID) would be adopted with whatever it holds.
[ ! -e "$H" ] || [ "$(stat -c %U:%G "$H")" = "root:$NAME" ] || die "$H exists and is not the root:$NAME home install.sh creates: move it away first"
install -d -o root -g "$NAME" -m 0750 "$H"
for d in bin data backups; do
	[ ! -L "$H/$d" ] || die "$H/$d is a symlink"
	install -d -o "$NAME" -g "$NAME" -m 0700 "$H/$d"
done
[ ! -L "$H/.ssh" ] || die "$H/.ssh is a symlink"
install -d -o root -g root -m 0755 "$H/.ssh"
# restrict: no forwarding, agent or pty. Root-owned, so the key holder cannot lift it.
printf 'restrict %s\n' "$(cat "$key")" >"$H/.ssh/authorized_keys.new"
chmod 0644 "$H/.ssh/authorized_keys.new"
mv "$H/.ssh/authorized_keys.new" "$H/.ssh/authorized_keys"

if $selinux; then
	# systemd refuses to execute files labelled as home content.
	semanage fcontext -l | grep -q "^$H/bin(/\.\*)?" || semanage fcontext -a -t bin_t "$H/bin(/.*)?"
	restorecon -R "$H/bin"
fi

sshd_cfg=$(sshd -T -C "user=$NAME,host=localhost,addr=127.0.0.1" 2>/dev/null || true)
if echo "$sshd_cfg" | grep -Eq '^(allowusers|allowgroups) '; then
	echo "warning: sshd has AllowUsers/AllowGroups: make sure they let $NAME in" >&2
fi

# sudo grants: exact command lines run by bin/admin. No Cmnd_Alias: alias names are global to sudoers.
S=/usr/bin/systemctl
cat >"/etc/sudoers.d/$NAME.new" <<EOF
# Installed by concerts install.sh; used by $H/bin/admin.
$NAME ALL=(root) NOPASSWD: $S restart $NAME.service, \\
	$S stop $NAME.socket $NAME.service $NAME-backup.timer $NAME-backup.service $NAME-sync.timer $NAME-sync.service, \\
	$S start $NAME.socket $NAME-backup.timer $NAME-sync.timer, \\
	$S reset-failed $NAME.service $NAME.socket $NAME-sync.service, \\
	$S start $NAME-sync.service, \\
	/usr/bin/journalctl -u $NAME-sync.service -n 50 --no-pager, \\
	/usr/bin/journalctl -u $NAME.service -u $NAME-backup.service -u $NAME-sync.service -n 200 --no-pager, \\
	/usr/bin/journalctl -f -u $NAME.service -u $NAME-backup.service -u $NAME-sync.service --no-pager
EOF
chmod 0440 "/etc/sudoers.d/$NAME.new"
visudo -cqf "/etc/sudoers.d/$NAME.new" || { rm -f "/etc/sudoers.d/$NAME.new"; die "sudoers check failed"; }
mv "/etc/sudoers.d/$NAME.new" "/etc/sudoers.d/$NAME"

# Started once the user, home and sudoers exist; a stop/start applies changed units
# (restarting the socket alone drops queued requests).
systemctl stop "$NAME.socket" "$NAME.service"
systemctl reset-failed "$NAME.service" "$NAME.socket" 2>/dev/null || true
systemctl enable --now "$NAME.socket" "$NAME-backup.timer" "$NAME-sync.timer"

# The proxy's configuration is the operator's: print the site block, never edit it.
cat <<EOF
installed $NAME. Add this site to the reverse proxy before the first deploy (Caddy shown; no access log,
paths hold group secrets; keep the original Host header). Only the proxy's user, $PROXY_USER,
can open /run/$NAME.sock.

EOF
sed -e "s/@HOST@/$host/" -e "s/@NAME@/$NAME/" site.caddy
echo
echo "Then set DEPLOY_HOST ?= $NAME@<server> in local.mk and run make deploy."
