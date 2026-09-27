# GUI manual test checklist

The manual part of the GUI test plan ([SPEC-restoresafe-gui.md](SPEC-restoresafe-gui.md), section 14.2). Run it before each release on a real Windows machine; the automated tests cover the logic, not the window.

Status of the last run: 2026-09-27, branch `v2` (GUI phase G6), Windows 11, one monitor at 150 %. The window was driven by a UI-automation script (clicks, keystrokes, screenshots) and checked on the screenshots.

## Backup, restore, verify

| Check | Status |
|---|---|
| Backup with new keys, password only | Passed |
| Backup with new keys and recovery code: code shown on two lines, cannot be copied, wrong confirmation shows the reason, right one completes | Passed (clipboard checked: code never in it) |
| Backup with new keys, YubiKey (modes 2 and 3), with spare YubiKey | **Open** (needs YubiKeys) |
| Differential backup with existing keys | Passed |
| Full backup override ("Full backup") and new-keys override ("New keys + full backup", with confirmation) | **Open** |
| Restore of a full backup; restored files compared byte for byte | Passed |
| Restore of a differential chosen in the tree; the preflight lists its full backup; restored content is the changed one | Passed |
| Restore unlocked with the recovery code only | Passed |
| Verify of a whole run | Passed |
| Wrong password: dialog shows "Wrong password. N attempt(s) remaining." in red; the right one continues | Passed |
| Restore into the backup directory itself (checkbox) | **Open** |
| Folder picker: cancel keeps the field; choosing a folder fills it | Passed |

## Cancel and close

| Check | Status |
|---|---|
| Cancel during a backup (1.4 GB): confirmation defaults to Continue; after cancelling no partial part files remain; log records it | Passed |
| Cancel during staging, restore, and verify | **Open** |
| Close the window during a question (e.g. the preflight) | **Open** |
| Close the window during a running backup: asks, cancels, cleans up, then closes | Passed |
| Log off or shut down during a backup: Windows shows "RestoreSafe is stopping the backup ..." and waits | **Open** (logs the tester off) |

## Display

| Check | Status |
|---|---|
| 150 %: layout, fonts, icons, status markers on every screen | Passed |
| 100 % and 200 % | **Open** (layout is unit-tested at 100-200 %) |
| Moving the window between monitors with different scaling (fonts, layout, and the report text size follow) | **Open** (needs two monitors) |
| Minimum window size (720 × 520): nothing overlaps, all buttons visible | Passed (unit test); **open** on screen |

## Keyboard and screen reader

| Check | Status |
|---|---|
| Keyboard only: Alt+B (Create backup), Enter (Start), password + Enter, Alt+B (Back to start), Alt+V, Enter in the tree, Enter (Start), password + Enter | Passed |
| Esc cancels questions and does nothing on the result screen | Passed |
| Access keys are unique per screen and hidden screens do not react to them | Passed (found and fixed: Alt+B on the result screen started a new backup through the hidden home button) |
| Every control has a role, a name, and its access key for screen readers (checked through MSAA) | Passed |
| Narrator reads the screens in a sensible order | **Open** (listen with Narrator) |

## Start

| Check | Status |
|---|---|
| Broken or missing `config.yaml`: error message box, then RestoreSafe ends | Passed |
| Missing source directory: health check shows the error, Create backup is disabled with the reason, Restore and Verify stay enabled | Passed |
| Disconnected YubiKey blocks, Recheck after connecting unblocks | **Open** (needs a YubiKey) |
| Missing or unreachable backup directory (e.g. disconnected NAS) | **Open** |
