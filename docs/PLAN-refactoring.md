# Plan: Refactoring round after 2.1.0

| | |
|---|---|
| Status | Prepared 2026-10-18; baseline measured 2026-10-18, review not started |
| Follows | [SPEC-refactoring.md](SPEC-refactoring.md) |
| Previous round | The round after 2.0.0 (RF-40, RF-65 to RF-97), done 2026-10-15; its plan is in git history (`git show v2.1.0:docs/PLAN-refactoring.md`). The round of 2.0 (RF-1 to RF-64): `git show v2.0.0:docs/PLAN-refactoring.md` |
| Baseline | `dev` at `139ab93` (2.1.0 is `735395c`; since then only `tools/ci` changed in Go) |
| Branch | `dev` |
| Scope | Decided with the review. **No change to the backup format of 2.x** (standing constraint 1). |

## 1. Purpose

RestoreSafe 2.1.0 is released: the round after 2.0.0 left every package at its coverage target and `deadcode` clean, and 2.1.0 added the YubiKey page (GUI spec 10.2), removed the spare YubiKey and `cmd/yubidiag`, and gave CI a tidy check. This round reviews what that feature work added and starts with the points the previous round carried over (section "Carried over from the round after 2.0.0").

New items start at RF-98 (IDs count on across rounds, SPEC-refactoring section 1), grouped by area in blocks of five from there.

## 2. Baseline (measured 2026-10-18)

The first two columns are the results of the previous rounds. The column of this round was measured on `139ab93` (SPEC-refactoring section 4): the CI measures from run 39147764145, the rest locally. Throughput ran in `sandbox\bench` on the SSD and on `M:\RestoreSafe-test`, nothing else running. On the SSD, the code was compared with `7dc1742`, the previous round's result, not with `v2.1.0`: since `v2.1.0` only `tools/ci` changed, so that comparison would measure the same code twice. Backup is within the machine's own variance; restore is 6 % lower in both rounds, which the review looks at when it profiles restore (SPEC-refactoring 5.3).

| Measure | Round of 2.0, result (2026-10-09, `a5986bb`) | Round after 2.0.0, result (2026-10-15, `7dc1742`) | This round, start |
|---|---|---|---|
| Tracked files / Go lines | 337 / 44,303 | 376 / 47,720 | 390 / 50,789 |
| Go toolchain | go 1.27.2 | go 1.27.2 (the current release) | go 1.27.2 (the current release) |
| Dependency updates | not recorded | none for a direct dependency or tool | none for a direct dependency (`x/crypto` 0.58.0, `x/sys` 0.49.0, `yaml` 3.0.5) or tool (staticcheck 0.8.2, govulncheck 1.9.0, `x/tools` 0.52.0, goversioninfo 1.7.0); updates only for modules in the graph, not in `go.mod` (testify, go-spew, goldmark, `x/exp`, `renameio`) |
| `go vet`, `gofmt -l` | nothing reported | nothing reported | nothing reported; `go mod tidy -diff` too (in CI since 2026-10-18) |
| staticcheck | nothing reported | nothing reported | nothing reported |
| `deadcode -test` | nothing reported | nothing reported; fails CI since RF-86 | nothing reported |
| `deadcode` (without `-test`) | not measured | nothing outside the test-support packages; fails CI since RF-86 | nothing outside the test-support packages |
| `govulncheck` | 0 called, 0 imported; 1 required-only (`x/crypto/openpgp`) | unchanged: 1 required-only, GO-2026-5932 | unchanged: 1 required-only, GO-2026-5932 (`x/crypto` 0.58.0, no fix) |
| `go test ./...` | passes, including the 2.0.0 format fixtures | passes, 610 tests; slowest packages `container` 56 s, `e2e` 26 s, `health` 17 s; the whole run 73 s on a slow day of the machine (50 s the day before) | passes, 637 tests (801 with subtests; `7dc1742` counted the same way today: 601 and 758); slowest packages `container` 45 s, `backup` 35 s, `e2e` 22 s, `gui/view` 18 s; the whole run 62 s |
| Race, fuzzing | pass in CI | pass in CI (run 38767528308 on `7dc1742`) | pass in CI (run 39147764145 on `139ab93`), 11 fuzz targets |
| Markers (`TODO`, `FIXME`, `XXX`, `HACK`) | none | none | none |
| CI | check, tests with coverage floors, race, fuzzing, GUI smoke test | the same five jobs, each one run of `tools/ci` (RF-87), with annotations per failure; about 7 min 20 s, fuzzing the longest; a pre-push hook runs `fast` | the same five jobs, once per commit (on push only); 7 min 20 s, fuzzing 7 min 20 s, race 2 min 15 s, test 2 min, static 1 min 50 s, gui 1 min 10 s |
| Coverage, total | 54.5 % | 56.6 %, every package at its target | 57.3 % in CI (57.6 % locally, with a YubiKey plugged in), every package at its floor |
| Largest production files | `yubikey/fido2.go` 908, `gui/view/strings.go` 516, `archive/build.go` 504 | `archive/build.go` 554, `gui/view/strings.go` 516, `gui/win32/listview.go` 487 | `gui/view/strings.go` 659, `archive/build.go` 554, `gui/view/createpage.go` 493, `gui/win32/listview.go` 487, `gui/view/result.go` 485 |
| Throughput, SSD (backup / restore) | 329–338 / 255 MiB/s | 217–254 / 144–150 MiB/s, alternating with `v2.0.0` at 252–254 / 142–150 on the same day | 266–315 / 148–176 MiB/s, alternating with `7dc1742` at 231–306 / 157–188 on the same data (two rounds, median of 3 runs each; restore 6 % lower than `7dc1742` in both rounds, under the 10 % of SPEC-refactoring 5.3) |
| Throughput, network share (backup / restore) | 29.3 / 56.9 MiB/s | 22.5 / 33.4 MiB/s (one backup run of three took 266 s instead of 75 s) | 26.6 / 54.6 MiB/s (runs within 3 %; `dev` only, as in the previous round) |
| Docs index | complete | complete, with SPEC-ci | complete; `PLAN-yubikey-page.md` deleted after its release |

Coverage by package at the result of the previous round (CI run of `7dc1742`): cryptox 94.1, recovery 98.5, yubikey 62.3; config 92.6, logging 91.8, problem 100; manifest 92.4, naming 92.5, container 85.5, setwriter 85.2, catalog 94.0, archive 86.8; fsx 92.9; health 95.1, plan 93.3, restorepoint 90.9, unlock 87.4, job 90.8, backup 85.7, restore 90.3, interact 100, verify 93.9; gui/view 91.0, gui/flow 95.8, gui/widget 85.2; tools/ci/check 98.4.

Coverage by package at the start of this round (CI run of `139ab93`): cryptox 94.1, recovery 98.5, yubikey 61.3; config 94.0, logging 91.8, problem 100; manifest 92.4, naming 92.5, container 85.7, setwriter 85.2, catalog 94.0, archive 86.8; fsx 92.9; health 95.5, plan 93.3, restorepoint 90.9, unlock 89.5, job 90.8, backup 86.9, restore 90.3, interact 100, verify 93.9, keycheck 100 (new); gui/view 92.7, gui/flow 95.9, gui/widget 86.4, gui/win32 10.4, gui 1.8; cmd/restoresafe 33.3; tools/ci/check 98.4.

## 3. Constraints

The standing constraints of SPEC-refactoring section 2 apply. The format fixtures in `internal/format/testdata/v2.0.0/` describe the released format: they never change.

## 4. Carried over from the round after 2.0.0

None of these is an item yet; the review decides, after re-checking each.

- **The cryptography point of SPEC-refactoring 5.4** was not looked at in the previous round. The review starts with it: algorithms, KDF bounds and defaults against current OWASP and RFC 9106 guidance; the defaults are 512 MB, 3 passes and 4 threads ([crypto.go:104](../internal/security/cryptox/crypto.go#L104)). A change to the KDF defaults is a feature with its own spec, not a refactoring item. **That spec now exists:** [SPEC-kdf.md](SPEC-kdf.md) (Draft 0.3, 2026-10-24, shared with MountSafe) sets the new defaults (4 GiB, t=2, p=4, measured), the enrollment and read limits and the memory check; it is implemented as a feature for 2.2.0 (SPEC-kdf 5, K4). What remains for the review is the rest of 5.4: the algorithms themselves.
- **`archive/build.go` has 554 lines** (still on 2026-10-18), over the 500 of SPEC-refactoring 5.1: the split of RF-68 made it longer by its doc comments. The walk (`tarBuilder` and its methods) could go to a file of its own.
- **The throughput of the development machine varies by a third from day to day** (RF-75). This round compares only with alternating runs of `v2.1.0` on the same day, as SPEC-refactoring 5.3 says, and records both.

## 5. Noted since the previous round

Inputs for the review from the work on 2.1.0; like the carried-over points, not items yet.

- **Coverage that depends on the machine's hardware.** `internal/security/yubikey` measures 72 % on the development machine (a YubiKey plugged in, Yubico entries in the registry) and 61 % on the CI runner, which has neither; a floor raised from the local number broke CI on 2026-10-16. The floor follows the CI number ([test-coverage-floors.txt](../scripts/ci/test-coverage-floors.txt)). SPEC-refactoring 5.5 names tests that depend on the runner's permissions, not on its hardware; a point for it is a candidate when this round closes.
- **CI changes after the previous round** (2026-10-18): the `static` job checks that `go.mod` and `go.sum` are tidy (the 2.1.0 release build stopped on stale `go.sum` lines), and CI runs on pushes only, once per commit, instead of a second time for the release pull request (SPEC-ci 6.1 and 7.1).
- **The code 2.1.0 added has not been reviewed as a round**: the YubiKey page (`internal/gui/yubikeypage.go`, `internal/gui/view/yubikey.go`, `internal/gui/widget/expander.go`), the diagnosis in `internal/security/yubikey`, `internal/workflow/keycheck`, and the device notification in `internal/gui/app.go`.
- **Retention right after a backup, on a NAS whose flushes are not durable** (found 2026-10-24 by the MountSafe spike on the Mac mini share, MountSafe SPEC-core 14.5 item 7). The Mac's SMB server confirms `FlushFileBuffers` after about an `fsync` (0.70 ms over the network; `F_FULLFSYNC` on the Mac takes 3.07 ms), so the last writes it confirmed can be lost when the Mac loses power. A backup counts as complete when its last flush returns, and retention then deletes older backups in the same run (**Clean up**, GUI spec BR-2); a power loss shortly after can leave the new set damaged and the older ones already gone. `verify_after_backup` does not help: it may read back from the Mac's cache. To look at: run retention at the start of the next run, after the newest set has been checked again (`health`), or keep the previous complete set until the next run has checked the newest one. A change of when retention deletes is a behaviour change with a CHANGELOG entry; the format does not change.
- **The backup lock after a network drop** (same spike, MountSafe SPEC-core 6.6). A RestoreSafe run that ends while the NAS is unreachable (killed, crashed, PC switched off) leaves its open of the lock file on the Mac until the SMB durable timeout, about 3 minutes after the connection is back; until then the next backup, restore or verification reports the backup directory as locked. Measured on the Mac: a drop of a few seconds is hidden by Windows; after about a minute, handles kept open reattach; a handle closed during a drop leaves the orphan. To look at: whether `fsx/lock.go` and the workflows close handles on network errors (they should not), and whether the "locked" message can say that a lock left by this PC clears itself within minutes (as MountSafe SPEC-core 6.2 step 2 does).

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
