# Plan: Refactoring round after 2.0

| | |
|---|---|
| Status | Proposed 2026-10-06; Phases A and B done 2026-10-07 (open in B: the Process Monitor check of RF-52), Phase C done 2026-10-08; phases D and E open; open questions answered except RF-60 (Phase E) |
| Follows | [SPEC-refactoring.md](SPEC-refactoring.md): how a round works, the standing constraints, the review checklist |
| Baseline | `gui-redesign` at `dbc9402` (all numbers in section 2 are measured on it) |
| Branch | `refactor-2.0`, merged into `v2` with one pull request per phase (Phase A: #1); CI runs on each push to the pull request |
| Scope | Structure, dead and legacy code, performance, security hardening, tests, tooling, and docs. **No change to the backup format, the keys, or what a backup, restore, or verify does for the user.** |

## 1. Purpose

RestoreSafe 2.0 is functionally complete and well layered: `internal/architecture` enforces the import rules, the view logic is plain Go, and most packages below the GUI have 75-95 % test coverage. This first refactoring round lists what an expert review of the code found worth changing before the codebase grows further, and in which order to change it.

Items follow the format and priorities (P1-P3) of SPEC-refactoring section 6. Their IDs (`RF-n`) are unique within this round; elsewhere, cite them as "refactoring 2.0 RF-n".

## 2. Baseline (measured 2026-10-06)

| Measure | Value |
|---|---|
| Tracked files / Go lines | 262 / 40,636 (production and tests) |
| Go toolchain, module deps | go 1.27.1; `go.yaml.in/yaml/v3`, `golang.org/x/crypto`, `golang.org/x/sys` only |
| `go vet ./...` | clean |
| `go test ./...` | passes |
| `go test -cover ./...` | **fails** in `internal/gui/view`: `error generating coverage report: write \|1: file already closed` (RF-10) |
| `go test -race` | not run: needs cgo, which the build disables (RF-11) |
| staticcheck 2026.x (`-checks all` minus naming/doc style) | 10 findings besides ST1005: 6 unused (U1000), 1 dead store (SA4006), 2 test signatures (ST1008); ST1005 (error string style) on ~100 lines (RF-25) |
| `deadcode -test ./...` | 16 unreachable functions (RF-20) |
| `govulncheck ./...` | 0 called; 1 in a required module (`GO-2026-5932`, `x/crypto/openpgp`, not imported, no fix) |
| `gofmt -l` | 18 files, all because of CRLF in the working tree (RF-4) |
| CI | none; no `.github/` |
| Coverage, total | 46.1 % of statements |

Coverage by package (statements):

| Layer | Packages and coverage |
|---|---|
| security | cryptox 89.0, recovery 92.2, **yubikey 20.8** |
| config, logging | config 92.7, logging 86.6 |
| format | manifest 92.3, naming 92.9, setwriter 86.6, catalog 79.7, archive 75.6, **container 72.2** |
| fsx | 80.4 |
| workflow | health 96.0, plan 93.9, backup 85.1, verify 84.1, unlock 84.0, restore 83.9, interact 82.1, job 72.9, **restorepoint 69.3** |
| gui | flow 80.1, widget 73.8, view (fails, RF-10), **win32 9.1, gui 1.8** |
| cmd | **restoresafe 0, yubidiag 0** |

Largest production files: `gui/backups.go` 948 lines, `gui/wizard.go` 931, `security/yubikey/fido2.go` 917, `gui/overview.go` 693, `gui/view/backups.go` 651.

After Phase A (2026-10-07, `refactor-2.0` at `9b6b196`): `gofmt -l .`, `go tool staticcheck ./...` (with `staticcheck.conf`) and `go tool deadcode -test ./...` print nothing; `go test -cover -count=3 ./...` passes; coverage 52.1 % in total, most of the rise because `gui/view` (91.4 %) is measured now; 267 files, 41,087 Go lines. `-race` passes in CI (RF-11).

After Phase B (2026-10-07, `refactor-2.0` at `22257c0`): `gofmt -l .`, staticcheck and deadcode print nothing; all tests pass, including the 2.0.0 format fixtures; coverage 52.3 % in total, every package above its floor; 291 files, 41,924 Go lines (the fixtures add 2.4 MB of test data).

After Phase C (2026-10-08, `refactor-2.0` at `82ae779`): gofmt, staticcheck and deadcode print nothing; all tests pass, including the format fixtures; coverage 52.4 % in total, every package above its floor; no production error text contains "Remedy:"; `gui/flow` matches no workflow text; no production file in `internal/gui` is over 500 lines except `view/strings.go` (515); 312 files, 42,510 Go lines.

## 3. Constraints

The standing constraints of SPEC-refactoring section 2 apply. This round adds none; RF-41 creates the format fixtures that constraint 1 relies on.

## 4. Tooling and CI (Phase A)

**RF-1 (P1) Add CI.** There is no automated check; everything relies on running `go test` by hand before a commit.
Add `.github/workflows/ci.yml` on `windows-latest`, triggered on push and pull request:

1. `gofmt` check (after RF-4), `go vet ./...`
2. `go build ./...` with `CGO_ENABLED=0` (the release configuration)
3. `go test -count=1 -coverprofile ./...` (needs RF-10)
4. `go test -race ./...` with cgo enabled (RF-11)
5. `staticcheck ./...` with the project configuration (RF-25)
6. `govulncheck ./...`
7. Upload the coverage profile as an artifact; fail when a package drops below its floor in RF-40.

Acceptance: a pull request against `v2` shows all seven steps green.
Done in `fe55e57`, as three jobs (check, test, race). Step 7 in `d21ad47`: the floors of RF-40 (`scripts/ci/coverage-floors.txt`), checked per package. `staticcheck.conf` already leaves out ST1005 (the last step of RF-25), so the check is green before the error texts change. Failing tests and data races are reported as an annotation (`71839dd`). Accepted with [pull request #1](https://github.com/phsc84/RestoreSafe/pull/1): all steps green. CI runs on pull requests and on pushes to `main` and `v2`.

**RF-2 (P1) Pin the developer tools in `go.mod`.** `staticcheck` on this machine was built with go 1.25 and cannot analyse a go 1.27 module; `goversioninfo` is installed with `@latest` (DEVELOPMENT.md section 8), so two developers can build with different versions.
Use the `tool` directive (`go get -tool ...`) for `staticcheck`, `govulncheck`, `deadcode`, and `goversioninfo`, and call them as `go tool <name>` in `build.bat`, CI, and DEVELOPMENT.md. Versions are then in `go.mod`/`go.sum` and update like any dependency.
Acceptance: a fresh clone runs `go tool staticcheck ./...` without a separate install.
Done in `789f734`.

**RF-3 (P2) Make `build.bat` side-effect free and reproducible.**

- It runs `go mod tidy` first, so a release build can change `go.mod`/`go.sum`. Replace it with `go mod verify` and fail if `git diff --exit-code go.mod go.sum` reports a change.
- It stamps `main.Version`, and `main` copies it into `buildinfo.Version` at runtime ([cmd/restoresafe/main.go:37](../cmd/restoresafe/main.go#L37)). Stamp `RestoreSafe/internal/buildinfo.Version` directly with `-X`, and delete `main.Version` and `Options.Version` (the GUI reads `buildinfo.Version`).
- Write a `SHA256SUMS.txt` next to the ZIP (RF-58).

Acceptance: two builds of the same commit produce identical `RestoreSafe.exe` hashes; the build leaves `git status` clean.
Done in `d7b800c`. `go mod tidy -diff` checks `go.mod` and `go.sum` without changing them; `SHA256SUMS.txt` is in sha256sum format. Two builds gave the same exe hash.

**RF-4 (P1) Fix line endings.** The index holds LF, but 18 Go files have CRLF in the working tree (`git ls-files --eol` shows `i/lf w/crlf`), so `gofmt -l` lists them and any formatting check fails on Windows. `.gitattributes` only covers `*.bat`.
Add `* text=auto` and `*.go text eol=lf` (plus `*.ps1 text eol=crlf`, `*.md text`), then renormalise once (`git add --renormalize .`) in a commit of its own.
Acceptance: `gofmt -l .` prints nothing on a fresh Windows checkout with `core.autocrlf=true`.
Done in `8981fd5`; `25bb563` formats the four files that were not gofmt-clean apart from their line endings.

## 5. Test-tooling defects (Phase A)

**RF-10 (P1) `testutil.CaptureStdout` is not safe for parallel tests.** It swaps the process-global `os.Stdout` ([internal/testutil/stdout.go](../internal/testutil/stdout.go)). `TestBackupPlanOfARealPlan` calls it from parallel subtests ([internal/gui/view/plan_test.go:163-168](../internal/gui/view/plan_test.go#L163-L168)); one subtest restores another's closed pipe as `os.Stdout`, and the coverage writer then fails with `file already closed`. 14 test files use the helper, many of them with `t.Parallel()`. The helper can also deadlock: it reads the pipe only after `fn` returns, so output larger than the pipe buffer blocks `fn`.
Change: tests stop capturing `os.Stdout`. `interacttest.Script` already has an `Out io.Writer` field; tests pass a `bytes.Buffer` there. Delete `CaptureStdout` and the `stdout` fallback writer in [interacttest/script.go:29-33](../internal/workflow/interact/interacttest/script.go#L29-L33), so nothing in tests writes to the real `os.Stdout`.
Acceptance: `go test -cover -count=3 ./...` passes; `grep -r "os.Stdout" internal` finds no test code.
Done in `56a454d`. A nil `Script.Out` and a nil logger console now discard the output (the program always passes the UI's writer); tests that check the output pass a `testutil.Output`, which takes concurrent writes. `internal` no longer mentions `os.Stdout` at all.

**RF-11 (P1) Run the race detector.** The workflows, the health checker, the GUI bridge, and the decrypt pipeline all use goroutines, atomics, and mutexes, yet `-race` has never run because it needs cgo. Run it in CI with a MinGW gcc (preinstalled on GitHub's Windows images) and `CGO_ENABLED=1`; release builds stay `CGO_ENABLED=0`.
Acceptance: `go test -race ./...` passes in CI. Races found are fixed under this item.
Done in `fe55e57` (the `race` job of CI). The first runs found no data race. They found that four tests of locked files failed on the runner: an elevated process with the backup privilege opens a file held without sharing, because `os.Open` asks for backup semantics. `testutil/filelock.Hold` (`2c0948d`) removes the privilege from the test process. CI run 37669132251 is green.

**RF-12 (P3) Test helper signatures.** staticcheck ST1008 in [backup/keys_test.go:87](../internal/workflow/backup/keys_test.go#L87) and [unlock/unlock_test.go:89](../internal/workflow/unlock/unlock_test.go#L89): the error is not the last result. Reorder.
Done in `0a2f913`.

## 6. Dead code (Phase B)

**RF-20 (P2) Delete unreachable code.** `deadcode -test` and staticcheck agree on these:

| Symbol | Where | Action |
|---|---|---|
| `Builder.Entries` | [format/manifest/manifest.go:165](../internal/format/manifest/manifest.go#L165) | delete |
| `card.badge` | [gui/overview.go:418](../internal/gui/overview.go#L418) | delete |
| `AppendText`, `NMListViewParam`, `NMLVKeyDownParam`, `NMLVCustomDrawParam`, `ListHitGroup`, `ListKeyOf`, `Checked`, `DrawItemParam`, `StockObject`, `Ellipse`, `DrawFocusRect`, `AddTooltip` | `gui/win32/*` | delete; the wrapper grows when a caller needs a function, not ahead of it |
| `AssertFileContentEqual` | [testutil/assert.go:10](../internal/testutil/assert.go#L10) | delete |
| `health.StateOf` | [workflow/health/snapshot.go:160](../internal/workflow/health/snapshot.go#L160) | delete |
| `yubicoVID` | [cmd/yubidiag/main.go:36](../cmd/yubidiag/main.go#L36) | use it to filter the HID list, or delete |
| `client` dead store | [gui/settings.go:259](../internal/gui/settings.go#L259) | remove the assignment |

Not dead: the unused fields `cbSecond`, `pbSecond`, `cCredWithHmacSecretSaltList`, `pCredWithHmacSecretSaltList` in [yubikey/fido2.go:220-230](../internal/security/yubikey/fido2.go#L220-L230) keep the struct layout of `winwebauthn.h`. Keep them and add `//lint:ignore U1000 ABI layout of WEBAUTHN_*`, and add a test that checks `unsafe.Sizeof` and `unsafe.Offsetof` against the sizes in the comments, for every WebAuthn struct.

Acceptance: `go tool deadcode -test ./...` prints nothing; staticcheck reports no U1000 or SA4006. CI runs `deadcode` as a report (not failing), because a wrapper function added in one commit may get its caller in the next.
Done in `9b6b196`; the procs, types and constants that only the deleted wrappers used went with them.

**RF-21 (P2) Remove the auth-mode round trip.** `restore.authFactors` turns the key set's `AuthMode` into two booleans, and `config.AuthModeFromFactors` turns them back into the same `AuthMode` ([restore/workflow.go:101](../internal/workflow/restore/workflow.go#L101), [:237](../internal/workflow/restore/workflow.go#L237); [verify/workflow.go:179](../internal/workflow/verify/workflow.go#L179)). This is left over from 1.x, which read the factors from a `.challenge` file. Pass `config.AuthMode` through, and delete `AuthModeFromFactors` and its test.
Done in `b0751d3`; `config.AuthMode.UsesYubiKey` answers what the two booleans did.

**RF-22 (P2) One set of auth-mode constants.** `container.AuthModePassword/PasswordYubiKey/YubiKey` (`int`, [keyset.go:25-29](../internal/format/container/keyset.go#L25-L29)) duplicate `config.AuthMode*` (typed). `container` already imports `config`. Make `KeySet.AuthMode` a `config.AuthMode` (JSON stays a number, so the format is unchanged) and delete the `container` constants.
Done in `adf34f2`; the format fixtures confirm the JSON is unchanged.

**RF-23 (P3) The FIDO2 debug output goes nowhere in the app.** `RESTORESAFE_FIDO2_DEBUG=1` prints with `fmt.Printf` ([fido2.go:27-33](../internal/security/yubikey/fido2.go#L27-L33)), but `RestoreSafe.exe` is linked with `-H=windowsgui` and has no console; only `yubidiag` can show it. Replace the environment switch with a package-level `io.Writer` that `yubidiag` sets and the app leaves nil. (The `GetConsoleWindow` fallback in `dialogParent` stays: `yubidiag` is a console program and needs it.)
Done in `6189d5d`. yubidiag sets `yubikey.DebugOutput` when `RESTORESAFE_FIDO2_DEBUG=1`, so the README stays correct.

## 7. Legacy code and protocol (Phase C)

**RF-24 (P1) v1 wording in user-facing errors.** The YubiKey code still talks about a `.challenge` file, which 2.0 no longer has: "challenge file is corrupted" and "Ensure the .challenge file is unchanged and belongs to the same backup run as the .enc files" ([fido2.go:338](../internal/security/yubikey/fido2.go#L338), [:466](../internal/security/yubikey/fido2.go#L466)), plus the section comment "Challenge file format" ([:277](../internal/security/yubikey/fido2.go#L277)) and [config/config.go:30](../internal/config/config.go#L30). The YubiKey challenge is now part of the key set in the backup header. Reword to "the YubiKey data in the backup header is damaged", with the remedy to use another backup. This is a user-visible text change; add it to CHANGELOG.
Done in `6179ed7`. A damaged challenge reaches the user inside the header error ("Invalid backup header: ... YubiKey challenge is damaged ... Remedy: Use an unmodified backup created by RestoreSafe."), which already gives the remedy. `DeriveFIDO2SecretForRestore`, the 1.x path with the `.challenge` remedy, had no caller left and is deleted; its tests now test `DeriveFIDO2SecretAny`. No CHANGELOG entry: the old wording was never released in a 2.x version.

**RF-25 (P2) Typed user errors instead of "Remedy:" strings.** 145 production error strings embed the user message and its remedy as text ("... Remedy: ..."), capitalised and with punctuation, which is why ST1005 fires on ~100 lines. The frontend can only show them whole; tests can only compare text; the same remedy is repeated with slightly different wording.
Introduce in the bottom layer (`fsx` or a new `internal/problem`, added to the architecture test):

```go
type Error struct {
    Code   interact.Code // or a code type moved down with it
    Msg    string        // what happened
    Remedy string        // what the user does about it
    Err    error         // cause, for errors.Is/As
}
```

`Error()` returns `Msg + " Remedy: " + Remedy`, so logs and the existing tests keep their text. The GUI uses `errors.As` to show the remedy on its own line and to pick the action button by `Code`. Convert package by package, starting with `container`, `archive`, and `config` (the most strings). Then add `staticcheck.conf` with `-ST1005` and a comment that error texts are user-facing sentences on purpose.
Acceptance: no production error string contains "Remedy:" literally; the GUI shows remedies from the field.
Decided 2026-10-07: all sites, package by package, starting with the errors that carry a `Code` (the ones the GUI acts on). The round may stop after those without leaving anything broken.
Done up to the last step (`c1bee60` to `2410eae`): `internal/problem` (bottom layer) holds `Error{Msg, Remedy, OwnLine, Err}`; every production error that carried "Remedy:" is one now, converted by a syntax-tree tool where the text was a literal and by hand elsewhere, with unchanged text (the tests compare it). Error types that callers find with `errors.As` (unreadable file, incomplete set, missing parts, renamed files) are the cause of a `problem.Error`. Issues and plans carry the remedy in a field (`Issue.Remedy`, `FolderPlan.Remedy`, `SetPlan.Remedy`, `RestoreSetPlan.OutputRemedy`), and the GUI shows it from there. Differences from the item: no `Code` field yet, because no caller acts on an error's code (the GUI picks actions by the codes of issues and problems); and log and console lines, the printed plan's advisory row and the health problems' Detail keep "Remedy:" as text, because they are the log format (standing constraint 2) or, for Detail, the full text GUI spec 11.8 asks for. Last step, after RF-26: the credential message is the only text the GUI still splits; then `problem.Split`'s text fallback and `textWithRemedy` go. Last step done in `e42d666`: `problem.Split` takes the remedy from a `problem.Error` only; the GUI splits no text.

**RF-26 (P2) Replace the text protocol between workflows and GUI.** `interact.UI` is still shaped like the old console: questions are prompts as strings, and messages are lines written to `Output()`. The GUI recovers meaning by matching text:

- `flow.UI.Password` decides whether a pending password applies by checking whether the prompt contains "recovery code" ([gui/flow/ui.go:147](../internal/gui/flow/ui.go#L147)).
- `flow.UI.recentMessage` takes the last output line as the dialog message ("Wrong password. 2 attempt(s) remaining.") unless it starts with `[`, which marks a log line ([gui/flow/ui.go:135-145](../internal/gui/flow/ui.go#L135-L145)).

Rewording a prompt or a log line silently changes the dialogs. Change:

- `Password(kind SecretKind, attempt Attempt)` with `SecretKind` = password, recovery code, and `Attempt{N, Max int; Reason}`; the GUI words the prompt in `gui/view/strings.go`. The question also carries the creation date of the key set it unlocks, so the unlock dialog names it as GUI spec CR-1 asks (accepted as a known difference for 2.0.0, PLAN-gui-redesign D4).
- A typed `Notice(n Notice)` for messages a dialog may show; `Output()` stays for the log text only.
- `interacttest.Script` prints the same wording as today, so the e2e scripts' expected output does not change.

Acceptance: `gui/flow` contains no `strings.Contains` or prefix check on workflow text; architecture test unchanged.
Done in `4f40d12`. The questions are types in `interact`: `SecretQuestion` (password or recovery code, the operation, `OtherKeys` with their creation date for CR-1, an `Attempt` with the reason the previous try failed), `UnlockChoice` (the `AuthMode` instead of its label), `NewPasswordQuestion` (the minimum length) and `SpareQuestion`. Instead of a general `Notice`, every message a dialog showed is part of the question it belongs to, which was enough: the workflows write no message for a dialog any more, and `Output()` carries log and console text only. `interacttest.Script` prints the old wording, so console output and the e2e tests are unchanged. `gui/flow` matches no text, and `gui/view` reads no prompt or message (its regular expressions are gone). One GUI text changed: the password dialog after a YubiKey touch no longer repeats "YubiKey connected. Follow the on-screen prompts", which came too late to help.

**RF-27 (P3) Stale package docs.** `restore`'s doc still lists "Let the user choose which backup(s) to restore" ([restore/workflow.go:1-5](../internal/workflow/restore/workflow.go#L1-L5)); the choice is made before `Run` since decision 2 of PLAN-gui-redesign. Review every `doc.go` and package comment against the current behaviour.
Done in `46112ff`: restore and backup list their steps in today's order, interact says what Output is for since RF-26, catalog got a package comment, and job, logging and fsx name what they hold now. The other packages' comments were checked and are current.

## 8. Modularity and duplication (Phase C)

**RF-30 (P2) Restore and verify share one preflight.** `restore/workflow.go` and `verify/workflow.go` duplicate: the preflight item build with base lookup (`buildRestorePreflight` / `buildVerifyPreflight`), the "Backup selection" rows, the size estimate, the "with full backup ... (parts: n)" detail, the auth rows, the per-entry loop, and the cancel handling. Move the shared part into `workflow/job` (it may not go into `restore` or `verify`, architecture rule): `job.SelectionPreflight(selected, inventory) []SelectionItem` and `job.SelectionRows(...)`. Restore adds its destination checks on top.
Acceptance: the duplicated functions exist once; the restore and verify tests pass unchanged.
Done in `c025396`: `job.SelectionPreflight`, `SelectionRows`, `SelectionBytes`, `SelectionItem.SetPlan`, `EachRestorePoint`, `OpenRestorePoint` and `LogStart`, with tests of their own (`job` 83.5 %). Verify uses the shared item as it is; restore embeds it and adds its destination folder. The tests pass with only the renames of verify's item. The cancel branches stay in each workflow: their messages differ.

**RF-31 (P2) Parameter structs for long signatures.** Several functions take 8-10 positional parameters, several of them strings or `*atomic.Int64` of the same type, which invites swapped arguments: `runRestoreOperation` (10), `RunSectionPipeline` (9, two of them prose strings), `restorePreflightReport` (7), `backupDirectory`, `logPartSummary`. Use a struct per operation (`restore.operation{...}`) with methods instead of passing the same eight values down three levels.
Done in `c9a6cdf`, `88d2792`, `20ec84b` and `afd6b68`: restore, verify and backup each have an `operation` struct whose methods share the run's values; `logPartSummary` takes `setwriter.Counters`; `restorepoint.Process(..., verifyOnly, ...)` became `Restore` and `Verify` with an `Output` (logger and byte counter), and the section reader derives its prose itself instead of taking it as two parameters. `restorePreflightReport` is down to 6 parameters since RF-21 and stays.

**RF-32 (P3) Context as a parameter, not a field.** `archive.BuildOptions.Context` and `setwriter.Params.Context` keep the context in a struct, and `nil` means background. Make it the first parameter (`BuildTar(ctx, w, opts, mb)`, `setwriter.Write(ctx, p)`), as the workflows already do.
Done in `974cc48`. `fsx.ContextWriter` keeps its context as a field on purpose: an `io.Writer` has nowhere else to take it from.

**RF-33 (P2) Split the large GUI files.** `gui/backups.go` (948), `gui/wizard.go` (931) and `gui/overview.go` (693) each mix building controls, layout, list custom-draw, and commands. `overview.go` also holds helpers that 8 files use (`card`, `layoutRow`, `toneColor`, `heroColors`, `badgeColors`, `segmentColor`, `glyphOf`, `measure`).

- Move `card` and `layoutRow` to `gui/card.go`, and the mapping from `view` tones, badges and glyphs to `widget` colours and glyphs to `gui/palette.go`.
- Split each page into `<page>.go` (struct, build, update), `<page>_layout.go`, and `<page>_list.go` (ListView fill and custom draw), where the page has a list.
- Target: no file in `gui` over 500 lines.
- Update the target structure in PLAN-gui-redesign section 3, which still lists files that were never created (`verifydialog.go`, `credentials.go`, `widget/list.go`, `widget/stack.go`, `view/progress.go`), or move the plan to the archive (RF-62).

Done in `515ff1d`, `28ae632`, `b49acf5` and `0250574`, moving code unchanged with a syntax-tree tool: `card.go`, `palette.go` and `page.go` hold what the pages share; the Create backup page, the Restore backup page and the Restore window are split into the page and its `_layout`, `_list` and (for the window) `_check` files; `gui/view` got `vocabulary.go`, `verifyplan.go`, `logviewer.go` and `restorepage_actions.go`, and `gui/win32` got `display.go`. The files the item names are `restorepage.go`, `restoredialog.go` and `createpage.go` since the pages were renamed. Every production file in `internal/gui` is under 500 lines except `view/strings.go` (515), which stays whole: it is the one place of every user-visible string (GUI spec 3.6). PLAN-gui-redesign's target structure is not updated: that plan is deleted after the 2.0.0 release (RF-62).

**RF-34 (P3) Package-level GUI state.** `theApp`, `activeDetails`, `activeCredential`, `dialogWindows`, and the `*ClassExists` flags are globals ([app.go:124](../internal/gui/app.go#L124), [details.go:30](../internal/gui/details.go#L30), [dialogs.go:47](../internal/gui/dialogs.go#L47), [dialogwin.go:33](../internal/gui/dialogwin.go#L33)). A Win32 window procedure needs one way to find its Go object, but one is enough: keep a single registry `map[HWND]handler` (or `GWLP_USERDATA` per window) in the `gui` package and make the dialog state fields of `app`. This lets more of `gui` be tested with a hidden window (RF-40).
Done in `82ae779`: `registry.go` maps every top-level window to its handler (`app`, `dialogWindow`, the details viewer, each with a `message` method), served by one window procedure; `registerClass` registers every class once; the open credential dialog is a field of `app`. `theApp`, `activeDetails`, `activeCredential`, `dialogWindows` and the class flags are gone. Checked on the real window with `Smoke-BackupRestore.ps1`, `Check-States.ps1` (all 13 conditions; `Argon2Capped` needs a configuration without its own `argon2` block, as the script appends one), `Accessibility.ps1`, and an ad-hoc check of the details viewer. Testing `gui` with a hidden window (the item's motive) is now possible; writing such tests belongs to the coverage targets of RF-40.

## 9. Performance (Phase D)

The two opt-in throughput benchmarks (DEVELOPMENT.md section 11) are the yardstick for this section. Before any change, record their medians on an SSD and on the network share in this document; every change in this section reports the before and after.

**RF-35 (P2) Profile first.** Add `-cpuprofile` and `-memprofile` runs of `TestThroughputBenchmarkBackup` and `...Restore` and note the top 10 functions here. RF-36 and RF-38 are only done if the profile shows their cost. AES-GCM with AES-NI and SHA-256 with SHA-NI are each well above 1 GB/s per core, so the bottleneck may be I/O, not crypto.
Done 2026-10-08. Baseline (1.7 GB of benchmark data, median of 3 runs; SSD: the local NVMe disk, network: the owner's share `M:\RestoreSafe-test`):

| Where | Backup | Restore |
|---|---|---|
| SSD | 5.1–5.2 s, 329–338 MiB/s | 6.8 s, 255 MiB/s |
| Network share | 58.8 s, 29.3 MiB/s | 30.3 s, 56.9 MiB/s |

Top functions by CPU (flat), backup: `runtime.cgocall` (the Windows file syscalls) 71 %, `sha256.blockSHANI` 8.5 %, `gcm.gcmAesEnc` 4.3 %, `runtime.memmove` 3.0 %, `runtime.memclrNoHeapPointers` 2.9 % (clearing the new Seal buffers), ChaCha8 (the benchmark's data generation, not RestoreSafe) 2.8 %, `runtime.semawakeup` 1.4 %, `runtime.sysUnusedOS` 1.1 %. Restore: `runtime.cgocall` 65 %, `sha256.blockSHANI` 14.7 %, `gcm.gcmAesDec` 5.9 %, `runtime.memmove` 5.5 %, `runtime.semawakeup` 1.4 %. So both are I/O-bound; crypto is under 25 % of the CPU time, as expected.
Allocations: `gcm.sliceForAppend` (the `Seal(nil, ...)` of RF-36) is 6.7 GB of 8.7 GB allocated in a backup, about 4 GB per GB backed up. Restore allocates little (`DecryptStream` reuses its buffer). RF-36 is done for the allocations and the GC work; it will not change the throughput much.
RF-59 (`CreateFile` without backup semantics) was checked against the commit before it, alternating runs on the same data: 337.6 and 328.8 MiB/s against 329.0 and 319.7 MiB/s, so no cost. A first run of 283 MiB/s was variance.

**RF-36 (P2) No allocation per chunk.** `cryptox.writeEncryptedChunk` calls `gcm.Seal(nil, ...)`, which allocates a new 8 MiB + 16 B slice for every chunk, and `chunkNonce` and `chunkAAD` allocate per chunk ([crypto.go:318](../internal/security/cryptox/crypto.go#L318)). A 100 GB backup makes ~12,800 large allocations, and the GC work that goes with them. Reuse one ciphertext buffer, one nonce array, and one AAD buffer per stream (`DecryptStream` already reuses its buffer). Combine the 5-byte prefix and the ciphertext into one `Write`.
Acceptance: a `testing.B` benchmark of `EncryptStream` over 256 MiB shows a constant number of allocations, independent of the size.
Done 2026-10-08. `chunkParams` holds the nonce and AAD buffers of a stream; `Seal` appends to one prefix-plus-ciphertext buffer, written with one `Write`; `DecryptStream` no longer allocates its prefix per chunk either. Tests: `TestStreamAllocationsIndependentOfSize` (encrypt and decrypt allocate the same for 2 and 16 chunks; a test instead of a `testing.B` benchmark, so that CI checks it on every run) and `TestEncryptStreamKnownAnswer` (the SHA-256 of the output for a fixed key and input, taken from the code before the change: the format fixtures check reading, this checks writing). SSD before and after: allocations in a backup 8.7 GB to 0.36 GB; backup 318 MiB/s, restore 257 MiB/s, both within the runs' variance.

**RF-37 (P2) Go benchmarks that CI can run.** The throughput tests need `RESTORESAFE_BENCH_ROOT` and 1.7 GB of data, so they never run by themselves. Add small `testing.B` benchmarks for `EncryptStream`, `DecryptStream`, `archive.BuildTar` over a temp tree, manifest encode and parse at 100k entries, and `catalog.Inventory` with 500 sets. CI runs them with `-benchtime=1x` only to keep them compiling; a developer compares with `benchstat`.
Done 2026-10-08. `bench_test.go` in cryptox, archive, manifest and catalog; CI runs them once in the test job; CONTRIBUTING.md section 4 shows `benchstat`. On the owner's PC: encrypt 1.7 GB/s, decrypt 2.1 GB/s (one core, so crypto is not the bottleneck); manifest encode 100k entries 0.28 s, decode 0.53 s. Two findings: `BuildTar` opens a just-written file in about 14 ms the first time and 70 µs afterwards, the same with `os.Open` and with RF-59's `CreateFile` (the virus scanner, not the code). `Inventory` read the backup directory twice per set (`InspectSet` and `OpenSet` each called `CollectParts`), quadratic in the number of sets and a round trip per set on a network share; it now reads it once: 500 sets 4.9 s to 2.0 s, 242 MB to 4 MB allocated.

**RF-38 (P3, only if RF-35 shows CPU-bound crypto) Parallel chunk encryption.** The pipeline is one producer (walk, read, SHA-256, TAR) and one consumer (encrypt, write) joined by an unbuffered `io.Pipe`. Chunks are independent (counter nonce, per-chunk AAD), so N workers can seal chunks while one writer keeps the order. Bounds: N = min(GOMAXPROCS, 4), memory ≤ 2 × N × 8 MiB. The on-disk output must be byte-identical to the sequential writer for the same key, which a test checks. Same for decrypt, with read-ahead of the next part.
Dropped 2026-10-07: the owner wants simple and robust code, so the crypto pipeline stays sequential whatever the profile shows. RF-35 still profiles, for RF-36 and RF-39.

**RF-39 (P3) Manifest memory against SPEC 5.3.** SPEC-2.0 section 5.3 says "the manifest is written as a stream", but `container.Write` takes the whole manifest as `[]byte` from `manifestFn` ([set.go:55](../internal/format/container/set.go#L55)) and `ReadManifest` reads it into a `bytes.Buffer`. At 1,000,000 files that is ~300 MB once more, on top of the builder's entries. Either stream it (`manifestFn func(io.Writer) error`) or correct the spec. Add a test with 1,000,000 synthetic entries that checks the peak heap stays under a stated limit.
Done 2026-10-08. `manifestFn` is `func(io.Writer) error`; `Builder.Encode` writes line by line, `manifest.Decode` parses from a reader; `container.Write` and `ReadManifest` join them to the crypto stream with an `io.Pipe` and hash the plaintext on the way. `Validate` drops its second path map (`seenFolded` finds exact duplicates too). `TestManifestStreamsAtOneMillionEntries` (211 MB manifest): writing adds 107 MiB to the heap at peak, reading 149 MiB on top of the parsed entries; the limit is the size of the serialized manifest, which the old code exceeded (424 MiB when writing from one buffer). Skipped under `-race`. SPEC 5.3 now says "written and read as a stream".

## 10. Security hardening (Phase B for P1, Phase D for the rest)

**RF-50 (P1) Restore and verify take the backup-directory lock.** Only `backup.Run` calls `fsx.AcquireBackupLock` ([backup/workflow.go:43](../internal/workflow/backup/workflow.go#L43)). A second RestoreSafe window (e.g. a second configuration with the same backup directory) can apply retention and delete the parts of a set that the first window is restoring. Make the lock a reader/writer lock: backup takes it exclusively (as now), restore and verify take it shared (`LockFileEx` without `LOCKFILE_EXCLUSIVE_LOCK`). Restore and verify then fail fast with "a backup is running" instead of failing halfway with a missing part.
Done in `0282e6d`. `fsx.AcquireReadLock` takes the lock shared; restore and verify call it through `job.LockForReading` before their first question. `TestRestoreAndVerifyWaitForARunningBackup` (e2e) covers all four combinations. CHANGELOG has an entry.

**RF-51 (P1) No silent no-op lock.** When the lock file cannot be created, `AcquireBackupLock` returns an empty lock and no error ([fsx/lock.go:27-33](../internal/fsx/lock.go#L27-L33)), so two backups can then run at the same time. Return a warning the workflow logs and shows in the plan ("RestoreSafe can't lock the backup folder; don't start a second backup"). A read-only backup directory can't be backed up into anyway, so for backup this is an error.
Done in `0282e6d`. A backup fails when the lock file can't be created. A restore or verification goes on and shows the warning `BACKUP_DIR_NOT_LOCKED` (new code, GUI spec 11.8) in its plan and log. Only the exclusive holder removes the lock file, so a reader never deletes it under another reader.

**RF-52 (P1) Restrict DLL search.** RestoreSafe is a portable exe that users run from a download or USB folder. The WebAuthn and Win32 procs use `NewLazySystemDLL` (good), but the process's default DLL search order still includes the exe's folder for DLLs that Windows or the runtime loads implicitly. Call `windows.SetDefaultDllDirectories(windows.LOAD_LIBRARY_SEARCH_SYSTEM32)` as the first statement of `main` in both commands, before any window or WebAuthn call. Check by hand with Process Monitor that no DLL is looked up in the exe's folder.
Done in `d11617c`. No package `init` touches the Windows API before `main`, and the window starts as before. Open: the check with Process Monitor by hand.

**RF-53 (P2) Recovery code as bytes.** `recovery.Code` stores the code in `string` fields and `Secret()` converts to a new `[]byte` ([recovery.go:25-32](../internal/security/recovery/recovery.go#L25-L32)). Strings cannot be zeroed, so every recovery code generated or typed stays in memory until the GC reuses the space. The same applies to `ShowRecoveryCode(code string)` in `interact.UI` and `win32.CopySecretText(owner, text string)`. Keep the code as `[]byte` end to end, give `Code` a `Zero()` method, and zero the UTF-16 copy after `SetClipboardData` copied it. Passwords already follow this rule; this brings the recovery code in line.
Done 2026-10-08. `recovery.Code` keeps its data characters as `[]byte` with `Zero()`; `Display()` replaces `String()`, `Parse` takes `[]byte` and fills a buffer that never grows. `ShowRecoveryCode`, the GUI flow and `view.CredentialDialog.Code` carry bytes (the separate `Copy` field is gone: the Copy button copies `Code`). New `win32.SetSecretText` and `win32.CopySecret` convert to UTF-16 without a string and zero their copy; what remains is the copy inside the STATIC control and on the clipboard, which Windows owns. The backup workflow zeroes the code once its slot is made, the unlock zeroes the parsed code. Checked with `Smoke-BackupRestore.ps1` on new keys with a recovery code (the script reads the code from the dialog).

**RF-54 (P2) Fuzz the parsers that read untrusted bytes.** A backup directory can hold files from anywhere. Add Go fuzz tests (`func FuzzX(f *testing.F)`) with the existing valid inputs as seeds for: `container.ReadHeader`, `KeySet.Validate`, `container.Open` (trailer), `cryptox.DecryptStream` (framing), `manifest` parsing, `manifest.ValidatePath`, `naming.ParsePartFileName` and `ParseLogFileName`, `yubikey.ParseChallengeJSON`, `recovery.Parse`, and `config.Load`. Properties: no panic; no allocation above the stated bounds (64 KiB header, 1 MiB manifest line, 4 GiB Argon2 memory); `ValidatePath` never accepts a path that `filepath.Join(dest, p)` puts outside `dest`. CI runs each fuzz target for 30 s; a crash is committed to `testdata/fuzz` as a regression test.
Done 2026-10-08. Fuzz targets: `FuzzReadHeader` (now also: no Argon2 memory above 4 GiB; `KeySet.Validate` runs inside it), `FuzzDecodeTrailer`, `FuzzOpen` (header and trailer from memory through `readStructure`; one file per input made the virus scanner the bottleneck), `FuzzDecryptStream` (what it accepts is exactly what `EncryptStream` writes), manifest `FuzzParse` and `FuzzValidatePath` (never outside the destination, never another volume), `FuzzParsePartFileName` and `FuzzParseLogFileName` (round trip), `FuzzParseChallengeJSON`, recovery `FuzzParse`, config `FuzzParse` (Argon2 within bounds). The 1 MiB manifest line bound is `maxLineBytes` in the scanner. CI job **Fuzzing** runs `scripts/ci/fuzz.sh 30`. Found: `ParsePartFileName` accepted `/` in the folder name, so the name written again was a path; it refuses path separators now, and the input is kept in `testdata/fuzz`. CONTRIBUTING.md section 4 describes the benchmarks and the fuzzing.

**RF-55 (P2) Restore into a destination without reparse points.** `Restorer.targetPath` checks the path lexically, and directories are created with `os.Mkdir` and files with `O_EXCL`, which is sound for a new folder. It does not check that the destination folder itself, or a folder created during the restore, is not swapped for a junction by another process between `Mkdir` and `OpenFile`. Open the destination root once with `FILE_FLAG_OPEN_REPARSE_POINT` and reject it if it is a reparse point, and check each created directory's attributes before writing into it. Low likelihood (needs a local process racing the restore); low cost.
Done 2026-10-08. `checkNotReparsePoint` (`GetFileAttributes`, which does not follow the last path element) refuses the destination, each directory right after `Mkdir`, and the parent directory before each file is created; one system call per file. A process that swaps a folder between that check and `CreateFile` is still possible; closing that gap would need handle-relative opens, which the low likelihood does not justify. Tests with real junctions (`mklink /J`, no administrator rights needed). CHANGELOG under Changed.

**RF-56 (P3) Cap Argon2 memory per unlock from a header.** A crafted header can ask for 4 GiB of Argon2 memory for each slot RestoreSafe tries ([crypto.go:83-88](../internal/security/cryptox/crypto.go#L83-L88)). That is only a denial of service of the local process, but the health check opens headers automatically at start. Check that the health check never derives keys (it should only read structure), and add a test that it does not.
Done 2026-10-08. The health check (`TakeSnapshot` and `Check`) only reads the structure: `TestHealthCheckDerivesNoKeys` adds a set whose key slots ask for 20 passes over 4 GiB and checks that both allocate less than 1 GiB together.

**RF-57 (P3) Document what is plaintext.** The set header is plaintext JSON: folder name, dates, chain and run ID, app version, and the key set (salts, wrapped keys, YubiKey credential IDs). The README says "AES-256-GCM encryption (content and metadata/file names)". Make the README precise: file and folder names *inside* a backup are encrypted; the backed-up folder's name, the dates, and the key slots are not.
Done 2026-10-08. README: the feature line names what is encrypted, and "What is not encrypted" under "How your backups are locked" lists the header fields readable without unlocking.

**RF-58 (P2) Release integrity.** Releases are a ZIP with no checksum or signature. Publish `SHA256SUMS.txt` with every release (RF-3) and the command to check it (`Get-FileHash`) in the README. Authenticode signing is an open question (section 14).
Decided 2026-10-07: checksums only, no Authenticode signing (no running costs for a tool that earns nothing). The README says that SmartScreen may warn about the downloaded exe ("unknown publisher", More info, Run anyway) and how to check `SHA256SUMS.txt`. Done in `d32c6d9` (README) and `d7b800c` (SHA256SUMS.txt).

**RF-59 (P1) Elevated, RestoreSafe reads files that other programs hold locked.** `archive.writeFile` opens a source file with `os.Open` ([build.go:314](../internal/format/archive/build.go#L314)), which asks for backup semantics (`FILE_FLAG_BACKUP_SEMANTICS`). In a process that holds the backup privilege, as an elevated administrator does, Windows then skips the share-mode check: a file another program holds without sharing, such as a PST open in Outlook, is read anyway, possibly while it is being written, and the backup holds an inconsistent copy instead of skipping the file. Found by the first CI runs (RF-11): the runner is elevated, and four tests of locked files failed until `testutil/filelock` removed the privilege from the test process (`2c0948d`). Unelevated, which is how RestoreSafe normally runs, the lock holds.
Change: open source files for reading without backup semantics (`windows.CreateFile` with `GENERIC_READ`, `FILE_SHARE_READ|FILE_SHARE_WRITE|FILE_SHARE_DELETE`, `FILE_FLAG_SEQUENTIAL_SCAN`), so a locked file is unreadable, and `on_unreadable_file` applies, whether RestoreSafe runs elevated or not. Opening for attributes keeps backup semantics; it reads no file content and needs them for directories. Then `filelock.Hold` stops removing the privilege, so the locked-file tests run elevated on CI. User-visible only when elevated: a locked file is skipped (or fails the backup) instead of being copied; add it to CHANGELOG.
Acceptance: the locked-file tests of `format/archive` and `e2e` pass on the elevated CI runner with the backup privilege in place.
Done in `22257c0`, with two changes from the item: the share mode is that of `os.Open` (no `FILE_SHARE_DELETE`), and no `FILE_FLAG_SEQUENTIAL_SCAN`, so nothing changes but the backup semantics (a performance flag would need a measurement first). `extendedPath` keeps the long-path handling of `os.Open` and now serves `openForAttributes` too. CI run 37684035665 passed on the elevated runner with the privilege in place.

## 11. Quality assurance (Phase B and ongoing)

**RF-40 (P1) Coverage floors per package, not a global number.** A global percentage hides that the format layer is covered and the GUI is not. CI fails when a package falls below its floor:

| Packages | Floor now | Target after this spec |
|---|---|---|
| security/cryptox, security/recovery, config, format/manifest, format/naming, workflow/plan, workflow/health | 85 % | 90 % |
| format/archive, format/catalog, format/container, format/setwriter, workflow/backup, restore, verify, unlock, restorepoint, job, interact, fsx, logging | 70 % | 85 % |
| gui/view, gui/flow, gui/widget | 70 % | 85 % |
| security/yubikey | 20 % | 60 % (everything except the WebAuthn calls, behind the existing `fido2MakeCredFn`/`fido2GetHmacFn` seams) |
| gui, gui/win32, cmd | none | measured only; covered by RF-43 |

`format/container` (72 %) and `restorepoint` (69 %) are the packages that decide whether a damaged backup is detected; their error paths come first.

Floors "now" enforced by CI since `d21ad47`; `cd5d90f` raised `restorepoint` from 65.3 % (it had fallen below its floor since the baseline) to 91.7 %. Open: the targets, starting with the error paths of `format/container` (72.2 %).
Phase B part done: the floors are enforced. The targets belong to the end of the round.

**RF-41 (P1) Format compatibility fixtures.** Nothing stops a refactoring from changing the bytes RestoreSafe writes or what it accepts. Commit a small set of backups made by the 2.0.0 release build to `internal/format/testdata/v2.0.0/`: a full and a differential of a tree with special cases (empty file, empty folder, Unicode and long names, read-only/hidden/system attributes, a skipped file), with password, password + recovery slot (the test uses the recovery code), and Argon2 at the minimum to keep the test fast. A test restores each and compares tree, content, times, and attributes with a recorded listing. A second test checks that a set written by the current code with the fixed inputs (seeded randomness through a test hook) produces the same header structure and trailer.
Acceptance: the fixtures exist before Phase C starts; they never change after 2.0.0 is released.
Done in `17942af`, with two changes from the item. The fixtures were written by the code of `refactor-2.0` before the 2.0.0 release, as no release build exists yet: nothing before the release may change the format, or they are written again (`TestWriteFormatFixtures`, after deleting the folder). And instead of seeded randomness, `TestFormatFixturesStructure` compares the structure of freshly written sets with the fixtures (header prefix, keys and types of the header JSON, trailer), which needs no randomness hook in the program. Both tests were checked by breaking the program on purpose: a new header field and attributes that are not restored each fail them. The folder of the restored source is new on restore; the format keeps the times of what is inside it, so the listing records none for it.

**RF-42 (P2) Fault-injection matrix.** Make sure the e2e tests cover each of these for backup, restore, and verify, and add the missing ones with `testutil/scenario`: disk full during a part; a part deleted, truncated, or with one bit flipped (header, data, manifest, trailer); the base of a differential missing; cancel at each phase; a source file locked or vanishing mid-read; the destination folder appearing between plan and start. Each row names the test that covers it, in a table in `internal/e2e/doc.go`.
Done 2026-10-08. The matrix is in `internal/e2e/doc.go` (now also the package comment). New: `TestDamagedSetIsRefused` (part deleted, truncated, one bit in the header syntax, a header value, data, manifest, trailer; verify and restore through the workflows, with the expected cause), `TestCancelAtEachPhase` (every phase of backup, restore, verify), `setwriter.TestWriteFailingInASecondPartRemovesAllParts`, `health.TestBaseMissingNextBackupIsFull`, and archive tests for a source file that vanishes, shrinks or grows while it is read (two test hooks in `writeFile`, nil in production). Changed: a restore cancelled before its first byte no longer leaves an empty folder marked INCOMPLETE. A cancel during the cleanup lets the run complete, by design (retention deletes whole chains). Disk full during a restore is not simulated: a write error takes the path of a damaged data section.

**RF-43 (P2) GUI smoke test in CI.** `scripts/gui-test` drives the real window through UI Automation, but only by hand. Run `Smoke-BackupRestore.ps1` and `Check-States.ps1` in CI on a Windows runner with an interactive session, with password-only keys. If no interactive session is available, they stay in the release checklist (GUI-TEST-CHECKLIST.md) and that is stated there.

**RF-44 (P3) WebAuthn struct layout test.** See RF-20: a test compares `unsafe.Sizeof`/`Offsetof` of every `webauthn*` struct with the sizes and offsets in its comment. A wrong offset there makes the YubiKey fail only on real hardware, which CI does not have.
Done in `9b6b196`: `TestWebAuthnStructLayout` reads the sizes and offsets from the comments in `fido2.go`, so a struct and its comment cannot drift apart.

## 12. Project structure and documentation (Phase E)

**RF-60 (P3) Module path.** The module is named `RestoreSafe`. Go tooling expects a lower-case path that can be fetched, e.g. `github.com/phsc84/restoresafe`; tools like `goimports` then group local imports correctly, and `-X` flags and `go install` work as usual. Renaming changes every import line (mechanical, one commit). Open question (section 14).
Decided 2026-10-08: rename to `github.com/phsc84/restoresafe` (lowercase; GitHub resolves the repository name case-insensitively) as the last step of Phase D, before the owner's full test. The import blocks show why: `"RestoreSafe/internal/..."` sorts into the standard library's group, as Go treats a first path element without a dot like the standard library.

**RF-61 (P3) Root package.** [sample.go](../sample.go) exists only to `//go:embed config-SAMPLE.yaml` (a package cannot embed files from a parent directory). Keep the technique; rename the file to `embed.go` and keep the package comment, so its purpose is clear from the file list.

**RF-62 (P2) Sort the docs.** Partly done on 2026-10-06: the specs are named `SPEC-2.0.md` and `SPEC-gui.md`, their status lines are current, the finished implementation phases are out of the 2.0 spec, code comments say "2.0 spec n" or "GUI spec n", [docs/README.md](README.md) lists the documents, the mockups are deleted, and SPEC-refactoring.md describes refactoring rounds in general. Left: once 2.0.0 is released, delete the GUI plan (its section 9), remove GUI spec 17.2 (which only points to it), and update the index. Finished plans are deleted, not archived (SPEC-refactoring section 8), so there is no `docs/archive/`.

```text
docs/
├── README.md                index: which document covers what
├── SPEC-2.0.md              living: format and behaviour
├── SPEC-gui.md              living: the window
├── SPEC-refactoring.md      how a refactoring round works
├── PLAN-refactoring-2.0.md  this round, until done
├── GUI-TEST-CHECKLIST.md
└── images/
```

**RF-63 (P2) Fix documentation drift.**

- README "Main configuration options" gives `retention_keep` a default of `3`; the program's default is `0` (keep all) and only `config-SAMPLE.yaml` sets 3 ([config/keys.go:30](../internal/config/keys.go#L30)). Name the column "In config-SAMPLE.yaml", or list both.
- DEVELOPMENT.md section 10 refers to `docs\REFACTORING-PLAN.md`, which does not exist. Point it at the architecture test and SPEC-refactoring.md. Done 2026-10-07 (DEVELOPMENT.md is not tracked).
- RF-57 (encryption wording).

**RF-64 (P3) Add a CLAUDE.md / contributor note.** One page: the layer rules, the commands (`go build`, `go vet`, `go test`, `go tool staticcheck`), the standing constraints of SPEC-refactoring section 2, the error convention of RF-25, and the commit style. It replaces the parts of the private DEVELOPMENT.md that a contributor needs.
Done for the contributor note: [CONTRIBUTING.md](../CONTRIBUTING.md) describes branches, commits, pull requests, CI and its coverage floors, CHANGELOG entries, the rules of the code, and releases; the README and DEVELOPMENT.md link to it. Open: whether a CLAUDE.md is still worth having next to it, and the error convention once RF-25 is decided.

## 13. Phases

Each phase is a series of small commits, each building and passing. Items inside a phase can be done in any order unless noted.

| Phase | Items | When | Why then |
|---|---|---|---|
| A. Tooling | RF-1, 2, 3, 4, 10, 11, 12 | now, on its own branch; merge into `v2` | Touches no product code (RF-3 only the build); makes every later phase checkable. RF-4 first, RF-10 before RF-1's coverage step. |
| B. Safety net and quick fixes | RF-20, 21, 22, 23, 24, 40, 41, 50, 51, 52, 59 | before the 2.0.0 release | RF-41 must capture 2.0.0's format before anything else changes; RF-24, 50, 51, 52, 59 are user-facing fixes worth shipping in 2.0.0. |
| C. Structure | RF-25, 26, 27, 30, 31, 32, 33, 34 | after 2.0.0; done before it (2026-10-08) | Large diffs; would conflict with the release fixes. RF-26 before RF-33 (the dialogs change shape). |
| D. Performance and hardening | RF-35 → 36, 37, 39 (RF-38 dropped); RF-53, 54, 55, 56, 57, 58; RF-42, 43, 44 | after C, before the 2.0.0 release (13.1) | Profiling needs the cleaned pipeline signatures (RF-31, 32). |
| E. Project and docs | RF-60, 61, 62, 63, 64 | last; all but RF-62 before the 2.0.0 release (13.1) | RF-60 touches every file; doing it last avoids conflicts with all other work. |

### 13.1 Before and after the 2.0.0 release (2026-10-08)

The owner tests the whole application once the refactoring is done, and then releases 2.0.0. So everything that does not need the release comes before it, and the test covers the code as it ships. Only these wait for the release:

- RF-62: deleting PLAN-gui-redesign.md and GUI spec 17.2; a plan is deleted because its work was released.
- Closing the round (SPEC-refactoring section 8): the final baseline and "Done" in docs/README.md, after the last item.

Phase D, item by item:

| Item | Before 2.0.0 | Note |
|---|---|---|
| RF-35 profiling, RF-37 benchmarks | yes | RF-35 measures on an SSD and on the owner's network share: the share needs its path (a folder for about 4 GB of test data, deleted afterwards) or the owner runs the command; otherwise profile on the SSD only. |
| RF-36 allocations per chunk | yes, if RF-35 shows the cost | The bytes written must not change; the format fixtures check it. |
| RF-39 manifest memory | yes | Stream the manifest or correct the spec; the format stays byte for byte. |
| RF-53 recovery code as bytes | yes | In memory only; touches the Copy button of the recovery code dialog. |
| RF-54 fuzzing, RF-56 no key derivation in the health check | yes | Tests only. |
| RF-55 restore into a reparse point | yes | Refuses a junction as destination (rare): a CHANGELOG entry. |
| RF-57 what is plaintext | yes | README only. |
| RF-42 fault-injection matrix | yes | Tests only. |
| RF-43 GUI smoke test in CI | try | GitHub's Windows runners may have no interactive desktop; then it stays a step of the release checklist, as the item says. |
| RF-44, RF-58 | done | |

Phase E, item by item:

| Item | Before 2.0.0 | Note |
|---|---|---|
| RF-61 `sample.go` to `embed.go`, RF-63 documentation drift | yes | Small. |
| RF-64 contributor note | mostly done | CONTRIBUTING.md. |
| RF-60 module path | yes, at the end of Phase D | Decided 2026-10-08: `github.com/phsc84/restoresafe`. Mechanical and checked completely by the compiler and CI. |
| RF-62 sort the docs | after the release | See above. |

## 14. Open questions

These are the owner's decisions; the spec works with either answer. Answered 2026-10-07 (bold).

1. **Module path (RF-60).** Rename to `github.com/phsc84/restoresafe`, or keep `RestoreSafe`? **Rename to `github.com/phsc84/restoresafe`, at the end of Phase D.**
2. **Parallel encryption (RF-38).** Accept the extra complexity in the crypto pipeline if the profile shows a gain of, say, 30 % or more, or keep the pipeline sequential regardless? **Dropped: the pipeline stays sequential.**
3. **Code signing (RF-58).** Buy an Authenticode certificate (or use a signing service) so SmartScreen stops warning, or publish checksums only? **Checksums only.**
4. **Typed errors (RF-25).** Convert all 145 sites, or only the ones the GUI wants to act on (codes), leaving the rest as text? **All sites, GUI-relevant first.**

## 15. Not part of this refactoring

- Any change to container format 2, the key hierarchy, or the KDF defaults.
- New features (single-file restore, compression, VSS, scheduling).
- Replacing the Win32 GUI with a toolkit, or `logging` with `log/slog`: the log format is user-facing and read back by the Restore backup page, and the custom logger is small and tested.
- Supporting platforms other than Windows 64-bit.

## 16. Done when

- Every P1 and P2 item is done or explicitly dropped with a reason recorded in its section.
- CI runs on every push: format, vet, build, tests with coverage floors, race, staticcheck, govulncheck, fuzz smoke, benchmarks compile.
- The 2.0.0 fixtures restore bit-exact with the final build.
- `deadcode` reports nothing; no production file in `internal/gui` exceeds 500 lines; no string matching on workflow text remains in `gui/flow`.
- README, CHANGELOG, and the docs layout match the code.

Then close the round as SPEC-refactoring section 8 describes.
