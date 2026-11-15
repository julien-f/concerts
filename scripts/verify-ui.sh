#!/usr/bin/env bash
# Checks every screen of a fresh `make demo` at 390 px, light and dark:
# 0 axe-core violations, 0 Nu HTML Checker errors, and the group page weight.
set -euo pipefail

PLAYWRIGHT_VERSION=1.64.1
AXE_VERSION=4.14.0
VNU_VERSION=26.11.3
MAX_WEIGHT=40000 # bytes, gzip -9 of HTML + CSS + JS + icons of /g/<secret>

repo=$(git rev-parse --show-toplevel)
# Shared by every worktree: tools live in the main checkout's bin/tools (git-ignored).
tools=$(cd "$(git rev-parse --git-common-dir)/.." && pwd)/bin/tools
stamp="$tools/.versions-$PLAYWRIGHT_VERSION-$AXE_VERSION-$VNU_VERSION"
export PLAYWRIGHT_BROWSERS_PATH="$tools/browsers"

if [[ ! -e $stamp ]]; then
	echo "installing verification tools into $tools"
	mkdir -p "$tools"
	npm_config_cache="$tools/.npm-cache" npm install --silent --no-audit --no-fund --prefix "$tools" \
		"playwright-core@$PLAYWRIGHT_VERSION" "axe-core@$AXE_VERSION" "vnu-jar@$VNU_VERSION"
	"$tools/node_modules/.bin/playwright-core" install chromium-headless-shell
	touch "$stamp"
fi

work=$(mktemp -d)
trap 'kill "$server" 2>/dev/null || true; wait 2>/dev/null || true; rm -rf "$work"' EXIT

make -C "$repo" --no-print-directory demo DEMO_DB="$work/demo.db" ADDR=127.0.0.1:0 >"$work/server.log" 2>&1 &
server=$!
for _ in $(seq 100); do
	grep -q 'msg=listening' "$work/server.log" 2>/dev/null && break
	kill -0 "$server" 2>/dev/null || { cat "$work/server.log"; exit 1; }
	sleep 0.1
done
addr=$(sed -n 's/.*msg=listening addr=\([^ ]*\).*/\1/p' "$work/server.log")
link=$(grep -o '/g/[A-Za-z0-9_-]*' "$work/server.log" | head -1)
[[ -n $addr && -n $link ]] || { cat "$work/server.log"; exit 1; }

node "$repo/scripts/verify-ui.mjs" "$tools" "http://$addr$link" "$work/html" "$MAX_WEIGHT"
java -jar "$tools/node_modules/vnu-jar/build/dist/vnu.jar" --errors-only "$work"/html/*.html
echo "vnu: 0 errors in $(ls "$work"/html/*.html | wc -l) pages"
