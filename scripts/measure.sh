#!/usr/bin/env bash
# Prints the measures logged per slice: Go lines (non-test), binary size, `make check` duration.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
lines=$(git ls-files '*.go' ':!:*_test.go' | xargs cat | wc -l)
make --no-print-directory build >/dev/null
size=$(stat -c %s bin/concerts)
start=$(date +%s.%N)
make --no-print-directory check >/dev/null
check=$(echo "$(date +%s.%N) - $start" | bc)
printf 'go lines (non-test): %d\nbinary: %.1f MB\nmake check: %.1f s\n' "$lines" "$(echo "$size / 1000000" | bc -l)" "$check"
