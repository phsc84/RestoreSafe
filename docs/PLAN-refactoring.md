# Plan: Refactoring round after 2.0.0

| | |
|---|---|
| Status | Agreed 2026-10-13 |
| Follows | [SPEC-refactoring.md](SPEC-refactoring.md) |
| Previous round | The round of 2.0 (RF-1 to RF-64), done 2026-10-09; its plan is in git history (`git show v2.0.0:docs/PLAN-refactoring.md`) |
| Baseline | `dev` at `d4d192a` |
| Branch | `dev` |
| Scope | Coverage (RF-40), code that only tests reach, duplicated checks and formats, console-era names, dependency updates, two large files. **No change to the backup format of 2.x** (standing constraint 1), and no user-visible change. |

## 1. Purpose

RestoreSafe 2.0.0 is released: a layered codebase with CI, coverage floors per package, format fixtures, fuzzing and a fault-injection matrix, all from the round of 2.0. The code has not changed since the release (only docs, names of windows and lint settings), so this round is small: it finishes the coverage targets the round of 2.0 left open, removes what the round of 2.0 could not see because its dead-code check counted tests as callers, and tidies a few places where one fact is known twice.

New items start at RF-65 (IDs count on across rounds, SPEC-refactoring section 1). They are grouped by area with gaps: RF-65 structure, RF-70 dead and legacy code, RF-75 performance, RF-80 tests, RF-85 tooling, RF-90 documentation.

## 2. Baseline (measured 2026-10-13)

| Measure | 2.0 round, start (2026-10-06) | 2.0 round, result (2026-10-09, `a5986bb`) | This round, start (2026-10-13, `d4d192a`) |
|---|---|---|---|
| Tracked files / Go lines | 262 / 40,636 | 337 / 44,303 | 340 / 44,341 |
| Go toolchain | go 1.27.1 | go 1.27.2 | go 1.27.2 (the current release) |
| Dependency updates | not recorded | not recorded | `x/crypto` 0.57.0 → 0.58.0, `x/sys` 0.48.0 → 0.49.0; tools: staticcheck 0.8.1 → 0.8.2, govulncheck 1.8.0 → 1.9.0, `x/tools` 0.51.0 → 0.52.0 |
| `go vet`, `gofmt -l` | vet clean; 18 files (CRLF) | nothing reported | nothing reported |
| staticcheck | 10 findings besides ST1005 | nothing reported | nothing reported, with the naming and doc-comment checks on since `2974fe1` |
| `deadcode -test` | 16 unreachable functions | nothing reported | nothing reported |
| `deadcode` (without `-test`) | not measured | not measured | 22 functions outside the test-support packages (RF-70) |
| `govulncheck` | 0 called; 1 required-only | 0 called, 0 imported; 1 required-only (`x/crypto/openpgp`) | unchanged: 1 required-only, GO-2026-5932 (`x/crypto/openpgp`, no fix) |
| `go test ./...` | passes; `-cover` failed in `gui/view` | passes, including the 2.0.0 format fixtures; race and fuzzing pass in CI | passes; slowest packages `e2e` 83 s, `container` 63 s, `backup` 56 s, `health` 31 s |
| Race, fuzzing | not run | pass in CI | pass in CI (run 38073462386 on `d4d192a`) |
| Markers (`TODO`, `FIXME`, `XXX`, `HACK`) | not counted | none | none |
| CI | none | check, tests with coverage floors, race, fuzzing, GUI smoke test | the same five jobs; about 7 min, of which fuzzing 7 min, tests 2 min 15 s, race 2 min 10 s |
| Coverage, total | 46.1 % | 54.5 % | 54.5 % |
| Largest production files | `gui/backups.go` 948, `gui/wizard.go` 931, `yubikey/fido2.go` 917 | `yubikey/fido2.go` 908, `gui/view/strings.go` 516, `archive/build.go` 504 | `yubikey/fido2.go` 910, `gui/view/strings.go` 516, `archive/build.go` 504 |
| Throughput, SSD (backup / restore) | not measured | 329–338 / 255 MiB/s | 316 / 178 MiB/s (RF-75) |
| Throughput, network share (backup / restore) | not measured | 29.3 / 56.9 MiB/s | 24.2 / 43.9 MiB/s (RF-75) |
| Docs index | not checked | complete | complete: every file in `docs/` is listed in `docs/README.md` |

Coverage by package is unchanged since the result of the 2.0 round: cryptox 89.1, recovery 94.0, yubikey 36.4; config 92.6, logging 88.5, problem 100; manifest 92.6, naming 92.5, container 85.5, setwriter 85.2, catalog 81.2, archive 78.1; fsx 82.7; health 95.1, plan 93.3, restorepoint 90.9, unlock 84.7, job 84.3, backup 83.3, restore 79.1, interact 78.3, verify 73.2; gui/view 91.0, gui/flow 77.0, gui/widget 73.6, gui/win32 10.4, gui 2.0; cmd/restoresafe 33.3.

Looked at without a finding: the layer rules (`internal/architecture` unchanged since the release); package documentation; secrets passed as `string` (none); the DLL search order (both programs call `SetDefaultDllDirectories`); `.gitattributes` against the file types; `config-SAMPLE.yaml` against the configuration keys; every "core spec n", "GUI spec n" and "GUI spec XX-n" citation in the code against the specs; `ids_test.go` against the GUI scripts.

Not looked at in this round: the cryptography point of SPEC-refactoring section 5.4 (algorithms, KDF bounds and defaults against current OWASP and RFC 9106 guidance). No deficiency is known; the defaults are 512 MB, 3 passes and 4 threads ([crypto.go:104](../internal/security/cryptox/crypto.go#L104)). The next round starts its review with it.

## 3. Constraints

The standing constraints of SPEC-refactoring section 2 apply. The format fixtures in `internal/format/testdata/v2.0.0/` now describe the released format: they never change.

## 4. Structure

**RF-65 (P3) Progress logging takes three counters in a row.** Evidence: `LogStreamProgress` and `LogProgressUntilDone` ([job/progress.go:34](../internal/workflow/job/progress.go#L34), [:52](../internal/workflow/job/progress.go#L52)) take 7 parameters, among them `inBytes, outBytes, outWriteCalls *atomic.Int64`; both are only called from the same file.
Change: one `streamCounters` struct for the three counters; unexport what has no caller outside the package.
Acceptance: no function in `job` takes more than 4 parameters; the log lines are unchanged (`job` tests).
Done 2026-10-13: `fsx.StreamCounters` replaces the three counters and setwriter's own `Counters` type, which held the same three; the two logging functions are methods of an unexported `streamProgress`. `OpenLogger` (5 parameters) takes the log path its callers had already computed. The log lines are unchanged.

**RF-66 (P3) The backup preflight and the backup plan take the same eight inputs.** Evidence: [backup/choose.go:49-53](../internal/workflow/backup/choose.go#L49-L53) calls `backupPreflightReport` (7 parameters, [preflight.go:23](../internal/workflow/backup/preflight.go#L23)) and `backupPlan` (9 parameters, [preflight.go:398](../internal/workflow/backup/preflight.go#L398)) one after the other with `cfg, backupDir, sources, keys, folders, est`; the comment of `backupPlan` says it must use "the values the details report was built from".
Change: one struct holding those inputs, built once in `choose.go`, with the report and the plan as its methods; the fact that both use the same values then holds by construction.
Acceptance: neither function takes more than 3 parameters; `backup` tests pass unchanged.
Done 2026-10-13: `proposal` with the methods `report` (1 parameter) and `plan` (3); `choosePlan` builds it once per plan shown.

**RF-67 (P3) The log line layout is known in three places.** Evidence: [logging.go:202](../internal/logging/logging.go#L202) writes `[<time>] <severity> - <message>`; [facts.go:132](../internal/logging/facts.go#L132) parses it for facts; [view/logviewer.go:34](../internal/gui/view/logviewer.go#L34) and [:57](../internal/gui/view/logviewer.go#L57) match `"] FACT  - "`, `line[20] == ']'` and `" WARN"` / `" ERROR"` by hand. `view` already imports `logging`.
Change: `logging` exports one function that splits a line into time, severity and message (used by `parseFact` too), and one that formats a line (used by the logger); the log viewer uses the first. The test of a failed run in `view/restorepage_test.go` writes its fact line with the second, with a time after the scenario's runs, instead of sleeping (RF-80).
Acceptance: no log-line literal or column index in `gui/view`; no `time.Sleep` in tests; the log file format is unchanged (`logging` tests).
Done 2026-10-13: `logging/line.go` holds the severities, `FormatLine` (the logger) and `ParseLine` (`parseFact` and the log window). The log format is unchanged. Found on the way: no logger method writes `ERROR`, so the log window's ERROR tone and the "Warnings and errors" filter (GUI spec BK-5) only ever see warnings; kept as they are, since the spec names them.

**RF-68 (P3) `BuildTar` is 160 lines.** Evidence: [archive/build.go:101](../internal/format/archive/build.go#L101) `BuildTar` 160 lines, and `writeFile` ([:320](../internal/format/archive/build.go#L320)) 7 parameters; `archive` is at 78.1 % coverage (RF-40). The other functions over 80 lines are a window procedure and dialog builders in `gui` (`message`, `runCredentialDialog`, `SettingsOf`), where a long switch or a list of controls reads better in one piece, and `setwriter.Write` (107), `backup.run` (109), each one sequence of steps.
Change: after RF-40 has brought `archive` to its target, split `BuildTar` into the walk and the writing of one entry, with the per-run state in a struct instead of `writeFile`'s parameters.
Acceptance: no function in `archive` over 80 lines or 5 parameters; a known-answer test, written before the change, shows that the tar output of a fixed tree is byte for byte the same after it.
Done 2026-10-14: `BuildTar` keeps its per-run state in `tarBuilder`; the walk is its method `visit`, with `unlisted`, `unstatable`, `addDir`, `addFile` and `writeFile` (2 parameters instead of 7) for the cases. `TestBuildTarKnownAnswer`, committed before the split, passes unchanged after it. The split moved archive's coverage from 85.3 to 84.8 %; `TestBuildTarHandlesAFolderItCannotList` (a folder denied listing with `icacls`, by default and with `on_unreadable_file: skip`) tests the branch that was never reached and brings it to 86.5 %.

**RF-69 (P3) `yubikey/fido2.go` holds four things in 910 lines.** Evidence: the file has the WebAuthn ABI structs (lines 105–298), the challenge data and its JSON (300–375), the public operations (379–525), the WebAuthn calls (526–810) and the HID enumeration through SetupAPI (811–910). It has been the largest production file for two rounds.
Change: split by those parts into `webauthn.go`, `challenge.go`, `hid.go` and `fido2.go`; a move only, no change to a function.
Acceptance: no file in `yubikey` over 400 lines; `git diff --stat -M` shows moves; `layout_test.go` passes.
Done 2026-10-14: `fido2.go` (200 lines: debug output, errors, test seams, public operations), `challenge.go` (75), `webauthn_abi.go` (252: bindings and the ABI structs, which `layout_test.go` now reads), `webauthn.go` (309: the calls) and `hid.go` (117). Five files instead of four, because the WebAuthn part alone had 539 lines. A move only: the sorted lines of the new files, without package clauses and imports, equal those of the old file.

## 5. Dead and legacy code

**RF-70 (P2) Production code that only tests call.** Evidence: `go tool deadcode -test ./...` reports nothing because tests count as callers; `go tool deadcode ./...` reports 22 functions outside `testutil` and `interacttest`:
`catalog.ScanBackups`, `catalog.InspectSet`; `manifest.Parse`, `manifest.Builder.Bytes`; `widget.Area.Width`, `widget.Area.Columns`, the whole `widget.Badge` (6 functions); `win32.ListSetGroupCollapsed`, `win32.Pixel`; `backup.estimateBackupSpace`; `health.Check`, `health.collectStartupHealthItemsWithConfigPath`, `health.checkBackupInventoryHealth`; `interact.ReadPasswordConfirmed` (RF-71), `interact.Note`, `interact.WriteReport`, `interact.writeRow` (only `interacttest` uses them to print a report).
Change: for each, delete it and change its tests to use what the program uses, or move it into the test files or test-support package that need it. `manifest.Parse` and `Builder.Bytes` may stay if the fuzz target and the format fixtures need the in-memory form; the item then says so. Add `deadcode` without `-test` to SPEC-refactoring section 4.
Acceptance: `go tool deadcode ./...` lists functions in `testutil`, `interacttest` and the functions kept with a reason only.
Done 2026-10-13; none kept. Deleted: `ScanBackups`, `InspectSet`, `Parse`, `Builder.Bytes`, the `Badge` window (`DrawBadge`, which the lists use, stays), `Area.Width`, `Area.Columns`, `ListSetGroupCollapsed`, `Pixel` (the widget tests call `GetPixel` themselves), and with `interact.Note` the whole `RowNote` kind, which nothing produces (also the window's RTF branch for it). Moved into the tests: `health.Check` and two health shorthands, `backup.estimateBackupSpace`; `WriteReport` into `interacttest`. The tests now use what the program uses (`Inventory`, `Encode`/`Decode`). `ReadPasswordConfirmed` went with RF-71. SPEC-refactoring section 4 and 5.2 run `deadcode` with and without `-test`; SPEC-gui no longer names `health.Check`. Coverage: interact fell to 34 % once `WriteReport`'s test moved and got its own tests (100 %); widget fell from 73.6 to 72.2 % with the tested `Badge`, floor 72 until RF-40.

**RF-71 (P2) A new password is checked against its confirmation twice.** Evidence: [interact/password.go:18](../internal/workflow/interact/password.go#L18) `ReadPasswordConfirmed` (from the console, unreachable in the program, RF-70) and [gui/flow/ui.go:148](../internal/gui/flow/ui.go#L148) `checkNewPassword` both refuse an empty password and a mismatch, and zero the buffers; only the second runs.
Change: one function in `interact` that checks a password and its confirmation (the body of `checkNewPassword`), used by `gui/flow`; `ReadPasswordConfirmed` goes.
Acceptance: `ErrPasswordEmpty` and `ErrPasswordMismatch` are returned in one place; the tests of both cases move with it.
Done 2026-10-13: `interact.CheckNewPassword`, used by `gui/flow` and by `interacttest` (which asks for the confirmation only after a non-empty password, as before).

**RF-72 (P3) Console-era names for the log pane.** Evidence: RestoreSafe.exe has no console since 2.0 ([fido2.go:32](../internal/security/yubikey/fido2.go#L32) says so), and the workflows' output goes to the window's log pane ([gui/flow/ui.go:54](../internal/gui/flow/ui.go#L54), [gui/operation.go:141](../internal/gui/operation.go#L141)). Still named after the console: `logging.Logger.console`, `NewConsoleLogger`, `IsConsoleOnly` ([logging.go:32](../internal/logging/logging.go#L32), [:102](../internal/logging/logging.go#L102), [:107](../internal/logging/logging.go#L107)), `job.OpenLogger(..., console)` ([job/runtime.go:19](../internal/workflow/job/runtime.go#L19)), the package documentation of `interact` ([interact.go:3-5](../internal/workflow/interact/interact.go#L3-L5)) and `Issue.Full` ([report.go:96](../internal/workflow/interact/report.go#L96)). `yubidiag` is a console program; its use is not affected.
Change: rename to "output" (the writer) and "without a log file" (`IsConsoleOnly`); fix the comments. Texts written to the output stay the same (standing constraint 2).
Acceptance: `git grep -i console -- '*.go'` finds only `yubidiag`, the `GetConsoleWindow` fallback of `yubikey`, and test names.
Done 2026-10-13: `NewOutputLogger` and `HasFile` (the inverse of `IsConsoleOnly`; a nil logger has no file), `output` for the writer in `logging` and `job`, and the comments of `interact`, `problem`, `Issue.Full` and `testutil.Output`. The tests' `console` variables went too. The texts written are unchanged. Only yubikey's console window, which yubidiag uses, keeps the word.

## 6. Performance

**RF-75 (P1) Restore is slower than in the round of 2.0.** Evidence: the throughput benchmarks on 2026-10-13 (median of 3 runs, 1.7 GB; the SSD run alone on the machine, in `sandbox\bench`):

| | 2.0 round (2026-10-08) | 2026-10-13 | Change |
|---|---|---|---|
| SSD, backup | 318–338 MiB/s | 316 MiB/s (312–337) | within the variance |
| SSD, restore | 255–257 MiB/s | 178 MiB/s (172–181) | −30 % |
| Network share, backup | 29.3 MiB/s | 24.2 MiB/s (23.6–24.9) | −17 % |
| Network share, restore | 56.9 MiB/s | 43.9 MiB/s (41.3–48.1) | −23 % |

No code on the backup or restore path changed between `a5986bb` and `d4d192a` (`git diff a5986bb..HEAD` touches only comments there), and the Go toolchain and `x/sys` are the same, so the cause is likely the machine (a Windows or virus scanner update) rather than the code. A first SSD run under `%TEMP%`, while other tests ran, gave 221 and 138 MiB/s and is not counted.
Change: run the benchmarks of `v2.0.0` and of `dev` alternately on the same data, as for RF-59 in the round of 2.0. If `v2.0.0` is as slow, the drop is the machine's: record the new reference numbers here and drop the item with that reason. If not, profile restore (`-cpuprofile`, `-memprofile`, SPEC-refactoring section 5.3) and fix what the profile shows.
Acceptance: alternating runs of `v2.0.0` and the fix differ by less than 10 %, or the item is dropped with the measurement that shows the machine changed.
Dropped 2026-10-13: the machine changed, not the code. `v2.0.0` (a worktree of the tag) and `dev` at `a8d5818`, alternately on the same data in `sandbox\bench` on the SSD, two rounds (median of 3 runs each):

| | `v2.0.0` | `dev` |
|---|---|---|
| Backup, round 1 / 2 | 266.9 / 251.0 MiB/s | 243.1 / 256.7 MiB/s |
| Restore, round 1 / 2 | 166.0 / 162.6 MiB/s | 159.4 / 153.6 MiB/s |

The release itself restores at 163–166 MiB/s here today, against 255 MiB/s on 2026-10-08; `dev` is within 6 % of it. Neither `Get-HotFix` nor the virus scanner's versions name a cause. The reference for the next comparison is this round's own numbers; the network share was not run alternately, since the SSD already separates the code from the machine.

## 7. Tests

**RF-40 (P1) Coverage targets per package** (carried over from the round of 2.0, re-checked 2026-10-13: the numbers are unchanged). CI enforces a floor per package (`scripts/ci/test-coverage-floors.txt`, checked by `test-coverage-floors.sh`); the targets set in the round of 2.0 are not reached everywhere. Below the target: cryptox 89.1 (90), archive 78.1, catalog 81.2, backup 83.3, restore 79.1, verify 73.2, unlock 84.7, job 84.3, interact 78.3, fsx 82.7, gui/flow 77.0, gui/widget 73.6 (85 each), yubikey 36.4 (60; everything except the WebAuthn calls, behind the `fido2MakeCredFn`/`fido2GetHmacFn` seams). The floors also lag the current values by up to 8 points (archive, backup, restore, unlock, job at 70), so coverage can drop without CI noticing.
Change: first raise every floor to its package's current value, rounded down. Then tests for the error paths, starting with verify and restore, which decide whether damage is reported; raise each floor to its target once it is reached. RF-70 removes functions that only tests reach, which lowers some packages a little; measure after it.
Acceptance: every package at or above its target, and the floors in CI raised to match.
Step 1 done 2026-10-13: every floor raised to its package's coverage in CI (run 38073462386, the same as locally), rounded down, e.g. archive 70 → 78, verify 70 → 73, restorepoint 85 → 90, yubikey 35 → 36. Where RF-70 deletes tested functions and a package drops below its new floor, RF-70 lowers that floor with the number, in the same commit.
The first CI run with the new floors failed: `recovery` measured 92.5 % against its floor of 94. Its coverage depended on the random code: the test of the Crockford readings (O as 0, I and L as 1) ran only when the code contained a 0 or a 1, so the package measured 92.5, 94.0 or 95.5 % from run to run. That test now uses a fixed code, and a new test covers `ValidCheck`'s refusals; `recovery` measures 98.5 % in every run and its floor is 98. Two more full runs showed one other package that varies: `gui/widget` 73.4 or 73.6 % (floor 73).
Step 2, 2026-10-13: tests to the targets, each floor raised to the new coverage in CI, rounded down. verify 73.2 → 93.8 %, restore 79.1 → 90.1 %, archive 78.1 → 85.3 %, cryptox 89.1 → 94.1 %, job 84.3 → 89.7 %, fsx 82.7 → 92.9 %, unlock 84.7 → 87.4 %, gui/flow 77.0 → 95.9 %, yubikey 36.4 → 62.3 % (CI and here alike; the WebAuthn calls stay untested). logging rose to 91.3 % with RF-95. The verify tests found RF-95. Still below the target: catalog 81.2, interact 78.3, gui/widget 73.6 and backup 83.3, all four with functions RF-70 deletes or moves (`ScanBackups`, `InspectSet`; `WriteReport`, `ReadPasswordConfirmed`; `Badge`, `Area.Width`; `estimateBackupSpace`); their tests follow RF-70, so that no test is written for code about to go.
Done 2026-10-13 (`b6b2bd2`, CI run of that commit): every package at or above its target, every floor at its coverage in CI, rounded down. After RF-70: catalog 82.7 → 94.0 %, backup 83.3 → 85.7 %, interact 34.3 → 100 % (its report test moved to `interacttest` with `WriteReport`), gui/widget 72.2 → 85.2 %. Total coverage 54.5 → 57.4 % (the window code in `gui` and `gui/win32`, which only the GUI scripts drive, keeps it low).

**RF-80 (P3) Slow tests and a test that sleeps.** Evidence: four packages take over 30 s (`e2e` 83 s, `container` 63 s, `backup` 56 s, `health` 31 s, SPEC-refactoring section 5.5 asks for 10 s); in `container`, `TestManifestStreamsAtOneMillionEntries` alone took 22 s. [view/restorepage_test.go:136](../internal/gui/view/restorepage_test.go#L136) sleeps 1.1 s because the logger takes its time from the clock. CI is not slowed by them (fuzzing takes longer); a developer running `go test ./...` waits about 1.5 min.
Change: profile the four packages' slowest tests (`go test -v` sorted by time) and fix what is setup cost (Argon2 parameters, file sizes) rather than what the test checks; let a test give the logger its time instead of sleeping.
Acceptance: no `time.Sleep` in tests; each of the four packages faster by the amount the item records, with the reason for what stays slow.
Done 2026-10-13, except the sleep. Measured alone, most of the baseline's time was the packages competing for the CPU in `go test ./...`: alone they took e2e 29 s, container 20 s, backup 24 s, health 10 s. Changed: the e2e tests run in parallel (none changes global state), as do the subtests of `TestCancelAtEachPhase` (12 s → 3 s) and of `TestEveryWarningAndErrorItemHasACode` (4.8 s → 1.3 s). Alone now: e2e 8 s, health 9 s; in the full run e2e 44 → 23 s, and `go test ./...` takes 50 s instead of 54 s. Stays slow, with the reason:
- container: `TestManifestStreamsAtOneMillionEntries` (19 s) measures the heap at the one million entries of core spec 5.3 and collects garbage after every 5 % of growth so that the peak shows live memory; that is what it checks. The rest of the package takes 1.5 s.
- backup: its slow tests replace the package's YubiKey and key seams (`checkYubiKeyConnectedFn` and others in `keys.go`) and so cannot run in parallel.
- The sleep in `view/restorepage_test.go` stays until RF-67: the test needs a failed run with a later time than the scenario's runs (the Create page describes the latest run), and RF-67's single function for the log line lets the test write that line with its own time. A clock in `Logger` only for this test would be code only tests call (RF-70).
  Removed with RF-67 on 2026-10-13: the test writes the failed run's fact line with `logging.FormatLine`, a minute after the scenario's runs. No `time.Sleep` is left in the tests.

## 8. Tooling and build

**RF-85 (P2) Dependencies and tools behind their current versions.** Evidence: `go list -m -u all` (baseline): `x/crypto` 0.58.0, `x/sys` 0.49.0, staticcheck 0.8.2, govulncheck 1.9.0, `x/tools` 0.52.0 are available.
Change: `go get -u ./...` and the tools, `go mod tidy`, one commit with the CHANGELOG line for the dependencies (CONTRIBUTING.md).
Acceptance: `go list -m -u all` shows no update for a direct dependency or tool; CI passes, including the format fixtures.
Done 2026-10-13. `x/crypto` 0.58.0, `x/sys` 0.49.0; staticcheck 0.8.2, govulncheck 1.9.0, `x/tools` 0.52.0 and their modules (`go get -u tool`). gofmt, vet, staticcheck, `deadcode -test` and `go test ./...` report nothing new, coverage 54.5 %; govulncheck still lists only GO-2026-5932 (`x/crypto/openpgp`, required, not imported). The updates `go list -m -u all` still shows (testify, go-spew, goldmark, `x/exp`, `renameio`) are in the module graph only, not in `go.mod`.

**RF-86 (P3) The dead-code check in CI only reports.** Evidence: [ci.yml:44-46](../.github/workflows/ci.yml#L44-L46) runs `go tool deadcode -test ./...` as "a report only", so a finding never fails CI and is seen only when someone reads the job's log; the round of 2.0 brought it to zero.
Change: fail the job when `deadcode -test` reports anything, and, after RF-70, when `deadcode` without `-test` reports anything outside the test-support packages and the functions RF-70 keeps (open question 1).
Acceptance: a function without a caller, added on a test branch, fails the static job.
Done 2026-10-13: `scripts/ci/static-deadcode.sh` runs `deadcode` with and without `-test` (open question 1) and fails on either finding, outside `testutil` and `interacttest`. Checked locally with a planted function without a caller and one only a test calls: each reported once, exit 1; the clean tree passes. CI.md describes the step.

**RF-87 (P2) The checks of CI are defined in three places.** Evidence: shell inside [ci.yml](../.github/workflows/ci.yml) (gofmt, `go test | tee`, the benchmarks), four bash scripts with awk and grep in `scripts/ci/`, and the "Locally" columns of [CI.md](CI.md), which repeat them as single commands. Running CI locally means typing those commands one by one, and the coverage floors need Git Bash, because Windows PowerShell 5.1 writes redirected output in UTF-16.
Change: one Go program, `tools/ci`, runs each job the same way on the development machine and on the runner, as specified in [SPEC-ci.md](SPEC-ci.md) (agreed 2026-10-14, decisions in its section 11), in that spec's four phases: the runner, the switch of `ci.yml`, the proof on a throwaway pull request, the pre-push hook.
Acceptance: the acceptance criteria of SPEC-ci section 13.

## 9. Documentation

**RF-90 (P3) SPEC-refactoring contradicts itself on item IDs.** Evidence: section 1 says IDs "count on across rounds"; section 6 says "IDs restart at RF-1 in every round" and gives "RF-1x tests" as the grouping example.
Change: section 6 says IDs count on and are grouped by area with gaps from the round's first free number, as in this plan.
Acceptance: section 1, section 6 and this plan agree.
Done 2026-10-13.

## 10. Found during the work

**RF-95 (P1) Two runs logging to the same file lose each other's lines.** Evidence: found by the verify tests of RF-40, whose parallel tests both logged to `verify.log`. `NewLogger` wrote to `%TEMP%\<name of the log>` ([logging.go:62](../internal/logging/logging.go#L62) at `c425eb0`), so every logger of one log file shared one temp file: a second `NewLogger` overwrote it with a fresh copy of the log, and `Close` overwrote the log with the temp file. A restore and a verification of the same backup run, started in two windows (the read lock lets both run), therefore lost the lines and facts the first one had written before the second one started. `TestLoggersOfTheSameFileAtOnceKeepEachOthersLines` fails on the old code with exactly that.
Change: each logger writes to a temp file of its own (`os.CreateTemp`); `Close` appends it to the log instead of overwriting the log. The log format is unchanged; the warnings about copying an existing log to the temp directory are gone with the copy. CHANGELOG: under "Fixed" for 2.1.0.
Acceptance: the test above passes; a temp file that cannot be appended is kept and reported (`TestCloseKeepsTheTempFileWhenTheLogCannotBeWritten`).
Done 2026-10-13.

**RF-96 (P2) Failures are not logged as errors, and their reason is not in the readable log.** Evidence: no workflow writes an `ERROR` line; every problem, from a skipped file to a failed run, is a `WARN` line (`git grep -n "log.Warn" -- internal/workflow`). When a run fails, its reason reaches the log file only as a `FACT` line (JSON), e.g. `backupFailed` ([backup/workflow.go:270](../internal/workflow/backup/workflow.go#L270)) and `verifyAll` ([verify/workflow.go:175](../internal/workflow/verify/workflow.go#L175)), and the log window hides fact lines; a failed restore writes no fact at all. A user who opens the log of a failed run sees the steps up to the failure, not why it failed. The log window's ERROR tone and the "Warnings and errors" filter (GUI spec BK-5) therefore only ever show warnings (found with RF-67).
Change: `logging` gets an `Error` method for `SeverityError`. `ERROR` is for what the user asked for and did not happen: a backup run that failed, a set that failed its post-backup verification, a verification that found damage, a restore that failed (with its "INCOMPLETE" line). Each workflow fails in one place (`backupFailed`, the error paths of `run` in verify and restore); the error line goes there, as text with its remedy, next to the fact. `WARN` stays for a run that completed with something to look at: unreadable files skipped as configured, retention skipped, a base not usable (full backup instead), stalled I/O, the directory lock unavailable, wrong password attempts, and a cancel (the user's own choice, not a failure). CHANGELOG: under "Changed" for 2.1.0 (standing constraint 2: the log's content changes, not its format).
Acceptance: tests show an `ERROR` line with the reason in the log of a failed backup, of a verification that found damage and of a failed restore, and none in the log of a cancelled run; the comment "never written" on `SeverityError` is gone.
Done 2026-10-14: `Logger.Error`; `backupFailed`, the post-backup verification of a set, and the error paths of `run` in verify and restore write the error with its remedy as an `ERROR` line. The "INCOMPLETE" line of a restore is `ERROR` when the restore failed and stays `WARN` when it was cancelled. Tests: the failure tests of backup, verify and restore check the `ERROR` line, their cancel tests and `TestCancelAtEachPhase` check that a cancel writes none. CHANGELOG under "Changed" for 2.1.0.

**RF-97 (P2) A folder deleted during a backup fails the backup.** Evidence: found by a test of RF-68. `addDir` ([archive/build.go](../internal/format/archive/build.go)) treated a folder that vanished between the walk listing it and reading its times as unreadable: by default the backup failed ("Cannot read ... set 'on_unreadable_file: skip'"), with `skip` it was reported as a file that could not be read. Core spec 6.5 says a file or directory deleted while the backup runs "never aborts a backup"; a file deleted in the same way is counted as vanished, and so is a folder the walk cannot list because it is gone. The code was the same before the split of RF-68.
Change: `addDir` counts a folder that no longer exists as vanished, like the other two places.
Acceptance: `TestBuildTarCountsAFolderThatVanishes` (the folder is deleted when the file before it is opened) fails before the change and passes after it. CHANGELOG under "Fixed" for 2.1.0.
Done 2026-10-14.

## Phases

| Phase | Items | When | Why then |
|---|---|---|---|
| 1. Tooling and safety net | RF-75, RF-85, RF-40 (floors raised to the current values), RF-90 | first | RF-75 compares with `v2.0.0` before the dependency update changes the code; the update and the raised floors guard everything after them |
| 2. Tests | RF-40 (tests to the targets), RF-80 | after phase 1 | the safety net before code is changed or removed in phase 3 |
| 3. Cleanup | RF-70, RF-71, RF-67, RF-65, RF-66, RF-72, then RF-86 | after phase 2 | small diffs inside single packages; RF-86 last, once RF-70 has brought `deadcode` to zero |
| 4. Log errors, then moves | RF-96, then RF-68, RF-69 | last | RF-96 changes what the log says, in three workflows; the moves are the largest diffs, and RF-68 needs `archive` at its coverage target first (reached) |
| 5. CI runner | RF-87 | after phase 4 | it replaces the CI scripts the earlier phases still use; its own four phases are in SPEC-ci section 10 |

There is no 2.0.1 (owner's decision, 2026-10-13): everything ships with 2.1.0, including the user-visible fixes RF-95 and RF-96.

Phase 1 done 2026-10-13 (`fc02339`): RF-75 dropped, RF-85 and RF-90 done, RF-40 step 1 done. Checks at the end of the phase: gofmt, vet, staticcheck and `deadcode -test` report nothing; govulncheck unchanged (1 required-only); `go test ./...` passes with every package at its new floor; coverage 54.5 %.

Phase 2 done 2026-10-13 (`b378846`), with RF-95 found and fixed in it: RF-40 brought nine packages to their targets, RF-80 is done except the sleep, which moved to RF-67. The four packages RF-70 touches reached their targets in phase 3 (`b6b2bd2`), which closed RF-40. Checks after RF-40: gofmt, vet, staticcheck, `deadcode -test` and `deadcode` (outside the test-support packages) report nothing; `go test ./...` passes in 50 s; coverage 57.4 %; CI passes.

Phase 3 done 2026-10-13 (`960ad01`): RF-70, RF-71, RF-67, RF-65, RF-66, RF-72 and RF-86 done, and RF-40 closed with the four packages RF-70 touched. Checks at the end of the phase: gofmt, vet, staticcheck and `static-deadcode.sh` report nothing; `go test ./...` passes; coverage 57.4 %; CI passes on all five jobs.

## Open questions

Answered 2026-10-13:

1. **RF-86: should `deadcode` without `-test` fail CI**, with the test-support packages filtered out, or only `deadcode -test`? Failing on both keeps RF-70 from coming back but means a helper used only by tests must live in a test file or `testutil`. Answer: both.
2. **RF-40: is 60 % the right target for `yubikey`?** RF-69 separates the WebAuthn calls, which no test can reach without a YubiKey, from the rest; a target for the rest alone may be more honest than one for the package. Answer: keep 60 % for the package.

## Not part of this round

- Replacing the Win32 GUI with a toolkit, or `logging` with `log/slog`.
- Splitting the long functions in `gui` (`message`, `runCredentialDialog`, `SettingsOf`), `setwriter.Write` and `backup.run` (RF-68 says why).
- `gui/view/strings.go` (516 lines): a table of texts, longer by nature.
- **yubidiag needs an overhaul** (owner, 2026-10-13): it may not do what it is for. Its new purpose and behaviour get a specification of their own first; then the work is a feature, not a refactoring item. Noted here so it is not forgotten. Specified on 2026-10-14 as the YubiKey page in [SPEC-gui.md](SPEC-gui.md) 10.2 (decision 10), which removes `cmd/yubidiag`.

## Done when

- RF-40 done: every package at its target, the floors in CI at the targets.
- Every P1 and P2 item done or dropped with a reason.
- `go tool deadcode ./...` reports nothing outside the test-support packages.
- The baseline of section 2 run again and recorded as this round's result, with throughput on the SSD and the network share.

## Carried over (filled in when the round closes)
