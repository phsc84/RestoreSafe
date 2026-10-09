# Implementation plan: status-first GUI

| | |
|---|---|
| Status | Agreed 2026-09-30; phases 0-9 done; phase 10 done except the manual checklist run. The work is on `v2`. |
| Implements | [SPEC-gui.md](SPEC-gui.md) |
| Branch | `gui-redesign`, from `v2` after the pending work is committed; the work is now on `v2` |
| Scope | The new window application and the workflow additions it needs. No change to the backup format, keys, or what a backup, restore, or verify does. |

## 1. Goals

1. **Clean layers, enforced.** Logic that decides what the user sees is plain Go without Win32 calls and has its own tests. Win32 code only renders and forwards input. `internal/architecture` checks both.
2. **One implementation per fact.** The Overview's "next: DIFF", the backup plan, the retention preview, and the backup run use the same planning code. What the UI promises is what the workflow does, by construction.
3. **The branch builds and tests pass after every phase.** A phase may leave a feature missing (it is added by a later phase), never broken. Until phase 5, the first GUI keeps working unchanged for the user.
4. **Delete, don't keep.** Code of the first GUI that the new design replaces is deleted in the phase that replaces it. There is no compatibility layer and no second frontend.

## 2. Decisions

Confirmed on 2026-09-30.

| # | Decision | Rationale |
|---|---|---|
| 1 | Work on a branch `gui-redesign` from `v2`; commit the pending changes on `v2` (staging removal, docs) first. | `v2` stays releasable with the first GUI until the redesign is complete. |
| 2 | The user's choice goes **into** the workflows as a request: `restore.Run(ctx, u, cfg, exeDir, restore.Request{Sets, Destination})`, `verify.Run(..., verify.Request{Sets})`. `interact.UI` loses `SelectBackups` and `RestoreDestination`. | In the new UI the choice is made before the workflow starts. A question the UI answers without asking the user is a hidden protocol. The spec's 12.3 mapping changes accordingly (section 8). |
| 3 | `ShowReport(Report)` is replaced by `ShowBackupPlan(BackupPlan)`, `ShowRestorePlan(RestorePlan)` and `ShowVerifyPlan(VerifyPlan)`. Each plan carries the text report as `Details`. | One call per plan instead of a report plus an optional second interface; `interacttest.Script` prints `Details` as it prints the report today, so the e2e scripts keep their output. |
| 4 | The data types that cross the contract (plans, problem codes, progress phases) live in `workflow/interact`; the code that computes them lives in the workflows and in the new `workflow/plan`. | `interact` may not import other workflow packages (architecture rule), and the GUI should depend on the contract, not on the computation. |
| 5 | Backup type planning, key planning, source resolution and retention selection move from `backup` (and `job/source_validation.go`) into `workflow/plan`. | `health` needs them for the status model and may not import `backup`. Source resolution exists twice today (`backup/source_names.go`, `job/source_validation.go`); it becomes one. |
| 6 | The status model is `health.Snapshot` (spec 11.1 calls it `Status`). | `interact.Status` (OK, Warn, Error) already exists; two `Status` types in the view code invite mistakes. |
| 7 | Hero, cards and panels are **containers** that paint only their background, border, icon circle and badges. All text in them is in standard `STATIC` and `SysLink` controls. | Screen readers and UI Automation get names, roles and `AutomationId` (the control ID) from standard controls for free. A custom UI Automation provider for painted text would be the largest single piece of Win32 code in the project. This refines spec 3.4. |
| 8 | Icons are glyphs of the Segoe Fluent Icons font, drawn with `DrawTextW`. | No image assets to scale per DPI; sharp at every size; the font ships with Windows 11. |
| 9 | Test conditions for the manual checklist are made by `scripts/gui-test/New-TestCondition.ps1` from a backup the smoke test created (moving, truncating and adding files). The Go tests build the same conditions with `internal/testutil/scenario`. | Production code (a `cmd` tool) may not import test support; the conditions are file operations, which PowerShell does directly. The condition names are the same in both. `Overdue` has no script variant (a header date can't be faked); the checklist tests it with a backup from the day before. |

## 3. Target structure

Only the changed parts; everything else stays as it is.

```text
internal/
├── gui/                        composition root and Win32 screens (package gui)
│   ├── app.go                  Run, the app struct, message loop, page switching, WM_APP dispatch
│   ├── shell.go                sidebar, status bar, content host, keyboard shortcuts
│   ├── createpage.go           Create backup page: renders view.CreatePage
│   ├── restorepage.go          Restore backup page: run list, action bar, log pane
│   ├── settings.go             Settings page
│   ├── plandialog.go           backup plan dialog
│   ├── verifydialog.go         verify confirmation
│   ├── wizard.go               restore wizard (pages, progress, result)
│   ├── credentials.go          unlock, new keys, recovery code dialogs
│   ├── details.go              "Check details" and "Show details" report dialog (rich edit)
│   ├── rtf.go                  report to RTF (kept from the first GUI)
│   ├── flow/                   operation lifecycle, no Win32 (package flow)
│   │   ├── bridge.go           worker <-> UI thread (moved from gui)
│   │   ├── ui.go               interact.UI implementation; questions go to a Dialogs interface
│   │   ├── machine.go          lifecycle state machine: choose, plan, unlock, run, result, cancel, close
│   │   └── speed.go            speed and time left (BR-3)
│   ├── view/                   view models, strings, formatting, no Win32 (package view)
│   │   ├── strings.go          every user-visible string
│   │   ├── format.go           sizes, relative dates, set names
│   │   ├── createpage.go       Snapshot -> CreatePage (hero, cards)
│   │   ├── restorepage.go      Snapshot + filter + selection -> RestorePage
│   │   ├── plan.go             BackupPlan -> PlanDialog
│   │   ├── wizard.go           wizard state -> pages, Next enablement
│   │   ├── progress.go         Progress + speed -> progress card
│   │   ├── result.go           workflow result -> result card (replaces outcome.go)
│   │   └── settings.go         Config + Snapshot -> Settings cards
│   ├── widget/                 reusable Win32 controls (package widget)
│   │   ├── theme.go            palette, metrics in DIP, fonts, glyph codes
│   │   ├── scale.go            DIP -> pixels (moved from layout.go)
│   │   ├── stack.go            row/column layout helper
│   │   ├── panel.go            card and hero container (painted background)
│   │   ├── badge.go            FULL / DIFF n, status icon + text
│   │   ├── bar.go              storage bar, step trail
│   │   ├── sidebar.go          navigation list, one tab stop
│   │   └── list.go             ListView with groups, custom-drawn cells
│   └── win32/                  API wrapper, no logic (existing, extended)
│       ├── listview.go         (new) report view, groups, custom draw, checkboxes
│       ├── taskbar.go          (new) ITaskbarList3, FlashWindowEx
│       ├── paint.go            (new) BeginPaint, rounded rects, DrawText, double buffering
│       └── ...                 existing files; tree.go is deleted with the selection tree
├── workflow/
│   ├── interact/               contract (extended)
│   │   ├── interact.go         UI without SelectBackups/RestoreDestination/ShowReport; with Show*Plan
│   │   ├── plan.go             (new) BackupPlan, FolderPlan, KeyPlan, RestorePlan, VerifyPlan
│   │   ├── code.go             (new) problem codes (spec 11.8) and Severity
│   │   ├── progress.go         + Phase, Index, Count
│   │   └── report.go, result.go, password.go   unchanged
│   ├── plan/                   (new) planning without password (package plan)
│   │   ├── sources.go          source resolution and backup names (from backup/source_names.go, job/source_validation.go)
│   │   ├── folders.go          full or differential per folder (from backup/plan.go)
│   │   ├── keys.go             key plan (from backup/keys.go: planKeys, keyPlan)
│   │   └── retention.go        chains, retention candidates, retention preview (from backup/retention.go)
│   ├── backup/                 keeps enrollment, space estimate, preflight, run, applying retention
│   ├── restore/, verify/       take a Request
│   ├── health/
│   │   ├── health.go           Check and Result (items get codes)
│   │   └── snapshot.go         (new) Snapshot (spec 11.1)
│   └── job/                    progress helpers set Phase/Index/Count; source_validation.go moves to plan
├── logging/
│   └── facts.go                (new) structured result lines: write and read (spec 11.4)
├── config/                     + reminder_days
└── testutil/
    └── scenario/               (new) backup directories in the conditions of spec 3.5 and 11.8
```

Deleted with the first GUI: `gui/home.go`, `gui/screens.go`, `gui/selection.go`, `gui/layout.go`, `gui/outcome.go`, `gui/dialogs.go` (the generic input dialog), `gui/operation.go`, `gui/window.go` (split into `app.go` and `shell.go`), `gui/win32/tree.go`, and their tests.

Why this split and not one package per page: pages share the app's state (snapshot, running operation, fonts, DPI), and Go packages don't share unexported state. Pages as files of one package keep that state private without exporting it; what needs to be testable is moved out into `view` and `flow`, which don't need that state.

## 4. Dependency rules

Added to `internal/architecture` (test and package documentation):

```text
gui              gui (root)                    may import flow, view, widget, win32, workflow, ...
                 flow                          may import interact, logging; not view, widget, win32
                 view                          may import flow, workflow packages, config, logging, format; not widget, win32
                 widget                        may import win32; nothing else internal
                 win32                         no internal imports (unchanged)
workflow         plan                          may import format, config, fsx; no other workflow package
                 health                        may import plan (new); still not backup/restore/verify
```

- `view` and `flow` are pure Go: the test fails if they (directly or through another package) import `win32` or `golang.org/x/sys/windows`.
- `widget` knows nothing about backups: it may not import `workflow`, `format` or `config`.
- `plan` is below the run packages: `backup` and `health` import it; it imports none of them.
- `flow` is below `view`: `view` words the runs `flow` holds (progress card, result card), so `flow` never imports `view`.

## 5. Package design

### 5.1 `workflow/interact`

```go
type UI interface {
    ProgressReporter
    Output() io.Writer
    ShowBackupPlan(p BackupPlan)
    ShowRestorePlan(p RestorePlan)
    ShowVerifyPlan(p VerifyPlan)
    ShowResult(r Result)
    ConfirmStart(action string) (bool, error)
    ConfirmBackupStart(opts BackupStartOptions) (BackupStart, error)
    ChooseUnlockMethod(regular string) (bool, error)
    Password(prompt string) ([]byte, error)
    NewPassword(prompt, confirmPrompt string) ([]byte, error)
    ShowRecoveryCode(code string)
    RetypeRecoveryCode() (string, error)
    WaitForSpareYubiKey() (bool, error)
}
```

- `plan.go`: plain data, no methods beyond small helpers (`BackupPlan.HasErrors`). `Details Report` in each plan.
- `code.go`: `type Code string` with the constants of spec 11.8, `type Severity int` (Info, Warning, Error). `Issue` gets a `Code`.
- `progress.go`: `Phase` (Unlocking, BackingUp, Verifying, CleaningUp, Restoring, Checking), `Index`, `Count`.

### 5.2 `workflow/plan`

Exported, documented, and tested where it is:

| Function | From | Used by |
|---|---|---|
| `ResolveSources(dirs, exeDir) []Source` | `backup.resolveBackupSources`, `job.InspectSourceDirectoriesForValidation` | backup, health |
| `KeysFor(cfg, infos) Keys` | `backup.planKeys` | backup, health |
| `Folders(cfg, infos, sources, keys, forceFull, now) map[string]*Folder` | `backup.planBackupTypes` | backup, health |
| `Retention(sources, infos, keep, keepDiffs) (map[string][]catalog.SetInfo, error)` | `backup.retentionCandidates` and the guard of `backup.applyRetentionPolicy` | backup (applying), `RetentionPreview` |
| `RetentionPreview(cfg, infos, sources, folders, now) []catalog.SetInfo` | new | backup plan, health snapshot |

`RetentionPreview` adds the planned sets to a copy of the inventory as synthetic entries and runs `Retention` on it, so preview and deletion can't diverge. It describes a successful run; a hold after unreadable files or a failed verification is known only to the run. `backup.applyRetentionPolicy` keeps the deletion, logging and orphan-log cleanup.

### 5.3 `workflow/health`

- `Check` keeps its `Result` for "Check details"; every item gets an `interact.Code`.
- `TakeSnapshot(Params) Snapshot` computes spec 11.1 from one inspection shared with `Check` (the backup directory is read once): problems and notes ordered by spec 3.5, folders with the next type from `plan.Folders`, runs and sets from `catalog`, storage, keys, retention preview, and run facts from `logging`. Problems carry codes and facts, not sentences; the view words them. `Checker` limits how long a caller waits (5 seconds): a check still blocked in a system call is abandoned, reported as unreachable, and never started twice.
- `internal/testutil/scenario` builds a backup directory in each condition (`scenario.All`); the table test of `TakeSnapshot` runs over all of them, and the view tests of phase 5 reuse them.

### 5.4 `logging/facts.go`

`Logger.Fact(Fact)` writes one line `FACT  - {"kind":"backup","result":"ok","warnings":2,"seconds":252}` to the log file only (JSON, because folder names may contain spaces). `ReadFacts(path) (RunFacts, error)` reads them back: the backup result and the newest verify result per set; unknown or garbled lines are skipped. The backup writes its result at the end (also when it fails or is cancelled) and a verify fact per set when it verifies after the backup; the verify operation writes a verify fact per set. A restore writes a `restore` fact per set with skipped or stale files, for its own result page (6g, D1); no screen shows restore results after the fact.

### 5.5 `gui/flow`

- `bridge.go`: moved unchanged, plus plan messages.
- `ui.go`: `type Dialogs interface { Plan(BackupPlan) …; Unlock(…) …; NewPassword(…) …; RecoveryCode(…); … }`, implemented by the `gui` package. `flow.UI` implements `interact.UI` by posting to the UI thread and calling `Dialogs` there. Tests use a fake `Dialogs`.
- `machine.go`: states `Idle, Planning, Unlocking, Running, Cancelling, Finished`, events from the window (start, confirm, cancel, close, session end) and from the worker (question, progress, done). Its output is a view of the operation for `view`, and commands for the window (show dialog, close window). One worker at a time is a property of the machine.
- `speed.go`: rate over the last 5 seconds and time left, from `(time, Done, Total)` samples.

### 5.6 `gui/view`

Functions only, from inputs to view structs. Every text comes from `strings.go`; every number and date is formatted by `format.go`; `now` is a parameter. The view structs name what to show (texts, icon kinds, badge kinds, enabled flags, action IDs), never how (no pixels, no colors).

### 5.7 `gui/widget` and `gui/win32`

- Widgets are Win32 windows with their own registered classes, created with a fixed control ID from a single table in `gui/ids.go`. That ID is the `AutomationId` the test scripts use.
- `theme.go` holds the palette of spec 3.3 and the metrics of 3.3 in DIP. Colors are only used by widgets and the page files; `view` names kinds (`Success`, `Warning`), not colors.
- `win32` gets only thin wrappers, as today: no logic, errors from `GetLastError`.

## 6. Phases

Each phase ends with `go build ./...`, `go vet ./...`, `go test ./...` green, a build through `build.bat`, and one or more commits. Tests are written in the phase of their code (spec section 16 names them).

| # | Phase | Content | Tests | The first GUI |
|---|---|---|---|---|
| 0 | Preparation | Commit pending work on `v2`; create `gui-redesign`; spec updates of section 8; this plan confirmed | — | unchanged |
| 1 | `workflow/plan` | Move and export source resolution, type planning, key planning, retention selection; merge the two source resolvers; add `RetentionPreview` | Existing tests move along; new: preview equals deletion (end-to-end, spec 16.2) | unchanged |
| 2 | Contract | `interact`: plans, codes, progress fields; `restore`/`verify` take a `Request`; `Show*Plan` instead of `ShowReport`; `interacttest.Script` and e2e scripts updated; workflows fill the plans from the values they already compute and set progress phases | Plan-is-what-happens and progress sequences (16.2); e2e green | adapted minimally: selects before starting, renders `Details` |
| 3 | Facts and config | `logging/facts.go`; backup, verify and restore write facts; `reminder_days` in config, `config-SAMPLE.yaml`, README option table | Facts round trip and tolerant reading; config bounds | unchanged |
| 4 | Status model | Codes on health items; `health.Snapshot`; `testutil/scenario` with every condition | Table test over all scenarios (16.2); unreachable directory within the limit | unchanged |
| 5 | GUI foundations | Architecture rules of section 4; `win32` additions; `widget` with theme; `view` strings and format; the bridge in `flow` (the `interact.UI` implementation and the state machine follow in phase 6 with the new dialogs); new shell (sidebar, status bar, pages); Overview page without operations; delete home screen code | View and machine tests (16.3), layout at 4 DPIs, access keys, strings lint | **replaced**: from here the new shell runs |
| 6 | Backup | Plan dialog, credential dialogs, progress card, result card, taskbar progress and flash, close and session end | Button matrix, all cancel/close paths through the machine, speed and time left | old operation screens deleted |
| 7 | Backups page | Run list with groups, filter, problem and information lines, retention line, log pane with filter, verify dialog and status. From phase 6: "Show log" of the progress and result cards opens Backups with the run selected and the log pane (BR-7; today a viewer of the output); a failed run's log reached from its run (a failed run reports no log path); the Last backup card marks a cancelled or failed newest run; the unlock dialog offers "Use your recovery code instead" and names the key set by its date (CR-1), replacing the `ChooseUnlockMethod` task dialog | Backups view per scenario | verify leaves the operation screen |
| 8 | Restore wizard | Pages 1–4, destination checks, progress and result pages | Wizard navigation and enablement, debounced checks | selection tree, destination screen and operation screen deleted (`opscreen.go`, `screens.go`, `selection.go`, `layout.go`, `outcome.go`) |
| 9 | Settings | Read-only cards, Edit config.yaml, Reload | Settings view; Reload swap and refusal | — |
| 10 | Release | From phase 8: the restore wizard's step indicator made clickable for completed steps (RW-1; Back does it today) and its "Show log" viewer given the BK-5 filter (RW-8); `scripts/gui-test` updated (`AutomationId`, `New-TestCondition.ps1`, `Check-States.ps1`; `Smoke-BackupRestore.ps1` moved to the new names: `RestoreSafePlan`, "&Back up now…", the run card's "Done"); checklist run; accessibility, DPI and high-contrast pass, including the plan and credential dialogs following a DPI change while open; polish from phase 5: tooltips (folder paths, the reason of the next backup type, exact sizes and dates, the config.yaml key of each Settings value), a Folders card that scrolls beyond five folders, the access-key check of all pages with `Accessibility.ps1`; usability session; README usage and screenshots, CHANGELOG; merge into `v2` | Release gate (spec 16.7) | — |

Phases 1–4 change no pixel of the first GUI, so they can be reviewed as pure workflow changes. Phases 5–9 are the new UI; phase 5 is the largest, because the shell, the widgets and the pure-Go layers come together.

## 6a. State after phase 5

- The new shell runs: sidebar, status bar, Overview, "Check details". Until phases 7 and 9, the Backups page has the first GUI's Restore and Verify buttons and the Settings page shows the configuration file and the backup directory (`interim.go`, deleted by those phases). "Back up now…" still opens the first GUI's operation screen until phase 6.
- The polish left open here (tooltips, scrolling Folders card, access-key check) is part of phase 10 in section 6.

## 6b. Phase 6 steps and the state after it

- **6a (done):** `flow.UI` (implements `interact.UI` behind `flow.Dialogs`), `flow.Machine` (stages, Cancel and close decisions), `flow.Speed`; the first GUI's screens implement `Dialogs` (`questions.go`).
- **6b (done):** view models without windows: `view/plan.go` (plan dialog, new-keys confirmation), `view/run.go` (progress card, Folders card states, status bar, cancel and close confirmations), `view/result.go` (result card, BR-7), `view/credentials.go` (spec 9). The backup writes the size of each set and a `cleanup` fact (spec 11.4) for the result card; the worker reads the run's facts before it reports the end.
- **6c (done):** the windows: plan dialog (`plandialog.go`, modal to the main window but answered from the main message loop), the run card in place of the hero with progress and result (`runcard.go`), folder states on the Folders card, credential dialogs in the theme (`dialogs.go`), spec-worded cancel and close task dialogs, taskbar progress (`ITaskbarList3`) and flashing. Shared dialog frame and vertical layout: `dialogwin.go`, `stack.go`. Backup no longer uses the operation screen; restore and verify keep it (`opscreen.go`, removed with phases 7 and 8).
- **Left for later phases:** what phase 6 left open is part of the rows of phases 7, 8 and 10 in section 6.

## 6c. State after phase 7

- **7a:** the snapshot reads every run log (`health.Snapshot.Logs`), so failed and cancelled runs without sets are listed; the workflows report their log as soon as it is open (`interact.UI.LogStarted`), so "Show log" works after a failure too.
- **7b:** `view/backups.go` (runs, rows, statuses, filter, retention and problem lines, selection, verify confirmation, log lines); the Last backup card marks cancelled and failed runs; the unlock dialog offers the recovery code (CR-1), and the password typed in it reaches the next password question through `flow.UI`.
- **7c:** list view with groups and custom draw, drop-down list, context menu, clipboard, splitter.
- **7d:** the Backups page (`backups.go`) replaces the interim page. Verify starts from it and runs on the Overview's run card; "Show log" of the cards opens the page with the run selected. A click on a run's group header selects the run (comctl32 sends no click for headers, so the page checks which group has the focus). Restore starts from the page with the chosen sets and still goes through the first GUI's destination and operation screens until phase 8; the selection tree is deleted.

## 6d. State after phase 8

- **8a:** `restore.PlanDestination` (the plan of `restore.Run` without questions, for page 3); `view/wizard.go` (steps, restore points, folders with the full backup read with a differential, destination checks, check page); restore wording in the progress and result cards ("Restore incomplete" in red, the incomplete folder, the folders not restored, Open folder).
- **8b:** the restore wizard (`wizard.go`), one resizable dialog. Pages 1 to 4 are modal to the main window; page 4 starts the workflow, which shows its plan and waits for **Restore…** (Back answers no; nothing is written before). While the restore runs, the main window is usable again and shows the run card; credential dialogs and confirmations are modal to the wizard, and so is the Windows Security prompt. A cancelled password goes back to page 4. The first GUI's operation screen, destination screen and their layout and outcome code are deleted, and the tree-view wrappers with them.


## 6e. State after phase 9

- The Settings page (`settings.go`, `view/settings.go`) replaces the interim page: configuration file with Edit config.yaml and Reload, folders with their state from the check, exclude patterns and the unreadable-file rule, backup directory with reachability and free space, full and differential, retention, checks, keys and unlocking (Argon2id behind a link), logging. Rows wrap within their cards; the page scrolls (`widget.Panel.SetScroll`).
- Reload (`reload.go`) runs `config.Load` on a worker: a broken file keeps the configuration in use and shows the error with its line on the card; a good file is used at once, or after the running operation, and the backups are checked again with it.

## 6f. State after phase 10 (without the manual parts)

- **10a:** tooltips (folder paths, the reason of the next type, exact sizes and dates, the config.yaml key of each Settings value); the Folders card scrolls beyond five folders; the wizard's step trail goes back to completed steps; its log viewer has the BK-5 filter; the plan and credential dialogs follow a DPI change while open; Windows high contrast uses the system colors.
- **10b:** `scripts/gui-test` for the new UI: controls by control ID (a Go test keeps `$Ids` equal to the code), `New-TestCondition.ps1` (13 conditions), `Check-States.ps1`, `Accessibility.ps1`, and the smoke test through the plan, the wizard and the Backups page.
- **10c:** pre-run of `docs/GUI-TEST-CHECKLIST.md` at 150 % (scripts and scripted walks), with a list of known differences from the spec to accept or change.
- **10d:** README (usage, screenshots from a neutral demo setup), CHANGELOG, config sample.
- **10e:** the known differences decided and carried out (6g).
- **10f:** the usability session, held successfully. The screenshots (README, `docs/images`) and the wireframes in `docs/SPEC-gui.md` recreated from the final UI (2026-10-09).
- Open: the manual checklist run (100 %, 125 %, 200 %, several monitors, YubiKeys, Narrator, high contrast).

## 6g. Known differences (decided 2026-10-06)

Found in the pre-run (10c); listed in `docs/GUI-TEST-CHECKLIST.md` under "Known differences from the spec". Each is **Accepted** for 2.0.0 (write the reason into the checklist, and change the spec where the code is right) or **Changed** in the code. "To change" names where the work is; it is a starting point, not a design.

| # | Spec | Difference | To change | Decision |
|---|---|---|---|---|
| D1 | OV-1, RW-8 | "Show files" for skipped files is not there: the run's log names the files. Restore results show the files "not in this backup", not the "older version" (stale) files. | The backup writes no fact for skipped and stale files (11.4); add one, read it in `logging/facts.go`, and offer "Show files" on the result card and in the wizard's result. | **Change** (wording) and **Accept** (no list): skipped and stale counted apart in the `set` fact; a restore writes a `restore` fact per set (it wrote none, so the restore result showed these lines only in the tests); the result says "restored in an older version from <date>" and the restore log names the stale files too. No "Show files": the log names every file. |
| D2 | OV-2 | When the check blocks a backup (e.g. a missing folder), the hero offers its fix actions (Check again, Edit config) instead of a disabled **Back up now…**; `Ctrl+B` does nothing then. | Either show the disabled button with the reason next to the fix actions, or change OV-2 to what the code does. | **Accept**: OV-2 changed to what the code does (OV-1 allows one primary and one secondary action). |
| D3 | BR-1 | During a backup, the Folders card shows "Done, <size>" with the size of the folder read, not of the set written; for a differential it is much larger than the size on the Restore backup page. | Report the written set size (`setwriter.Result`) when a folder finishes, and show that; or label the read size as such. | **Change**: the last progress report of a folder carries `Progress.Written`, the size of the set; the run machine keeps it for the Folders card. The requirement is BR-4. |
| D4 | CR-1 | The unlock dialog does not name the key set by its date; when a selection spans older keys, the workflow's notice above the field says which keys. | The password question carries no key set; pass its creation date with the question (fits refactoring 2.0 RF-26, typed questions). | **Accept** until RF-26: the notice above the field names the backup and the keys' date. CR-1 changed; RF-26 notes it. |
| D5 | RW-1 | The wizard is resizable; its progress page leaves empty space below the card. | Lay out the progress page to fill the height (as the Overview's run card does), or fix the wizard's size while it runs. | **Change**: the run card fills the wizard's page and centres its content when it is taller than the content. |
| D6 | 15 | Turning high contrast on or off rebuilds the pages; a dialog open at that moment keeps its colors until it closes. | Forward the theme change to the open plan, credential and details dialogs and restyle them like the pages. | **Accept**: rare, and it corrects itself when the dialog closes. Section 15 changed. |
| D7 | 16.4 | `Overdue` has no script variant: a backup's date is in its header, which is authenticated (its hash is part of every chunk's AAD). | Nothing reasonable: a script that edits the date breaks the backup's authentication. The checklist already tests it with a backup from the day before (decision 9). | **Accept**. |

## 7. Risks

| Risk | Mitigation |
|---|---|
| ListView groups with custom-drawn cells behave differently at 150–200 % or with keyboard navigation | Build `widget/list.go` in phase 5 with a scratch page and check it at all DPIs before the Backups page depends on it. |
| An unreachable NAS blocks `os.Stat` far longer than 5 seconds | The snapshot runs on its own goroutine with a deadline (5.3); the UI never waits for it; only one check runs at a time. |
| The plan changes between showing it and starting (a file changes, another program writes) | Unchanged from today: the workflow computes the plan once and acts on it; the GUI shows the workflow's plan, not its own. |
| `RetentionPreview` diverges from `applyRetentionPolicy` after a later change | Both go through `RetentionCandidates`; the end-to-end test compares them for every retention setting. |
| Phase 5 is large | It is split into commits in the order of its table cell; the shell with an empty Overview is the first runnable state. |
| Segoe UI Variable or Segoe Fluent Icons is missing (a stripped Windows image) | Fall back to the message font and to text markers (✔ ⚠ ✖ ⓘ), as the first GUI does. |

## 8. Spec updates (done in phase 0)

- 3.4 and 15: hero and cards are containers with standard text controls (decision 7); icons from Segoe Fluent Icons (decision 8).
- 11.1: `Snapshot` instead of `Status` (decision 6); its computation lives in `health`, with planning in `workflow/plan` (already in 12.1).
- 11.2: `Show*Plan` replaces `ShowReport` and the optional `PlanReceiver` interface (decision 3).
- 12.1: the package table of section 3 of this plan; `gui/flow`, `gui/view`, `gui/widget`.
- 12.3: `SelectBackups` and `RestoreDestination` are removed; restore and verify take a request (decision 2).
- 16.4: `New-TestCondition.ps1` works on a smoke-test backup; `Overdue` is tested with a backup from the day before (decision 9).
- 17.1 criterion 6: the e2e tests change for the request parameters and the plan calls, not only for progress fields.

## 9. When this plan is done

Mark it done in the status table, like the refactoring plan was, and delete it once `v2` is merged and released; the spec, the architecture test and the code carry everything that stays true.
