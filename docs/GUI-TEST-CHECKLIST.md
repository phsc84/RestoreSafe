# GUI manual test checklist

The manual part of the GUI test plan ([SPEC-gui.md](SPEC-gui.md), sections 16.5 to 16.7). Run it on a real Windows 11 machine for every release candidate, after `go test ./...` and the scripts in [scripts/gui-test](../scripts/gui-test/README.md) have passed. Use test configurations and directories in `sandbox\`, never your real backups; `New-TestCondition.ps1` prepares the conditions named below.

Release gate (16.7): every row is **Passed**, or **Accepted** with a reason in the Notes column. No row may stay **Open**.

Status of the last run: **pre-run** on 2026-10-02 by Claude (Claude Code): `go test ./...` and the scripts of `scripts/gui-test` at 150 % on one monitor, plus scripted walks through the pages and dialogs. Rows those runs covered completely are **Passed (script)**; the notes of Open rows say what was pre-checked. Everything that needs a person, real YubiKeys, other scalings, Narrator or high contrast is Open for the manual run. The results of the first GUI (2026-09-27) no longer apply.

| Run | |
|---|---|
| Date, commit | Pre-run 2026-10-02, `gui-redesign` at fade221 and later (docs only) |
| Windows build, scaling, monitors | Windows 11 Pro 10.0.26200.9457, 150 %, one monitor |
| YubiKeys used | none (password-only test configurations) |
| Tester | pre-run: Claude; manual run: open |

## Known differences from the spec

Found while building; they are not defects of the run. Decided on 2026-10-06 (plan section 6g): **Accepted** for 2.0.0 with the reason, the spec changed to match; or **Changed** in the code.

| Spec | Difference | Decision |
|---|---|---|
| OV-1, RW-8 | "Show files" for skipped files is not there: the run's log names the files. Restore results showed stale files as "not in this backup", and only in the tests: a restore wrote no fact. | **Changed** in part: the backup records skipped and stale files apart, a restore records both per set, and the result page says "not in this backup" and "restored in an older version from <date>". **Accepted**: no "Show files"; the log names every file, and the result's Show log filters it. |
| OV-2 | When the check blocks a backup (e.g. a missing folder), the hero offers its fix actions (Check again, Edit config) instead of a disabled **Back up now…**; `Ctrl+B` does nothing then. | **Accepted**: OV-1 allows one primary and one secondary action; a disabled third button adds nothing the hero doesn't say. OV-2 changed. |
| BR-4 | During a backup, the Folders card showed "Done, <size>" with the size of the folder read, not of the set written. | **Changed**: it shows the size of the set written, as the Restore backup page does. |
| CR-1 | The unlock dialog does not name the key set by its date; when a selection spans older keys, the workflow's notice above the field says which backup and the keys' date. | **Accepted**: the question carries no key set; refactoring 2.0 RF-26 (typed questions) adds it. CR-1 changed. |
| RW-1 | The wizard is resizable; its progress page left empty space below the card. | **Changed**: the card fills the page and centres its content. |
| 15 | Turning high contrast on or off rebuilds the pages; a dialog open at that moment keeps its colors until it closes. | **Accepted**: rare, and it corrects itself when the dialog closes. Section 15 changed. |
| 16.4 | `Overdue` has no script variant: a backup's date is in its header, which is authenticated, so a script can't change it. | **Accepted**: tested with a backup from the day before (plan decision 9). |

## 1. Status and Overview

For each condition: prepare it, start RestoreSafe, compare the hero with figure 5.3 and the requirement, and use its fix action.

| ID | Check | Status | Notes |
|---|---|---|---|
| OV-1 | Protected: green hero, **Refresh** updates it | Open | Script: hero title and action ok (Check-States). Refresh: by hand. |
| OV-1 | Empty (no backups): neutral hero, "Create your first backup", **Back up now…** leads to key setup | Open | Script: hero ok; key setup from Back up now… ran in the smoke test. |
| OV-1 | `Overdue` (`reminder_days: 1`, newest backup older than a day): amber hero, **Back up now…** | Open | |
| OV-1 | `SkippedFiles` (a file held open, `on_unreadable_file: skip`): amber hero names the folder and count; the run's log (Restore backup page) names the files | Open | Script: hero names folder and count. |
| OV-1 | `BaseMissing` (FULL files of a chain moved away): red hero, **Show backups** marks the affected rows | Open | Script: red hero, Show backups. Marked rows on Restore backup: by hand. |
| OV-1 | `SourceMissing` (source folder renamed): red hero naming the folder, with **Check again** and **Edit config** in place of **Back up now…** (OV-2); restore and verify still possible | Open | Script: red hero with Check again and Edit config. |
| OV-1 | `BackupDirUnreachable` (USB drive removed, or NAS unreachable): red hero within 5 seconds, the window stays responsive, **Check again** recovers after reconnecting | Open | Script: missing drive letter, red hero. Pulling a real USB drive or NAS: by hand. |
| OV-1 | Several problems at once: the most urgent one shows, the sub line says "and N more" | Open | |
| OV-3 | Folders card: the splitter below it makes the table taller or shorter (three rows at least), the cards below keep their height, and the page scrolls when they no longer fit | Open | |
| OV-3 | Folders card: next type and its reason in the tooltip match the backup plan that follows | Open | Tooltips added in 10a. |
| 3.4 | Tables (Create backup, backup plan, Settings, restore wizard page 3): a column dragged wider stays wider during a backup's progress updates and after "Full backup instead"; resizing the window refits the filling column; the row tooltip shows the path; more than five folders scroll within the table | Open | |
| 3.4 | One-line text cut off with "…" (a long backup directory path in a narrow window) shows its full text in a tooltip; text that fits has none | Open | |
| OV-4 | Backup directory bar: segments and tooltips plausible compared with Explorer's drive properties | Open | |
| OV-6 | Keys card for each authentication mode; YubiKey connected and not connected; "next backup creates new keys" after changing `recovery_code` and Reload | Open | |
| OV-8 | Switching to another window for more than 5 minutes and back runs the check again | Open | |

## 2. Backup

| ID | Check | Status | Notes |
|---|---|---|---|
| BP-1 | Plan shows type, number and reason per folder; a folder with a problem shows it instead of a type | Open | Pre-checked: differential and full rows, a missing folder. |
| BP-2 | Space line: fits (ok), only the estimate fits (warning), doesn't fit (error, no **Start**) on a small USB stick | Open | |
| BP-2 | Afterwards line names the chain retention removes; after the run exactly those files are gone from the backup directory (compare in Explorer) | Open | |
| BP-4 | **Full backup instead**: plan switches to full for every folder and back with **Back to plan** | Passed (script) | Full backup instead and Back to plan, 2026-10-02. |
| BP-4 | **New keys + full backup…**: confirmation (figure 6.2), then key setup, then full backups | Open | |
| CR-2 | First backup, mode 1 with recovery code: password twice, code shown on one line, can't be selected; **Copy** puts the whole code on the clipboard (one line with dashes), the button then reads Copied, and the code doesn't appear in the clipboard history (Win+V); **I have stored it** continues without a retype step; the title has no "Step n of N"; closing it with × or Esc ends the backup as cancelled, with nothing written | Open | Script: password twice, code read from its static line. Copy and clipboard history: by hand. |
| CR-2 | Mode 2 with spare YubiKey: two prompts per key, the swap step, inserting the first key again is refused | Open | Needs YubiKeys. |
| CR-2 | Mode 3 (YubiKey only): no password dialog, "Follow the Windows Security prompt" shows | Open | Needs a YubiKey. |
| CR-1 | Wrong password: "Wrong password. 2 attempts left." under the field; the right one continues | Passed (script) | "Wrong password. 2 attempts left." under the field, then the right one, 2026-10-02. |
| BR-1 | Progress card: step trail, folder n of N, bytes, speed appear as specified, no time left; Folders card follows; a done folder shows the size of its set (for a differential the same as on the Restore backup page) | Open | Pre-checked with 800 MB: trail, folder, bytes, speed, Folders card states. |
| BR-5 | Taskbar button shows progress, indeterminate while unlocking, amber after warnings, red after a failure | Open | |
| BR-7 | Result cards: finished, finished with warnings, failed (backup directory full), cancelled | Open | Pre-checked: finished, cancelled. Warnings and a full backup directory: by hand. |
| BR-8 | Operation ends while another window is in front: taskbar button flashes until activated | Open | |

## 3. Backups page

| ID | Check | Status | Notes |
|---|---|---|---|
| BK-1 | Runs newest first, the newest expanded; group header with size, duration, warnings, "new keys" | Open | Pre-checked: order, newest expanded, header with size, duration, "new keys", failed and cancelled runs. |
| BK-2 | Types, "based on", chain IDs and sizes match the files in Explorer | Open | |
| BK-3 | Folder filter, including a folder removed from the configuration ("Old: …") | Open | |
| BK-5 | Log pane: log of the selected run, filter ("No warnings or errors in this log." when there are none), **Open in Editor**; live while an operation runs; the pane shows at least 15 lines, and dragging the splitter far down or making the window small gives the page a scroll bar | Open | Pre-checked: selected run, live during verify and restore. |
| BK-6 | Problem and information lines for `BaseMissing`, `IncompleteNewest`, `Legacy1x`, `LeftoverTmp` | Open | |
| BK-8 | Verify a run and a single set; "Verified <time>" survives a restart | Open | Script: verify of a set from the Restore backup page; its progress and result card at the top of that page. |
| BK-8 | `Damaged` (one byte changed in a part file): verify reports it, the set shows "Damaged", the hero turns red | Open | |
| BK-9 | Empty state with **Back up now…** | Open | |

## 4. Restore

| ID | Check | Status | Notes |
|---|---|---|---|
| RW-3 | Wizard from a run, also opened by clicking one of its folders: the run is preselected and page 2 checks all restorable folders | Open | |
| RW-4 | Differential folders show the full backup read with them; the single-file note is visible | Open | |
| RW-5 | Existing target folder blocks Next; renaming it in Explorer and returning unblocks | Open | Pre-checked: an existing folder blocks Next. |
| RW-5 | "Restore into the backup directory" fills the path | Open | |
| RW-6 | Nothing exists in the destination before **Restore…** is pressed | Open | |
| RW-1 | Progress and result pages: the card fills the wizard, its content centred, also after resizing the wizard during a restore | Open | |
| RW-8 | Full and differential restore; restored files compared byte for byte with the sources, including timestamps and attributes | Open | Script: restored files equal to the source by hash. Timestamps and attributes: by hand. |
| RW-8 | Restore of a backup with skipped files: amber lines "not in this backup" and "older version" | Open | Needs a differential made while a file was held open. |
| RW-8 | `Damaged`: "Restore incomplete" in red, naming the folder | Open | |
| CR-1 | Restore unlocked with the recovery code only | Open | Pre-checked for a verify through "Use your recovery code instead". |
| CR-1 | Restore with the spare YubiKey only | Open | Needs YubiKeys. |

## 5. Cancel and close

| ID | Check | Status | Notes |
|---|---|---|---|
| BR-6 | Cancel during a backup (1 GB or more): confirmation, "Cancelling…", no `.tmp` parts left, completed folders kept, no retention ran | Open | Pre-checked with 800 MB: confirmation, cancelled card, no .tmp parts left. |
| BR-6 | Cancel during restore and during verify | Open | Pre-checked: restore. |
| BP-6 | Cancel in the plan, in the unlock dialog, in each key-setup step: nothing written | Open | Pre-checked: plan (Esc), unlock dialog. |
| 6.4 | Close the window during a question and during a running backup | Open | Pre-checked: during planning, during a running backup. |
| 12.4 | Log off during a backup: Windows shows "RestoreSafe is stopping a backup" and waits | Open | |
| 15 | Backup directory disconnected during a backup: failed result card, red hero, the next backup removes the leftovers | Open | |

## 6. Settings

| ID | Check | Status | Notes |
|---|---|---|---|
| ST-1 | **Edit config.yaml** opens the loaded file (also with `-config`) | Open | |
| ST-2 | Reload after a valid change: all pages update | Passed (script) | retention_keep and verify_after_backup changed and shown, 2026-10-02. |
| ST-2 | Reload after an invalid change: error on the card, previous configuration still active | Passed (script) | YAML error with its line on the card; values unchanged, 2026-10-02. |
| ST-2 | Reload disabled while an operation runs | Open | |
| ST-3 to ST-9 | Every value matches `config.yaml`; tooltips name the keys | Open | |
| ST-3 | Folders card: the splitter below it makes the table taller or shorter (three rows at least), the cards below keep their height, and the page scrolls | Open | |
| 14 | Broken `config.yaml` at start: message box, then RestoreSafe ends | Open | |

## 7. Display

| ID | Check | Status | Notes |
|---|---|---|---|
| 3.3 | A screenshot of every page and dialog (`Screenshot.ps1`) reviewed against GUI spec 3.3 (colours, spacing, fonts) and the wireframes | Open | |
| 15 | 100%, 125%, 150% and 200%: layout, fonts, icons, badges on every page and dialog | Open | 150 % pre-checked; a simulated DPI change of the plan and password dialogs to 100 % pre-checked. |
| 15 | Moving the window between monitors with different scaling | Open | |
| 3.2 | Minimum window size: nothing overlaps, all buttons visible | Open | |
| 15 | High contrast (Aquatic and Desert): every status still readable, icons visible, focus visible | Open | New in 10a: system colors in high contrast. Not run (it changes the system theme). |

## 8. Keyboard and screen reader

| ID | Check | Status | Notes |
|---|---|---|---|
| 3.2 | Keyboard only: `Ctrl+1` to `Ctrl+3`, `Ctrl+B`, `F5`, Tab order on every page, `Enter` and `Esc` in every dialog, a full backup and restore without the mouse | Open | |
| 15 | Access keys are unique per page and dialog; hidden pages don't react to them (`Accessibility.ps1`) | Passed (script) | Accessibility.ps1: pages, plan dialog, wizard pages, 2026-10-02. |
| 15 | Narrator: sidebar, hero state, card contents, list rows with status, step trail, progress, credential fields are announced in a sensible order | Open | MSAA pre-check: the Backups list exposes a named list with its rows. |

## 9. Usability session (16.6)

| Task | Result | Notes |
|---|---|---|
| 1. Are your folders protected? | Open | |
| 2. Back up; what type does Documents get, and why? | Open | |
| 3. Documents as of last Sunday into D:\Restore | Open | |
| 4. One file from Pictures | Open | |
| 5. What's wrong (`BaseMissing`), what to do? | Open | |
