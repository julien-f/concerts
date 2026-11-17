#!/usr/bin/env bash
# Checks every screen of a fresh `make demo` at 390 px, light and dark:
# 0 axe-core violations, 0 Nu HTML Checker errors, and the group page weight.
set -euo pipefail

MAX_WEIGHT=40000 # bytes, gzip -9 of HTML + CSS + JS + icons of /g/<secret>

source "$(dirname "$0")/ui-lib.sh"
ensure_tools
start_demo

node "$repo/scripts/verify-ui.mjs" "$tools" "http://$addr$link" "$work/html" "$MAX_WEIGHT"
java -jar "$tools/node_modules/vnu-jar/build/dist/vnu.jar" --errors-only "$work"/html/*.html
echo "vnu: 0 errors in $(ls "$work"/html/*.html | wc -l) pages"
