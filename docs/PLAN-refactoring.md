# Plan: Refactoring round after 2.1.0

| | |
|---|---|
| Status | Prepared 2026-10-18; baseline and review not started |
| Follows | [SPEC-refactoring.md](SPEC-refactoring.md) |
| Previous round | The round after 2.0.0 (RF-40, RF-65 to RF-97), done 2026-10-15; its plan is in git history (`git show v2.1.0:docs/PLAN-refactoring.md`). The round of 2.0 (RF-1 to RF-64): `git show v2.0.0:docs/PLAN-refactoring.md` |
| Baseline | `dev` at `<commit>`, set when the baseline is measured (2.1.0 is `735395c`) |
| Branch | `dev` |
| Scope | Decided with the review. **No change to the backup format of 2.x** (standing constraint 1). |

## 1. Purpose

RestoreSafe 2.1.0 is released: the round after 2.0.0 left every package at its coverage target and `deadcode` clean, and 2.1.0 added the YubiKey page (GUI spec 10.2), removed the spare YubiKey and `cmd/yubidiag`, and gave CI a tidy check. This round reviews what that feature work added and starts with the points the previous round carried over (section "Carried over from the round after 2.0.0").

New items start at RF-98 (IDs count on across rounds, SPEC-refactoring section 1), grouped by area in blocks of five from there.

## 2. Baseline (measured <date>)

The first two columns are the results of the previous rounds. The column of this round is filled in when the baseline is measured on the round's starting commit (SPEC-refactoring section 4).

| Measure | Round of 2.0, result (2026-10-09, `a5986bb`) | Round after 2.0.0, result (2026-10-15, `7dc1742`) | This round, start |
|---|---|---|---|
| Tracked files / Go lines | 337 / 44,303 | 376 / 47,720 | |
| Go toolchain | go 1.27.2 | go 1.27.2 (the current release) | |
| Dependency updates | not recorded | none for a direct dependency or tool | |
| `go vet`, `gofmt -l` | nothing reported | nothing reported | |
| staticcheck | nothing reported | nothing reported | |
| `deadcode -test` | nothing reported | nothing reported; fails CI since RF-86 | |
| `deadcode` (without `-test`) | not measured | nothing outside the test-support packages; fails CI since RF-86 | |
| `govulncheck` | 0 called, 0 imported; 1 required-only (`x/crypto/openpgp`) | unchanged: 1 required-only, GO-2026-5932 | |
| `go test ./...` | passes, including the 2.0.0 format fixtures | passes, 610 tests; slowest packages `container` 56 s, `e2e` 26 s, `health` 17 s; the whole run 73 s on a slow day of the machine (50 s the day before) | |
| Race, fuzzing | pass in CI | pass in CI (run 38767528308 on `7dc1742`) | |
| Markers (`TODO`, `FIXME`, `XXX`, `HACK`) | none | none | |
| CI | check, tests with coverage floors, race, fuzzing, GUI smoke test | the same five jobs, each one run of `tools/ci` (RF-87), with annotations per failure; about 7 min 20 s, fuzzing the longest; a pre-push hook runs `fast` | |
| Coverage, total | 54.5 % | 56.6 %, every package at its target | |
| Largest production files | `yubikey/fido2.go` 908, `gui/view/strings.go` 516, `archive/build.go` 504 | `archive/build.go` 554, `gui/view/strings.go` 516, `gui/win32/listview.go` 487 | |
| Throughput, SSD (backup / restore) | 329–338 / 255 MiB/s | 217–254 / 144–150 MiB/s, alternating with `v2.0.0` at 252–254 / 142–150 on the same day | |
| Throughput, network share (backup / restore) | 29.3 / 56.9 MiB/s | 22.5 / 33.4 MiB/s (one backup run of three took 266 s instead of 75 s) | |
| Docs index | complete | complete, with SPEC-ci | |

Coverage by package at the result of the previous round (CI run of `7dc1742`): cryptox 94.1, recovery 98.5, yubikey 62.3; config 92.6, logging 91.8, problem 100; manifest 92.4, naming 92.5, container 85.5, setwriter 85.2, catalog 94.0, archive 86.8; fsx 92.9; health 95.1, plan 93.3, restorepoint 90.9, unlock 87.4, job 90.8, backup 85.7, restore 90.3, interact 100, verify 93.9; gui/view 91.0, gui/flow 95.8, gui/widget 85.2; tools/ci/check 98.4.

## 3. Constraints

The standing constraints of SPEC-refactoring section 2 apply. The format fixtures in `internal/format/testdata/v2.0.0/` describe the released format: they never change.

## 4. Carried over from the round after 2.0.0

None of these is an item yet; the review decides, after re-checking each.

- **The cryptography point of SPEC-refactoring 5.4** was not looked at in the previous round. The review starts with it: algorithms, KDF bounds and defaults against current OWASP and RFC 9106 guidance; the defaults are 512 MB, 3 passes and 4 threads ([crypto.go:104](../internal/security/cryptox/crypto.go#L104)). A change to the KDF defaults is a feature with its own spec, not a refactoring item.
- **`archive/build.go` has 554 lines** (still on 2026-10-18), over the 500 of SPEC-refactoring 5.1: the split of RF-68 made it longer by its doc comments. The walk (`tarBuilder` and its methods) could go to a file of its own.
- **The throughput of the development machine varies by a third from day to day** (RF-75). This round compares only with alternating runs of `v2.1.0` on the same day, as SPEC-refactoring 5.3 says, and records both.

## 5. Noted since the previous round

Inputs for the review from the work on 2.1.0; like the carried-over points, not items yet.

- **Coverage that depends on the machine's hardware.** `internal/security/yubikey` measures 72 % on the development machine (a YubiKey plugged in, Yubico entries in the registry) and 61 % on the CI runner, which has neither; a floor raised from the local number broke CI on 2026-10-16. The floor follows the CI number ([test-coverage-floors.txt](../scripts/ci/test-coverage-floors.txt)). SPEC-refactoring 5.5 names tests that depend on the runner's permissions, not on its hardware; a point for it is a candidate when this round closes.
- **CI changes after the previous round** (2026-10-18): the `static` job checks that `go.mod` and `go.sum` are tidy (the 2.1.0 release build stopped on stale `go.sum` lines), and CI runs on pushes only, once per commit, instead of a second time for the release pull request (SPEC-ci 6.1 and 7.1).
- **The code 2.1.0 added has not been reviewed as a round**: the YubiKey page (`internal/gui/yubikeypage.go`, `internal/gui/view/yubikey.go`, `internal/gui/widget/expander.go`), the diagnosis in `internal/security/yubikey`, `internal/workflow/keycheck`, and the device notification in `internal/gui/app.go`.

## 6.-n. Items by area

<RF items in the format of SPEC-refactoring section 6, written after the review>

## n. Found during the work

## Phases

| Phase | Items | When | Why then |
|---|---|---|---|

## Open questions

## Not part of this round

- Editing the configuration in the window and dark mode: feature work, planned for later (GUI spec, decisions 1 and 4).

## Done when

## Carried over (filled in when the round closes)
