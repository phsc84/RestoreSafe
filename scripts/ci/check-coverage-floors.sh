#!/usr/bin/env bash
# Usage: check-coverage-floors.sh <floors file> <go test -cover output>
# Compares the coverage go test printed per package with the floors and
# fails when a package is below its floor or was not measured.
awk '
NR == FNR {
	sub(/\r$/, "")  # the runner checks the floors file out with CRLF
	if ($0 !~ /^#/ && NF == 2) floor[$1] = $2
	next
}
/coverage: [0-9.]+% of statements/ {
	pkg = $2; sub(/^RestoreSafe\//, "", pkg)
	match($0, /coverage: [0-9.]+%/)
	cov[pkg] = substr($0, RSTART + 10, RLENGTH - 11) + 0
}
END {
	for (p in floor) {
		if (!(p in cov)) {
			printf "::error title=coverage floor::%s: not measured (floor %s %%)\n", p, floor[p]; fail = 1
		} else if (cov[p] < floor[p]) {
			printf "::error title=coverage floor::%s: %.1f %% is below its floor of %s %%\n", p, cov[p], floor[p]; fail = 1
		} else {
			printf "%-36s %5.1f %%  (floor %s %%)\n", p, cov[p], floor[p]
		}
	}
	exit fail
}' "$1" "$2" | sort
exit "${PIPESTATUS[0]}"
