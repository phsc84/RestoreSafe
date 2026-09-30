# GUI test scripts

PowerShell tools that drive the RestoreSafe window like a user: they click buttons, fill dialogs, press keys, read control texts, and take screenshots. They support the manual checklist in [docs/GUI-TEST-CHECKLIST.md](../../docs/GUI-TEST-CHECKLIST.md); they are not part of `go test`.

They rely on the first GUI's control texts (e.g. `Create &backup`) and window classes (`RestoreSafeMainWindow`, `RestoreSafeInputDialog`). The status-first UI replaces that GUI; section 16.4 of [docs/SPEC-restoresafe-gui.md](../../docs/SPEC-restoresafe-gui.md) specifies the updated scripts, which find controls by `AutomationId` instead of text.

| Script | Use |
|---|---|
| `GuiDriver.ps1` | Helpers, dot-sourced by the other scripts: find windows and buttons, click, fill input dialogs, set edit text, send keys, read the result line, save a screenshot. |
| `Smoke-BackupRestore.ps1` | Backup, restore of the newest run, and verify through the window; compares the restored files with the sources. Exit code 0 on success. Password-only configurations (it cannot answer YubiKey prompts); it handles new-key setup and a recovery code. |
| `Screenshot.ps1` | Starts RestoreSafe and saves a screenshot cropped to the visible frame, optionally scaled (the README screenshots use `-Scale 0.667` at 150 %). |
| `Accessibility.ps1` | `Show-Accessibility <hwnd>` lists role, name, and keyboard shortcut of every visible control, as screen readers see them (MSAA). |

## Examples

Run them from Windows PowerShell 5.1 in an interactive session (the window must be able to come to the front). Use a test configuration and directories, e.g. in `sandbox\`, never your real backups.

```powershell
cd scripts\gui-test

# Smoke test with screenshots of every step.
.\Smoke-BackupRestore.ps1 -Exe ..\..\sandbox\RestoreSafe.exe -Config C:\dev\RestoreSafe\sandbox\gui-test\config.yaml `
    -Password "correct horse battery" -RestoreTo C:\dev\RestoreSafe\sandbox\gui-test\restored `
    -ScreenshotDir C:\dev\RestoreSafe\sandbox\gui-test\shots

# Screenshot of the start screen for the README.
.\Screenshot.ps1 -Exe ..\..\sandbox\RestoreSafe.exe -ExeArgs '-config="C:\dev\RestoreSafe\sandbox\gui-test\config.yaml"' `
    -Out ..\..\docs\images\Screenshot_v2.0.0_home.png -Scale 0.667

# Accessibility of the start screen.
. .\GuiDriver.ps1; . .\Accessibility.ps1
$p = Start-Process ..\..\sandbox\RestoreSafe.exe -PassThru
$main = Wait-Until { Find-Window $p.Id "RestoreSafeMainWindow" } 20 "window"
Show-Accessibility $main
Stop-Process $p
```

The smoke test's restore destination must not exist yet; delete it between runs.
