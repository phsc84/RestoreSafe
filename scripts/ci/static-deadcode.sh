#!/usr/bin/env bash
# Usage: static-deadcode.sh
# Fails when deadcode finds an unreachable function (RF-86):
# - with -test: any function that nothing calls, not even a test;
# - without -test: any production function that only tests call, except in
#   the test-support packages testutil and interacttest (RF-70).
# A helper only tests need belongs in a _test.go file or in testutil.
status=0

if ! unreached=$(go tool deadcode -test ./...); then
  exit 1
fi
if [ -n "$unreached" ]; then
  echo "::error title=deadcode::functions nothing calls"
  echo "$unreached"
  status=1
fi

if ! all=$(go tool deadcode ./...); then
  exit 1
fi
# Leave out the test-support packages and what the first check reported.
testonly=$(printf '%s\n' "$all" |
  grep -v -E '^internal[\\/](testutil|workflow[\\/]interact[\\/]interacttest)[\\/]' |
  grep -v -x -F -f <(printf '%s\n' "$unreached") |
  grep -v '^$')
if [ -n "$testonly" ]; then
  echo "::error title=deadcode::production functions only tests call"
  echo "$testonly"
  status=1
fi

exit $status
