#!/usr/bin/env bash
# Prints the measures for a merge commit's body: Go lines (non-test), binary size, `make check` duration.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
echo "go lines (non-test): $(git ls-files --cached --others --exclude-standard '*.go' ':!:*_test.go' | xargs cat | wc -l)"
make --no-print-directory build >/dev/null
stat -c %s bin/concerts | awk '{printf "binary: %.1f MB\n", $1 / 1e6}'
TIMEFORMAT='make check: %R s'
time make --no-print-directory check >/dev/null
