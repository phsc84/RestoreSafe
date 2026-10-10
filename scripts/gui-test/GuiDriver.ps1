# GuiDriver.ps1 - helpers to drive the RestoreSafe window from PowerShell.
#
# Dot-source it:  . .\GuiDriver.ps1
# Controls are found by their control ID, which is also their UI Automation
# AutomationId (spec 15), not by their texts: see $Ids. Windows are found by
# class: RestoreSafeMainWindow, RestoreSafePlan (Create backup window),
# RestoreSafeVerify (verify), RestoreSafeRestore (restore),
# RestoreSafeInputDialog (credentials), RestoreSafeDetails (reports and
# logs), "#32770" (task dialogs).

$ErrorActionPreference = "Stop"
Add-Type -AssemblyName System.Drawing, System.Windows.Forms, Accessibility
Add-Type @"
using System;
using System.Text;
using System.Collections.Generic;
using System.Runtime.InteropServices;
public static class U {
  public delegate bool EnumProc(IntPtr h, IntPtr l);
  [DllImport("user32.dll")] static extern bool EnumChildWindows(IntPtr p, EnumProc f, IntPtr l);
  [DllImport("user32.dll")] static extern bool EnumWindows(EnumProc f, IntPtr l);
  [DllImport("user32.dll", CharSet=CharSet.Unicode)] static extern int GetWindowText(IntPtr h, StringBuilder s, int n);
  [DllImport("user32.dll", CharSet=CharSet.Unicode)] static extern int GetClassName(IntPtr h, StringBuilder s, int n);
  [DllImport("user32.dll", CharSet=CharSet.Unicode)] static extern IntPtr SendMessage(IntPtr h, uint m, IntPtr w, StringBuilder l);
  [DllImport("user32.dll")] static extern uint GetWindowThreadProcessId(IntPtr h, out uint pid);
  [DllImport("user32.dll")] public static extern int GetDlgCtrlID(IntPtr h);
  [DllImport("user32.dll")] public static extern bool IsWindowVisible(IntPtr h);
  [DllImport("user32.dll")] public static extern bool IsWindowEnabled(IntPtr h);
  [DllImport("user32.dll")] public static extern bool PostMessage(IntPtr h, uint m, IntPtr w, IntPtr l);
  [DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern IntPtr SendMessage(IntPtr h, uint m, IntPtr w, string l);
  [DllImport("user32.dll")] public static extern IntPtr SendMessage(IntPtr h, uint m, IntPtr w, IntPtr l);
  [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr h);
  [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h, out RECT r);
  [DllImport("user32.dll")] public static extern bool PrintWindow(IntPtr h, IntPtr hdc, uint f);
  [DllImport("user32.dll")] public static extern bool SetProcessDpiAwarenessContext(IntPtr v);
  [DllImport("dwmapi.dll")] public static extern int DwmGetWindowAttribute(IntPtr h, int a, out RECT r, int s);
  [StructLayout(LayoutKind.Sequential)] public struct RECT { public int L, T, R, B; }
  // Window caption or static text; for edit controls of another process use EditText.
  public static string Text(IntPtr h) { var s = new StringBuilder(1024); GetWindowText(h, s, 1024); return s.ToString(); }
  // Text of an edit control in another process (WM_GETTEXT).
  public static string EditText(IntPtr h) { var s = new StringBuilder(4096); SendMessage(h, 0x000D, (IntPtr)4096, s); return s.ToString(); }
  public static string Class(IntPtr h) { var s = new StringBuilder(256); GetClassName(h, s, 256); return s.ToString(); }
  public static int Id(IntPtr h) { return GetDlgCtrlID(h); }
  public static List<IntPtr> Children(IntPtr p) { var l = new List<IntPtr>(); EnumChildWindows(p, (h, x) => { l.Add(h); return true; }, IntPtr.Zero); return l; }
  public static List<IntPtr> TopLevel(uint pid) { var l = new List<IntPtr>(); EnumWindows((h, x) => { uint p; GetWindowThreadProcessId(h, out p); if (p == pid && IsWindowVisible(h)) l.Add(h); return true; }, IntPtr.Zero); return l; }
}
"@
# MSAA (IAccessible), which screen readers use for classic Win32 controls.
Add-Type -ReferencedAssemblies Accessibility @"
using System; using System.Runtime.InteropServices; using Accessibility;
public static class Acc {
  [DllImport("oleacc.dll")] static extern int AccessibleObjectFromWindow(IntPtr h, uint id, ref Guid iid, [MarshalAs(UnmanagedType.Interface)] out object o);
  public static IAccessible Of(IntPtr h) { Guid iid = new Guid("618736E0-3C3D-11CF-810C-00AA00389B71"); object o; AccessibleObjectFromWindow(h, 0xFFFFFFFC, ref iid, out o); return (IAccessible)o; }
  public static int Count(IntPtr h) { return Of(h).accChildCount; }
  // Select selects and focuses child (1-based), as a click does.
  public static void Select(IntPtr h, int child) { Of(h).accSelect(3, child); }
}
"@
# Physical pixels in screenshots and window rectangles.
[U]::SetProcessDpiAwarenessContext([IntPtr]-4) | Out-Null

# Control IDs of RestoreSafe (the AutomationIds). internal/gui checks in a
# test that they match the code.
$Ids = [ordered]@{
  Sidebar          = 301
  HeroPrimary      = 401
  HeroSecondary    = 402
  HeroTitle        = 409
  RunCancel        = 420
  RunLog           = 421
  RunDetails       = 422
  RunDone          = 423
  RunOpen          = 424
  RunTitle         = 425
  PlanStart        = 451
  PlanFull         = 452
  PlanNewKeys      = 453
  PlanCancel       = 454
  PlanDetails      = 455
  VerifyStart      = 461
  VerifyCancel     = 462
  VerifyDetails    = 463
  CredentialOK     = 1
  CredentialCancel = 470
  CredentialLink   = 471
  BackupsFilter    = 601
  BackupsList      = 602
  BackupsRestore   = 603
  BackupsVerify    = 604
  RestoreStart     = 701
  RestoreCancel    = 702
  RestoreList      = 703
  RestoreDest      = 704
  RestoreBrowse    = 705
  RestoreBackupDir = 706
  RestoreDetails   = 707
  SettingsEdit     = 801
  SettingsReload   = 802
  SettingsOpen     = 803
  SettingsMore     = 804
  SettingsAdd      = 805
}

# Wait-Until runs $cond until it returns something truthy, and returns it.
function Wait-Until([scriptblock]$cond, [int]$seconds = 30, [string]$what = "condition") {
  $end = (Get-Date).AddSeconds($seconds)
  while ((Get-Date) -lt $end) { $r = & $cond; if ($r) { return $r }; Start-Sleep -Milliseconds 200 }
  throw "timeout waiting for $what"
}

# Find-Window returns the first visible top-level window of the process with
# the class (and title pattern).
function Find-Window([int]$procId, [string]$class, [string]$titleLike = "*") {
  [U]::TopLevel($procId) | Where-Object { [U]::Class($_) -eq $class -and [U]::Text($_) -like $titleLike } | Select-Object -First 1
}

# Find-Control returns the visible control with the ID $Ids[$name] in the
# window $parent; -Enabled requires it to be enabled.
function Find-Control($parent, [string]$name, [switch]$Enabled) {
  $id = $Ids[$name]
  [U]::Children($parent) | Where-Object { [U]::Id($_) -eq $id -and [U]::IsWindowVisible($_) -and (-not $Enabled -or [U]::IsWindowEnabled($_)) } | Select-Object -First 1
}

# Find-Button returns the visible, enabled button with the exact text,
# including the & of its access key. Prefer Find-Control.
function Find-Button($parent, [string]$text) {
  [U]::Children($parent) | Where-Object { [U]::Class($_) -eq "Button" -and [U]::Text($_) -eq $text -and [U]::IsWindowVisible($_) -and [U]::IsWindowEnabled($_) } | Select-Object -First 1
}

# Click posts BM_CLICK, so it also works when the click opens a modal dialog.
function Click($button) { [U]::PostMessage($button, 0x00F5, [IntPtr]::Zero, [IntPtr]::Zero) | Out-Null }

# Click-Control waits for the enabled control $name in $parent and clicks it.
function Click-Control($parent, [string]$name, [int]$seconds = 30) {
  Click (Wait-Until { Find-Control $parent $name -Enabled } $seconds "$name enabled")
  Start-Sleep -Milliseconds 300  # the click is posted; let the window act on it
}

# Text-Of returns the text of the visible control $name in $parent, or "".
function Text-Of($parent, [string]$name) {
  $c = Find-Control $parent $name
  if ($c) { [U]::Text($c) } else { "" }
}

# Go-Page shows a page of the main window: 0 Create backup, 1 Restore backup, 2
# Settings, through the sidebar's keys.
function Go-Page($main, [int]$page) {
  $bar = Find-Control $main Sidebar
  [U]::PostMessage($bar, 0x0100, [IntPtr]0x24, [IntPtr]::Zero) | Out-Null  # WM_KEYDOWN, VK_HOME
  for ($i = 0; $i -lt $page; $i++) { [U]::PostMessage($bar, 0x0100, [IntPtr]0x28, [IntPtr]::Zero) | Out-Null }  # VK_DOWN
  Start-Sleep -Milliseconds 500
}

# Select-ListItem selects item $index (0-based) of a list view through MSAA,
# as a click does.
function Select-ListItem($list, [int]$index = 0) {
  $n = [Acc]::Count($list)
  if ($n -le $index) { throw "the list has $n items" }
  [Acc]::Select($list, $index + 1)
  Start-Sleep -Milliseconds 400
}

# Set-Text sets an edit control's text; the edit tells its window, as typing
# does (EN_CHANGE).
function Set-Text($edit, [string]$text) { [U]::SendMessage($edit, 0x000C, [IntPtr]::Zero, $text) | Out-Null }

# Fill-Dialog fills the edit fields of a credential dialog in order and
# presses its OK button.
function Fill-Dialog($dlg, [string[]]$values) {
  $edits = @([U]::Children($dlg) | Where-Object { [U]::Class($_) -eq "Edit" })
  for ($i = 0; $i -lt $values.Count; $i++) { Set-Text $edits[$i] $values[$i] }
  [U]::PostMessage($dlg, 0x0111, [IntPtr]1, [IntPtr]::Zero) | Out-Null
}

# Click-TaskButton clicks the button with the text in the process's task
# dialog (e.g. "Cancel backup", "Cancel restore").
function Click-TaskButton([int]$procId, [string]$text, [int]$seconds = 30) {
  $b = Wait-Until {
    $task = [U]::TopLevel($procId) | Where-Object { [U]::Class($_) -eq "#32770" } | Select-Object -First 1
    if ($task) { [U]::Children($task) | Where-Object { [U]::Class($_) -eq "Button" -and [U]::Text($_) -eq $text } | Select-Object -First 1 }
  } $seconds "task dialog button $text"
  Click $b
}

# Answer-Credentials answers the credential dialogs until $done returns
# true: a new password (two fields), the password (one masked field), the
# recovery code shown at key setup (read, then confirmed), and nothing
# else. It returns the recovery code it saw, if any.
function Answer-Credentials([int]$procId, [string]$password, [scriptblock]$done, [int]$seconds = 180) {
  $code = $script:RecoveryCode
  $end = (Get-Date).AddSeconds($seconds)
  while ((Get-Date) -lt $end) {
    if (& $done) { return }
    $dlg = Find-Window $procId "RestoreSafeInputDialog"
    if (-not $dlg) { Start-Sleep -Milliseconds 300; continue }
    $edits = @([U]::Children($dlg) | Where-Object { [U]::Class($_) -eq "Edit" })
    $lines = @([U]::Children($dlg) | Where-Object { [U]::Class($_) -eq "Static" } | ForEach-Object { [U]::Text($_) } | Where-Object { $_ -match '^[0-9A-Z]{4,}(-[0-9A-Z]{4,})+$' })
    if ($edits.Count -eq 0 -and $lines.Count -gt 0) {
      $script:RecoveryCode = $lines -join "-"
      [U]::PostMessage($dlg, 0x0111, [IntPtr]1, [IntPtr]::Zero) | Out-Null
    } elseif ($edits.Count -eq 2) {
      Fill-Dialog $dlg @($password, $password)
    } elseif ($edits.Count -eq 1) {
      Fill-Dialog $dlg @($password)
    }
    Start-Sleep -Milliseconds 800
  }
  throw "timeout answering credentials"
}

# Press-Keys brings the window to the front and sends keystrokes (SendKeys
# syntax: "%b" = Alt+B, "{ENTER}", "+{TAB}" = Shift+Tab).
function Press-Keys($hwnd, [string]$keys) {
  [U]::SetForegroundWindow($hwnd) | Out-Null
  Start-Sleep -Milliseconds 300
  [System.Windows.Forms.SendKeys]::SendWait($keys)
}

# Shot saves a PNG of the window (including the invisible resize border).
function Shot($hwnd, [string]$path) {
  $r = New-Object U+RECT; [U]::GetWindowRect($hwnd, [ref]$r) | Out-Null
  $bmp = New-Object System.Drawing.Bitmap ($r.R - $r.L), ($r.B - $r.T)
  $g = [System.Drawing.Graphics]::FromImage($bmp)
  $hdc = $g.GetHdc(); [U]::PrintWindow($hwnd, $hdc, 2) | Out-Null; $g.ReleaseHdc($hdc)
  $bmp.Save($path, [System.Drawing.Imaging.ImageFormat]::Png)
  $g.Dispose(); $bmp.Dispose()
}

# Save-Shot saves a PNG of the window cropped to its visible frame, without
# the invisible resize border (DWMWA_EXTENDED_FRAME_BOUNDS = 9), and scaled
# (0.667 for the README screenshots, taken at 150 %).
function Save-Shot($hwnd, [string]$path, [double]$scale = 1.0) {
  $raw = [System.IO.Path]::GetTempFileName() + ".png"
  Shot $hwnd $raw
  $wr = New-Object U+RECT; [U]::GetWindowRect($hwnd, [ref]$wr) | Out-Null
  $fr = New-Object U+RECT; [U]::DwmGetWindowAttribute($hwnd, 9, [ref]$fr, 16) | Out-Null
  $src = [System.Drawing.Image]::FromFile($raw)
  $crop = New-Object System.Drawing.Rectangle ($fr.L - $wr.L), ($fr.T - $wr.T), ($fr.R - $fr.L), ($fr.B - $fr.T)
  $w = [int]($crop.Width * $scale); $h = [int]($crop.Height * $scale)
  $dst = New-Object System.Drawing.Bitmap $w, $h
  $g = [System.Drawing.Graphics]::FromImage($dst)
  $g.InterpolationMode = [System.Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
  $g.DrawImage($src, (New-Object System.Drawing.Rectangle 0, 0, $w, $h), $crop, [System.Drawing.GraphicsUnit]::Pixel)
  $dst.Save($path, [System.Drawing.Imaging.ImageFormat]::Png)
  $g.Dispose(); $dst.Dispose(); $src.Dispose(); Remove-Item $raw
  "Saved $path (${w}x${h})"
}

# Test-Overlaps returns the visible labels of $parent that lie over a
# visible button, link, list or field: a label with a tooltip receives the
# mouse, so it would take the control's clicks. A click message (Click)
# doesn't notice; a user's click does.
function Test-Overlaps([IntPtr]$parent, [string]$where) {
  $shown = @([U]::Children($parent) | Where-Object { [U]::IsWindowVisible($_) })
  $labels = @($shown | Where-Object { [U]::Class($_) -eq "Static" })
  $controls = @($shown | Where-Object { @("Button", "SysLink", "SysListView32", "Edit", "ComboBox", "RichEdit50W") -contains [U]::Class($_) })
  foreach ($l in $labels) {
    $a = New-Object U+RECT; [U]::GetWindowRect($l, [ref]$a) | Out-Null
    if ($a.R -le $a.L -or $a.B -le $a.T) { continue }
    foreach ($c in $controls) {
      $b = New-Object U+RECT; [U]::GetWindowRect($c, [ref]$b) | Out-Null
      if ($a.L -lt $b.R -and $b.L -lt $a.R -and $a.T -lt $b.B -and $b.T -lt $a.B) {
        "$($where): label '$([U]::Text($l))' lies over $([U]::Class($c)) '$([U]::Text($c))'"
      }
    }
  }
}
