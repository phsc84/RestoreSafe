# Specification: RestoreSafe graphical user interface

| | |
|---|---|
| Status | Draft |
| Target release | RestoreSafe 2.0.0 (together with the 2.0 format; the console frontend is removed before the release) |
| Builds on | [SPEC-restoresafe-2.0.md](SPEC-restoresafe-2.0.md); formats, keys, and workflow behavior are unchanged |
| Dependencies | None new: Win32 through `golang.org/x/sys/windows`, no cgo |

Main topics:

- A native Win32 window replaces the console menu (sections 5-8).
- Every question of the workflows becomes a screen or dialog (section 6).
- Progress, cancellation, and closing the window during an operation (section 7).
- Handling of passwords and recovery codes in controls (section 9).

## 1. Goals and non-goals

### 1.1 Goals

1. **Clear decisions.** Choosing a restore point among chains, reading the preflight, and understanding key and YubiKey prompts is easier than in the console.
2. **Same guarantees.** The GUI is a frontend over the existing workflows (`ui.UI`, phases 8-10). It never changes what a backup, restore, or verify does, and it adds no new ways for them to fail silently.
3. **Small and fast.** One portable `RestoreSafe.exe`, no runtime, no installer, starts instantly, uses a few MB of memory.
4. **Secrets stay as short-lived as in the console.** Passwords and recovery codes are zeroed after use (2.0 spec section 10).

### 1.2 Not part of the first version

- Editing `config.yaml` in the GUI. The configuration is shown read-only and can be opened in the default editor; changes take effect after a restart (decision 3, section 13).
- Dark mode (section 8.4).
- Localization; the texts stay English like the console.
- Single-file restore, browsing backup contents (2.0 spec 13.5).

### 1.3 Permanently out of scope

- **Unattended or scheduled backups, tray icons, background services.** Unchanged from the 2.0 spec 1.3: every operation needs the user to authenticate. The GUI does not stay resident.

## 2. Terminology

| Term | Meaning |
|---|---|
| UI thread | The OS thread that owns the window and runs the Win32 message loop. |
| Worker | The goroutine that runs one workflow (`backup.Run`, `restore.Run`, `verify.Run`). |
| Screen | The content of the main window in one state (home, preflight, running, result). |
| Dialog | A modal window for one question (password, recovery code, ...). |
| Bridge | The code that forwards the worker's `ui.UI` calls to the UI thread and returns the answers. |
| DIP | Device-independent pixel: 1/96 inch. Layout is defined in DIPs and scaled to the window's DPI. |

## 3. Key design decisions

| # | Decision | Rationale |
|---|---|---|
| G1 | Native Win32 controls through `golang.org/x/sys/windows`, with a small in-house wrapper. No GUI framework, no WebView. | No new dependency, pure Go, instant start, low memory. Secrets can be read from controls into buffers that are zeroed (section 9); a WebView keeps them as immutable JavaScript strings in another process. |
| G2 | The GUI implements `ui.UI`; the workflows change only by reporting their result through `ShowResult`. | Phases 8-10 built this interface; the console and the GUI stay interchangeable and the workflows keep their tests. |
| G3 | One main window with screens, plus modal dialogs for credentials. | A backup is a linear flow (preflight, credentials, progress, result); screens keep context visible, dialogs make secrets and decisions stand out. |
| G4 | The worker blocks on each question; the bridge posts it to the UI thread and waits for the answer. | Matches the synchronous `ui.UI` contract; the UI thread never blocks on the worker. |
| G5 | Progress is coalesced: the worker stores the latest `ui.Progress`, and the window is posted at most one progress message until it has taken it. The workflows report four times per second. | The window stays responsive regardless of how often progress arrives. |
| G6 | Cancelling cancels the workflow's context and waits for the worker to finish. | The workflows already clean up on cancellation (phase 10): incomplete parts are removed, the lock is released, the log is written. |
| G7 | Per-monitor DPI awareness (v2) and common controls 6 through an application manifest. | Sharp text on every monitor and modern control visuals. |
| G8 | Output (log lines) is shown in a read-only log pane, reports as formatted text. | Everything the console shows remains visible; nothing is lost when a message has no dedicated screen. |

## 4. Architecture

### 4.1 Packages

| Package | Content |
|---|---|
| `internal/win32` | Thin wrapper over the Win32 functions, structs, and constants the GUI uses (user32, gdi32, comctl32, shell32, ole32, msftedit). No logic. |
| `internal/gui` | The application: main window, screens, dialogs, layout, the bridge, and `gui.UI` (implements `ui.UI`). |
| `internal/security` | Gains `SetParentWindow(hwnd)` (section 10). |
| `internal/startup` | `CheckHealth` runs the health check without printing; `HealthCheckResult.Report()` returns its findings as a `ui.Report` (section 5.1). |
| `internal/ui` | Gains `ShowResult(ui.Result)`: the workflows report their warning count and log file path at the end instead of only printing them (section 7.4). |
| `cmd` | Starts the GUI (section 11). |

The wrapper exposes only what is used. Structs mirror the Windows SDK layout; every call that can fail returns an error built from `GetLastError`.

### 4.2 Threads

```
UI thread (locked OS thread)           Worker goroutine
────────────────────────────           ────────────────
message loop                           backup.Run(ctx, gui.UI, cfg, exeDir)
  │                                      │
  │  ◄── PostMessage(WM_APP_QUESTION) ── u.Password(prompt)   (blocks)
  │  show dialog, read answer            │
  │  ── reply channel ─────────────────► returns []byte
  │                                      │
  │  ◄── PostMessage(WM_APP_OUTPUT) ──── Output().Write(...)  (buffered, never blocks)
  │  ◄── PostMessage(WM_APP_PROGRESS) ── Progress(p)          (coalesced, never blocks)
  │                                      │
  │  Cancel button → cancel(ctx)         │  stops, cleans up
  │  ◄── PostMessage(WM_APP_DONE) ────── returns error
```

- `main` calls `runtime.LockOSThread` before creating any window; all Win32 UI calls happen on that thread.
- At most one worker runs at a time; the home screen's buttons are disabled while it runs.
- **Questions:** the bridge puts a request (kind, parameters, reply channel) into a queue and posts `WM_APP_QUESTION`. The UI thread shows the screen or dialog and sends exactly one reply. If the window is closing or the operation is cancelled, pending and later questions are answered with `ui.ErrCancelled`.
- **Output:** writes are appended to a mutex-protected buffer; `WM_APP_OUTPUT` is posted only when the buffer changes from empty to non-empty. The UI thread takes the whole buffer and appends it to the log pane.
- **Progress:** the latest `ui.Progress` is stored under a mutex; `WM_APP_PROGRESS` is posted only when no progress message is pending; the workflows report four times per second, which bounds the repaints.
- **Reports:** `ShowReport` is a question without an answer: the worker waits until the preflight screen shows it, so the report and the following `ConfirmStart` appear together.

## 5. Main window

Title `RestoreSafe <version>`, application icon, resizable, minimum size 720 × 520 DIP, default 900 × 640 DIP, centered on the monitor of the cursor at start. The last size and position are not stored (RestoreSafe keeps no state outside the backup directory).

### 5.1 Home screen

```
┌─────────────────────────────────────────────────────────────────┐
│ Configuration  D:/Configs/home-backup.yaml          [Open]      │
│ Backups        //nas/backup/restoresafe             [Open]      │
├─────────────────────────────────────────────────────────────────┤
│ Startup health check                                            │
│   ✔ Config          loaded                                      │
│   ✔ Source dirs     2 directories                               │
│   ⚠ YubiKey         not connected                               │
│   ✔ Backup inventory 3 chains, 7 backup sets                    │
├─────────────────────────────────────────────────────────────────┤
│  [ Create backup ]   [ Restore backup ]   [ Verify backup ]     │
└─────────────────────────────────────────────────────────────────┘
```

- The health check runs at start as today; `startup.CheckHealth` returns the findings without printing them, `HealthCheckResult.Report()` describes them as a `ui.Report`, which is shown in the report view (section 8.3). **Recheck** runs it again (e.g. after connecting a YubiKey or a network drive).
- The buttons are disabled when the health check blocks the operation (`BlocksBackup`, `BlocksRestoreOrVerify`); a line under the buttons names the reason.
- **Open** opens `config.yaml` in its default application and the backup directory in Explorer (`ShellExecuteW`). After editing the configuration, the user restarts RestoreSafe; a hint says so. A changed configuration is not reloaded automatically.
- A configuration that cannot be loaded (or an invalid `-config` argument) shows an error message box with the message; closing it ends RestoreSafe (console today: message and "Press Enter to exit").

### 5.2 Operation screens

An operation replaces the home screen with a sequence of screens. The window title shows the operation ("RestoreSafe - Restore backup").

| Screen | Shown for | Content | Buttons |
|---|---|---|---|
| Selection | `SelectBackups` | Section 6.2 | Next (default), Cancel |
| Destination | `RestoreDestination` | Section 6.3 | Next (default), Cancel |
| Preflight | `ShowReport` + `ConfirmStart` / `ConfirmBackupStart` | Report view; issues highlighted | Start (default), the offered alternatives, Cancel |
| Running | after the start is confirmed | Step, directory, progress bar with percentage and bytes, elapsed time, log pane | Cancel |
| Result | the workflow returned | Outcome line (section 7.4), log pane, log file path | Open log, Back to start |

A preflight with blocking issues (`Report.HasErrors`) has no Start button; the workflow returns the preflight error and the result screen shows it.

## 6. Questions

### 6.1 Mapping of `ui.UI`

| Method | GUI |
|---|---|
| `Output` | Log pane of the running and result screens. |
| `ShowReport` | Preflight screen (report view). |
| `ShowResult` | Result screen (warning count, Open log). |
| `SelectBackups` | Selection screen. |
| `RestoreDestination` | Destination screen. |
| `ConfirmStart` | Preflight buttons **Start restore** / **Start verification**, **Cancel**. |
| `ConfirmBackupStart` | Preflight buttons **Start backup**, **Full backup** (if offered), **New keys + full backup** (if offered), **Cancel**. New keys asks for confirmation with the explanation the console prints. |
| `ChooseUnlockMethod` | Unlock dialog: radio buttons "<regular method>" (default) and "Recovery code". |
| `Password` | Password dialog: prompt text, masked field, OK/Cancel. An "attempts remaining" message from the output is shown in the dialog when it is reopened. |
| `NewPassword` | New-password dialog: two masked fields, minimum length hint; OK is enabled when both are non-empty. Mismatch and length errors from the workflow are shown in the dialog on the next call. |
| `ShowRecoveryCode` + `RetypeRecoveryCode` | Recovery-code dialog (section 9.3). |
| `WaitForSpareYubiKey` | Dialog "Remove YubiKey 1 and insert your spare YubiKey", Continue/Cancel. |
| `Progress` | Running screen. |

Cancel in any question returns `ui.ErrCancelled` (selection, destination) or the method's "no" answer (`ConfirmStart` false, `BackupCancel`, `WaitForSpareYubiKey` false). Cancel in a password dialog returns an error "Cancelled."; the workflow aborts before anything is written.

Messages the workflows print between questions (e.g. "Wrong password. 2 attempt(s) remaining.", YubiKey instructions) appear in the log pane. The bridge also remembers the last output line, and a password dialog opened right after it shows that line above the field. This is a presentation aid only; the workflow logic does not depend on it.

### 6.2 Backup selection

A tree view lists backup runs, newest first: `ABC123  2026-09-01 21:12` with the backup sets of the run as children; differentials are marked "differential 002 of chain ABC123". The newest run is preselected.

- Selecting a run node restores or verifies all its sets; selecting a set node only that set (the console's "backup ID" and "set name" inputs).
- The detail line below the tree says what will be selected and, for a differential, which full backup it needs.
- Keyboard: arrows, Enter = Next, Esc = Cancel.

### 6.3 Restore destination

An edit field with **Browse...** (folder picker `IFileOpenDialog` with `FOS_PICKFOLDERS`) and a checkbox "Restore into the backup directory itself" (the console's "."). Next is enabled for a non-empty path. Existing-directory and free-space checks stay in the restore preflight.

## 7. Progress, cancellation, and results

### 7.1 Running screen

- Step and directory from `ui.Progress` ("Restoring - Documents").
- Progress bar from `Progress.Fraction()`; marquee style while it is -1 (total unknown) and between steps.
- Bytes as "1.2 GiB of 3.4 GiB" (`util.FormatBytesBinary`), elapsed time; no time estimate in the first version.
- Between credentials and the first progress (key derivation takes seconds), the screen shows "Unlocking keys ..." with a marquee bar.

### 7.2 Cancel

**Cancel** asks "Cancel the running backup? Backup sets completed so far are kept." (restore: "Directories restored so far are kept; the one being restored will be incomplete."). On Yes the context is cancelled, the button is disabled and shows "Cancelling ...", and the screen waits for the worker.

### 7.3 Closing the window

- No operation running: the window closes.
- During a question: the question is answered with `ui.ErrCancelled`, the worker finishes, the window closes.
- During a running operation: the same confirmation as Cancel; on Yes the context is cancelled and the window closes after the worker has finished (it stays open, disabled, showing "Cancelling ..."). This keeps the guarantees of phase 10: no incomplete parts, lock released, log written.
- Windows shutdown or logoff (`WM_QUERYENDSESSION`): the operation is cancelled, and `ShutdownBlockReasonCreate` ("RestoreSafe is stopping a backup") asks Windows to wait until the worker has finished. Windows may still end the process after its timeout; the incomplete parts are then removed by the next backup (existing leftover cleanup, 2.0 spec 4.7).

### 7.4 Result screen

| Workflow result | Outcome line |
|---|---|
| `nil`, no warnings | ✔ "Backup completed successfully." |
| `nil`, `ShowResult` reports warnings | ⚠ "Backup completed with N warning(s). See the log." |
| matches `context.Canceled` | "Backup cancelled." with what was kept (the log line of phase 10) |
| preflight error | ✖ "Backup not started: ..." with the issues from the report |
| other error | ✖ "Backup failed: <message>" including the Remedy text |

The warning count and the log file path come from `ShowResult` (section 4.1), which the workflows call before they return successfully; the console implementation prints the same "Warnings: N" and "Log file:" lines as today. **Open log** opens the run's log file.

## 8. Visual design

### 8.1 Controls and fonts

Standard controls only: buttons, edit, static, tree view, progress bar, rich edit (`MSFTEDIT_CLASS`) for report and log. Font: the system message font (`SystemParametersInfoW(SPI_GETNONCLIENTMETRICS)`, usually Segoe UI 9 pt) scaled to the window's DPI; the log and the recovery code use Consolas.

### 8.2 Layout and DPI

- Layout is defined in DIPs by a small row/column helper (fixed heights, stretching columns); no layout engine.
- The manifest declares `PerMonitorV2` DPI awareness. On `WM_DPICHANGED` the window takes the suggested rectangle, fonts are recreated for the new DPI, and the layout is recomputed.
- Test matrix: 100 %, 150 %, 200 %; moving the window between monitors with different scaling.

### 8.3 Report view

A `ui.Report` is rendered into a read-only rich edit: the title as heading, headings bold, status as colored markers (✔ OK green, ⓘ INFO blue, ⚠ WARN amber, ✖ ERROR red) followed by the text, details indented, fields in two aligned columns, issues at the end. The colors come from a fixed palette with sufficient contrast on the light system background; the marker symbol carries the meaning, so color is never the only signal.

### 8.4 Dark mode

Win32 common controls have no supported dark mode. The first version follows the light system theme. Dark mode is a later enhancement (custom-drawn backgrounds, `DwmSetWindowAttribute` for the title bar) and is not specified here.

### 8.5 Keyboard and accessibility

- Every screen has a default button (Enter) and a cancel button (Esc); mnemonics (`&Start backup`) on all buttons; logical tab order.
- `IsDialogMessageW` in the message loop gives standard dialog navigation in the main window too.
- Standard controls expose their names to screen readers; every control that has no visible label gets one through its window text.

## 9. Secrets in controls

### 9.1 Reading a password

1. The password field is an edit control with `ES_PASSWORD` (Windows prevents copying its text).
2. On OK, `GetWindowTextW` reads it into a `[]uint16` buffer the GUI allocates, which is converted to UTF-8 into a `[]byte` returned to the workflow (which zeroes it, as today).
3. The `[]uint16` buffer and every intermediate buffer are zeroed immediately.
4. The control's text is overwritten with the same number of filler characters and then cleared, and its undo buffer is emptied, before the dialog is destroyed, so neither its buffer nor Undo keeps the password.

**Limits** (documented, same class as the console's): the edit control's buffer belongs to Windows; step 4 overwrites it in place in practice but Windows does not guarantee that no copy remains in freed memory. Go strings are never used for secrets.

### 9.2 New password

As 9.1 for both fields. The dialog returns both entries; `gui.UI.NewPassword` checks them with the same rules and errors as `security.ReadPasswordConfirmed` (`ErrPasswordEmpty`, `ErrPasswordMismatch`), zeroes the confirmation, and returns the password. The length rule stays in the workflow.

### 9.3 Recovery code

- Shown once in its own dialog, in large bold Consolas on two lines of three groups, with the console's instructions. The code is a static control, so it cannot be selected or copied; there is no Print or Save button. A Windows task dialog is not used, because task dialogs copy their text to the clipboard on Ctrl+C.
- **I have written it down** (or closing the dialog) overwrites the displayed code and closes the dialog; the retype dialog follows (an edit control, not masked, like the console), so the code is not visible while retyping. The code reaches the GUI as a Go string, like the console's, so it is not zeroed.
- A wrong code shows the workflow's message and allows the next attempt; after the last attempt the workflow aborts as today.

## 10. YubiKey and Windows Security dialogs

The WebAuthn calls take a parent window; today `consoleWindow()` supplies the console window. `security.SetParentWindow(hwnd)` sets the window used instead; the GUI sets its main window at start. The Windows Security dialog (PIN, touch) is then modal to RestoreSafe and appears in front of it. While it is open, the running screen shows "Follow the Windows Security prompt" (the workflow's output line).

## 11. Build and start

- `cmd/main.go` starts the GUI. It keeps the `-config=<absolute path>` flag; configuration errors are shown in the window (5.1).
- The executable uses the Windows GUI subsystem (`-ldflags "-H=windowsgui"`); no console window opens.
- `versioninfo.json` gains `ManifestPath: assets/RestoreSafe.manifest`: common controls 6, `PerMonitorV2` DPI awareness, `asInvoker`, supported OS Windows 10/11.
- The size target for the GUI code is a few hundred KB in the binary; no new modules in `go.mod`.
- `yubidiag` stays a console tool.

## 12. Console frontend

The console frontend (`ui.Console`, the menu in `cmd/main.go`) is removed in phase G7, before the 2.0.0 release; there is no separate console build (decision 2, section 13). `ui.Console` stays in the code as long as tests use it to script sessions; the release binary does not reference it, so it is not linked. There is no hidden `-console` mode: a GUI-subsystem executable cannot use the console of the terminal it was started from reliably.

## 13. Decisions

Decided on 2026-09-26:

1. **Release:** the GUI ships with 2.0.0. There is no 2.0 release with the console menu.
2. **Console:** the console frontend is removed completely in G7; no separate console build.
3. **Configuration:** read-only in the GUI (shown on the home screen, **Open** in the default editor, restart to apply) is enough for 2.0.0.

## 14. Test plan

### 14.1 Automated

- Bridge: questions are answered exactly once; cancellation answers pending and later questions with `ui.ErrCancelled`; output is never lost and keeps its order; progress is coalesced (a burst of reports posts at most one message).
- Screen state machine (which screen follows which event) without windows, through an interface for the window side.
- Secret handling: the reader functions zero their buffers (as in the 2.0 spec tests).
- Result mapping (7.4) for each outcome, including the warning count and the log path from `ShowResult`.
- The workflows' own tests are unchanged; the e2e tests keep driving them through `ui.Console`.

### 14.2 Manual checklist (per release)

- Backup with new keys (password, YubiKey + spare, recovery code), differential backup, full override, new-keys override.
- Restore of a full and a differential; verify; wrong password and recovery code paths.
- Cancel during backup, staging, restore, and verify; close the window during a question and during an operation; log off during a backup.
- DPI 100/150/200 %, moving between monitors; keyboard-only operation; Narrator reads buttons and fields.
- Startup with a broken `config.yaml`, with a missing backup directory, with a disconnected YubiKey (health check blocks, Recheck unblocks).

## 15. Implementation phases

| Phase | Content |
|---|---|
| G1 | `startup.CheckHealth` and `HealthCheckResult.Report()`, `ui.ShowResult` (console: the "Log file:" line is now last for restore and verify too), `security.SetParentWindow`, application manifest in the build. Done. |
| G2 | `internal/win32` wrapper, main window, message loop, fonts, DPI handling, layout helper, home screen. Until G7 the GUI is built from `cmd/gui` as `RestoreSafe-gui.exe`, next to the console version. Done. |
| G3 | Bridge, `gui.UI`, preflight, running and result screens, log pane, progress, cancel, closing and session end. A first version of every question: password, new password, unlock method, recovery code, spare YubiKey, and confirmations (task dialogs and an input dialog); interim run selection (a task dialog with one option per backup run) and destination (text field). Done. |
| G4 | Dialog refinements: recovery-code dialog in large monospaced type (9.3), password dialogs per 9.1 and 9.2 reviewed (undo buffer emptied). Done. |
| G5 | Selection tree with single backup sets, destination with folder picker. |
| G6 | Keyboard and accessibility pass, manual checklist, README screenshots. |
| G7 | GUI becomes the only frontend: console menu removed, build switched to the GUI subsystem, CHANGELOG and README updated for 2.0.0. |
