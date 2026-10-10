#!/usr/bin/env bash
# Usage: fuzz-targets.sh [seconds per target, default 30]
# Runs every fuzz target of the module for a while, one after the other (go
# test fuzzes one target per run). A failing input is saved by go test under
# the package's testdata/fuzz/<target>/; commit it as a regression test.
set -o pipefail
fuzztime=${1:-30}s
status=0
go test -list '^Fuzz' ./... | awk '
  /^Fuzz/ { names[++n] = $0; next }
  /^ok/   { for (i = 1; i <= n; i++) print $2, names[i]; n = 0 }
' > fuzz-targets.txt || exit 1
while read -r pkg name; do
  echo "== $name ($pkg)"
  if ! go test -run '^$' -fuzz "^${name}\$" -fuzztime "$fuzztime" "$pkg"; then
    status=1
  fi
done < fuzz-targets.txt
rm -f fuzz-targets.txt
exit $status
