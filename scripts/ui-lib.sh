# Sourced by verify-ui.sh and measure-scroll.sh, which set `set -euo pipefail`.
# start_demo creates $work and owns the only EXIT trap: callers set no trap and keep files under $work.

PLAYWRIGHT_VERSION=1.64.1
AXE_VERSION=4.14.0
VNU_VERSION=26.11.3

repo=$(git rev-parse --show-toplevel)
# Shared by every worktree: tools live in the main checkout's bin/tools (git-ignored).
tools=$(cd "$(git rev-parse --git-common-dir)/.." && pwd)/bin/tools
export PLAYWRIGHT_BROWSERS_PATH="$tools/browsers"

ensure_tools() {
	local stamp="$tools/.versions-$PLAYWRIGHT_VERSION-$AXE_VERSION-$VNU_VERSION"
	[[ -e $stamp ]] && return
	echo "installing verification tools into $tools"
	mkdir -p "$tools"
	npm_config_cache="$tools/.npm-cache" npm install --silent --no-audit --no-fund --prefix "$tools" \
		"playwright-core@$PLAYWRIGHT_VERSION" "axe-core@$AXE_VERSION" "vnu-jar@$VNU_VERSION"
	"$tools/node_modules/.bin/playwright-core" install chromium-headless-shell
	touch "$stamp"
}

# Serves a fresh `make demo` on a free local port; sets $addr and $link (the group page path).
start_demo() {
	work=$(mktemp -d)
	trap 'stop_demo; rm -rf "$work"' EXIT
	# Built first: a cold build (about 11 s) would eat the wait below.
	make -C "$repo" --no-print-directory build >/dev/null
	# Own process group, so stop_demo also stops the server make started.
	setsid sh -c 'echo $$ >"$0"; exec make -C "$1" --no-print-directory demo DEMO_DB="$2" ADDR=127.0.0.1:0' \
		"$work/demo.pid" "$repo" "$work/demo.db" >"$work/server.log" 2>&1 &
	for _ in $(seq 100); do
		grep -q 'msg=listening' "$work/server.log" 2>/dev/null && break
		[[ -s $work/demo.pid ]] && ! kill -0 "$(cat "$work/demo.pid")" 2>/dev/null && { cat "$work/server.log"; exit 1; }
		sleep 0.1
	done
	addr=$(sed -n 's/.*msg=listening addr=\([^ ]*\).*/\1/p' "$work/server.log")
	link=$(grep -o '/g/[A-Za-z0-9_-]*' "$work/server.log" | head -1)
	[[ -n $addr && -n $link ]] || { cat "$work/server.log"; exit 1; }
}

stop_demo() {
	[[ -s ${work:-}/demo.pid ]] || return 0
	kill -- "-$(cat "$work/demo.pid")" 2>/dev/null || true
	rm -f "$work/demo.pid"
	wait 2>/dev/null || true
}
