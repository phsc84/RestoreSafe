# GUI test scripts

PowerShell tools that drive the RestoreSafe window like a user: they click buttons, fill dialogs, read control texts, and take screenshots (spec section 16.4 of [docs/SPEC-restoresafe-gui.md](../../docs/SPEC-restoresafe-gui.md)). They support the manual checklist in [docs/GUI-TEST-CHECKLIST.md](../../docs/GUI-TEST-CHECKLIST.md); they are not part of `go test`.

Controls are found by their control ID, which is also their UI Automation `AutomationId` (table `$Ids` in `GuiDriver.ps1`; a Go test in `internal/gui` keeps it equal to the code). Windows are found by class: `RestoreSafeMainWindow`, `RestoreSafePlan`, `RestoreSafeWizard`, `RestoreSafeInputDialog`, `RestoreSafeDetails`, and `#32770` for task dialogs.

| Script | Use |
|---|---|
| `GuiDriver.ps1` | Helpers, dot-sourced by the other scripts: find windows and controls, click, switch pages, select list items (MSAA), fill and answer credential dialogs, click task-dialog buttons, save a screenshot. |
| `Smoke-BackupRestore.ps1` | Two backups through the plan dialog (a full, then differentials), a restore of one folder of the newest run through the wizard, a verification of the newest run; compares the restored folder with its source. Exit code 0 on success. Password-only configurations (it cannot answer YubiKey prompts); it handles new-key setup and a recovery code. |
| `New-TestCondition.ps1` | Copies a smoke-test setup and turns the copy into a condition of spec 3.5 and 11.8 (`-Condition BaseMissing`, ...; the names of the Go fixtures). |
| `Check-States.ps1` | For every condition: makes it, starts RestoreSafe, compares the hero's title and primary action with the expected ones, saves a screenshot. Exit code 0 when all match. |
| `Accessibility.ps1` | Library: `Show-Accessibility` (role, name, AutomationId, access key of every control, as screen readers see them), `Test-AccessKeys`. As a check: walks the pages, the plan dialog and the wizard and reports buttons without access keys and duplicate access keys. |
| `Screenshot.ps1` | Starts RestoreSafe and saves a screenshot cropped to the visible frame, optionally scaled (the README screenshots use `-Scale 0.667` at 150 %). |

## Examples

Run them from Windows PowerShell 5.1 in an interactive session. Use a test configuration and directories, never your real backups: a password-only `config.yaml` with two small source folders, e.g. in `sandbox\gui-test\`.

```powershell
cd scripts\gui-test
$exe = "C:\dev\RestoreSafe\sandbox\RestoreSafe.exe"
$cfg = "C:\dev\RestoreSafe\sandbox\gui-test\config.yaml"

# Smoke test with screenshots of every step (the restore destination must not exist).
.\Smoke-BackupRestore.ps1 -Exe $exe -Config $cfg -Password "correct horse battery" `
    -RestoreTo C:\dev\RestoreSafe\sandbox\gui-test\restored -ScreenshotDir C:\dev\RestoreSafe\sandbox\gui-test\shots

# Every status condition, from the smoke test's backups.
.\Check-States.ps1 -Exe $exe -Config $cfg -Out C:\dev\RestoreSafe\sandbox\gui-test\states

# Access keys of all pages and dialogs.
.\Accessibility.ps1 -Exe $exe -Config $cfg

# Screenshot of the Overview for the README.
.\Screenshot.ps1 -Exe $exe -ExeArgs "-config=`"$cfg`"" -Out ..\..\docs\images\Screenshot_v2.0.0_overview.png -Scale 0.667
```

The release gate (spec 16.7) runs `Smoke-BackupRestore.ps1` and `Check-States.ps1` at 100 % and 150 % display scaling.
