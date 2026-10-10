# Plan: Refactoring round after 2.0.0

| | |
|---|---|
| Status | Started 2026-10-10 with the carried-over item RF-40; the baseline, the review, and the new items follow (SPEC-refactoring section 3, steps 2 to 6) |
| Follows | [SPEC-refactoring.md](SPEC-refactoring.md) |
| Previous round | The round of 2.0 (RF-1 to RF-64), done 2026-10-09; its plan is in git history (`git show v2.0.0:docs/PLAN-refactoring.md`) |
| Baseline | `dev` at `<commit>`, to be measured when the review starts |
| Branch | `dev` |
| Scope | To be set with the review. **No change to the backup format of 2.x** (standing constraint 1). |

## 1. Purpose

RestoreSafe 2.0.0 is released: a layered codebase with CI, coverage floors per package, format fixtures, fuzzing and a fault-injection matrix, all from the round of 2.0. This round starts from what that round left open, and looks for what the first months after the release show.

New items start at RF-65 (IDs count on across rounds, SPEC-refactoring section 1).

## 2. Baseline

One column per round. The column of this round is filled in when its review starts.

| Measure | 2.0 round, start (2026-10-06) | 2.0 round, result (2026-10-09, `a5986bb`) | This round, start |
|---|---|---|---|
| Tracked files / Go lines | 262 / 40,636 | 337 / 44,303 | |
| Go toolchain | go 1.27.1 | go 1.27.2 | |
| `go vet`, `gofmt -l` | vet clean; 18 files (CRLF) | nothing reported | |
| staticcheck | 10 findings besides ST1005 | nothing reported | |
| `deadcode -test` | 16 unreachable functions | nothing reported | |
| `govulncheck` | 0 called; 1 required-only | 0 called, 0 imported; 1 required-only (`x/crypto/openpgp`) | |
| `go test ./...` | passes; `-cover` failed in `gui/view` | passes, including the 2.0.0 format fixtures; race and fuzzing pass in CI | |
| Markers (`TODO`, `FIXME`, `XXX`, `HACK`) | not counted | none | |
| CI | none | check, tests with coverage floors, race, fuzzing, GUI smoke test | |
| Coverage, total | 46.1 % | 54.5 % | |
| Largest production files | `gui/backups.go` 948, `gui/wizard.go` 931, `yubikey/fido2.go` 917 | `yubikey/fido2.go` 908, `gui/view/strings.go` 516, `archive/build.go` 504 | |

Coverage by package at the result of the 2.0 round: cryptox 89.1, recovery 94.0, yubikey 36.4; config 92.6, logging 88.5, problem 100; manifest 92.6, naming 92.5, container 85.5, setwriter 85.2, catalog 81.2, archive 78.1; fsx 82.7; health 95.1, plan 93.3, restorepoint 90.9, unlock 84.7, job 84.3, backup 83.3, restore 79.1, interact 78.3, verify 73.2; gui/view 91.0, gui/flow 77.0, gui/widget 73.6, gui/win32 10.4, gui 2.0; cmd/restoresafe 33.3.

## 3. Constraints

The standing constraints of SPEC-refactoring section 2 apply. The format fixtures in `internal/format/testdata/v2.0.0/` now describe the released format: they never change.

## 4. Quality assurance

**RF-40 (P1) Coverage targets per package** (carried over from the round of 2.0). CI enforces a floor per package (`scripts/ci/coverage-floors.txt`, checked by `check-coverage-floors.sh`); the targets set in the round of 2.0 are not reached everywhere. Below the target on 2026-10-09: cryptox 89.1 (90), archive 78.1, catalog 81.2, backup 83.3, restore 79.1, verify 73.2, unlock 84.7, job 84.3, interact 78.3, fsx 82.7, gui/flow 77.0, gui/widget 73.6 (85 each), yubikey 36.4 (60; everything except the WebAuthn calls, behind the `fido2MakeCredFn`/`fido2GetHmacFn` seams).
Change: tests for the error paths first, starting with verify and restore, which decide whether damage is reported; raise each floor to its target once it is reached. Re-check the numbers at this round's baseline.
Acceptance: every package at or above its target, and the floors in CI raised to match.

## Phases

To be planned with the review.

## Open questions

None yet.

## Not part of this round

- Any change to container format 2, the key hierarchy, or the KDF defaults.
- Replacing the Win32 GUI with a toolkit, or `logging` with `log/slog`.
- Supporting platforms other than Windows 64-bit.

## Done when

To be set with the review; at least: RF-40 done, and every P1 and P2 item done or dropped with a reason.

## Carried over (filled in when the round closes)
