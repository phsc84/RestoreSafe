# Specification: RestoreSafe graphical user interface

| | |
|---|---|
| Status | Specified 2026-09-30 (status-first redesign); implemented on branch `gui-redesign`. Open before the release: the manual checklist run, the usability session, and the merge into `v2` ([PLAN-gui-redesign.md](PLAN-gui-redesign.md)). |
| Target release | RestoreSafe 2.0.0. Editing the configuration and dark mode follow in 2.1.0 (section 18). |
| Builds on | [SPEC-2.0.md](SPEC-2.0.md); formats, keys and workflow behavior are unchanged |
| Manual tests | [GUI-TEST-CHECKLIST.md](GUI-TEST-CHECKLIST.md) |
| Dependencies | None new: Win32 through `golang.org/x/sys/windows`, no cgo |

## 0. How to use this document (read first)

This document specifies RestoreSafe's window application: a status-first interface that replaces the first GUI's home screen (configuration paths, health check report, three buttons) and its linear operation screens. The architecture of the first GUI (bridge, threads, secret handling, section 12 to 14) stays. It is written for developers and coding agents.

- **Requirement IDs** (`OV-1`, `BP-3`, `BK-5`, `RW-2`, `ST-4`) identify testable requirements. Reference them in commits, tests and questions; section 16 maps them to tests.
- **Text wins over wireframes.** Wireframes show layout, content and states, not pixels. Colors, sizes and spacing are defined in section 3.3. If a wireframe and the text disagree, follow the text and flag the difference.
- **Behavior does not change.** The UI is a frontend over the existing workflows (`backup.Run`, `restore.Run`, `verify.Run`, `health.Check`, `catalog.Inventory`). What a backup, restore or verify does, what it checks and when it fails stays as specified in [SPEC-2.0.md](SPEC-2.0.md). Where the UI needs data the workflows don't expose yet, section 11 names the addition. Add it there; don't guess and don't parse text meant for people.
- **Keep what already works.** The bridge (12.2), the secret handling in controls (13), the recovery-code dialog (13.3), the YubiKey parent window (13.4), cancellation and session end (12.4) are reused unchanged.
- **Ask when something is unclear.** Section 18 lists the decisions taken. Do not silently pick a different answer.
- **Pure Win32.** No WebView2, no web technology, no third-party UI toolkit, no cgo, no new modules. Standard controls first, custom drawing only where section 3.4 says so.

### Wireframe legend

| Symbol | Meaning |
|---|---|
| `[ Text ]` | Push button |
| `[x]` `[ ]` | Checkbox: checked, unchecked |
| `(o)` `( )` | Radio button: selected, not selected |
| `(ok)` `(!)` `(x)` `(i)` | Status icons: success, warning, error, information |
| `#` `=` `-` in a bar | Progress or storage bar segments (backups, other data, free) |
| `>` at the start of a row | Selected row, or collapsed group. `v` means expanded. |
| `FULL` `DIFF 3` | Type badges (full backup, differential number 3 of its chain) |
| `[•••••••••••  ]` | Masked password field |

## 1. Purpose and scope

**Goal:** a user sees at a glance whether their folders are protected, starts a backup with one click, understands what RestoreSafe will do before it does it, and gets folders back without reading a report.

**In scope:** Create backup page, backup plan and run, Restore backup page (runs, sets, logs, verify), Restore window, credential dialogs, Settings (configuration view), window behavior when an operation finishes or the window is closed, and the additions to the workflows the UI needs.

**Out of scope, permanently** (2.0 spec 1.3): scheduled or unattended backups, a tray icon, background processes, start with Windows, stored credentials. Every backup, restore and verify needs the user to authenticate. Toast notifications are not used, because nothing runs while the user is away.

**Out of scope for this redesign:** restoring single files or subfolders (2.0 spec 13.5), deleting backups from the UI (decision 5), cloud destinations, more than one backup directory per configuration, a first-run wizard, localization.

**What RestoreSafe does today, and the UI must represent:**

| Capability | Where it shows |
|---|---|
| Full and differential backups, chosen automatically **per source folder** with a stated reason; override to full backups or to new keys + full backups | Backup plan (6) |
| Chains per folder (a full backup and its differentials, same chain ID); backup runs that write one set per folder | Restore backup (7) |
| Restore of one run or single sets into a new folder per set; checksum check of every file; stops and reports "incomplete" on a mismatch | Restore window (8) |
| Verify of a run or set (full decrypt and checksum check, nothing written); optional verify after backup | Restore backup (7), Settings (10) |
| Retention after each successful backup: `retention_keep` chains per folder, `retention_keep_differentials` per chain; held back after a failed verification or skipped files | Backup plan (6), Restore backup (7) |
| Password, password + YubiKey or YubiKey-only; spare YubiKey; recovery code; new keys | Credential dialogs (9), Create backup (5) |
| Startup health check (config, folders, backup directory, YubiKey, keys, inventory, 1.x files, leftovers) | Create backup hero and Check details (5) |
| Exclude patterns (global), `on_unreadable_file`, split size, Argon2 parameters, log level | Settings (10) |
| One log file per run in the backup directory; restore and verify append to the log of the run they read | Log window of a run (BK-5) |
| Several configurations via `-config` | Title bar, Settings (10) |

## 2. Design principles

1. **Status first.** The first thing a user sees is whether their folders are protected, not configuration values or a report.
2. **One primary action.** "Back up now…" is the only accent-filled button in the main window. Restore and Verify act on a selected backup.
3. **Plan before action.** Backup, restore and verify show what will happen (type and reason per folder, space, prompts, what retention removes afterwards) before anything is written or anyone is asked for a password. This is RestoreSafe's preflight in a readable form.
4. **Secrets are asked for last.** Password, YubiKey and recovery code come after the user confirmed the plan, in their own dialog, never as a field on a page.
5. **No console.** Logs belong to a backup run and live with it. Technical text (file names with IDs, OS errors, `Remedy:` lines) is never the primary message; it's one click away.
6. **Plain language.** Every problem says what happened and what the user can do next.
7. **Never hide the chain.** A differential is always shown with the full backup it needs.
8. **Native controls first.** Custom drawing is limited to backgrounds and borders of the hero and cards, icons, badges, bars, the sidebar and the step trail. Text is always in standard controls.
9. **Tables for lists, lines for statements.** A list of like items with several attributes (folders, sets, destinations) is a table whose columns the user can widen. Statements, single facts and setting-value pairs are lines; a line never hides text: what it cuts off is in its tooltip.

## 3. Foundations

### 3.1 Platform and manifest

| Item | Requirement |
|---|---|
| OS | Windows 11. YubiKey needs 22H2 or later (unchanged). |
| Common Controls | Version 6 via the existing manifest `build/RestoreSafe.manifest`. Required for `TaskDialog`, ListView groups and themed controls. |
| DPI | `PerMonitorV2` (existing). All metrics, fonts, icons and custom drawing scale with the window DPI and are recomputed on `WM_DPICHANGED`. |
| Text | UTF-16 (`W` APIs) throughout. |
| Theme | Light only in 2.0.0. Dark mode follows in 2.1.0 (decision 4): follow the system setting live, title bar via `DWMWA_USE_IMMERSIVE_DARK_MODE`, controls via `SetWindowTheme`. High contrast mode uses system colors only (required in 2.0.0). |
| Fonts | Segoe UI Variable Text (fallback: the system message font, as today) for UI text. Icons are glyphs of Segoe Fluent Icons drawn with `DrawTextW`, so there are no image assets to scale (fallback: the text markers ✔ ⚠ ✖ ⓘ). Consolas for the log, IDs and the recovery code. |
| State | RestoreSafe is portable and keeps no state outside the backup directory. The UI stores nothing: no registry, no settings file, no remembered window position or column widths. |

### 3.2 Window and navigation

- Default size 1000 × 700 DIP, minimum 820 × 600 DIP. Centered on the monitor of the cursor at start.
- Title: `RestoreSafe <version>`. When the configuration isn't the default `config.yaml` next to the exe, the title adds the file name: `RestoreSafe 2.0.0 · home-backup.yaml`.
- Layout: title bar, sidebar (160 DIP), content area, status bar (24 DIP).
- Sidebar items: **Create backup** (section 5), **Restore backup** (section 7), **Settings**. The sidebar names the pages by their action. The selected item has a filled row and a 3 DIP accent bar on its left edge. Keyboard: `Ctrl+1`, `Ctrl+2`, `Ctrl+3`. The sidebar is one tab stop with arrow-key navigation.
- Other shortcuts: `Ctrl+B` opens the backup plan, `F5` runs the health check again, `Esc` closes the active dialog. Access keys (`&Back up now…`) on all buttons, as today.
- Status bar: current activity on the left ("Ready", "Checking…", "Backing up Documents · 62%"). On the right: the backup directory's free space on Create backup and Settings; the number of runs and total size on Restore backup.
- While an operation runs, all three pages stay available. Actions that would start a second operation are disabled (one worker at a time, 12.2).

### 3.3 Visual language

| Role | Light | Dark (2.1.0) | Use |
|---|---|---|---|
| Surface | `#FFFFFF` | `#1D2126` | Window content, cards |
| Surface, secondary | `#F0F2F5` | `#252A30` | Sidebar, title bar, group headers, action bars |
| Accent | `#0F6CBD` | `#2F7FD0` | Primary button, selection bar (accent text: `#0B5CAD` / `#6CB0F5`) |
| Selection tint | `#E3EFFB` | `#1C3550` | Selected rows |
| Success | `#177A3C` on `#E3F4EA` | `#5FD08A` on `#1B3626` | Protected, complete, verified |
| Warning | `#8A5A00` on `#FDF1D3` | `#F0C15A` on `#3B2F10` | Overdue, skipped files, low space |
| Error | `#B3261E` on `#FBE6E4` | `#FF8F88` on `#411F1D` | Unreachable, incomplete, base missing, failed |
| Full badge | `#5B3FA6` on `#ECE7F8` | `#C1ACF5` on `#33285A` | `FULL` |
| Differential badge | `#0B5CAD` on `#E3EFFB` | `#8CC4FA` on `#1C3550` | `DIFF n` |
| Text, secondary | `#5A6572` | `#9AA5B1` | Captions, hints |
| Lines | `#D8DCE2` | `#2C333B` | Card borders, dividers |

The implementation may use the user's Windows accent color instead of the accent value. Semantic colors are fixed. Color is never the only carrier of meaning: every status has an icon and a text label (as the report view does today).

| Element | Size at 96 dpi |
|---|---|
| Body text | 13 px regular. Secondary and table text 12 px. Captions and status bar 11 px. |
| Hero title, page title | 20 px and 18 px, semibold |
| Spacing grid | 4 px. Content padding 16 px top and bottom, 18 px left and right. Gap between cards 10 to 12 px. |
| Corner radius | Cards 8 px, controls and badges 4 px |
| Control height | Buttons 28 px (small 26 px), edit fields 26 px, list rows 26 to 28 px, group headers 28 px, sidebar rows 34 px |
| Hero icon | 52 px circle, 28 px glyph |
| Focus outline | 2 px in the text color, following the rounded shape of buttons, toggles and the selected sidebar row; shown only after keyboard use (Tab, arrow keys), never after a click |

### 3.4 Control mapping

| UI element | Win32 implementation |
|---|---|
| Sidebar | Custom child window, one tab stop, exposes UI Automation names and selection state |
| Status hero, cards, progress card | Container windows that paint only their background, border and icon circle (double buffered, DPI aware). All text in them is in standard `STATIC` and `SysLink` controls, so screen readers and UI Automation get names, roles and `AutomationId` without a custom provider. Redrawn only on state change. |
| Step trail, badges, bars | Small painted controls; each sets its accessible name to its text ("Step 2 of 4, Back up 2 of 3", "Differential 3"). |
| Backup list | `SysListView32`, report mode, groups enabled (one group per run). Badges and status are custom-drawn cells (`NM_CUSTOMDRAW`). Column headers as for the folder lists. |
| Folder lists (Create backup, backup plan, Settings, Restore window) | `SysListView32`, report mode, full-row select, column headers the user can drag wider; checkboxes (`LVS_EX_CHECKBOXES`) in the Restore window. A filling column takes the width the others leave until the table's width changes; widths the user set stay meanwhile. A row's tooltip shows what its cells may cut off (path, exact time, reason). Tones color the cells, type badges are custom-drawn (`NM_CUSTOMDRAW`). More than five rows scroll within the table. Every list's column header is drawn on the secondary surface with a line under it and between the columns, so it stands apart from the rows (`widget.StyleListHeader`); high contrast keeps the system header. |
| One-line text | `STATIC` with an ellipsis (`SS_ENDELLIPSIS`, `SS_PATHELLIPSIS` for paths). When the text is cut off, its tooltip shows it in full; when it fits, there is no such tooltip. |
| Progress bars | `msctls_progress32` (`PBS_SMOOTH`, marquee while the total is unknown and while keys are unlocked), plus taskbar progress via `ITaskbarList3` |
| Confirmations | `TaskDialog` with headline, explanation, custom button labels and expandable details. **Never for the recovery code** (task dialogs copy their text on `Ctrl+C`, which bypasses the clipboard protection of 13.3). |
| Password fields | Edit with `ES_PASSWORD`, read and wiped as in 13.1 |
| Log window | Read-only rich edit (`MSFTEDIT_CLASS`, as today) with a severity filter |
| Health check details | The existing report view (rich edit) in a dialog |
| Backup plan, Restore window | Modal dialogs that close on Start, no property sheets |
| Folder pickers | `IFileOpenDialog` with `FOS_PICKFOLDERS` (existing) |

### 3.5 Application state

The hero renders one state. Priority: Running, then Error, then Warning, then Protected. Empty applies when no backup exists yet. The state is computed from the health check, the inventory (headers and trailers, no password) and the result of the last operation in this session (section 11.5).

| State | Triggers |
|---|---|
| **Running** | A backup, restore or verify is active (including its plan and credential dialogs). |
| **Error** (red) | The backup directory can't be reached, read or written. A source folder is missing or can't be read. A differential's full backup is missing or incomplete. A backup set can't be used (missing parts, renamed files, unreadable, damaged). The last backup in this session failed. The last verification in this session found damage. |
| **Warning** (amber) | The newest backup is older than the reminder limit (decision 2). The newest backup of a folder has skipped files, so retention is holding older backups back. An incomplete set is newer than the newest complete set of its folder. Free space is below the size of a new full backup of all folders. An Argon2 value was capped. |
| **Protected** (green) | None of the above, and every configured folder has a complete backup. |
| **Empty** (neutral) | No complete backup set exists in the backup directory. |

**Not a warning:** a YubiKey that isn't connected (people keep it on their key ring; it's shown on the Keys card and asked for when needed), 1.x backups and leftover `.tmp` files (shown as information lines on Restore backup), and "the next backup creates new keys" (shown on the Keys card and in the backup plan). Amber that can't be cleared by the user trains them to ignore amber.

If several Error or Warning triggers apply, the hero shows the most urgent one (order as listed) with its fix action. The sub line ends with "and N more problems"; "Check details" lists all of them.

The hero on Create backup leaves out the problems of existing backups: a missing or incomplete full backup, an incomplete set, and an incomplete set newer than the newest complete one. They don't keep a backup from running, and the Restore backup page lists them (BK-6). Without other problems, the hero is neutral: "Ready to back up" with the facts of Protected and **Back up now…**. It isn't Protected, since a backup can't be restored.

### 3.6 Writing

- Sentence case everywhere. Buttons start with a verb. A button that opens another dialog ends with "…".
- Say what happened, then what to do, in one or two sentences. The workflow's full message, file names and `Remedy:` text go into "Show details" or the log.
- No exclamation marks, no "successfully", no "please". Relative dates ("today, 09:12", "Sun 27 Sep, 20:05") with the exact timestamp in a tooltip.
- Folders are named by their backup name (`Documents`, or the alias `Documents__from__C_RootA` when two folders share a name) with the full path in the tooltip and on Settings.
- A backup set is referred to as "Documents, differential 3 of 27 Sep" in text; the set name (`Documents_ABC123_2026-09-27_DIFF003`) appears only in details, logs and tooltips.
- Sizes use binary units labeled KB, MB, GB like Explorer, with one decimal below 10 (`view.Size`; the logs use `fsx.FormatBytesBinary`, which has the same labels).
- Dates, times and numbers use fixed English formats, matching the English-only text: "today, 09:12", "yesterday, 18:40", "Sun 27 Sep, 20:05", "Tue 1 Sep 2025, 17:45", a 24-hour clock, and "1,240" (`view/format.go`).
- All user-visible strings live in one place in `internal/gui`. Sentences with variable parts use format strings, never concatenation. English only.

## 4. Operation lifecycle

This section connects the screens. Every operation follows the same five steps; the workflow drives them through `interact.UI` exactly as today.

| Step | Backup | Restore | Verify |
|---|---|---|---|
| 1. Choose | "Back up now…" | Selected run on Restore backup (7) | Selected run or set on Restore backup |
| 2. Plan | Backup plan dialog (6.1) | Restore window (8) | Verify window (figure 7.3) |
| 3. Unlock | Unlock or new-keys dialogs (9) | Unlock dialog (9) | Unlock dialog (9) |
| 4. Run | Progress card on Create backup (6.2) | Progress card on Restore backup | Progress card on Restore backup |
| 5. Result | Result card on Create backup (6.3) | Result card on Restore backup | Result card and status on Restore backup |

The plan comes from the workflow (`ShowBackupPlan`, `ShowRestorePlan`, `ShowVerifyPlan`, section 11.2), and the choice of step 1 goes into the workflow as a request (restore and verify, 12.3), so the UI never computes a plan of its own that could disagree with what the workflow then does.

## 5. Create backup page

Purpose: answer "are my folders safe?" and start a backup.

**Figure 5.1: Create backup, state Protected.** The window frame shown here applies to all main-window wireframes.

```text
┌──────────────────────────────────────────────────────────────────────────────────────────────────────┐
│ RestoreSafe 2.0.0                                                                      _    [ ]    x │
├──────────────┬───────────────────────────────────────────────────────────────────────────────────────┤
│              │ (ok)  Your folders are protected                                  [ > Back up now... ] │
│ > Overview   │       3 folders · Last backup today, 09:12 · Check details                            │
│   Backups    │                                                                                       │
│   Settings   │ ┌─ Folders (3) ───────────────────────── Details ┐ ┌─ \\NAS\Backup\RestoreSafe ──────┐ │
│              │ │ Documents   today, 09:12   DIFF 3   next: DIFF │ │ 530 of 900 GB used              │ │
│              │ │ Projects    today, 09:12   DIFF 3   next: DIFF │ │ [#######=============---------] │ │
│              │ │ Pictures    today, 09:12   DIFF 6   next: FULL │ │ # Backups 186 GB  = Other 344 GB│ │
│              │ └────────────────────────────────────────────────┘ │ - Free 370 GB                   │ │
│              │                                                    │ A full backup of all folders    │ │
│              │                                                    │ needs about 96 GB               │ │
│              │                                                    └─────────────────────────────────┘ │
│              │ ┌─ Last backup ────────────────────── Show backups ┐ ┌─ Keys ─────────────────────────┐ │
│              │ │ (ok) Today, 09:12 · 3 folders · 3.4 GB · 4 min   │ │ Password + YubiKey             │ │
│              │ │      Documents  DIFF 3  based on FULL of 1 Sep   │ │ 2 YubiKeys · recovery code     │ │
│              │ │      Projects   DIFF 3  based on FULL of 1 Sep   │ │ Created 1 Sep 2026             │ │
│              │ │      Pictures   DIFF 6  based on FULL of 3 Aug   │ │ (i) YubiKey not connected      │ │
│              │ └──────────────────────────────────────────────────┘ └────────────────────────────────┘ │
├──────────────┴───────────────────────────────────────────────────────────────────────────────────────┤
│ Ready                                                                  370 GB free in backup directory │
└──────────────────────────────────────────────────────────────────────────────────────────────────────┘
```

**Figure 5.2: Create backup while a backup runs.** The progress card replaces the hero; the Folders card shows each folder's progress.

```text
┌──────────────────────────────────────────────────────────────────────────────────────────────────────┐
│ RestoreSafe 2.0.0                                                                      _    [ ]    x │
├──────────────┬───────────────────────────────────────────────────────────────────────────────────────┤
│              │ ┌───────────────────────────────────────────────────────────────────────────────────┐ │
│ > Overview   │ │ Backing up                                                             [ Cancel ] │ │
│   Backups    │ │                                                                                   │ │
│   Settings   │ │ v Unlock keys  >  [Back up 2 of 3]  >  Verify  >  Clean up                        │ │
│              │ │ Projects · differential                                                           │ │
│              │ │ [################################################-----------------------------]   │ │
│              │ │ 0.7 of about 1.1 GB · 86 MB/s                                                     │ │
│              │ │                                                                        Show log   │ │
│              │ └───────────────────────────────────────────────────────────────────────────────────┘ │
│              │                                                                                       │
│              │ ┌─ Folders (3) ──────────────────────────────────┐ ┌─ \\NAS\Backup\RestoreSafe ──────┐ │
│              │ │ (ok) Documents   DIFF 4   Done, 0.2 GB         │ │ ...                             │ │
│              │ │ ( )  Projects    DIFF 4   Backing up, 62%      │ │                                 │ │
│              │ │ ( )  Pictures    FULL     Waiting              │ │                                 │ │
│              │ └────────────────────────────────────────────────┘ └─────────────────────────────────┘ │
├──────────────┴───────────────────────────────────────────────────────────────────────────────────────┤
│ Backing up Projects · 62%                                              370 GB free in backup directory │
└──────────────────────────────────────────────────────────────────────────────────────────────────────┘
```

**Figure 5.3: Hero variants.**

```text
┌─ Warning: overdue ────────────────────────────────────────────────────────────────┐
│ (!)  Your last backup is 9 days old                           [ > Back up now... ] │
│      Your reminder limit is 7 days · Last backup Mon, 21 Sep, 18:20               │
└───────────────────────────────────────────────────────────────────────────────────┘
┌─ Warning: skipped files ──────────────────────────────────────────────────────────┐
│ (!)  2 files in Documents weren't backed up                   [ > Back up now... ] │
│      They couldn't be read. Older backups of Documents are kept until a backup    │
│      without skipped files succeeds. · Check details                              │
└───────────────────────────────────────────────────────────────────────────────────┘
┌─ Error: backup directory ─────────────────────────────────────────────────────────┐
│ (x)  The backup directory isn't reachable                       [ Check again ]   │
│      \\NAS\Backup\RestoreSafe didn't respond. Check the network connection.       │
│      · Show details                                                               │
└───────────────────────────────────────────────────────────────────────────────────┘
┌─ Error: full backup missing ──────────────────────────────────────────────────────┐
│ (x)  4 backups of Documents can't be restored                [ Show backups ]     │
│      Their full backup of 1 Sep (chain ABC123) is missing or incomplete.          │
│      Restore its FULL files from your copy, or delete the DIFF files of ABC123.   │
└───────────────────────────────────────────────────────────────────────────────────┘
┌─ Error: source folder ────────────────────────────────────────────────────────────┐
│ (x)  E:\Photos can't be found                                    [ Check again ]  │
│      Connect the drive, or remove the folder from config.yaml.  [ Edit config ]   │
└───────────────────────────────────────────────────────────────────────────────────┘
┌─ Empty ───────────────────────────────────────────────────────────────────────────┐
│ ( )  Create your first backup                                 [ > Back up now... ] │
│      3 folders to \\NAS\Backup\RestoreSafe. RestoreSafe creates your keys first.  │
└───────────────────────────────────────────────────────────────────────────────────┘
```

### Requirements

| ID | Requirement |
|---|---|
| OV-1 | The hero shows the state from 3.5. The title is a short statement; the sub line carries facts and ends with the link "Check details", which opens the full health check report (the existing report view) in a dialog with **Check again** and **Close**. Each Warning and Error variant offers exactly one primary fix action; Error may add one secondary action. |
| OV-2 | "Back up now…" opens the backup plan (6.1). When the health check blocks a backup (`health.Result.BlocksBackup`), the hero names the reason and offers its fix actions (OV-1) instead of **Back up now…**, and `Ctrl+B` does nothing: a disabled button next to them would be a third action that does nothing. There is no split button: the full-backup and new-keys overrides are offered in the plan, next to the reason for the planned type. |
| OV-3 | The Folders card is a table of every configured source folder: **Folder** (backup name), **Last backup** (the date of its newest complete set) and **Next backup** (`DIFF` or `FULL`, the type the next backup would get). The row's tooltip has the path, the exact time and the plan's reason (e.g. "Next backup FULL: Full backup is 31 days old (limit 30)"). The card needs no password (2.0 spec 6.1). A folder that is missing or unreadable shows the problem in red in place of the date. More than five folders scroll within the table. A splitter below the card sets the table's height (at least three rows; until it is dragged, three to five rows as the folders need); the cards below keep their height and move down, and the page scrolls when they no longer fit. "Details" opens Settings at the folder list. |
| OV-4 | The backup directory card shows the path, a segmented bar (backups, other data, free) and a legend. Backups is the size of all 2.0 set parts in the directory; Other is used space minus Backups; Free comes from the file system. Each segment's tooltip shows exact bytes. The last line estimates a new full backup of all folders as the sum of each folder's newest full backup (trailer data length). |
| OV-5 | The page shows only what a backup needs: the type, size and base of existing sets, and the size of runs, are on Restore backup. Below the folders, the Folders card says when the newest backup failed or was cancelled ("The backup of today, 09:12 failed; the Restore backup page has its log."), which the folders' dates alone don't show. |
| OV-6 | The Keys card shows the current key set: unlock methods (from `authentication_mode`, spare YubiKey, recovery code) and the creation date; for YubiKey modes whether a YubiKey is connected (information, not a warning). When the configuration no longer matches the keys, it shows (i) "Your next backup creates new keys and full backups" with the reason (`catalog.KeySetMismatch`). Without keys: "Your first backup creates your keys." |
| OV-7 | The page starts with its title "Create backup", like Restore backup; the hero or the progress and result cards sit below it. While a backup runs, the progress card (6.2) replaces the hero. After it ends, the result card (6.3) replaces the progress card until the user dismisses it or starts another operation; then the hero returns with the new state. A restore or verification shows these cards on the Restore backup page instead (BK-8, RW-9); meanwhile the hero stays, with "Back up now…" disabled. |
| OV-8 | The health check runs at start, after each operation, on **Check again**, on **Refresh** (next to the page title on Create backup and Restore backup, centered on it; disabled while a check or an operation runs), on `F5`, and when the window is activated and the last check is older than 5 minutes. It runs on a worker goroutine; the hero keeps the last state and shows "Checking…" in its sub line meanwhile. A check never blocks the UI thread, even when the backup directory is an unreachable network share. |

## 6. Backup plan and run

### 6.1 Backup plan

The plan dialog is the backup preflight (2.0 spec 6.2). It opens immediately with a marquee while the workflow scans the folders and estimates sizes, then shows the plan.

**Figure 6.1: Backup plan.**

```text
┌────────────────────────────────────────────────────────────────────────────┐
│ Back up                                                                [x] │
├────────────────────────────────────────────────────────────────────────────┤
│ Back up 3 folders to \\NAS\Backup\RestoreSafe                              │
│                                                                            │
│ ┌────────────────────────────────────────────────────────────────────────┐ │
│ │ Folder      Backup type          Why                        About      │ │
│ ├────────────────────────────────────────────────────────────────────────┤ │
│ │ Documents   Differential (DIFF 4) Full of 1 Sep is 29 days   0.2 GB    │ │
│ │ Projects    Differential (DIFF 4) Full of 1 Sep is 29 days   1.1 GB    │ │
│ │ Pictures    Full                 Last differential was 57%   54 GB     │ │
│ │                                  of its full (limit 50%)               │ │
│ └────────────────────────────────────────────────────────────────────────┘ │
│                                                                            │
│ Space        About 55 GB needed · 370 GB free                   (ok)       │
│ Unlock       One YubiKey touch, then your password                          │
│ Afterwards   Verify each new backup. Remove Pictures chain of 6 Jul        │
│              (full + 5 differentials, 41 GB). > Show what's removed        │
│                                                                            │
│ (i) Differential sizes are estimates; moved or renamed files are stored    │
│     again. > Show details                                                  │
├────────────────────────────────────────────────────────────────────────────┤
│ [ Full backup instead ]  [ New keys + full backup... ]  [ Start ] [Cancel] │
└────────────────────────────────────────────────────────────────────────────┘
```

**Figure 6.2: Confirm new keys.** (`TaskDialog`)

```text
┌────────────────────────────────────────────────────────────────┐
│ (!)  Create new keys?                                          │
│      Every folder gets a full backup, locked with a new        │
│      password, YubiKey registration and recovery code. Your    │
│      old password and recovery code keep opening your older    │
│      backups, but not the new ones.                            │
│                                                                │
│      Use this to change your password, replace a lost          │
│      YubiKey, or get a new recovery code.                      │
├────────────────────────────────────────────────────────────────┤
│                          [ Keep current keys ]  [ Create keys ]│
└────────────────────────────────────────────────────────────────┘
```

| ID | Requirement |
|---|---|
| BP-1 | The plan lists every source folder in a table: **Folder**, **Type** (badge: differential with its number, or full), **Why** (the reason in plain words) and **About** (the size (for a differential the estimate from 2.0 spec 6.2; for a full the folder size)). A folder with a blocking problem has no type and shows the problem in red under Why. The row's tooltip has the path and the full reason. The table shows up to five rows; a splitter below it makes it taller or shorter (not below its rows, at most three), and the dialog grows or shrinks with it, as in the Restore window (RW-5). |
| BP-2 | Below the table: **Space** (needed, free, status icon; a warning when only the estimate fits, an error when it doesn't fit), **Unlock** (the prompts that will follow: "Password", "One YubiKey touch, then your password", "One YubiKey touch", or for new keys "New password, register 2 YubiKeys (4 prompts), store a recovery code"), **Afterwards** (verify after backup on or off; what retention will remove if the run succeeds, section 11.3). "Show what's removed" expands a list of the sets and their sizes. When nothing would be removed: "Nothing is removed (keeps 3 chains per folder)". |
| BP-3 | New keys (first backup, configuration changed, or chosen): a note at the top in the Information color: "New keys will be created: <reason>. Every folder gets a full backup." |
| BP-4 | Buttons: **Start** (default), **Full backup instead** (only when at least one differential is planned; replans every folder as full and checks the space again, the dialog stays open), **New keys + full backup…** (only when existing keys are reused; confirms with figure 6.2, then replans), **Cancel**. They map to `interact.BackupAsPlanned`, `BackupFull`, `BackupNewKeys` and `BackupCancel`. "Full backup instead" becomes "Back to plan" after it was used. |
| BP-5 | Blocking issues (`Report.HasErrors`) remove **Start**; the issues are listed in red above the buttons with their remedy. Warnings are listed in amber and don't block. "Show details" shows the full preflight report (split size, exclude patterns, unreadable-file rule, Argon2, log level) in the report view. |
| BP-6 | **Start** closes the plan and opens the credential dialogs (section 9). Cancelling a credential dialog ends the backup before anything is written; the Create backup page shows no result card for it. |

### 6.2 Progress

| ID | Requirement |
|---|---|
| BR-1 | The progress card shows a title ("Backing up", "Restoring", "Verifying"), the step trail, the current folder and its type, a progress bar, bytes done of the (estimated) total, speed, **Cancel** and "Show log". |
| BR-2 | Step trail for a backup: **Unlock keys** › **Back up n of N** › **Verify** (only with `verify_after_backup`) › **Clean up** (retention). For verify: **Unlock keys** › **Verify n of N**. Completed steps show a check. While keys are unlocked (Argon2 takes seconds) or a Windows Security prompt is open, the bar is a marquee and the line under the title reads "Unlocking keys…" or "Follow the Windows Security prompt". |
| BR-3 | The speed is computed by the UI from `Progress.Done` of the current folder over the last 5 seconds. There is no time left: an estimate would be unreliable, since network speed varies. The bytes line shows the current folder's total, which the workflow measures before it starts, so it carries no "about". |
| BR-4 | While a backup runs, the Folders table has the columns **Folder**, **Type** (the badge the run gives the folder) and **Status**: done folders show "Done" and the size of the backup set written in green (the last progress report of the folder carries it, `Progress.Written`), the current folder its percentage, the others "Waiting"; a folder that failed or was skipped by `on_unreadable_file: fail` says so in red. A folder the run leaves out shows its problem. |
| BR-5 | Progress is coalesced (12.2): at most one pending progress message; the workflows report four times per second. The taskbar button shows progress (`ITaskbarList3`): normal while running, indeterminate while unlocking, error on failure, paused (amber) when finished with warnings. |
| BR-6 | **Cancel** asks with figure 6.3 (restore: figure 8.2). On confirmation the button shows "Cancelling…" and is disabled until the worker has finished (12.4). |

**Figure 6.3: Cancel a backup.** (`TaskDialog`)

```text
┌────────────────────────────────────────────────────────────────┐
│ (!)  Cancel this backup?                                       │
│      Folders already backed up in this run are kept. The       │
│      folder being backed up now is discarded. No old backups   │
│      are removed.                                              │
├────────────────────────────────────────────────────────────────┤
│                            [ Keep running ]  [ Cancel backup ] │
└────────────────────────────────────────────────────────────────┘
```

### 6.3 Result

**Figure 6.4: Result cards.**

```text
┌───────────────────────────────────────────────────────────────────────────────────┐
│ (ok)  3 folders backed up                                   [ Show log ] [ Done ] │
│       3.4 GB in 4 min. Verified. Removed 6 old backups (41 GB).                   │
└───────────────────────────────────────────────────────────────────────────────────┘
┌───────────────────────────────────────────────────────────────────────────────────┐
│ (!)  3 folders backed up with 2 warnings                    [ Show log ] [ Done ] │
│      2 files in Documents were in use and weren't backed up. Old backups of       │
│      Documents are kept.                                                          │
└───────────────────────────────────────────────────────────────────────────────────┘
┌───────────────────────────────────────────────────────────────────────────────────┐
│ (x)  Backup failed                                          [ Show log ] [ Done ] │
│      Projects: the backup directory ran out of space. Documents was backed up;    │
│      the unfinished Projects backup was removed. > Show details                   │
└───────────────────────────────────────────────────────────────────────────────────┘
┌───────────────────────────────────────────────────────────────────────────────────┐
│ ( )  Backup cancelled                                                    [ Done ] │
│      Documents was backed up. The unfinished Projects backup was removed.         │
└───────────────────────────────────────────────────────────────────────────────────┘
```

| ID | Requirement |
|---|---|
| BR-7 | The result card maps the workflow result as in the table below: the first line is the outcome, the second what happened per folder and whether retention ran. "Show details" shows the workflow's message with its remedy. "Show log" opens the operation's log in the log window of BK-5, on every page. The same mapping applies to a restore (RW-8: "2 folders restored", then "About 92 GB to D:\Restore in 18 min. Every file matched its checksum.") and to verify, whose title answers what a verification is for: "The backup of today, 09:12 can be restored", then "Checked 3 folders in 1 min, including the full backups they're based on. Every file matched its checksum." (a differential is only checked together with its full backup, BK-7a). The title claims the whole backup only when every folder of it was verified; when some of its folders can't be verified (their full backup is missing), it names the folders that were: "Pictures from the backup of today, 09:12 can be restored". A verification that found damage: "Damage found in the backup of today, 09:12". The problem line ("Another backup has a problem…") is added only to a green card: the check vouches for what the run did, not for the other backups. |
| BR-8 | If the window isn't in the foreground when an operation ends, the taskbar button flashes (`FlashWindowEx`, until the window is activated). No other notification is shown. |

| Workflow result | Icon | First line |
|---|---|---|
| `nil`, `ShowResult` reports no warnings | (ok) | "3 folders backed up" (the title says what the user has now); when the page then shows an error about other backups, a last line says so: "Another backup has a problem; see below." on Restore backup, "…; the Restore backup page shows it." elsewhere |
| `nil`, `ShowResult` reports warnings | (!) | "3 folders backed up with N warnings" |
| matches `context.Canceled` | neutral | "Backup cancelled", with what was kept |
| preflight error | (x) | "Backup didn't start", with the issues from the plan |
| other error | (x) | "Backup failed", with the message and its remedy under "Show details" |

The warning count and the log file path come from `ShowResult`, which the workflows call before they return without error.

### 6.4 Closing the window

Closing the window while an operation runs shows the cancel dialog of 6.2 with the headline "Close RestoreSafe?" and the buttons **Keep running** / **Cancel and close**. On confirmation the window stays open and disabled with "Cancelling…" until the worker has finished, then closes (12.4). Closing during a question cancels the question. Log off and shutdown: 12.4.

## 7. Restore backup page

Purpose: see every backup run, understand chains, act on a run or a set, read its log.

**Figure 7.1: Restore backup page.**

```text
┌──────────────────────────────────────────────────────────────────────────────────────────────────────┐
│ RestoreSafe 2.0.0                                                                      _    [ ]    x │
├──────────────┬───────────────────────────────────────────────────────────────────────────────────────┤
│              │ Backups                                                     [ All folders v ]         │
│   Overview   │                                                                                       │
│ > Backups    │ ┌───────────────────────────────────────────────────────────────────────────────────┐ │
│   Settings   │ │ (i) If your next backup succeeds, it removes 6 backups (41 GB): Pictures: full   │ │
│              │ │     backup of 6 Jul and 5 differentials, 41 GB.                                   │ │
│              │ └───────────────────────────────────────────────────────────────────────────────────┘ │
│              │ ┌───────────────────────────────────────────────────────────────────────────────────┐ │
│              │ │   Folder        Type      Based on         Size     Chain    Status               │ │
│              │ ├───────────────────────────────────────────────────────────────────────────────────┤ │
│              │ │ v Today, 09:12 · 3 folders · 3.4 GB · (!) 1 warning                     Show log  │ │
│              │ │ >   Documents   DIFF 3    FULL of 1 Sep    0.2 GB   ABC123   (!) 1 skipped file   │ │
│              │ │     Projects    DIFF 3    FULL of 1 Sep    1.1 GB   ABC123   (ok) Complete         │ │
│              │ │     Pictures    DIFF 6    FULL of 3 Aug    2.1 GB   KLM456   (ok) Verified 09:16   │ │
│              │ │ > Sun 27 Sep, 20:05 · 3 folders · 1.9 GB                                Show log  │ │
│              │ │ > Tue 1 Sep, 17:45 · 3 folders · 96 GB · new keys                       Show log  │ │
│              │ ├───────────────────────────────────────────────────────────────────────────────────┤ │
│              │ │ Documents, differential 3 of today             [ Restore... ]  [ Verify... ]      │ │
│              │ └───────────────────────────────────────────────────────────────────────────────────┘ │
├──────────────┴───────────────────────────────────────────────────────────────────────────────────────┤
│ Ready                                                                           12 runs · 186 GB     │
└──────────────────────────────────────────────────────────────────────────────────────────────────────┘
```

**Figure 7.2: Problem and information lines** (shown above the list, one per finding).

```text
┌───────────────────────────────────────────────────────────────────────────────────┐
│ (x)  4 backups of Documents can't be restored. Their full backup (chain ABC123)   │
│      is missing part 003. Restore its FULL files from your copy, or delete the    │
│      DIFF files of ABC123.                                [ Open backup folder ]  │
├───────────────────────────────────────────────────────────────────────────────────┤
│ (x)  A backup of Pics can't be used. The full backup of 1 Sep (chain XYZ789) has  │
│      renamed files. Restore the original file names.                             │
├───────────────────────────────────────────────────────────────────────────────────┤
│ (i)  12 files from RestoreSafe 1.x are in the backup folder. RestoreSafe 2 can't  │
│      restore them; keep RestoreSafe 1.0.2 for that. They're never changed.        │
├───────────────────────────────────────────────────────────────────────────────────┤
│ (i)  Leftovers of an interrupted backup (3 files) are removed by the next backup. │
└───────────────────────────────────────────────────────────────────────────────────┘
```

**Figure 7.3: Verify.** Laid out and worded like the backup plan (figure 6.1).

```text
┌────────────────────────────────────────────────────────────────────────────┐
│ Verify                                                                 [x] │
├────────────────────────────────────────────────────────────────────────────┤
│ Verify 3 folders from the backup of today, 09:12                           │
│                                                                            │
│ ┌────────────────────────────────────────────────────────────────────────┐ │
│ │ Folder                                       Type          About       │ │
│ ├────────────────────────────────────────────────────────────────────────┤ │
│ │ Documents                                    DIFF 3        38 GB       │ │
│ │ Projects                                     DIFF 3        5 GB        │ │
│ │ Pictures                                     DIFF 6        54 GB       │ │
│ └────────────────────────────────────────────────────────────────────────┘ │
│                                                                            │
│ Read         About 97 GB to read · nothing is written                      │
│ Unlock       One YubiKey touch, then your password, or recovery code       │
│                                                                            │
│ (i) Every file is decrypted and checked against its checksum.              │
│     Differentials are read with their full backups of 1 Sep.               │
│ Show details                                                               │
├────────────────────────────────────────────────────────────────────────────┤
│                                                     [ Start ]  [ Cancel ]  │
└────────────────────────────────────────────────────────────────────────────┘
```

### Requirements

| ID | Requirement |
|---|---|
| BK-1 | The list has one group per backup run (`catalog.BackupRunSummaries`), newest first. The group header shows date, number of folders, total size, the number of warnings (section 11.4) and "new keys" when the run created a key set. Each row is one backup set. The newest run is expanded, the others collapsed. |
| BK-2 | Columns: Folder (backup name, tooltip path), Type (`FULL` / `DIFF n` badge), Based on (for a differential: "FULL of <date>", tooltip the full's set name; for a full: "-"), Size (the set's parts on disk), Chain (chain ID in Consolas; it's what the user sees in Explorer), Status. Status values: Complete (green), Verified <time> (green, section 11.4), n skipped files (amber), Incomplete (red), Full backup missing (red), Damaged (red, verify failed), Running with progress. |
| BK-3 | The folder filter offers "All folders" and each source folder, plus folders that only exist in the backup directory ("Old: Music" for folders no longer in the configuration). Filtering keeps the run grouping and hides empty runs. |
| BK-4 | The selection is a run: clicking its header or any of its sets selects the run, shows only its header selected, not its sets. An incomplete set has no run and is selected by itself. The action bar shows the selection in words and its actions: **Restore…** (opens the Restore window on that run, section 8) and **Verify…** (opens the Verify window, figure 7.3; verifies all the run's sets). A set whose full backup is missing or that is incomplete can't be restored or verified; the action bar says why. With no selection the action bar shows a hint. Double-click or `Enter` starts Restore. A context menu offers Restore…, Verify…, Show log, Copy set name, Open backup folder. |
| BK-5 | Each run's header ends with the link "Show log" (a list-view group task link); the context menu has "Show log" too, for the keyboard. It opens the run's log in a log window over the main window: title "Log of <date> (<log file>)", filter (All, Warnings and errors), **Open in Editor** (opens the file in the default application for log files, Notepad if there is none) and **Close**. The log is read from the backup directory when the window opens and doesn't follow a running operation. When "Warnings and errors" finds none, the window says "No warnings or errors in this log." in the secondary text color. Log lines are shown as the log file stores them, with WARN and ERROR lines colored and marked. Restore and verify append to the log of the run they read, so their entries appear in the same log. A group without a log (incomplete sets) has no link. The page has no log pane: the list fills the page above its action bar and the page scrolls when the list would be lower than 160 pixels at 100%. |
| BK-6 | Problem lines (figure 7.2) sit above the list: health check errors and warnings about the inventory (missing full backup, incomplete sets, sets that can't be used) with their remedy, and information lines for 1.x backups and leftover `.tmp` files. Affected rows are marked in the list. Each line is complete in itself: it names the set and says why it can't be used (missing parts, renamed files, unreadable, damaged; `catalog.FaultOf`) with the remedy for that reason, as listed in 11.8; the page has no details for these lines. A full backup that is there but can't be used is reported once, in the line of its differentials (`BASE_MISSING`), not again on its own. |
| BK-7 | The retention line (figure 7.1) names the backups the next backup would remove, from the same prediction as BP-2. It is shown only then: the rule itself is on Settings (`retention_keep`, `retention_keep_differentials`) and in the backup plan's Afterwards line, so the page doesn't repeat it. Retention never runs from this page; it runs only after a successful backup. |
| BK-7a | The Verify window (figure 7.3) is the verification's plan, laid out and worded like the backup plan dialog (6.1): same width, sized to its content. It opens when Verify… starts the workflow, with a marquee until the plan (`ShowVerifyPlan`) arrives. Heading: "Verify 3 folders from the backup of today, 09:12". Table: **Folder**, **Type** (badge), **About** (the size read, for a differential with its full backup); a set that can't be verified is red, with the reason as its tooltip; a splitter below the table sets its height, as in the backup plan (BP-1). **Read** ("About 97 GB to read · nothing is written") and **Unlock** (the prompts that follow Start, worded as in BP-2, plus ", or recovery code"). The note says that every file is decrypted and checked against its checksum, and which full backups the differentials are read with. Issues as in BP-5, then **Show details**. Buttons at the bottom right: **Start** (default, primary) and **Cancel**, as in BP-4; Cancel answers no, and nothing is read. When the workflow finds a blocking issue, it ends before asking; the window stays open with the issues and only Cancel, as a blocked backup plan does. |
| BK-8 | Verify: **Start** closes the Verify window, the credential dialogs follow and the progress card appears at the top of the Restore backup page, above the problem lines; the page stays shown and shows the running set with progress in its Status cell. When verification ends, the result card (6.3) replaces the progress card until it is dismissed, and the Status cells of the verified sets show "Verified <time>" or "Damaged"; damage turns the hero red (3.5) and the log lists the affected files. |
| BK-9 | No empty state: without backups the page shows the empty list with its columns and the action bar, as it does when a backup was cancelled or failed before it backed up a folder. |

## 8. Restore window

Purpose: get folders back with confidence. RestoreSafe restores whole folders (backup sets), each into a new folder, and checks every file against its checksum. It never overwrites existing files: the destination folders must not exist yet. Restoring single files is not available (2.0 spec 13.5); the window says so where users look for it.

The Restore window is the restore's plan, as the backup plan dialog (6.1) is the backup's: one page that shows what will happen, with **Start** and **Cancel**. Start closes it; the progress and the result show on the Restore backup page, as for a verification (BK-8). The restore point is chosen on the Restore backup page: the window opens from there (button, double-click, `Enter`) on the selected run, like Verify (figure 7.3). To restore from another run, the user cancels and selects it; nothing has been written before Start.

Its layout and wording follow the backup plan dialog: a heading that says what happens, the folder table, labeled **Space** and **Unlock** lines, a note, the issues, **Show details**, and **Start** and **Cancel** at the bottom right. The folders appear once, in the table, with everything the restore needs to say about each.

**Figure 8.1: Restore.**

```text
┌────────────────────────────────────────────────────────────────────────────┐
│ Restore                                                                [x] │
├────────────────────────────────────────────────────────────────────────────┤
│ Restore 2 folders from the backup of today, 09:12                          │
│                                                                            │
│ To           [D:\Restore                                 ]  [ Browse... ]  │
│              Restore into the backup directory                             │
│                                                                            │
│ ┌────────────────────────────────────────────────────────────────────────┐ │
│ │ Folder                       Type      About      Check                │ │
│ ├────────────────────────────────────────────────────────────────────────┤ │
│ │ [x] Documents                DIFF 3    38 GB      New folder           │ │
│ │ [ ] Projects                 DIFF 3    1 GB       -                    │ │
│ │ [x] Pictures                 DIFF 6    54 GB      Already exists       │ │
│ └────────────────────────────────────────────────────────────────────────┘ │
│                                  ═════                                     │
│ Space        (ok) About 92 GB needed · 212 GB free                         │
│ Unlock       One YubiKey touch, then your password, or recovery code       │
│                                                                            │
│ (i) Whole folders are restored, and every file is checked against its      │
│     checksum. To get a single file back, restore its folder to a new       │
│     place and copy the file.                                               │
│ (!) Projects can't be restored: its full backup is missing.                │
│ (x) Choose another place, or rename or move the folder that already        │
│     exists.                                                                │
│ Show details                                                               │
├────────────────────────────────────────────────────────────────────────────┤
│                                                     [ Start ]  [ Cancel ]  │
└────────────────────────────────────────────────────────────────────────────┘
```

**Figure 8.2: Restore progress, and cancel.**

```text
┌────────────────────────────────────────────────────────────────────────────┐
│ Restore                                                                [x] │
├────────────────────────────────────────────────────────────────────────────┤
│ Restoring                                                                  │
│ v Unlock keys  >  [Restore 1 of 2]                                         │
│ Documents · differential 3, with its full backup of 1 Sep                  │
│ [###############################-----------------------------------------] │
│ 14 of about 38 GB · 92 MB/s                                                │
├────────────────────────────────────────────────────────────────────────────┤
│                                                                 [ Cancel ] │
└────────────────────────────────────────────────────────────────────────────┘

┌────────────────────────────────────────────────────────────────┐
│ (!)  Cancel this restore?                                      │
│      Folders restored so far are kept. D:\Restore\Documents    │
│      stays incomplete; don't use it as a full copy.            │
├────────────────────────────────────────────────────────────────┤
│                           [ Keep restoring ]  [ Cancel restore ]│
└────────────────────────────────────────────────────────────────┘
```

**Figure 8.3: Restore results.**

```text
┌──────────────────────────────────────────────────────────────────┐
│ (ok)  2 folders restored                                         │
│       About 92 GB to D:\Restore in 18 min.                       │
│       Every file matched its checksum.                           │
│                                                                  │
│ (!)   Documents: 2 files aren't in this backup. They couldn't be │
│       read when the backup was made; the log names them.         │
│                                                                  │
│                   [ Show log ]  [ Open folder ]  [ Close ]       │
└──────────────────────────────────────────────────────────────────┘
┌──────────────────────────────────────────────────────────────────┐
│ (x)  Restore incomplete                                          │
│      Documents: a file didn't match its checksum, so the restore │
│      stopped. D:\Restore\Documents is incomplete; don't use it   │
│      as a full copy. Pictures wasn't restored.                   │
│      Verify this backup, or restore from an older one.           │
│      > Show details                                              │
│                   [ Show log ]  [ Open folder ]  [ Close ]       │
└──────────────────────────────────────────────────────────────────┘
```

### Requirements

| ID | Requirement |
|---|---|
| RW-1 | Modal dialog, about 680 × 560 DIP (the width of the backup plan dialog), centered on the main window, resizable. Buttons at the bottom right: **Start** (default, primary) and **Cancel**, as in the backup plan dialog (BP-4). `Enter` triggers Start, `Esc` cancels. Cancel before Start closes without asking; nothing has been written. |
| RW-2 | The heading says what Start does: "Restore 2 folders from the backup of today, 09:12" (the checked folders; "Restore from the backup of …" while none is checked), as the backup plan's "Back up 3 folders to …". |
| RW-3 | The window restores from the run selected on Restore backup (BK-4) and has no way to choose a run: Restore backup is where runs are compared, with their types, status and log. Restore… is only offered for a run with a restorable set (BK-4), so the window never opens on a run it can't restore. |
| RW-4 | **To**, labeled like Space and Unlock: the destination path with **Browse…**, and below it the link **Restore into the backup directory**, which fills in the path with backslashes, as Browse… does, even when the configuration writes it with slashes. The last destination of the session is filled in, else `%USERPROFILE%\Restore`. |
| RW-5 | The table lists the run's sets with checkboxes: **Folder**, **Type** (badge), **About** (the size read, for a differential with its full backup) and **Check**: "New folder" in green, "Already exists" or "Can't be created" in red for a checked folder, "-" for an unchecked one; the row's tooltip names the new folder (`destination\<backup name>`). All restorable folders are checked when the window opens. A set whose full backup is missing is shown disabled and unchecked. Which full backup a differential reads is "Based on" on Restore backup and in Show details; the table doesn't repeat it. A splitter below the table sets its height (as on Create backup, OV-3): at least as many rows as it has, up to three, at most the room the lines below leave; the height stays while the window is open. |
| RW-6 | Below the table, as in the backup plan (BP-2): **Space** ("About 92 GB needed · 212 GB free" with (ok); a warning when it may not fit; an error when it can't fit or the free space is unknown; the size is an estimate, exact only after unlocking) and **Unlock** (the prompts that follow Start, worded as in BP-2, plus ", or recovery code" when the key set has one). Then the note (whole folders, checksums, single files), the issues, and **Show details** (the full preflight report, once there is one). Issues: a warning line naming the folders whose full backup is missing; one error line saying what to do about the folders that already exist, without naming them again (their Check cell says "Already exists"); a line per folder that can't be created, with the reason; other issues of the plan in amber or red, as BP-5. While there is nothing to check, a hint replaces Space and Unlock: "Choose at least one folder.", "Enter or browse to the folder to restore into.", "Enter a full path, such as D:\Restore.", "Checking…". |
| RW-6a | The checks run when the window opens, when a folder is checked or unchecked, and 300 ms after the path stops changing, on a worker goroutine. They use `restore.PlanDestination`, the plan `restore.Run` shows (including whether a YubiKey is connected), so the window and the restore cannot disagree. A checked folder that already exists or can't be created, and an error of the plan, disable Start; warnings don't. |
| RW-6b | **Start** starts the restore with the checked folders and the destination. The workflow checks again (`ShowRestorePlan`); the window shows that plan and answers its start question (`ConfirmStart`) itself, because Start was the confirmation, and the credential dialogs (section 9) follow. If the workflow finds a blocking issue (something changed since the check), it ends before asking for anything, nothing is written, and the window shows the issue in place of the check. Once the workflow asks to start, the window closes and the credential dialogs follow, as after Start in the Verify window (BK-8). Cancelling a credential dialog ends the restore before anything is written; the page shows no result card for it. |
| RW-7 | Progress card on Restore backup (RW-9): step trail (**Unlock keys** › **Restore n of N**; every file is checked against its checksum while it is written, so there is no separate check step), current folder and its type ("differential 3, with its full backup of 1 Sep"), bar, bytes, speed (as BR-3), taskbar progress. Cancel asks with figure 8.2. |
| RW-8 | Result card on Restore backup (RW-9): success with folder count, size, time and "Every file matched its checksum"; skipped and stale files from the backup (2.0 spec 7.7: "not in this backup", "restored in an older version from <date>") as amber lines per folder, counted from the restore's `restore` fact (11.4); the restore's log names the files, so there is no separate file list; failure as **Restore incomplete** in red (never amber), naming the folder that stopped, stating that it's incomplete, and which folders weren't restored. "Open folder" opens the destination in Explorer. "Show log" shows the log in the log window of BK-5. |
| RW-9 | While a restore runs, the progress card is at the top of the Restore backup page, then the result card until it is dismissed, as for a verification (BK-8); the main window stays usable for reading; starting another operation is disabled. |

## 9. Credential dialogs

The dialogs keep the behavior and secret handling of 12.3 and 13. This section only restyles them and adds context. All are modal to the window that opened the operation, and none is a `TaskDialog`.

**Figure 9.1: Unlock.**

```text
┌────────────────────────────────────────────────────┐
│ Unlock your backups                            [x] │
├────────────────────────────────────────────────────┤
│ Enter the password for the keys of 1 Sep 2026.     │
│                                                    │
│ Password  [•••••••••••••          ]                │
│ (!) Wrong password. 2 attempts left.               │
│                                                    │
│ > Use your recovery code instead                   │
├────────────────────────────────────────────────────┤
│                              [ Unlock ] [ Cancel ] │
└────────────────────────────────────────────────────┘
```

**Figure 9.2: New keys, password.**

```text
┌────────────────────────────────────────────────────┐
│ Create your keys  ·  Step 1 of 2               [x] │
├────────────────────────────────────────────────────┤
│ Choose a password for your backups.                │
│ At least 12 characters. You need it for every      │
│ backup and every restore.                          │
│                                                    │
│ Password         [•••••••••••••••        ]         │
│ Confirm password [•••••••••••••••        ]         │
│                                                    │
│ Steps: password > register YubiKey and spare >    │
│        recovery code                               │
├────────────────────────────────────────────────────┤
│                              [ Next ]  [ Cancel ]  │
└────────────────────────────────────────────────────┘
```

**Figure 9.3: Recovery code** (static control, can't be selected; **Copy** copies the whole code, 13.3).

```text
┌────────────────────────────────────────────────────┐
│ Your recovery code                                 │
├────────────────────────────────────────────────────┤
│ Store this code in your password manager or write  │
│ it down now. It's shown only once and opens your   │
│ backups on its own.                                │
│                                                    │
│           K7QF-9M2D-XW4P-HT6N-3JBV-R8LC            │
│                                                    │
│ Keep it in a safe place, never next to your        │
│ backups or unencrypted on this computer.           │
├────────────────────────────────────────────────────┤
│ [ Copy ]                      [ I have stored it ] │
└────────────────────────────────────────────────────┘
```

| ID | Requirement |
|---|---|
| CR-1 | Unlock (`Password`, `ChooseUnlockMethod`): when the selection uses more than one key set, the notice above the field names the backup that needs other keys and their creation date, which the question carries (`interact.OtherKeys`). After a failed try, the reason and the attempts left, which the question also carries (`interact.Attempt`), are shown under the field. The YubiKey touch comes before the password (the workflow derives the YubiKey secret first), so while the keys are unlocked in a YubiKey mode the progress card reads "Unlocking keys… Follow the Windows Security prompt."; in YubiKey-only mode there is no password dialog at all. "Use your recovery code instead" appears only when the key set has a recovery slot and answers `ChooseUnlockMethod` with true; the dialog then asks for the code (unmasked, grouped as it's printed). |
| CR-2 | New keys (`NewPassword`, YubiKey registration, `WaitForSpareYubiKey`, `ShowRecoveryCode`): one dialog frame with "Step n of N", where N counts the dialogs that ask something (password; spare YubiKey), and a single one isn't numbered. YubiKey registration has no dialog of its own (the Windows Security prompt) and the recovery code dialog asks nothing, so neither is numbered; the "Steps:" line still names every step. Mismatch and length errors from the workflow appear under the fields. The spare step reads "Remove your YubiKey and connect your spare YubiKey" with **Continue**; a refused same-key registration shows the workflow's message. |
| CR-3 | Recovery code: 13.3 applies (static control, **Copy** button kept out of the clipboard history, overwritten on close, no retype step). It has no Cancel button, but closing it (title bar ×, Esc) cancels the backup before anything is written. |
| CR-4 | While a Windows Security prompt is open, the RestoreSafe window shows "Follow the Windows Security prompt" (progress card or dialog line) and doesn't steal focus (13.4). |

## 10. Settings page

Purpose: show what RestoreSafe is configured to do and where to change it. The configuration is `config.yaml` (or the file given with `-config`). In 2.0.0 the page is **read-only**; editing in the app follows in 2.1.0 (decision 1). Each card shows the effective values in plain words, with the `config.yaml` key in the tooltip, so a user knows what to change in the file.

**Figure 10.1: Settings.**

```text
┌──────────────────────────────────────────────────────────────────────────────────────────────────────┐
│ RestoreSafe 2.0.0 · home-backup.yaml                                                   _    [ ]    x │
├──────────────┬───────────────────────────────────────────────────────────────────────────────────────┤
│              │ Settings                                                                              │
│   Overview   │ ┌─ Configuration file ──────────────────────────────────────────────────────────────┐ │
│   Backups    │ │ D:\Configs\home-backup.yaml                   [ Edit config.yaml ]  [ Reload ]    │ │
│ > Settings   │ │ (i) After saving your changes in the editor, click Reload.                        │ │
│              │ └───────────────────────────────────────────────────────────────────────────────────┘ │
│              │ ┌─ Folders to back up ──────────────────────────────────────────────────────────────┐ │
│              │ │ Name        Path                              Status                              │ │
│              │ │ Documents   C:\Users\phs\Documents            (ok) Found                          │ │
│              │ │ Projects    D:\Projects                       (ok) Found                          │ │
│              │ │ Pictures    E:\Photos                         (x) Not found                       │ │
│              │ │ Leave out   *.tmp, ~$*, Thumbs.db, node_modules (all folders)                     │ │
│              │ │ Unreadable files   Stop the backup of that folder                                 │ │
│              │ └───────────────────────────────────────────────────────────────────────────────────┘ │
│              │ ┌─ Backup directory ────────────────────────────────────────────────────────────────┐ │
│              │ │ \\NAS\Backup\RestoreSafe        (ok) Reachable, 370 GB free   [ Open in Explorer ]│ │
│              │ │ Split files at 4 GB                                                               │ │
│              │ └───────────────────────────────────────────────────────────────────────────────────┘ │
│              │ ┌─ Full and differential ─────────────────┐ ┌─ Retention ───────────────────────────┐ │
│              │ │ Differential backups   On               │ │ Keep 3 chains per folder              │ │
│              │ │ New full backup after  30 days          │ │ Keep all differentials of a chain     │ │
│              │ │ or when a differential reaches 50% of   │ │ Runs after each successful backup     │ │
│              │ │ its full backup                         │ │                                       │ │
│              │ └─────────────────────────────────────────┘ └───────────────────────────────────────┘ │
│              │ ┌─ Checks ────────────────────────────────┐ ┌─ Keys and unlocking ──────────────────┐ │
│              │ │ Verify each backup after it's written   │ │ Password + YubiKey                    │ │
│              │ │   Off                                   │ │ Spare YubiKey: on · Recovery code: on │ │
│              │ │ Remind me after 7 days without backup   │ │ New passwords: at least 12 characters │ │
│              │ └─────────────────────────────────────────┘ │ > Key derivation (Argon2id)           │ │
│              │                                             └───────────────────────────────────────┘ │
│              │ ┌─ Logging ─────────────────────────────────────────────────────────────────────────┐ │
│              │ │ Log level   Info · I/O diagnostics off                                            │ │
│              │ └───────────────────────────────────────────────────────────────────────────────────┘ │
├──────────────┴───────────────────────────────────────────────────────────────────────────────────────┤
│ Ready                                                                  370 GB free in backup directory │
└──────────────────────────────────────────────────────────────────────────────────────────────────────┘
```

| ID | Requirement |
|---|---|
| ST-1 | Configuration file: the loaded path, **Edit config.yaml** (opens it in its default application, `ShellExecuteW` as today) and **Reload**. |
| ST-2 | **Reload** reads the file again, validates it and runs the health check; on success every page shows the new values. A file that doesn't load keeps the previous configuration and shows the error with its line and remedy on this card (the app keeps running). Reload is disabled while an operation runs. This replaces "restart RestoreSafe" (section 11.6). |
| ST-3 | Folders to back up: a table of backup name (including the generated alias for duplicate names), resolved path, and status from the health check (Found, Not found, Can't be read). Exclude patterns (they apply to all folders, 2.0 spec 6.4) and the unreadable-file rule ("Stop the backup of that folder" / "Skip the file and warn") are part of this card. A splitter below the card sets the table's height, as on Create backup (OV-3). |
| ST-4 | Backup directory: path, reachability and free space (from the health check), split size, **Open in Explorer**. |
| ST-5 | Full and differential: `differential.enabled`, `full_backup_interval_days`, `max_size_percent`, in words. |
| ST-6 | Retention: `retention_keep` and `differential.retention_keep_differentials` in words ("Keep all backups" for 0; when `retention_keep` is missing from the file, the value adds "(the default; 3 chains recommended)"), and "Runs after each successful backup; skipped after a failed verification or when the newest backup has skipped files". |
| ST-7 | Checks: `verify_after_backup`; the reminder limit (decision 2). |
| ST-8 | Keys and unlocking: authentication mode in words, spare YubiKey, recovery code, minimum password length. "Key derivation (Argon2id)" expands time, memory and threads with the note that changes apply to new keys only. A (i) line appears when the configuration differs from the current keys: "Your next backup creates new keys: <reason>." |
| ST-9 | Logging: `log_level`, `io_diagnostics`. |
| ST-10 | Missing settings (decision 9): when the file lacks settings that have a default (11.6), a (i) line on the configuration card names their keys, "3 settings aren't in config.yaml, so their defaults apply: reminder_days, recovery_code, differential.", with **Add to config.yaml**. It saves a copy of the file next to it (`<name>.<yyyy-mm-dd_hhmmss>.bak`), adds each missing setting with its default value and its explanation from `config-SAMPLE.yaml`, and reloads (ST-2). Settings of a block that exists already (`differential`, `argon2`) are added at the end of that block, all others at the end of the file under one comment line that says when they were added. Lines already in the file are not changed. Nothing is written unless the user clicks; the button is disabled while an operation runs. Afterwards a line names the copy until the next Reload; a file that can't be written keeps the configuration in use and shows the error as ST-2 does. |

## 11. Workflow interface

The UI keeps the `interact.UI` contract and the bridge (section 12). It never parses text written for people: not `Output()` lines and not rendered reports. What the redesign changes in today's contract is listed here. Two changes remove parts: the plans of 11.2 replace `ShowReport`, and restore and verify receive the user's choice as a request instead of asking for it (12.3). [PLAN-gui-redesign.md](PLAN-gui-redesign.md) has the package layout and the order of the work.

### 11.1 Status model: `health.Snapshot` (read-only, no password)

`health.TakeSnapshot(Params{Config, ExeDir, ConfigPath, Now})` returns everything the Create backup, Restore backup and Settings pages show. It is computed from one inspection of the configuration, the folders and the backup directory (shared with `health.Check`), `workflow/plan` (next type per folder, retention preview) and the run facts of 11.4. It's named `Snapshot`, not `Status`, because `interact.Status` (OK, Warn, Error) already exists. The code in `internal/workflow/health/snapshot.go` is the reference; in short:

```go
type State int // StateEmpty, StateProtected, StateWarning, StateError (Running is set by the UI)

type Snapshot struct {
    State     State
    Problems  []Problem          // errors and warnings, most urgent first (3.5)
    Notes     []Problem          // information: YubiKey not connected, new keys needed, 1.x files, leftovers, duplicate sources
    Folders   []FolderStatus     // one per configured source folder, in config order
    BackupDir string
    Runs      []catalog.BackupRunSummary
    Sets      []catalog.SetInfo  // newest first, including incomplete sets
    Facts     map[naming.BackupID]logging.RunFacts // 11.4, per run
    SetFacts  map[string]logging.Fact // per set name: what its backup recorded (skipped files)
    Verified  map[string]logging.Fact // per set name: its newest verification
    Storage   Storage            // Known, TotalBytes, FreeBytes, BackupBytes, FullEstimate
    Keys      KeysSummary        // Exists, Created, Methods, SpareYubiKey, RecoveryCode, YubiKeyConnected *bool, NewKeysReason
    Retention []catalog.SetInfo  // what the next backup removes if it succeeds (11.3)
    Check     Result             // the health check: "Check details" and what it blocks
    Checked   time.Time
}

type Problem struct {
    Code    interact.Code   // stable, 11.8
    Status  interact.Status // StatusError, StatusWarn or StatusInfo
    Folder  string          // backup name of the folder concerned
    Path    string
    ChainID naming.BackupID
    Set     naming.BackupEntry
    Count   int             // files, sets or days, depending on the code
    Bytes   int64
    Detail  string          // the technical text with its remedy, for "Show details"
}

type FolderStatus struct {
    plan.Source              // path, backup name, error, duplicate
    Newest *catalog.SetInfo  // newest complete set
    Next   *plan.Folder      // what the next backup does: full or differential and why (the same code the backup uses)
}
```

A problem carries its code and the facts that describe it, not sentences: the words are chosen by `gui/view`, where all user-visible strings live (3.6).

`health.Checker` takes snapshots at most one at a time. A caller waits only until its context ends (`health.SnapshotTimeout`, 5 seconds) and then gets a snapshot that reports the backup directory as not responding; a snapshot still blocked in a system call keeps running, and later callers wait for it instead of starting another.

`health.Result` keeps its items for the report view ("Check details"); every warning and error item carries a code (11.8).

### 11.2 Structured plans

`ShowReport(Report)` is replaced by one typed call per workflow, made before `ConfirmBackupStart` / `ConfirmStart`. The types are plain data in `interact` (`plan.go`); each carries the text report as `Details`, which "Show details" renders and `interacttest.Script` prints:

```go
ShowBackupPlan(p BackupPlan)
ShowRestorePlan(p RestorePlan)
ShowVerifyPlan(p VerifyPlan)

type BackupPlan struct {
    Folders     []FolderPlan // Name, Path, IsDiff, DiffNumber, BaseDate, Reason, EstimatedBytes, AllBytes, Problem
    NeededBytes, FreeBytes int64
    SpaceStatus Status
    Keys        KeyPlan      // existing or new, reason, prompts: password, YubiKey prompts, recovery code
    VerifyAfter bool
    Removes     []catalog.SetInfo // 11.3
    Issues      []Issue
    Details     Report
}
```

The workflows fill the plan from the values they already compute (`plan.Folders`, the space estimate, `plan.KeysFor`, the restore and verify preflights), and build `Details` from the same values.

### 11.3 Retention preview (new)

`plan.Retention` selects what retention removes, including its guard (nothing is removed while a set's metadata can't be read); `applyRetentionPolicy` deletes what it selects. `plan.RetentionPreview` applies `plan.Retention` to a copy of the inventory **as if** the planned run had succeeded (a new full adds a chain; a differential adds a set to its chain), for the backup plan (BP-2) and the retention line on Restore backup (BK-7). Preview and deletion therefore can't diverge. The preview describes a successful run: retention is held when a new backup misses unreadable files or its verification fails, which only the run can tell. The plan says "if the backup succeeds" when verification after backup is on, and the result card says when retention was held (BR-7).

### 11.4 Run facts from the log (new)

The Restore backup page needs, per run, the warning count and the verification results; the result card also shows the duration. They are in the run's log file today only as text. Add structured, machine-readable lines to the log (one per fact, for example `FACT  - {"kind":"backup","result":"ok","warnings":2,"seconds":252}`, `FACT  - {"kind":"verify","result":"ok","set":"Documents_ABC123_2026-09-30_DIFF003"}`) written by backup and verify through `Logger.Fact`, and `logging.ReadFacts`, which extracts them; missing or unknown lines give zero values, never an error. The log file is the right place: it lives in the backup directory next to the sets and retention deletes it with them (decision 3). The backup also writes a `set` fact per folder with the number of files it could not read, skipped and stale (a differential keeps a stale file in its older version) counted apart (`{"kind":"set","set":"Documents_ABC123_2026-09-30_DIFF003","skipped":2,"stale":1}`): the plaintext header and trailer do not record them, and SKIPPED_FILES needs them without a password. A restore writes a `restore` fact with the same two counts per set that has any, from the decrypted manifest, for its result page (RW-8). The set fact also carries the size of the set's part files (`"bytes"`), and retention writes one `cleanup` fact with the number of sets and bytes it removed (`{"kind":"cleanup","result":"ok","removed":6,"bytes":44040192000}`; `failed` or `warnings` with the reason when it could not remove or held back); the result card (BR-7) reads them.

### 11.5 Session result

The result of the last backup and verify (`ShowResult`, the returned error, and which sets failed verification) is kept in memory for this session and feeds the Error triggers of 3.5. After a restart, the state comes from the backup directory only.

### 11.6 Reload

A function that loads and validates the configuration again, returning the same errors as at start. The GUI swaps the configuration only when no operation runs. It is `config.Load` itself, run on a worker goroutine; a file read while an operation runs is used once the operation has finished. `-config` stays the way to select a different file.

`config.Load` is strict about names and lenient about omissions (decision 9):

- **Unknown keys are an error** (`CONFIG_INVALID`), at any level: a misspelled key would otherwise be ignored and its default used without a word (`retention_kep: 2` would keep every backup). The error names the key as written in the file, with its block (`argon2.memroy_mb`), and its line. Every key of RestoreSafe 1.x is still a 2.0 key, so 1.x files don't fail on this.
- **Missing keys use their defaults** and are listed in `Config.MissingKeys` in the order of `config-SAMPLE.yaml`. A block that is missing entirely is listed by its name (`differential`), single missing keys of a block by their path (`differential.max_size_percent`). `source_directories` and `backup_directory` have no default and stay required.
- `config.AddMissing(path, now)` writes them (ST-10). It reads the file again, builds the new text, and writes it only when the new text loads, lists no missing keys and gives the same effective configuration as before, so adding defaults can never change what RestoreSafe does. The comments come from `config-SAMPLE.yaml`, embedded in the executable; the values are the defaults of `config`, not the sample's (the sample keeps 3 chains, the default is to keep all). A file in YAML flow style (`{ … }`) is refused with a remedy to add the settings by hand. The new file replaces the old one by rename, keeping its line endings.

### 11.7 Progress

`interact.Progress` gains two fields; `Step` and `Item` stay for the log and the tests:

```go
type Progress struct {
    Step        string
    Item        string
    Done, Total int64
    Phase       Phase // Unlocking, BackingUp, Verifying, CleaningUp, Restoring, Checking
    Index, Count int  // folder n of N in this run (1-based), 0 when not applicable
}
```

The UI computes the speed (BR-3). The current file isn't shown (not reported by the workflows; the log has it).

### 11.8 Problem codes

Every health check finding and every preflight issue gets a stable code. The UI shows Message and Hint; Detail (the current text with `Remedy:`) goes into "Show details" and the log. The codes are the `interact.Code` constants in `internal/workflow/interact/code.go`; the severity is an `interact.Status`.

| Code | Severity | Message (UI) | Hint and action |
|---|---|---|---|
| `CONFIG_INVALID` | Blocking at start | RestoreSafe can't read its configuration | Shown in a message box at start (as today); after Reload on the Settings card. Includes unknown keys (11.6). |
| `BACKUP_DIR_UNREACHABLE` | Error | The backup directory isn't reachable | Check the drive or network connection. Actions: Check again, Edit config. |
| `BACKUP_DIR_NOT_WRITABLE` | Error | RestoreSafe can't write to the backup directory | Check the permissions. Action: Open in Explorer. |
| `SOURCE_MISSING` | Error | A folder to back up can't be found | Connect the drive, or remove the folder from config.yaml. Actions: Check again, Edit config. |
| `SOURCE_INVALID` | Error | A folder to back up can't be used | It isn't a folder, can't be read, or its backup name collides with another folder's. The detail says which. |
| `BASE_MISSING` | Error | Backups of a folder can't be restored | Their full backup (chain X) is missing, or is there and says why it can't be used (as for `SET_INCOMPLETE`: "is incomplete", "is missing part 003"). Restore its FULL files from your copy, or delete the DIFF files of that chain. Action: Open backup folder. |
| `SET_INCOMPLETE` | Error | A backup of <folder> can't be used (can't be read, when unreadable) | Names the set ("The full backup of 5 Oct (chain X)", "Differential 2 of 5 Oct (chain X)") and why, with its remedy: "is missing part 003" or "is missing 34 parts (001 to 034)", "is damaged": Restore its files from your copy, or delete them. "has renamed files": Restore the original file names. "can't be read": Check the drive, then click Refresh. Not shown for a full backup that a `BASE_MISSING` line names. |
| `LAST_BACKUP_FAILED` | Error (session) | Your last backup failed | The reason from the workflow. Action: Back up now. |
| `VERIFY_FAILED` | Error (session or log) | A backup is damaged | Create a new backup; don't rely on the damaged one. Action: Back up now. |
| `OVERDUE` | Warning | Your last backup is N days old | Action: Back up now. (Decision 2.) |
| `SKIPPED_FILES` | Warning | N files in <folder> weren't backed up | Older backups are kept until a backup without skipped files succeeds. Action: Back up now. |
| `INCOMPLETE_NEWEST` | Warning | An unfinished backup of <folder> is newer than its last complete one | Probably a crash. Look at the log of that run. Action: Show backups. |
| `SPACE_LOW` | Warning | The backup directory is running out of space | A full backup of all folders needs about N GB. |
| `ARGON2_CAPPED` | Warning | A key setting was too high and was capped | Lower the value in config.yaml. |
| `YUBIKEY_NOT_CONNECTED` | Info | YubiKey not connected | Connect it before you back up, restore or verify. |
| `NEW_KEYS_NEEDED` | Info | Your next backup creates new keys | The reason from `KeySetMismatch`. |
| `LEGACY_1X` | Info | RestoreSafe 1.x backups found | Keep RestoreSafe 1.0.2 to restore them. |
| `LEFTOVER_TMP` | Info | Leftovers of an interrupted backup | Removed by the next backup. |
| `SOURCE_DUPLICATE` | Info | A folder is listed twice | It is backed up once. Remove the duplicate from config.yaml. |
| `FOLDER_NOT_BACKED_UP` | Warning | <folder> has no backup yet | Action: Back up now. |
| `BACKUP_DIR_NEW`, `NO_BACKUPS` | (health check only) | The backup directory is new or empty | The snapshot shows this as the Empty state, not as a problem. |
| `BASE_MISSING`, `SET_INCOMPLETE` | Error (preflight) | This backup can't be used | Its full backup is missing, or the set is incomplete. |
| `SPACE_INSUFFICIENT` | Error (preflight) | There isn't enough space | Free up space, or choose another place. |
| `SPACE_ESTIMATE_ONLY` | Warning (preflight) | The differential fits only by its estimate | If everything is stored again, the space runs out and the backup stops. |
| `PART_LIMIT` | Error (preflight) | A folder is too large for the split size | Increase `split_size_mb`. |
| `FREE_SPACE_UNKNOWN` | Error (preflight) | The free space at the destination is unknown | Check the destination. |
| `RESTORE_TARGET_EXISTS`, `RESTORE_TARGET_INVALID` | Error (preflight) | The folder to restore into exists already, or its name isn't valid | Choose another place, or rename or move that folder. |
| `BACKUP_DIR_NOT_LOCKED` | Warning (restore and verify preflight) | RestoreSafe can't lock the backup directory | Don't start a backup in it while this runs (e.g. on a read-only medium, where no lock file can be created). |

### 11.9 State computation

```text
if operation running                                        -> Running (UI)
if no complete 2.0 set and no error of the backup directory or the configuration -> Empty   (other problems still shown)
if any Error problem                                        -> Error
if any Warning problem                                      -> Warning
else                                                        -> Protected
```

## 12. Architecture

Carried over from the first GUI; unchanged unless noted.

### 12.1 Packages

| Package | Content |
|---|---|
| `cmd/restoresafe` | Starts the GUI (section 14). |
| `internal/gui` | Composition root and Win32 screens: `Run`, the app, the message loop, the shell (sidebar, status bar), one file per page and dialog. Page files only render views and forward input. |
| `internal/gui/flow` | Operation lifecycle without Win32: the bridge (12.2), the `interact.UI` implementation (questions go to a `Dialogs` interface the `gui` package implements), the state machine of section 4, the speed. |
| `internal/gui/view` | View models without Win32: plain functions from snapshot, plans and state to what each page and dialog shows; every user-visible string (`strings.go`) and all formatting (`format.go`). |
| `internal/gui/widget` | Reusable Win32 controls and the theme: palette and metrics (3.3), fonts and glyphs, DIP scaling, layout helper, card and hero containers, badges, bars, step trail, sidebar, list view with groups. Knows nothing about backups. |
| `internal/gui/win32` | Thin wrapper over the Win32 functions, structs and constants the GUI uses (user32, gdi32, comctl32, shell32, ole32, msftedit). No logic. Structs mirror the Windows SDK layout; every call that can fail returns an error built from `GetLastError`. |
| `internal/workflow/interact` | The contract between the workflows and the frontend: `interact.UI`, the plan types (11.2), problem codes (11.8), `Report`, `Progress`, `Result`, `ErrCancelled`. |
| `internal/workflow/plan` | Planning without a password: source resolution and backup names, full or differential per folder, key plan, retention candidates and preview. |
| `internal/workflow/health` | `health.Check` and `health.Result` (items with problem codes), `health.Snapshot` (11.1). |
| `internal/logging` | Run log files; facts (11.4). |
| `internal/security/yubikey` | `SetParentWindow(hwnd)` (13.4). |

Imports point downward only; `internal/architecture` checks it (its package documentation lists the layers and rules). Two rules shape this layout:

- No workflow package may import `backup`, `restore`, `verify` or `health`. Therefore the backup type planning and the retention selection live in `workflow/plan`, which `backup` and `health` both import. The "next" type on Create backup, the backup plan, the retention preview and the retention the run performs share one implementation (11.2, 11.3).
- `gui/flow` and `gui/view` import neither `gui/widget` nor `gui/win32` nor `golang.org/x/sys/windows`; `gui/widget` imports only `gui/win32`. Everything that decides what the user sees is testable without a window (16.1).

### 12.2 Threads and the bridge

```
UI thread (locked OS thread)           Worker goroutines
────────────────────────────           ─────────────────
message loop                           backup.Run(ctx, guiUI, cfg, exeDir)
  │                                      │
  │  ◄── PostMessage(WM_APP_QUESTION) ── u.Password(prompt)   (blocks)
  │  show dialog, read answer            │
  │  ── reply channel ─────────────────► returns []byte
  │                                      │
  │  ◄── PostMessage(WM_APP_OUTPUT) ──── Output().Write(...)  (buffered, never blocks)
  │  ◄── PostMessage(WM_APP_PROGRESS) ── Progress(p)          (coalesced, never blocks)
  │  Cancel button → cancel(ctx)         │  stops, cleans up
  │  ◄── PostMessage(WM_APP_DONE) ────── returns error
  │
  │  ◄── PostMessage(WM_APP_STATUS) ──── health.Snapshot (11.1), destination checks, Reload
```

- `main` calls `runtime.LockOSThread` before creating any window; all Win32 UI calls happen on that thread. `IsDialogMessageW` in the message loop gives standard dialog navigation in the main window too.
- At most one operation worker runs at a time. Status computation, restore destination checks and Reload run on their own short-lived goroutines and post their results; a result that arrives after a newer request was made is dropped.
- **Questions:** the bridge puts a request (kind, parameters, reply channel) into a queue and posts `WM_APP_QUESTION`. The UI thread shows the dialog and sends exactly one reply. If the window is closing or the operation is cancelled, pending and later questions are answered with `interact.ErrCancelled`.
- **Output:** writes are appended to a mutex-protected buffer; `WM_APP_OUTPUT` is posted only when the buffer changes from empty to non-empty. The UI thread takes the whole buffer and appends it to the live log.
- **Progress:** the latest `interact.Progress` is stored under a mutex; `WM_APP_PROGRESS` is posted only when no progress message is pending. The workflows report four times per second, which bounds the repaints.
- **Plans:** the `Show*Plan` calls of 11.2 are questions without an answer: the worker waits until the plan dialog shows them, so the plan and the following `ConfirmBackupStart` / `ConfirmStart` appear together.

### 12.3 Mapping of `interact.UI`

| Method | GUI |
|---|---|
| `Output` | The operation's output; the log window reads the log file. |
| `ShowBackupPlan` | Backup plan dialog (6.1); its `Details` for "Show details". |
| `ShowRestorePlan` | Restore window (8): the check of Start (RW-6b); its `Details` for "Show details". |
| `ShowVerifyPlan` | Verify window (figure 7.3); its `Details` for "Show details". |
| `ShowResult` | Result card (6.3). |
| `LogStarted` | The log file of the run, reported as soon as it is open: "Show log" of the progress and result cards, also after a failure (new in 2.0; the workflows call it after opening the log). |
| `ConfirmStart` | **Start** in the Verify window (BK-8); answered by the Restore window itself after its **Start** (RW-6b). |
| `ConfirmBackupStart` | **Start**, **Full backup instead**, **New keys + full backup…**, **Cancel** (BP-4). |
| `ChooseUnlockMethod`, `Password` | Unlock dialog (9.1). |
| `NewPassword`, `ShowRecoveryCode`, `WaitForSpareYubiKey` | New-keys dialogs (9.2, 9.3). |
| `Progress` | Progress card (6.2). |

The user's choice is not a question: the GUI passes it when it starts the workflow, `restore.Run(ctx, u, cfg, exeDir, restore.Request{Sets, Destination})` and `verify.Run(ctx, u, cfg, exeDir, verify.Request{Sets})`. The first GUI's questions `SelectBackups` and `RestoreDestination` are removed from `interact.UI`.

Cancel in any question returns `interact.ErrCancelled` or the method's "no" answer (`ConfirmStart` false, `BackupCancel`, `WaitForSpareYubiKey` false). Cancel in a password dialog returns an error "Cancelled."; the workflow ends before anything is written.

Messages the workflows print between questions (e.g. "Wrong password. 2 attempt(s) remaining.", YubiKey instructions) go to the live log. The bridge also remembers the last output line, and a password dialog opened right after it shows that line under the field. This is a presentation aid only; the workflow logic does not depend on it.

### 12.4 Cancellation, closing and session end

- **Cancel** cancels the workflow's context and waits for the worker to finish. The workflows clean up on cancellation: incomplete parts are removed, the lock is released, the log is written.
- **Closing the window:** no operation running: the window closes. During a question: the question is answered with `interact.ErrCancelled`, the worker finishes, the window closes. During a running operation: the confirmation of 6.4; on confirmation the context is cancelled and the window closes after the worker has finished (it stays open, disabled, showing "Cancelling…").
- **Windows shutdown or logoff** (`WM_QUERYENDSESSION`): the operation is cancelled, and `ShutdownBlockReasonCreate` ("RestoreSafe is stopping a backup") asks Windows to wait until the worker has finished. Windows may still end the process after its timeout; the incomplete parts are then removed by the next backup (2.0 spec 4.7).

## 13. Secrets and YubiKey

### 13.1 Reading a password

1. The password field is an edit control with `ES_PASSWORD` (Windows prevents copying its text).
2. On OK, `GetWindowTextW` reads it into a `[]uint16` buffer the GUI allocates, which is converted to UTF-8 into a `[]byte` returned to the workflow (which zeroes it).
3. The `[]uint16` buffer and every intermediate buffer are zeroed immediately.
4. The control's text is overwritten with the same number of filler characters and then cleared, and its undo buffer is emptied, before the dialog is destroyed, so neither its buffer nor Undo keeps the password.

**Limits:** the edit control's buffer belongs to Windows; step 4 overwrites it in place in practice, but Windows does not guarantee that no copy remains in freed memory. Go strings are never used for passwords.

### 13.2 New password

As 13.1 for both fields. The dialog returns both entries; `guiUI.NewPassword` checks them with the same rules and errors as `interact.ReadPasswordConfirmed` (`ErrPasswordEmpty`, `ErrPasswordMismatch`), zeroes the confirmation, and returns the password. The length rule stays in the workflow.

### 13.3 Recovery code

- Shown once in its own dialog (figure 9.3), in large bold Consolas on one line (one code, so one line; the font is one and a half times the message font, so all six groups fit). The code is a static control, so it cannot be selected; there is no Print or Save button. A task dialog is never used, because task dialogs copy their text to the clipboard on `Ctrl+C` without the protection below.
- **Copy** puts the whole code on the clipboard as one line with dashes, e.g. for a password manager, and then reads **Copied**. The clipboard data carries the formats `ExcludeClipboardContentFromMonitorProcessing`, `CanIncludeInClipboardHistory` = 0 and `CanUploadToCloudClipboard` = 0, so Windows keeps it out of the clipboard history and the cloud clipboard.
- **I have stored it** (or closing the dialog) overwrites the displayed code and closes the dialog; the backup continues. There is no retype step: like the recovery codes of other services, the user is trusted to store it. The code reaches the GUI as a Go string, so it is not zeroed.

### 13.4 YubiKey and Windows Security dialogs

The WebAuthn calls take a parent window: `yubikey.SetParentWindow(hwnd)` is set to the main window at start and to the Restore window while it is open. The Windows Security dialog (PIN, touch) is then modal to RestoreSafe and appears in front of it. While it is open, the progress card or dialog shows "Follow the Windows Security prompt".

## 14. Build and start

- `cmd/restoresafe/main.go` starts the GUI. It keeps the `-config=<absolute path>` flag.
- A configuration that cannot be loaded at start (or an invalid `-config` argument) shows an error message box with the message; closing it ends RestoreSafe. After start, Reload (ST-2) keeps the app running instead.
- The executable uses the Windows GUI subsystem (`-ldflags "-H=windowsgui"`); no console window opens. There is no console frontend and no hidden console mode; the scripted text UI the tests use is `interacttest.Script`, which the release binary does not import.
- `build/versioninfo.json` references the application manifest `build/RestoreSafe.manifest`: common controls 6, `PerMonitorV2` DPI awareness, `asInvoker`, supported OS Windows 11.
- `yubidiag` stays a console tool.

## 15. Non-functional requirements

| Area | Requirement |
|---|---|
| Threading | As 12.2: the UI thread only handles messages and painting; workflows, health checks, status and plan computation run on goroutines; results reach the UI through `PostMessage`. No file, network or workflow call on the UI thread. |
| Responsiveness | The window opens in under 1 second with the last content shown as "Checking…" until the first status arrives. The Restore backup page handles 1,000 sets without noticeable lag. Progress redraws at most four times per second and never flickers. |
| Secrets | As section 13: masked edits, buffers zeroed, no Go strings for passwords, no task dialogs for codes, nothing written to disk or to the log. |
| Accessibility | All functions reachable by keyboard with a logical tab order and visible focus; access keys on all buttons, unique per page and dialog. Controls on hidden pages are disabled, so their access keys can't fire. Text is in standard controls; painted controls (step trail, badges, bars) set an accessible name (3.4), so the hero's title and the current step are read out. Contrast at least 4.5:1 for text. Status never conveyed by color alone. Works in high contrast; switching it on or off rebuilds the pages at once, while a dialog open at that moment keeps its colors until it closes. |
| DPI | Verified at 100%, 125%, 150% and 200%, and when moving between monitors with different scaling. |
| Robustness | If the backup directory disappears during an operation, the operation ends as the workflow defines it, the result card explains it, and the hero turns red. |
| Testability | Every page and dialog is rendered from a view model computed by plain Go functions (16.1). Every interactive control has a fixed control ID, exposed as its UI Automation `AutomationId`, so test scripts don't depend on texts. |
| Persistence | None (3.1). |
| Size | No new modules; the binary grows by at most a few hundred KB. |

## 16. Test plan

The UI is only as trustworthy as the backups it describes. The tests therefore focus on three risks, in this order:

1. **The UI says something that isn't true.** A hero that shows green while a full backup is missing, a plan that promises a differential and the run writes a full, a retention preview that names the wrong chain. These are the most dangerous defects and get the most tests.
2. **The UI loses or leaks something.** A cancelled operation that leaves parts behind, a password in a Go string, a question answered twice.
3. **The UI is hard to use.** Missing states, unreachable controls, unreadable layouts at other DPIs.

Every test names the requirement it covers (`// OV-3` in Go tests, the ID column in the checklist), so coverage per requirement can be read off with a search.

### 16.1 Testable structure

- **View models.** For each page and dialog, a plain Go function computes what is shown from its inputs: `(Status, session, now) → CreatePage`, `(BackupPlan) → PlanView`, `(Status, filter, selection) → RestorePage`, `(restore choices, check) → RestoreView`, and so on. A view holds the texts, icons, badge kinds, enabled states and actions, but no Win32 handles. The window code only renders views and forwards input. Tests call the functions directly.
- **Operation state machine.** The lifecycle of section 4 (choose, plan, unlock, run, result, and cancel or close at each step) is a state machine without windows, driven through an interface for the window side, as the first GUI's screen state machine was.
- **Clock and file system.** The status model, the view models and the speed and time-left calculation take `now` and a clock as parameters. Tests never sleep and never depend on today's date.
- **Fixtures.** `internal/testutil/scenario` builds backup directories with real sets (written through `setwriter`, like the existing `testutil` fixtures, with password-only keys and injectable creation times) and then damages them on purpose: delete the FULL parts of a chain, truncate the last part, add an `.enc.tmp` leftover, add 1.x file names, back up a file held open without sharing (skipped with `on_unreadable_file: skip`), and set the configuration so that the next backup needs new keys. Each condition of 3.5 and 11.8 has one fixture.

### 16.2 Workflow additions (Go tests, `go test ./...`)

| Area | Tests | Covers |
|---|---|---|
| Status model | One table-driven test of `health.Snapshot` over all fixtures of 16.1: expected `State`, the problem codes in order, "and N more", `Folders` with next type and reason, `Keys`, `Storage`. Includes: YubiKey not connected is information only; 1.x and leftovers are notes; Empty wins over Warning but Error problems are still listed; an unreachable backup directory returns within the 5-second timeout. | 3.5, 11.1, 11.9, OV-1, OV-3 to OV-6 |
| Problem codes | Every health finding and every preflight issue carries a code from 11.8 (a test runs all fixtures and fails on an empty or unknown code). Message and Hint are non-empty and contain no `Remedy:`, no Go error text and no file names with IDs. | 11.8, 3.6 |
| Plan is what happens | End-to-end per scenario (first backup with new keys; differential; full because the full is too old, with an injected clock; full because the last differential exceeded `max_size_percent`; forced full; new keys; differentials disabled; a missing source folder): the plan's type per folder equals the sets the run writes, and the plan and the text report come from the same values. | 11.2, BP-1 to BP-4 |
| Retention preview is what happens | End-to-end: the sets named by the preview are exactly the sets the run deletes, for a new chain with `retention_keep` 1, 2 and 3; a new differential with `retention_keep_differentials` 1 and 2; both limits together; an old incomplete set; and nothing to remove (retention off, below the limits). The hold after skipped files or a failed verification is tested at `applyRetentionPolicy`. | 11.3, BP-2, BK-7 |
| Run facts | Backup (also when it fails or is cancelled) and verify (also after a backup) write the facts of 11.4 into the right run log; the reader returns duration, warnings and verify results. A log without such lines (older 2.0 runs, a hand-edited or truncated log) yields "unknown" and never an error. | 11.4, BK-1, BK-2, BK-8 |
| Progress | A recording UI checks per workflow: phases in the order of BR-2, `Index` from 1 to `Count`, `Done` never decreasing within a step, a final report per step. | 11.7, BR-2, RW-7 |
| Reload | A valid file replaces the configuration; an invalid file keeps the previous one and returns the error; Reload during an operation is refused. `reminder_days` bounds and default. | 11.6, ST-2, decision 2 |
| Config keys | An unknown key, top level and in a block, fails with its name and line; every key of `Config` and every key of `config-SAMPLE.yaml` is known, and every key with a default can be reported missing. `MissingKeys` for an empty block, a partial block and a minimal file. `AddMissing` on the minimal file, a partial block, a CRLF file and a file without a final newline: the result loads, lists nothing missing, has the same effective configuration, keeps every original line, and the copy equals the old file; a flow-style file is refused and left unchanged. | 11.6, ST-10, decision 9 |
| Unchanged workflows | The existing workflow and e2e tests pass; they change only for the request parameters, the plan calls, the progress fields and the facts. | 17.1 (criterion 6) |

### 16.3 GUI logic without windows (Go tests)

| Area | Tests | Covers |
|---|---|---|
| Create backup view | Every hero variant of figure 5.3 plus Protected and Running, from fixtures: title, sub line, icon, primary and secondary action, "Back up now…" or, when the check blocks a backup, the fix actions in its place (OV-2). The page title; the Folders card (dates, next type, the note on a failed or cancelled backup), the backup directory and Keys cards per fixture. | OV-1 to OV-7 |
| Operation state machine | Every transition of section 4: cancel in the plan, in each credential dialog, during the run and during cleanup; close during a question and during a run; session end; a workflow error in each phase. After each path: exactly one worker ran, no question is left unanswered, the right result card is shown, "Back up now…" is enabled again. | 4, 6.4, 12.4, BR-6, BR-7 |
| Backup plan view | The button matrix of BP-4 (differential planned or not, keys reused or new, blocking issues present or not), the Unlock line for every authentication mode with spare and recovery code, the Afterwards line with and without removals. | BP-1 to BP-6 |
| Restore backup view | Grouping per run, newest first; status per set for every fixture; folder filter including folders that are no longer configured; which actions are available for a run, a set, a set with a missing full and an incomplete set. | BK-1 to BK-9 |
| Restore window | Preselection from a run and from a set, Start enablement (no folder checked; an existing target folder; a space warning, which doesn't block), the heading, Check column and issue lines, checks debounced with a fake timer, stale check results dropped. | RW-1 to RW-6b |
| Speed | Synthetic progress sequences: the rate over the last 5 seconds, a new folder starts a new rate, bytes going back start over. | BR-3 |
| Formatting | Relative dates across midnight, weekdays and years with an injected clock; binary sizes with one decimal below 10; set names in text ("Documents, differential 3 of 27 Sep"). | 3.6 |
| Strings | All user-visible strings from the string table: no "!", no "successfully", no "please"; buttons start with a verb; "…" on buttons that open a dialog; format verbs match their arguments. | 3.6 |
| Access keys and names | Per page and dialog: access keys are unique, and every interactive control has an accessible name. (The first GUI found a real defect this way: a hidden page's access key started a backup.) | 15 |
| Layout | Every page and dialog at its minimum and default size at 96, 120, 144 and 192 dpi: no control overlaps another or leaves the client area, and every text fits its measured width. | 3.2, 15 |
| Bridge and secrets | The existing bridge tests (questions answered exactly once, cancellation, output order, progress coalescing) and secret tests (buffers zeroed) stay and are extended to the plan messages. | 12.2, 13 |

### 16.4 Window automation (`scripts/gui-test`)

The PowerShell scripts drive the real window. They are updated for the new UI and find controls by `AutomationId` (15), not by text.

| Script | Purpose |
|---|---|
| `New-TestCondition.ps1` (new) | Turns a backup directory made by the smoke test into one of the conditions of 3.5 and 11.8 by moving, truncating and adding files, e.g. `-Condition BaseMissing`. The condition names are those of the Go fixtures (`internal/testutil/scenario`). `Overdue` has no script variant (a header date can't be faked); the checklist uses a backup from the day before. |
| `Check-States.ps1` (new) | For each condition: start RestoreSafe, wait for the status, read the hero's accessible name and primary action, compare them with the expected values, save a screenshot. |
| `Smoke-BackupRestore.ps1` | Back up through the plan dialog, restore one folder of the newest run through the Restore window, verify the run; compare the restored files with the sources. |
| `Screenshot.ps1` | One screenshot per page and dialog, for the visual review against 3.3 and the wireframes (16.5), and for the README. |
| `Accessibility.ps1` | Role, name, `AutomationId` and access key of every control on every page and dialog. |

They run on every release candidate at 100% and 150% scaling.

### 16.5 Manual tests

[GUI-TEST-CHECKLIST.md](GUI-TEST-CHECKLIST.md) holds what automation can't judge: YubiKey prompts with real keys, listening with Narrator, high contrast, moving between monitors, pulling a USB drive or network cable during an operation, logging off during a backup, the visual review of the screenshots against 3.3 and the wireframes, and a short usability session (16.6). It records the status of the last run.

### 16.6 Usability session

Before the release, give one person who hasn't seen the new UI a test configuration and these tasks, without further help, and write down where they hesitate or go wrong:

1. "Are your folders protected? How do you know?"
2. "Back up now. What kind of backup will Documents get, and why?"
3. "Get your Documents folder back as it was last Sunday, into D:\Restore."
4. "One file you need is in Pictures. Get it back." (Expected: restore the folder to a new place, then copy the file. The note in the Restore window should get them there.)
5. "Something is wrong with your backups (condition BaseMissing). What is it and what should you do?"

A task that fails or needs help is a defect in the UI or its texts, not in the user.

### 16.7 Release gate

The redesign ships with 2.0.0 only when:

- `go test ./...` passes, including `internal/architecture` and `internal/e2e`;
- `Check-States.ps1` and `Smoke-BackupRestore.ps1` pass at 100% and 150%;
- every row of the checklist is Passed or explicitly accepted with a reason in the checklist;
- the usability session found no task that failed.

## 17. Acceptance criteria and build order

### 17.1 Acceptance criteria

1. All wireframes in this document are implemented with the described states, texts and behaviors.
2. The hero shows the correct state for every trigger in 3.5, and each Warning and Error variant offers its fix action.
3. A user can back up, see the planned type and reason per folder and what retention removes, force a full backup, create new keys, watch progress, cancel, and read the result and log without opening a file.
4. A user can restore one folder of a differential run to a new folder using only the Restore window; nothing is written before **Start**, and a checksum mismatch is reported as "Restore incomplete".
5. A user can verify a run or a set and see the result in the backups list.
6. Every behavior of the 2.0 workflows is unchanged; the e2e tests pass and change only for the request parameters, the plan calls, the progress fields and the facts.
7. All interactions work with keyboard only. Narrator announces the sidebar, hero, lists, buttons, progress and credential fields.
8. The UI stays responsive during every operation and while the backup directory is unreachable.
9. No user-visible string shows raw OS or workflow text outside "Show details" and the log.
10. The release gate of 16.7 is met.

### 17.2 Build order

The phases, the package layout and the order of the work are in [PLAN-gui-redesign.md](PLAN-gui-redesign.md). Tests are written in the phase of their code, not afterwards.

## 18. Decisions

"Decision N" elsewhere in this document refers to row N of 18.1.

### 18.1 Status-first redesign (decided 2026-09-30)

| # | Topic | Decision |
|---|---|---|
| 1 | Editing the configuration in the app | 2.0.0: read-only with **Edit config.yaml** and **Reload** instead of a restart. 2.1.0: editing in the app; it needs a YAML writer that keeps the comments of `config.yaml`, validation per field, and a Save / Discard flow. The Settings cards are laid out so their values can become controls. |
| 2 | Backup reminder | New configuration key `reminder_days` (default 7, 0 = off), added to `config-SAMPLE.yaml`, the config validation and the README's option table. The hero turns amber when the newest complete backup is older. It's only evaluated while RestoreSafe is open; there is no background check (1). |
| 3 | Verification history | Verify results are written as structured lines into the run's log (11.4), so "Verified <time>" and "Damaged" survive a restart and disappear with the run. |
| 4 | Dark mode | 2.0.0: light only (Win32 common controls have no supported dark mode). 2.1.0: dark mode (the tokens in 3.3 are ready); common controls need `DarkMode_Explorer` themes and custom drawing for buttons and group boxes. |
| 5 | Deleting backups in the app | Not offered. Retention removes chains safely after each backup; manual deletion stays in Explorer, and the health check reports what it breaks. |
| 6 | Single-file restore | Not offered (2.0 spec 13.5). It needs the manifest, which is encrypted, so browsing would require unlocking first. |
| 7 | Damaged backups | A damaged backup can't be restored beyond the first mismatch (2.0 spec 7.7). Recovery mode (2.0 spec 13.6) isn't part of this redesign. |
| 8 | Target release | 2.0.0. The redesign replaces the first GUI before the release; there is no release with the first GUI's home screen. |
| 9 | Settings missing from config.yaml (decided 2026-10-04) | Missing settings keep using their defaults, so a file from an older version still works; blocking until the file is complete would break every configuration with each new setting. Unknown keys are an error, because a misspelled key silently falls back to its default. The Settings page names the missing settings and adds them on request (ST-10), with a copy of the old file; RestoreSafe never writes config.yaml on its own. Defaults aren't written without asking because a written value can't be told from a chosen one: a default raised in a later version (for example `argon2.memory_mb`) would no longer reach that file. |

### 18.2 Still valid from the first GUI (decided 2026-09-26)

| # | Decision | Rationale |
|---|---|---|
| G1 | Native Win32 controls through `golang.org/x/sys/windows`, with a small in-house wrapper. No GUI framework, no WebView. | No new dependency, pure Go, instant start, low memory. Secrets can be read from controls into buffers that are zeroed (13); a WebView keeps them as immutable JavaScript strings in another process. |
| G2 | The GUI implements `interact.UI`; the workflows report their result through `ShowResult`. | The workflows keep their tests; the e2e tests drive them through `interacttest.Script`. |
| G4 | The worker blocks on each question; the bridge posts it to the UI thread and waits for the answer. | Matches the synchronous `interact.UI` contract; the UI thread never blocks on the worker. |
| G5 | Progress is coalesced: at most one pending progress message. | The window stays responsive regardless of how often progress arrives. |
| G6 | Cancelling cancels the workflow's context and waits for the worker. | The workflows already clean up on cancellation. |
| G7 | Per-monitor DPI awareness (v2) and common controls 6 through the application manifest. | Sharp text on every monitor and modern control visuals. |
| — | The GUI ships with 2.0.0; the console frontend is removed completely, with no separate console build. | A GUI-subsystem executable cannot use the console of the terminal it was started from reliably. |
| — | Unattended or scheduled backups, tray icons and background services are permanently out of scope. | Every operation needs the user to authenticate (2.0 spec 1.3). |

The first GUI's decisions G3 (one window with linear screens), G8 (log and reports as the main content) and "configuration read-only, restart to apply" are replaced by this document.
