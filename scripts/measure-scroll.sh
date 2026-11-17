#!/usr/bin/env bash
# Scroll smoothness of style.css at REF vs the working tree's, measured on a phone on the same network.
# Compares CSS only: both run on the working tree's HTML. Details: measure-scroll.mjs.
set -euo pipefail

ref=${1:-main}
source "$(dirname "$0")/ui-lib.sh"
ensure_tools
ref_css=$(git -C "$repo" show "$ref:internal/web/static/style.css")
start_demo
printf '%s\n' "$ref_css" >"$work/ref.css"
cp "$repo/internal/web/static/style.css" "$work/work.css"
node "$repo/scripts/measure-scroll.mjs" snapshot "$tools" "http://$addr$link" "$work"
stop_demo
read -r iface lan < <(ip -4 route get 1.1.1.1 | sed -n 's/.* dev \([^ ]*\).* src \([^ ]*\).*/\1 \2/p')
[[ -n ${lan:-} ]] || { echo "no default route: is this machine on a network?"; exit 1; }
node "$repo/scripts/measure-scroll.mjs" serve "$work" "$lan" "$iface" "$ref"
