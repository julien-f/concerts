# Sourced by install.sh and uninstall.sh (as root), and by make install/uninstall to check arguments locally
# before they reach a remote shell.

# First line of every unit install.sh writes: what is not marked belongs to someone else.
MARKER='# Installed by concerts install.sh, which replaces the NAME and PROXY_USER placeholders.'
UNITS='.socket .service -backup.service -backup.timer -sync.service -sync.timer'

die() {
	echo "${0##*/}: $*" >&2
	exit 1
}

# Names end up in sed, sudoers, unit files and rm paths.
check_name() {
	case $2 in
	'' | [!a-z_]* | *[!a-z0-9_-]*) die "$1 '$2': lowercase letters, digits, _ and -, not starting with a digit or -" ;;
	esac
	[ ${#2} -le 32 ] || die "$1 '$2': 32 characters at most"
}

check_url() {
	case $1 in
	https://*) ;;
	*) die "base URL '$1': must start with https://" ;;
	esac
	case $1 in
	*[!A-Za-z0-9._~:/-]*) die "base URL '$1': letters, digits and . _ ~ : / - only" ;;
	esac
	# One trailing slash is allowed (install.sh strips it); after it, host[:port] only.
	case ${1%/} in
	https:/ | https:// | https://:*) die "base URL '$1': no host name" ;;
	https://*/* | *:) die "base URL '$1': https://host[:port] only, no path" ;;
	https://*:*:* | https://*:*[!0-9]*) die "base URL '$1': the port must be a number" ;;
	https://*:*) port=${1%/}; port=${port##*:}
		[ "${#port}" -le 5 ] && [ "$port" -ge 1 ] && [ "$port" -le 65535 ] || die "base URL '$1': port 1 to 65535" ;;
	esac
}

# True when $1's units are ours; dies if a unit with that name exists that is not.
installed() {
	ours=false
	for s in $UNITS; do
		f=$(systemctl show -P FragmentPath "$1$s" 2>/dev/null || true)
		[ -n "$f" ] || continue
		head -n 1 "$f" | grep -qxF "$MARKER" ||
			die "$f exists and was not installed by concerts: pick another name"
		ours=true
	done
	$ours
}
