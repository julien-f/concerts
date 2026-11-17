# Sourced by install.sh and uninstall.sh (as root), and by make install/uninstall to check arguments locally
# before they reach a remote shell.

# First line of every unit install.sh writes: what is not marked belongs to someone else.
MARKER='# Installed by concerts install.sh, which replaces the NAME and PROXY_USER placeholders.'
UNITS='.socket .service -backup.service -backup.timer'

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
