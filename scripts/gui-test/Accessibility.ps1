# Accessibility.ps1 - what screen readers see of RestoreSafe, and the access
# keys (spec 15: on all buttons, unique per page and dialog).
#
# As a library (after GuiDriver.ps1):  . .\Accessibility.ps1
#   Show-Accessibility $hwnd   role, name, AutomationId and access key of
#                              every visible control (MSAA, as screen
#                              readers read classic Win32 controls)
#   Test-AccessKeys $hwnd      problems: buttons without an access key,
#                              access keys used twice
# As a check:
#   .\Accessibility.ps1 -Exe ..\..\sandbox\RestoreSafe.exe -Config C:\...\smoke\config.yaml
#   walks the three pages, the backup plan dialog and the restore wizard's
#   pages and exits 1 on a problem.

param([string]$Exe = "", [string]$Config = "")
if (-not ("U" -as [type])) { . "$PSScriptRoot\GuiDriver.ps1" }

Add-Type -ReferencedAssemblies Accessibility @"
using System; using System.Runtime.InteropServices; using Accessibility;
public static class Msaa {
  [DllImport("oleacc.dll")] static extern int AccessibleObjectFromWindow(IntPtr h, uint id, ref Guid iid, [MarshalAs(UnmanagedType.Interface)] out object o);
  [DllImport("oleacc.dll", CharSet=CharSet.Unicode)] static extern uint GetRoleText(uint role, System.Text.StringBuilder s, uint n);
  [DllImport("user32.dll")] static extern int GetDlgCtrlID(IntPtr h);
  public static string Describe(IntPtr h) {
    Guid iid = new Guid("618736E0-3C3D-11CF-810C-00AA00389B71"); object o;
    if (AccessibleObjectFromWindow(h, 0xFFFFFFFC, ref iid, out o) != 0) return "(no IAccessible)";
    var a = (IAccessible)o;
    string name = "", key = ""; uint role = 0;
    try { name = a.get_accName(0); } catch {}
    try { key = a.get_accKeyboardShortcut(0); } catch {}
    try { role = Convert.ToUInt32(a.get_accRole(0)); } catch {}
    var sb = new System.Text.StringBuilder(64); GetRoleText(role, sb, 64);
    return string.Format("{0,-20} id {1,-4} '{2}'{3}", sb, GetDlgCtrlID(h), name, string.IsNullOrEmpty(key) ? "" : "  [" + key + "]");
  }
}
"@

# Show-Accessibility prints every visible control of $parent, in creation
# (tab) order.
function Show-Accessibility([IntPtr]$parent) {
  foreach ($h in [U]::Children($parent)) { if ([U]::IsWindowVisible($h)) { [Msaa]::Describe($h) } }
}

# Test-AccessKeys returns the access-key problems of the visible buttons of
# $parent. Cancel and OK of dialogs need none: Esc and Enter press them.
function Test-AccessKeys([IntPtr]$parent, [string]$where) {
  $keys = @{}
  foreach ($h in [U]::Children($parent)) {
    if (-not [U]::IsWindowVisible($h) -or [U]::Class($h) -ne "Button") { continue }
    $text = [U]::Text($h)
    if ($text -in @("Cancel", "OK", "Close", "")) { continue }
    if ($text -notmatch '&(.)') { "$($where): '$text' has no access key"; continue }
    $k = $Matches[1].ToUpper()
    if ($keys.ContainsKey($k)) { "$($where): '$text' and '$($keys[$k])' share Alt+$k" } else { $keys[$k] = $text }
  }
}

if ($Exe) {
  $problems = @()
  $p = Start-Process $Exe -ArgumentList "-config=`"$Config`"" -PassThru
  try {
    $main = Wait-Until { Find-Window $p.Id "RestoreSafeMainWindow" } 20 "window"
    Wait-Until { (Text-Of $main HeroTitle) -notlike "Checking*" } 30 "health check" | Out-Null
    foreach ($page in 0, 1, 2) {
      Go-Page $main $page
      $problems += Test-AccessKeys $main ("page " + @("Overview", "Backups", "Settings")[$page])
    }
    Go-Page $main 0
    Click-Control $main HeroPrimary
    $plan = Wait-Until { Find-Window $p.Id "RestoreSafePlan" } 20 "plan"
    Wait-Until { Find-Control $plan PlanStart -Enabled } 60 "plan" | Out-Null
    $problems += Test-AccessKeys $plan "backup plan"
    Click-Control $plan PlanCancel
    Start-Sleep -Seconds 1
    Go-Page $main 1
    Select-ListItem (Find-Control $main BackupsList) 0
    Click-Control $main BackupsRestore
    $wiz = Wait-Until { Find-Window $p.Id "RestoreSafeWizard" } 10 "wizard"
    foreach ($n in 1, 2, 3) {
      $problems += Test-AccessKeys $wiz "wizard page $n"
      Click-Control $wiz WizardNext
    }
    $problems += Test-AccessKeys $wiz "wizard page 4"
    Click-Control $wiz WizardCancel
  } finally { Stop-Process $p -Force -ErrorAction SilentlyContinue }
  if ($problems) { $problems; exit 1 }
  "access keys ok"
  exit 0
}
