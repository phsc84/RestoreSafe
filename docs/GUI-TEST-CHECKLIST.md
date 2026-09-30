# GUI manual test checklist

The manual part of the GUI test plan ([SPEC-restoresafe-gui.md](SPEC-restoresafe-gui.md), sections 16.5 to 16.7). Run it on a real Windows 11 machine for every release candidate, after `go test ./...` and the scripts in [scripts/gui-test](../scripts/gui-test/README.md) have passed. Use test configurations and directories in `sandbox\`, never your real backups; `New-TestCondition.ps1` prepares the conditions named below.

Release gate (16.7): every row is **Passed**, or **Accepted** with a reason in the Notes column. No row may stay **Open**.

Status of the last run: not run yet. The status-first UI is specified, not implemented. The results of the first GUI (2026-09-27) no longer apply.

| Run | |
|---|---|
| Date, commit | |
| Windows build, scaling, monitors | |
| YubiKeys used | |
| Tester | |

## 1. Status and Overview

For each condition: prepare it, start RestoreSafe, compare the hero with figure 5.3 and the requirement, and use its fix action.

| ID | Check | Status | Notes |
|---|---|---|---|
| OV-1 | Protected: green hero, "Check details" opens the full health report, **Check again** updates it | Open | |
| OV-1 | Empty (no backups): neutral hero, "Create your first backup", **Back up now…** leads to key setup | Open | |
| OV-1 | `Overdue` (`reminder_days: 1`, newest backup older than a day): amber hero, **Back up now…** | Open | |
| OV-1 | `SkippedFiles` (a file held open, `on_unreadable_file: skip`): amber hero names the folder and count, "Show files" lists them | Open | |
| OV-1 | `BaseMissing` (FULL files of a chain moved away): red hero, **Show in Backups** marks the affected rows | Open | |
| OV-1 | `SourceMissing` (source folder renamed): red hero, **Back up now…** disabled with the reason; restore and verify still possible | Open | |
| OV-1 | `BackupDirUnreachable` (USB drive removed, or NAS unreachable): red hero within 5 seconds, the window stays responsive, **Check again** recovers after reconnecting | Open | |
| OV-1 | Several problems at once: the most urgent one shows, the sub line says "and N more", Check details lists all | Open | |
| OV-3 | Folders card: next type and its reason in the tooltip match the backup plan that follows | Open | |
| OV-4 | Backup directory bar: segments and tooltips plausible compared with Explorer's drive properties | Open | |
| OV-6 | Keys card for each authentication mode; YubiKey connected and not connected; "next backup creates new keys" after changing `recovery_code` and Reload | Open | |
| OV-8 | Switching to another window for more than 5 minutes and back runs the check again | Open | |

## 2. Backup

| ID | Check | Status | Notes |
|---|---|---|---|
| BP-1 | Plan shows type, number and reason per folder; a folder with a problem shows it instead of a type | Open | |
| BP-2 | Space line: fits (ok), only the estimate fits (warning), doesn't fit (error, no **Start**) on a small USB stick | Open | |
| BP-2 | Afterwards line names the chain retention removes; after the run exactly those files are gone from the backup directory (compare in Explorer) | Open | |
| BP-4 | **Full backup instead**: plan switches to full for every folder and back with **Back to plan** | Open | |
| BP-4 | **New keys + full backup…**: confirmation (figure 6.2), then key setup, then full backups | Open | |
| CR-2 | First backup, mode 1 with recovery code: password twice, code shown on two lines, can't be selected or copied (clipboard checked), wrong retype shows the reason, right one continues | Open | |
| CR-2 | Mode 2 with spare YubiKey: two prompts per key, the swap step, inserting the first key again is refused | Open | |
| CR-2 | Mode 3 (YubiKey only): no password dialog, "Follow the Windows Security prompt" shows | Open | |
| CR-1 | Wrong password: "Wrong password. 2 attempts left." under the field; the right one continues | Open | |
| BR-1 | Progress card: step trail, folder n of N, bytes, speed, time left appear as specified; Folders card follows | Open | |
| BR-5 | Taskbar button shows progress, indeterminate while unlocking, amber after warnings, red after a failure | Open | |
| BR-7 | Result cards: finished, finished with warnings, failed (backup directory full), cancelled | Open | |
| BR-8 | Operation ends while another window is in front: taskbar button flashes until activated | Open | |

## 3. Backups page

| ID | Check | Status | Notes |
|---|---|---|---|
| BK-1 | Runs newest first, the newest expanded; group header with size, duration, warnings, "new keys" | Open | |
| BK-2 | Types, "based on", chain IDs and sizes match the files in Explorer | Open | |
| BK-3 | Folder filter, including a folder removed from the configuration ("Old: …") | Open | |
| BK-5 | Log pane: log of the selected run, filter, **Open**; live while an operation runs | Open | |
| BK-6 | Problem and information lines for `BaseMissing`, `IncompleteNewest`, `Legacy1x`, `LeftoverTmp` | Open | |
| BK-8 | Verify a run and a single set; "Verified <time>" survives a restart | Open | |
| BK-8 | `Damaged` (one byte changed in a part file): verify reports it, the set shows "Damaged", the hero turns red | Open | |
| BK-9 | Empty state with **Back up now…** | Open | |

## 4. Restore

| ID | Check | Status | Notes |
|---|---|---|---|
| RW-3 | Wizard from a run and from a single set: preselection as specified | Open | |
| RW-4 | Differential folders show the full backup read with them; the single-file note is visible | Open | |
| RW-5 | Existing target folder blocks Next; renaming it in Explorer and returning unblocks | Open | |
| RW-5 | "Restore into the backup directory" fills the path | Open | |
| RW-6 | Nothing exists in the destination before **Restore…** is pressed | Open | |
| RW-8 | Full and differential restore; restored files compared byte for byte with the sources, including timestamps and attributes | Open | |
| RW-8 | Restore of a backup with skipped files: amber lines "not in this backup" and "older version" | Open | |
| RW-8 | `Damaged`: "Restore incomplete" in red, naming the folder | Open | |
| CR-1 | Restore unlocked with the recovery code only | Open | |
| CR-1 | Restore with the spare YubiKey only | Open | |

## 5. Cancel and close

| ID | Check | Status | Notes |
|---|---|---|---|
| BR-6 | Cancel during a backup (1 GB or more): confirmation, "Cancelling…", no `.tmp` parts left, completed folders kept, no retention ran | Open | |
| BR-6 | Cancel during restore and during verify | Open | |
| BP-6 | Cancel in the plan, in the unlock dialog, in each key-setup step: nothing written | Open | |
| 6.4 | Close the window during a question and during a running backup | Open | |
| 12.4 | Log off during a backup: Windows shows "RestoreSafe is stopping a backup" and waits | Open | |
| 15 | Backup directory disconnected during a backup: failed result card, red hero, the next backup removes the leftovers | Open | |

## 6. Settings

| ID | Check | Status | Notes |
|---|---|---|---|
| ST-1 | **Edit config.yaml** opens the loaded file (also with `-config`) | Open | |
| ST-2 | Reload after a valid change: all pages update | Open | |
| ST-2 | Reload after an invalid change: error on the card, previous configuration still active | Open | |
| ST-2 | Reload disabled while an operation runs | Open | |
| ST-3 to ST-9 | Every value matches `config.yaml`; tooltips name the keys | Open | |
| 14 | Broken `config.yaml` at start: message box, then RestoreSafe ends | Open | |

## 7. Display

| ID | Check | Status | Notes |
|---|---|---|---|
| 3.3 | Every figure of the mockups compared side by side with its screenshot (`Screenshot.ps1`) | Open | |
| 15 | 100%, 125%, 150% and 200%: layout, fonts, icons, badges on every page and dialog | Open | |
| 15 | Moving the window between monitors with different scaling | Open | |
| 3.2 | Minimum window size: nothing overlaps, all buttons visible | Open | |
| 15 | High contrast (Aquatic and Desert): every status still readable, icons visible, focus visible | Open | |

## 8. Keyboard and screen reader

| ID | Check | Status | Notes |
|---|---|---|---|
| 3.2 | Keyboard only: `Ctrl+1` to `Ctrl+3`, `Ctrl+B`, `F5`, Tab order on every page, `Enter` and `Esc` in every dialog, a full backup and restore without the mouse | Open | |
| 15 | Access keys are unique per page and dialog; hidden pages don't react to them (`Accessibility.ps1`) | Open | |
| 15 | Narrator: sidebar, hero state, card contents, list rows with status, step trail, progress, credential fields are announced in a sensible order | Open | |

## 9. Usability session (16.6)

| Task | Result | Notes |
|---|---|---|
| 1. Are your folders protected? | Open | |
| 2. Back up; what type does Documents get, and why? | Open | |
| 3. Documents as of last Sunday into D:\Restore | Open | |
| 4. One file from Pictures | Open | |
| 5. What's wrong (`BaseMissing`), what to do? | Open | |
