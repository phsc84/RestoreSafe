# Plan: Refactoring round after 2.1.0

| | |
|---|---|
| Status | Prepared 2026-10-18; baseline measured 2026-10-18; review started 2026-10-24 with the size of the code (sections 6 to 9), the other areas of the checklist not yet |
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

Added for this round: **fewer lines is not a goal of its own.** An item that makes the code smaller must not loosen a layer rule or blur what a package is for, move logic from a well-tested package (`gui/view` 92.7 %, `gui/flow` 95.9 %, the workflows) into one that is barely tested (`gui` 1.8 %, `gui/win32` 10.4 %), lower a coverage floor, add production code that only tests call, or replace plain types with options and flags. Candidates from the size review that would have done so are left out (section "Not part of this round").

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

### 5.1 From the specification review of 2026-10-24

A three-round review of SPEC-core, SPEC-gui and SPEC-kdf for gaps, security and robustness changed the specs only; these are the code follow-ups it leaves. Like the points above, they are inputs, not items yet. "Check" means the code probably does it already and the spec only now says so; "change" is a behaviour change with a CHANGELOG entry (standing constraint 2); none changes the format.

Round 1 (format precision, trust in plaintext, restore safety):

- **Check: recovery code against the spec** (core 4.4.2, now exact: checksum prefix, 25-bit check, secret = 25 ASCII characters). Add a known-answer test with a fixed code, so the frozen format is pinned by a vector and not only by the code that computes it ([recovery.go](../internal/security/recovery/recovery.go)).
- **Change: authenticate the key set before reuse** (core 4.4.1, 6.3). After unlock, decrypt the first manifest chunk of the set the current key set comes from; stop with an error if it fails. Today a forged newest set with a replaced recovery slot would be copied into every new backup.
- **Change: sets dated in the future** (core 6.1.1, GUI `SET_FUTURE_DATE`): not a key set source or base, reported, kept by retention and not ranked newest.
- **Check: path rules** (core 5.2.1) against `manifest.ValidatePath` ([manifest.go:420](../internal/format/manifest/manifest.go#L420)), which rejects `\`, `:`, NUL, absolute and unclean paths but not control characters, `< > " | ? *`, trailing dot or space, or device names; and the backup side, which must record such a source file as skipped.
- **Check: TAR binding in a full restore** (core 7.4): type flag, name, `off` and size against the manifest, bytes after end-of-archive. 7.5 already required it for a differential.
- **Check: part numbering without gaps** (core 4.6): `001`-`N` exactly, not only the count.
- **Change: change detection also compares `b` and `a`** (core D6, 6.8). The first differential after the update stores again the files whose creation time or attributes changed without a change time (FAT/exFAT sources); the CHANGELOG says so.
- **Change: say that the log reveals file names** (core 10): README (encryption section) and the enrollment screen text.

Round 2 (backup and retention):

- **P1 candidate, change: files behind non-link reparse points are skipped silently** (core 6.4.1). `isRegularOrDir` ([winfile.go:142](../internal/format/archive/winfile.go#L142)) keeps only `Mode().IsRegular()` and `IsDir()`; Go sets `ModeIrregular` for every reparse tag other than symlink, AF_UNIX and dedup (`os/types_windows.go`), so OneDrive files (local ones too), WOF-compressed files (`compact /exe`) and other data-bearing reparse points are left out of the backup without a log line. Classify by `IsReparseTagNameSurrogate`; count and log what is not recorded. Needs a test on a real OneDrive folder and a `compact /exe:lzx` file, done by the owner before the release that ships it (SPEC-refactoring 5.5).
- **Check: retention preconditions and counting** (core 9) against `applyRetentionPolicy` ([retention.go:23](../internal/workflow/backup/retention.go#L23)): only directories with a new, re-read set from this run; chains with an incomplete full not counted; newest complete differential kept; deletion order differentials-then-full; stop a chain's cleanup on a failed deletion.
- **Change: torn-copy detection** (core 6.5): metadata from the open handle before and after the copy.
- **Check: overlapping folders** (core 6.4.2): the backup directory is already left out of a source (`normalizeExcludes`, [build.go:452](../internal/format/archive/build.go#L452)); a source inside the backup directory and a nested source are not yet reported.
- **Change: recovery code round trip at enrollment** (core 6.3 step 4).
- **Change: early `o:"F"` check in a differential restore** (core 7.5 step 1).
- **Check: lock semantics** (core 14) match [lock.go](../internal/fsx/lock.go); one difference: `Release` of an exclusive lock deletes the lock file, which the spec doesn't need (the file's existence means nothing). Deleting it while another process has just opened it is harmless only while Go opens files without `FILE_SHARE_DELETE`; keeping the file removes the question.

Round 3 (restore outcome, platform limits, secrets, KDF consistency):

- **P1 candidate, part of the KDF feature for 2.2.0: old configurations** (SPEC-kdf 5.1, GUI 11.6). `config-SAMPLE.yaml` sets `time: 3`, `memory_mb: 512`, `threads: 4` explicitly ([config-SAMPLE.yaml:158](../config-SAMPLE.yaml#L158)); with SPEC-kdf 0.3, such a file would stop RestoreSafe at start, restores included, and an explicit 512 would keep these users off the new defaults. Treat that block as a retired default; replace the capping in `clampArgon2` ([config.go:265](../internal/config/config.go#L265)) and `ARGON2_CAPPED` ([health.go:167](../internal/workflow/health/health.go#L167)) with `ARGON2_OUT_OF_RANGE`, which blocks only a backup that creates new keys. Update the sample's argon2 block and its comment (lines 156-161) with RF-98.
- **Part of the KDF feature: enrollment memory rule** on the installed memory (SPEC-kdf 4.4 step 1-2), with a test for an 8 GiB machine reporting 7.8 GiB.
- **Change: restore into `<backup name> (incomplete)`, renamed on success; delete a file whose hash failed** (core 7.2.1, 7.7; GUI RW-5, RW-8).
- **Change: destination file-system check** (core 7.2): FAT32 file size limit, FAT/exFAT warning.
- **Check: password encoding** (core 6.6): UTF-16 to UTF-8 without normalization or trimming; lone surrogates rejected (today probably replaced by U+FFFD in the conversion, which would make two different passwords equal); the hint for non-ASCII characters is new.
- **P2 candidate: the recovery code passes through a Go string** in the GUI (GUI 13.3 said so; now `[]byte` end to end), against SPEC-refactoring 5.4. Also: empty the clipboard on close when it still holds the code; the typed recovery code follows 13.1.
- **Change: PIN warning after the second YubiKey refusal** (GUI CR-5).
- **Change: facts carry the set's nonce** (GUI 11.4), so a reused differential number doesn't inherit a verification result.
- **Owner, before 2.2.0:** after these changes, add a dated line to the Status rows of SPEC-core and SPEC-gui naming the review, and copy SPEC-kdf 0.4 to MountSafe if it has diverged again (done for 0.4 on 2026-10-24).

## 6. Structure: the window (RF-98 to RF-102)

From the size review of 2026-10-24. The code is lean by the tools: `deadcode` and staticcheck report nothing, and `dupl` (threshold 40) finds only short clone pairs, mostly the constants of `gui/view/strings.go`. What is left is the same structure written several times, almost all of it in `internal/gui`, whose root package has 1.8 % coverage. So every item of this section is checked by the GUI scripts (`scripts/gui-test`) and the rows of `GUIDE-gui-test.md` it names, one structure per commit. Line counts are estimates of production lines removed.

**RF-98 (P2) One plan window for Create backup and Verify.** Evidence: [plandialog.go](../internal/gui/plandialog.go) (308 lines) and [verifydialog.go](../internal/gui/verifydialog.go) (273) repeat the same frame line for line: opening with `newDialogTable` and `defID`, the waiting state with its marquee, the button row, `layout`, `focus`, `takeAnswer` and `close` (`dupl` reports 118-123, 189-196, 236-242, 299-308 of `plandialog.go`).
Change: a `planWindow` that both embed: the frame, the stack, the table, the waiting state and the button rows. Each dialog keeps its own content, commands and answer type; no flags select behaviour.
Acceptance: about 150 lines fewer; GUI scripts pass; manual rows of the Create backup window (6.1) and the Verify window (7.3) pass, including a blocked plan (BP-5) and Cancel while waiting.

**RF-99 (P2) One table that a splitter sizes.** Evidence: three implementations of "a table whose height a splitter sets": `tableSizer` and `dialogTable` ([table.go:252](../internal/gui/table.go#L252), [table.go:316](../internal/gui/table.go#L316)) and the Restore backup window's `listHeights`, `rowsHeight`, `splitMoved` and `fitColumns` ([restoredialog_layout.go:67-138](../internal/gui/restoredialog_layout.go#L67-L138)), which also repeats `table.fit`.
Change: one sizer for the page cards and the dialogs; the Restore backup window uses it for its list.
Acceptance: about 80 lines fewer; dragging the splitter on Create backup, Settings, the Create backup window and the Restore backup window behaves as before, including at a second DPI.

**RF-100 (P3) One way to draw a table's cells.** Evidence: three `customDraw` functions draw tone colours, greyed rows and type badges ([table.go:207](../internal/gui/table.go#L207), [restoredialog_list.go:62](../internal/gui/restoredialog_list.go#L62), [restorepage_list.go:259](../internal/gui/restorepage_list.go#L259)). The Restore backup window decides in `gui` which colour its Check column takes, from `w.view.Checks`.
Change: `table` gains check boxes and disabled rows, and the Restore backup window's list becomes a `table` fed with `view.TableRow`s, so the colour of each cell is decided in `gui/view`, where it is tested. The Restore backup page keeps its own list (groups, context menu) and shares only the drawing of a badge cell.
Acceptance: about 120 lines fewer in `gui`; the Check column's tones covered by a `gui/view` test; `gui/view` stays at or above its floor; the `win32` list wrappers that lose their last caller are deleted (`deadcode`).

**RF-101 (P2) Pages laid out on a stack.** Evidence: the four pages each write the same steps by hand: panel, title and Refresh, cards, total height, scroll bar, place, restyle ([createpage.go:324](../internal/gui/createpage.go#L324), [yubikeypage.go:223](../internal/gui/yubikeypage.go#L223), [settings.go:213](../internal/gui/settings.go#L213), [restorepage_layout.go:25](../internal/gui/restorepage_layout.go#L25)). They react in three different ways to a scroll bar that comes or goes: Create backup and Restore backup measure twice, YubiKey calls `update`, Settings waits for the next `layout`. `child` and `setButton` exist twice ([restoredialog.go:187](../internal/gui/restoredialog.go#L187), [restorepage.go:127](../internal/gui/restorepage.go#L127); [createpage.go:313](../internal/gui/createpage.go#L313), [yubikeypage.go:120](../internal/gui/yubikeypage.go#L120)).
Change: a scrolling page built on the existing `stack`: one place that handles the scroll bar, and one `child` and `setButton`. Settings' card pairs are a row of two cells.
Acceptance: about 130 lines fewer; every page scrolls, resizes and changes DPI as before (manual rows of GUI spec 5, 7, 10.1 and 10.2), at a width at which the scroll bar appears.

## 7. Structure: workflows and view (RF-103 to RF-107)

**RF-103 (P3) Restore and verification start the same way.** Evidence: [restore/run.go:44-112](../internal/workflow/restore/run.go#L44-L112) and [verify/workflow.go:37-98](../internal/workflow/verify/workflow.go#L37-L98) lock the backup directory, read the inventory, select the sets, open the log, add the lock issue, ask to start and unlock, in the same order with the same texts.
Change: two plain functions in `job` (open the selection and its log; confirm and unlock), called by both. No callbacks; each workflow keeps its own preflight, plan and checks.
Acceptance: about 50 lines fewer; the restore and verify tests pass unchanged; `restore`, `verify` and `job` stay at or above their floors.

**RF-104 (P3) One helper for singular and plural.** Evidence: `folderPhrase`, `backupCount`, `chainCount`, `more`, `otherWarnings` and about 13 more `if n == 1` branches in `gui/view` choose between two constants the same way.
Change: `plural(n, one, many)` in `gui/view`; the constants stay as they are.
Acceptance: about 50 lines fewer; the texts unchanged (the existing `gui/view` tests).

## 8. Dead and legacy code (RF-108 to RF-112)

**RF-108 (P2) The workflows' text output reaches nobody.** Evidence: everything the workflows write to `interact.UI.Output` ends in `app.logText` ([operation.go:142](../internal/gui/operation.go#L142)), which nothing reads ([app.go:101](../internal/gui/app.go#L101)). It carries about 12 `Fprintln` calls in the workflows (`Restore cancelled.`, the YubiKey steps), every log line the logger mirrors there ([logging.go:27-35](../internal/logging/logging.go#L27-L35); the "window's log pane" it names is gone), and the bridge's buffer and note (`Write`, `TakeOutput`, `NoteOutput`, [bridge.go:125-145](../internal/gui/flow/bridge.go#L125-L145)). It is the first GUI's protocol, left over. Neither tool reports it, because the field is written (new checklist point in SPEC-refactoring 5.2).
Change: after RF-109, delete `Output` from `interact.UI`, the bridge's output, `onOutput` and `logText`, the logger's mirror and its `output` parameter, `NewOutputLogger`, and the prints in the workflows. `InfoLogOnly` and `WarnLogOnly` become `Info` and `Warn`. GUI spec 11 (first paragraph), 12.2 (`WM_APP_OUTPUT` and the "live log" it fills) and 12.3 (the `Output` row) lose the channel in the same commit. Tests that read the output (about 10 test files set `Out`, plus `bridge_test.go:93` and `flow_test.go:106`) read the log file instead, which is what the user sees.
Acceptance: about 110 lines fewer; the log file's content unchanged (a test compares a run's log with the one before); `logging`, `job`, `flow` and the workflows at or above their floors; `deadcode` with and without `-test` clean.

**RF-109 (P1) Problems with the log file itself are lost.** Evidence: when the log can't be opened, synced, read back from TEMP or appended to the backup directory, the logger and `job.OpenLogger` write that only to the output ([logging.go:112-126](../internal/logging/logging.go#L112-L126), [runtime.go:21](../internal/workflow/job/runtime.go#L21)), which reaches nobody (RF-108). When the append fails, "Log file is located in temp directory" is the user's only pointer to their only copy of the log, and nobody sees it. A log that couldn't be opened is counted as a warning; the other cases aren't even that.
Change: the logger returns its problems (`Close` returns an error; opening already does), and the workflow adds them to the result as a warning that names the path of the copy in TEMP. This is a behaviour change: CHANGELOG entry and the wording in GUI spec 6.3 and 11.5 (open question 1).
Acceptance: a test makes the append fail (the backup directory read-only) and finds the warning with the TEMP path in the result; then RF-108 can delete the output without losing a message.

## 9. Tests (RF-113 to RF-117)

**RF-113 (P3) The test UI answers with values, not typed text.** Evidence: `interacttest.Script` ([script.go](../internal/workflow/interact/interacttest/script.go), 291 lines) still plays the 1.x console: it builds prompts ("Start %s now? [Y/n]: "), parses typed answers ("y", "yes", "n"), loops on "Please enter y (yes) or n (no).", and prints the announcements of other keys. 35 tests build a `Script` and answer through `ReadLine` and `ReadPassword` with strings, a protocol that relies on text (SPEC-refactoring 5.2).
Change: `Script` gets one typed field or function per question of `interact.UI` (start, backup choice, unlock method, password, recovery code); the prompts, parsing and printing go. Do it after RF-108, which removes `Output`.
Acceptance: about 140 lines fewer in `interacttest` and about 100 to 200 in the tests; every test that used a `Script` still runs and the coverage of the workflows doesn't drop.

## n. Found during the work

## Phases

| Phase | Items | When | Why then |
|---|---|---|---|

## Open questions

1. **RF-109: how to show a problem with the log file.** Decided 2026-10-24: as a warning in the result card that names the TEMP copy, shipped with a patch release as a fix.

## Not part of this round

- Editing the configuration in the window and dark mode: feature work, planned for later (GUI spec, decisions 1 and 4).
- From the size review (2026-10-24), left out because they would cost architecture or testability (section 3):
  - **The Restore backup window on the plan window of RF-98.** It is sized by the user, checks while the user types and has its own destination field; sharing more than the table and sizer of RF-99 and RF-100 would put options for all three on the shared window.
  - **One registry for window procedures** (`handlers` in [registry.go](../internal/gui/registry.go), `controls` in [widget/window.go](../internal/gui/widget/window.go)). A shared one would put Go objects into `gui/win32`, which only wraps the Windows API, or the top-level windows into `gui/widget`, whose controls are reusable. It would save about 40 lines.
  - **A common core of the three plan views** (`BackupPlanView`, `VerifyPlanView`, `RestoreView`). Their fields differ (Start is a `*Button` in two, a `Button` in one; the restore view has hints and checks), so it would couple three views that are tested on their own for a few lines.
  - **Merging `gui/view` into `gui`**: fewer types, but the window's logic would leave its tested package (92.7 %).
  - **Comments (about 4,200 lines) and the texts of `gui/view/strings.go`**: not code; the texts are frozen by standing constraint 2.
  - **Generated Win32 bindings and merging the 56 `win32` wrappers that each have one caller**: generated code counts too, and the wrappers keep `unsafe` in one package.

## Done when

## Carried over (filled in when the round closes)
