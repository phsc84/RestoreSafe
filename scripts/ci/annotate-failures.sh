#!/usr/bin/env bash
# Usage: annotate-failures.sh <go test output>
# Reports the failing tests and data races of a go test log as one GitHub
# error annotation, so they show on the pull request without opening the log.
log=$1
lines=$(grep -E -A 25 -- 'WARNING: DATA RACE' "$log" | head -n 120
        grep -E -- '--- FAIL|^FAIL|^panic:|_test\.go:[0-9]+:|^\s+\S+\.go:[0-9]+: ' "$log" | head -n 80)
[ -z "$lines" ] && lines=$(tail -n 40 "$log")
# Workflow commands need %, CR and LF escaped.
msg=$(printf '%s' "$lines" | sed -e 's/%/%25/g' -e 's/\r/%0D/g' | awk 'BEGIN{ORS="%0A"} {print}')
echo "::error title=go test failed::$msg"
