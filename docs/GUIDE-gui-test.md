# GUI manual test checklist

What to test by hand in the window: the manual part of the GUI test plan ([SPEC-gui.md](SPEC-gui.md), sections 16.5 and 16.6). Pick the sections that cover what changed since the last release; the full list fits a major release or a redesign of the window.

Test on a real Windows 11 machine, after `go test ./...` and the scripts in [scripts/gui-test](../scripts/gui-test/README.md) have passed. Use test configurations and directories in `sandbox\`, never your real backups; `New-TestCondition.ps1` prepares the conditions named below.

CI runs `Smoke-BackupRestore.ps1` and `Check-States.ps1` on every pull request (job **gui: smoke test**, [scripts/ci/gui-smoke.ps1](../scripts/ci/gui-smoke.ps1)), at 100 % on the runner's 1024 × 768 desktop. A run at 150 % is manual.

## 1. Status and Create backup page

For each condition: prepare it, start RestoreSafe, compare the hero with figure 5.3 and the requirement, and use its fix action.

| ID | Check | Hints |
|---|---|---|
| OV-1 | Protected: green hero, **Refresh** updates it | `Check-States.ps1` checks the hero; Refresh by hand. |
| OV-1 | Empty (no backups): neutral hero, "Create your first backup", **Back up now…** leads to key setup | `Check-States.ps1` checks the hero; the smoke test runs the key setup. |
| OV-1 | `Overdue` (`reminder_days: 1`, newest backup older than a day): amber hero, **Back up now…** | Needs a backup from the day before. |
| OV-1 | `SkippedFiles` (a file held open, `on_unreadable_file: skip`): amber hero names the folder and count; the run's log (Restore backup page) names the files | `Check-States.ps1` checks the hero. |
| OV-1 | `BaseMissing` (FULL files of a chain moved away): neutral hero "Ready to back up" with **Back up now…** on Create backup; Restore backup names the problem and marks the affected rows | `Check-States.ps1` checks the hero; the Restore backup page by hand. |
| OV-1 | `SourceMissing` (source folder renamed): red hero naming the folder, with **Check again** and **Edit config** in place of **Back up now…** (OV-2); restore and verify still possible | `Check-States.ps1` checks the hero. |
| OV-1 | `BackupDirUnreachable` (USB drive removed, or NAS unreachable): red hero within 5 seconds, the window stays responsive, **Check again** recovers after reconnecting | `Check-States.ps1` uses a missing drive letter; pull a real USB drive or NAS by hand. |
| OV-1 | Several problems at once: the most urgent one shows, the sub line says "and N more" | |
| OV-3 | Folders card: the splitter below it makes the table taller or shorter (three rows at least), the cards below keep their height, and the page scrolls when they no longer fit | |
| OV-3 | Folders card: next type and its reason in the tooltip match the Create backup window that follows | |
| 3.4 | Tables (Create backup page, Create backup window, Settings, Restore backup window): a column dragged wider stays wider during a backup's progress updates and after "Full backup instead"; resizing the window refits the filling column; the row tooltip shows the path; more than five folders scroll within the table | |
| 3.4 | One-line text cut off with "…" (a long backup directory path in a narrow window) shows its full text in a tooltip; text that fits has none | |
| OV-4 | Backup directory bar: segments and tooltips plausible compared with Explorer's drive properties | |
| OV-6 | Keys card for each authentication mode; YubiKey connected and not connected; "next backup creates new keys" after changing `recovery_code` and Reload | Needs a YubiKey. |
| OV-8 | Switching to another window for more than 5 minutes and back runs the check again | |

## 2. Backup

| ID | Check | Hints |
|---|---|---|
| BP-1 | Plan shows type, number and reason per folder; a folder with a problem shows it instead of a type | |
| BP-1 | The splitter below the table makes it taller and shorter, not below its rows (at most three); the dialog grows and shrinks with it, up to the height of the screen | |
| BP-2 | Space line: fits (ok), only the estimate fits (warning), doesn't fit (error, no **Start**) on a small USB stick | |
| BP-2 | Afterwards line names the chain retention removes; after the run exactly those files are gone from the backup directory (compare in Explorer) | |
| BP-4 | **Full backup instead**: plan switches to full for every folder and back with **Back to plan** | Scripted walk possible. |
| BP-4 | **New keys + full backup…**: confirmation (figure 6.2), then key setup, then full backups | |
| CR-2 | First backup, mode 1 with recovery code: password twice, code shown on one line, can't be selected; **Copy** puts the whole code on the clipboard (one line with dashes), the button then reads Copied, and the code doesn't appear in the clipboard history (Win+V); **Continue** stays disabled (and Enter does nothing) until **I have stored my recovery code** is ticked, then continues without a retype step; the title has no "Step n of N"; closing it with × or Esc ends the backup as cancelled, with nothing written | The smoke test enters the password and reads the code; Copy and the clipboard history by hand. |
| CR-2 | Mode 2 with `recovery_code: false`: password twice, two Windows prompts to register the YubiKey (a YubiKey without a PIN is asked to create one), then the recovery code shows anyway | Needs a YubiKey. |
| CR-2 | Mode 3 (YubiKey only): no password dialog, "Follow the Windows Security prompt" shows | Needs a YubiKey. |
| CR-1 | Wrong password: "Wrong password. 2 attempts left." under the field; the right one continues | Scripted walk possible. |
| BR-1 | Progress card: step trail, folder n of N, bytes, speed appear as specified, no time left; Folders card follows; a done folder shows the size of its set (for a differential the same as on the Restore backup page) | Use about 1 GB of data to see it change. |
| BR-5 | Taskbar button shows progress, indeterminate while unlocking, amber after warnings, red after a failure | |
| BR-7 | Result cards: "N folders backed up", with warnings, failed (backup directory full), cancelled; "N folders restored"; with an error about other backups on Restore backup, a green card adds "Another backup has a problem…" | A small USB stick fills the backup directory. |
| BR-8 | Operation ends while another window is in front: taskbar button flashes until activated | |

## 3. Restore backup page

| ID | Check | Hints |
|---|---|---|
| BK-1 | Runs newest first, the newest expanded; group header with size, duration, warnings, "new keys" | |
| BK-2 | Types, "based on", chain IDs and sizes match the files in Explorer | |
| BK-3 | Folder filter, including a folder removed from the configuration ("Old: …") | |
| BK-5 | "Show log" at the right of each run's header, and in the context menu, opens the run's log window: title with date and file, filter ("No warnings or errors in this log." when there are none), **Open in Editor**, **Close**; no link on a group of incomplete sets; no log pane below the list, and a small window gives the page a scroll bar | |
| BK-6 | Problem and information lines for `BaseMissing`, `IncompleteNewest`, `Legacy1x`, `LeftoverTmp` | `New-TestCondition.ps1` |
| BK-7 | No line about the retention rule; with `retention_keep: 1` and two chains, the line names what the next backup removes | |
| BK-7a | Verify window and Create backup window side by side: same width, same order (heading, table, Read, Unlock, note, issues, Show details), **Start** and **Cancel** at the bottom right; Cancel reads nothing; Start goes on to the password and the progress card | `Smoke-BackupRestore.ps1 -ScreenshotDir` |
| BK-7a | The splitter below the table makes it taller and shorter, not below its rows (at most three); the window grows and shrinks with it, up to the height of the screen | |
| BK-8 | Verify a run and a single set; "Verified <time>" survives a restart | The smoke test verifies a set. |
| BK-8 | `Damaged` (one byte changed in a part file): "Damage found in the backup of <date>", the set shows "Damaged", the hero turns red | `New-TestCondition.ps1` |
| BK-8 | A verified differential run: "The backup of <date> can be restored", "Checked N folders …, including the full backups they're based on."; a run with a folder whose full backup is missing (`BaseMissing`): "<folders> from the backup of <date> can be restored" and "Another backup has a problem; see below." | |
| BK-9 | All backups deleted in Explorer, **Refresh**: the empty list, no "No backups yet" page; the same after a cancelled backup | |
| OV-8 | **Refresh** next to the title on Create backup and Restore backup, vertically centered on it, in the same place on both; it checks again (disabled meanwhile and during an operation) and the list shows a set deleted in Explorer as gone, also the last one | |

## 4. Restore

| ID | Check | Hints |
|---|---|---|
| RW-1 | Restore backup window and Create backup window side by side: same width, same order (heading, table, Space, Unlock, note, issues, Show details), **Start** and **Cancel** at the bottom right; `Enter` starts, `Esc` and Cancel close without writing anything; the window fits its content until it is resized | `Smoke-BackupRestore.ps1 -ScreenshotDir` |
| RW-3 | Restore… on a run, also with one of its folders clicked: the window opens on that run, its heading names the run's date and the number of checked folders, all restorable folders are checked | |
| RW-4 | "Restore into the backup directory" fills the path with backslashes, like Browse…, and the restore into it works | |
| RW-5 | Folder, Type, About and Check columns; a folder whose full backup is missing (`BaseMissing`) is disabled and named in the warning line; an unchecked folder shows "-" | |
| RW-5 | The splitter below the table makes it taller and shorter, not below its rows (at most three) | |
| RW-6 | Space and Unlock worded as in the Create backup window; the hints ("Choose at least one folder.", "Enter a full path…", "Checking…") replace them while there is nothing to check | |
| RW-6a | Existing target folders show "Already exists" in red, are named once below with the remedy and disable Start; renaming them in Explorer and changing the path unblocks | |
| RW-6b | Start closes the window and goes straight to the password, as Verify does; cancelling it ends the restore without a result card; nothing exists in the destination before the password is entered | |
| RW-6b | A folder created in the destination after the check: Start ends before the password and the window shows it in red | |
| RW-9 | Progress and result show only on the card at the top of Restore backup, as for Verify; no window stays in front during the restore | |
| RW-8 | Full and differential restore; restored files compared byte for byte with the sources, including timestamps and attributes | The smoke test compares hashes; timestamps and attributes by hand. |
| RW-8 | Restore of a backup with skipped files: amber lines "not in this backup" and "older version" | Needs a differential made while a file was held open. |
| RW-8 | `Damaged`: "Restore incomplete" in red, naming the folder | |
| CR-1 | Restore unlocked with the recovery code only | |

## 5. Cancel and close

| ID | Check | Hints |
|---|---|---|
| BR-6 | Cancel during a backup (1 GB or more): confirmation, "Cancelling…", no `.tmp` parts left, completed folders kept, no retention ran | |
| BR-6 | Cancel during restore and during verify | |
| BP-6 | Cancel in the plan, in the unlock dialog, in each key-setup step: nothing written | |
| 6.4 | Close the window during a question and during a running backup | |
| 12.4 | Log off during a backup: Windows shows "RestoreSafe is stopping a backup" and waits | |
| 15 | Backup directory disconnected during a backup: failed result card, red hero, the next backup removes the leftovers | |

## 6. Settings

| ID | Check | Hints |
|---|---|---|
| ST-1 | **Edit config.yaml** opens the loaded file (also with `-config`) | |
| ST-2 | Reload after a valid change: all pages update | Scripted walk possible. |
| ST-2 | Reload after an invalid change: error on the card, previous configuration still active | Scripted walk possible. |
| ST-2 | Reload disabled while an operation runs | |
| ST-3 to ST-9 | Every value matches `config.yaml`; tooltips name the keys | |
| ST-3 | Folders card: the splitter below it makes the table taller or shorter (three rows at least), the cards below keep their height, and the page scrolls | |
| 14 | Broken `config.yaml` at start: message box, then RestoreSafe ends | |

## 7. YubiKey page

Use a test YubiKey: some checks block its PIN or reset it, and a reset makes it forget every registration. One YubiKey covers most rows; the hints name what needs more.

| ID | Check | Hints |
|---|---|---|
| YK-1 | **YubiKey** sits at the bottom of the sidebar, below a line, in modes 2 and 3, and in mode 1 while older backups need a YubiKey; it is hidden in mode 1 without such backups. Changing the mode and **Reload** while the page is shown switches to Settings | |
| YK-1 | `Ctrl+4` opens the page while the item is shown, and does nothing while it is hidden; the arrow keys skip the hidden item | |
| YK-3 | Plug the YubiKey in and pull it out with the page shown: within about half a second the hero and the Checks card follow, and "YubiKey connected" on the Keys card of Create backup too. **Check again** and `F5` check again | |
| YK-4 | Checks card: Windows, YubiKey, FIDO2, Firmware and Your keys in the words of the summary in Technical details; the notes under Windows and YubiKey; icon, label and text on one line | |
| YK-4 | Two YubiKeys connected: YubiKey in red ("2 connected…"), the hero on Create backup "More than one YubiKey is connected", **Back up now…** disabled; restore and verify still possible. Pulling one out ends it | Needs two YubiKeys (decision 12). |
| YK-5 | **Test YubiKey…** with YubiKey keys: the PIN and one touch, then "Your YubiKey opens your backups" with the time, and Your keys "…: your YubiKey answered" | |
| YK-5 | **Test YubiKey…** without YubiKey keys: the PIN twice and two touches, then "Your YubiKey is ready"; nothing stays on the YubiKey | A test backup directory without backups. |
| YK-6 | While the test runs: "Follow the Windows Security prompt", **Test YubiKey…** and **Check again** disabled, no backup or restore can start; pulling the YubiKey out after a test clears the result | |
| YK-7 | **Cancel** in the Windows dialog: "The test was cancelled" with both hints | |
| YK-7 | A YubiKey that isn't registered for the keys (reset, or another one): Windows says it doesn't look familiar; after **Abbrechen**/**Cancel**, the cancel hero | YK-11 |
| YK-11 | A reset YubiKey without a PIN: Windows asks to create one during the test or the first backup | Reset in Windows Settings first. |
| YK-11 | Wrong PINs: Windows' own messages (3 wrong: unplug and plug in; then a typed phrase; a warning before the last try; blocked); the page shows the cancel hero | Blocks the YubiKey's PIN: reset it afterwards. |
| YK-8 | YubiKey help is always shown, as short points; **Open Windows settings…** opens Sign-in options | |
| YK-8 | **Open Yubico Authenticator…**: installed, Windows asks for administrator permission and the app starts; refusing the permission opens nothing; not installed, the download page opens in the browser | Rename the install folder to test the download page. |
| YK-4 | FIDO2 turned off in Yubico Authenticator: FIDO2 "Off or missing…" in red, the hero "This YubiKey can't protect your backups", no **Test YubiKey…**; turned on again, everything back | Optional. |
| YK-4 | A YubiKey with firmware below 5.2, or a FIDO-only Security Key (no `MI_xx` in its path) | Needs such a key. |
| YK-9 | **Technical details** opens and closes with a click, `Enter` and `Space`, the chevron turns; **Copy details** shows only while open; the page doesn't get wider when the scroll bar appears; long lines scroll sideways | |
| YK-9 | **Copy details** pasted into Notepad: the sections of figure 10.5, values in one column, no PIN, secret, salt or credential ID; after a test, LAST TEST with the error code in words and the trace | |
| YK-9 | Narrator announces Technical details as collapsed or expanded | |

## 8. Display

| ID | Check | Hints |
|---|---|---|
| 3.3 | A screenshot of every page and dialog (`Smoke-BackupRestore.ps1 -ScreenshotDir`, `Check-States.ps1`; the Settings page by hand) reviewed against GUI spec 3.3 (colours, spacing, fonts) and the wireframes | |
| 15 | 100%, 125%, 150% and 200%: layout, fonts, icons, badges on every page and dialog | |
| 15 | Moving the window between monitors with different scaling | Without a second monitor: change the scaling in Windows settings while RestoreSafe runs, with the main window and a dialog open; Windows sends the same `WM_DPICHANGED`. |
| 3.2 | Minimum window size: nothing overlaps, all buttons visible | |
| 3.2 | Sidebar: the row under the mouse is filled, without the accent bar; the fill goes when the mouse leaves | |
| 3.4 | Scrolling slowly (mouse wheel, scroll bar arrows, dragging the thumb) on every page that scrolls: no controls painted over each other | |
| 15 | High contrast (Aquatic and Desert): every status still readable, icons visible, focus visible | |

## 9. Keyboard and screen reader

| ID | Check | Hints |
|---|---|---|
| 3.2 | Keyboard only: `Ctrl+1` to `Ctrl+3`, `Ctrl+4` while the YubiKey page is in the sidebar, `Ctrl+B`, `F5`, Tab order on every page, `Enter` and `Esc` in every dialog, a full backup and restore without the mouse | |
| 15 | Access keys are unique per page and dialog; hidden pages don't react to them | `Accessibility.ps1` |
| 15 | Narrator: sidebar, hero state, card contents, list rows with status, step trail, progress, credential fields are announced in a sensible order | |

## 10. Usability session (16.6)

For a major change of the window: give one person who hasn't seen it a test configuration and the tasks of GUI spec 16.6, without further help, and write down where they hesitate or go wrong.

1. Are your folders protected?
2. Back up; what type does Documents get, and why?
3. Documents as of last Sunday into D:\Restore.
4. One file from Pictures.
5. What's wrong (`BaseMissing`), what to do?
