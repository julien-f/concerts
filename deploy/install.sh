#!/bin/sh
# Installs or updates the concerts service; run as root by make install, rerun after any change to deploy/.
# Usage: install.sh <base-url> <deploy-public-key-file>
# Never writes inside a concerts-owned directory: the user could have planted symlinks there.
set -eu
umask 022
cd "$(dirname "$0")"

[ $# = 2 ] || { echo "usage: $0 <base-url> <deploy-public-key-file>" >&2; exit 2; }
base_url=${1%/}
key=$2
H=/home/concerts

die() { echo "install: $*" >&2; exit 1; }

[ "$(id -u)" = 0 ] || die "run as root"
case $base_url in https://*) ;; *) die "base URL must be https://…" ;; esac
host=${base_url#https://}
host=${host%%/*}
grep -q '^ssh-' "$key" || grep -q '^ecdsa-' "$key" || die "$key: not an SSH public key"
[ "$(grep -c . "$key")" = 1 ] || die "$key: one key expected"
command -v sudo >/dev/null || die "sudo is required (the deploy user restarts the service through it)"
command -v curl >/dev/null || die "curl is required (health check)"
command -v caddy >/dev/null || die "caddy is required"
getent group caddy >/dev/null || die "no caddy group: the socket is shared with it"
[ "$(systemctl show -P Group caddy.service)" = caddy ] || die "caddy.service does not run as group caddy: it could not open /run/concerts.sock"
[ -f /etc/caddy/Caddyfile ] || die "/etc/caddy/Caddyfile missing"
if findmnt -no OPTIONS -T /home | tr , '\n' | grep -qx noexec; then die "/home is mounted noexec"; fi
selinux=false
if command -v selinuxenabled >/dev/null && selinuxenabled; then
	selinux=true
	command -v semanage >/dev/null || die "SELinux is enabled: install semanage (policycoreutils-python-utils)"
fi

# User and home. The home is root-owned so concerts cannot swap bin/data/backups/.ssh for symlinks.
id concerts >/dev/null 2>&1 || useradd -r -U -d $H -s /bin/sh concerts
usermod -p '*' concerts # no password; not "!", which OpenSSH without PAM treats as locked
[ ! -L $H ] || die "$H is a symlink"
install -d -o root -g concerts -m 0750 $H
for d in bin data backups; do
	[ ! -L $H/$d ] || die "$H/$d is a symlink"
	install -d -o concerts -g concerts -m 0700 $H/$d
done
install -d -o root -g root -m 0755 $H/.ssh
# restrict: no forwarding, agent or pty. Root-owned, so the key holder cannot lift it.
printf 'restrict %s\n' "$(cat "$key")" >$H/.ssh/authorized_keys.new
chmod 0644 $H/.ssh/authorized_keys.new
mv $H/.ssh/authorized_keys.new $H/.ssh/authorized_keys

if $selinux; then
	# systemd refuses to execute files labelled as home content.
	semanage fcontext -l | grep -q "^$H/bin(/\.\*)?" || semanage fcontext -a -t bin_t "$H/bin(/.*)?"
	restorecon -R $H/bin
fi

sshd_cfg=$(sshd -T -C user=concerts,host=localhost,addr=127.0.0.1 2>/dev/null || true)
if echo "$sshd_cfg" | grep -Eq '^(allowusers|allowgroups) '; then
	echo "warning: sshd has AllowUsers/AllowGroups: make sure they let concerts in" >&2
fi

# sudo grants: exact command lines used by bin/admin.
cat >/etc/sudoers.d/concerts.new <<'EOF'
# Installed by concerts install.sh; used by /home/concerts/bin/admin.
Cmnd_Alias CONCERTS = \
	/usr/bin/systemctl restart concerts.service, \
	/usr/bin/systemctl stop concerts.socket concerts.service concerts-backup.timer concerts-backup.service, \
	/usr/bin/systemctl start concerts.socket concerts-backup.timer, \
	/usr/bin/systemctl reset-failed concerts.service concerts.socket, \
	/usr/bin/journalctl -u concerts.service -u concerts-backup.service -n 200 --no-pager, \
	/usr/bin/journalctl -f -u concerts.service -u concerts-backup.service --no-pager
concerts ALL=(root) NOPASSWD: CONCERTS
EOF
chmod 0440 /etc/sudoers.d/concerts.new
visudo -cqf /etc/sudoers.d/concerts.new || { rm -f /etc/sudoers.d/concerts.new; die "sudoers check failed"; }
mv /etc/sudoers.d/concerts.new /etc/sudoers.d/concerts

# Units, then a stop/start so a rerun applies changed ones (restarting the socket alone drops queued requests).
install -m 0644 concerts.socket concerts.service concerts-backup.service concerts-backup.timer /etc/systemd/system/
install -d -m 0755 /etc/systemd/system/concerts.service.d
printf '[Service]\nEnvironment=CONCERTS_BASE_URL=%s\n' "$base_url" >/etc/systemd/system/concerts.service.d/base-url.conf
systemctl daemon-reload
systemctl stop concerts.socket concerts.service
systemctl reset-failed concerts.service concerts.socket 2>/dev/null || true
systemctl enable --now concerts.socket concerts-backup.timer

# Caddy site: no log directive (paths hold group secrets); the shared Caddyfile is never edited.
sed "s|concerts.example.com|$host|" concerts.caddy >/etc/caddy/concerts.caddy
chmod 0644 /etc/caddy/concerts.caddy
if grep -Eq '^[[:space:]]*import[[:space:]]+(/etc/caddy/)?concerts\.caddy' /etc/caddy/Caddyfile; then
	out=$(caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile 2>&1) || { echo "$out" >&2; die "caddy validate failed"; }
	systemctl reload caddy
else
	echo "add this line to /etc/caddy/Caddyfile, then: systemctl reload caddy" >&2
	echo "import /etc/caddy/concerts.caddy" >&2
fi

if [ ! -x $H/bin/concerts ]; then echo "installed: now run make deploy DEPLOY_HOST=concerts@<server>"; fi
