# GuiDriver.ps1 - helpers to drive the RestoreSafe window from PowerShell.
#
# Dot-source it:  . .\GuiDriver.ps1
# It finds windows and controls by class and text, clicks buttons, fills the
# RestoreSafe input dialogs, sends keystrokes, and saves screenshots. The
# helpers depend on the GUI's control texts (e.g. "Create &backup") and window
# classes (RestoreSafeMainWindow, RestoreSafeInputDialog); update them when
# the GUI changes.

$ErrorActionPreference = "Stop"
Add-Type -AssemblyName System.Drawing, System.Windows.Forms
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
  [DllImport("user32.dll")] public static extern bool IsWindowVisible(IntPtr h);
  [DllImport("user32.dll")] public static extern bool IsWindowEnabled(IntPtr h);
  [DllImport("user32.dll")] public static extern bool PostMessage(IntPtr h, uint m, IntPtr w, IntPtr l);
  [DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern IntPtr SendMessage(IntPtr h, uint m, IntPtr w, string l);
  [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr h);
  [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h, out RECT r);
  [DllImport("user32.dll")] public static extern bool PrintWindow(IntPtr h, IntPtr hdc, uint f);
  [DllImport("user32.dll")] public static extern bool SetProcessDpiAwarenessContext(IntPtr v);
  [StructLayout(LayoutKind.Sequential)] public struct RECT { public int L, T, R, B; }
  // Window caption or static text; for edit controls of another process use EditText.
  public static string Text(IntPtr h) { var s = new StringBuilder(1024); GetWindowText(h, s, 1024); return s.ToString(); }
  // Text of an edit control in another process (WM_GETTEXT).
  public static string EditText(IntPtr h) { var s = new StringBuilder(4096); SendMessage(h, 0x000D, (IntPtr)4096, s); return s.ToString(); }
  public static string Class(IntPtr h) { var s = new StringBuilder(256); GetClassName(h, s, 256); return s.ToString(); }
  public static List<IntPtr> Children(IntPtr p) { var l = new List<IntPtr>(); EnumChildWindows(p, (h, x) => { l.Add(h); return true; }, IntPtr.Zero); return l; }
  public static List<IntPtr> TopLevel(uint pid) { var l = new List<IntPtr>(); EnumWindows((h, x) => { uint p; GetWindowThreadProcessId(h, out p); if (p == pid && IsWindowVisible(h)) l.Add(h); return true; }, IntPtr.Zero); return l; }
}
"@
# Physical pixels in screenshots and window rectangles.
[U]::SetProcessDpiAwarenessContext([IntPtr]-4) | Out-Null

# Wait-Until runs $cond until it returns something truthy, and returns it.
function Wait-Until([scriptblock]$cond, [int]$seconds = 30, [string]$what = "condition") {
  $end = (Get-Date).AddSeconds($seconds)
  while ((Get-Date) -lt $end) { $r = & $cond; if ($r) { return $r }; Start-Sleep -Milliseconds 200 }
  throw "timeout waiting for $what"
}

# Find-Window returns the first visible top-level window of the process with
# the class (and title pattern). Task dialogs and the folder picker have the
# class "#32770".
function Find-Window([int]$procId, [string]$class, [string]$titleLike = "*") {
  [U]::TopLevel($procId) | Where-Object { [U]::Class($_) -eq $class -and [U]::Text($_) -like $titleLike } | Select-Object -First 1
}

# Find-Button returns the visible, enabled button with the exact text,
# including the & of its access key (e.g. "&Start backup").
function Find-Button($parent, [string]$text) {
  [U]::Children($parent) | Where-Object { [U]::Class($_) -eq "Button" -and [U]::Text($_) -eq $text -and [U]::IsWindowVisible($_) -and [U]::IsWindowEnabled($_) } | Select-Object -First 1
}

# Click posts BM_CLICK, so it also works when the click opens a modal dialog.
function Click($button) { [U]::PostMessage($button, 0x00F5, [IntPtr]::Zero, [IntPtr]::Zero) | Out-Null }

# Fill-Dialog fills the edit fields of a RestoreSafe input dialog in order and
# presses OK.
function Fill-Dialog($dlg, [string[]]$values) {
  $edits = @([U]::Children($dlg) | Where-Object { [U]::Class($_) -eq "Edit" })
  for ($i = 0; $i -lt $values.Count; $i++) { [U]::SendMessage($edits[$i], 0x000C, [IntPtr]::Zero, $values[$i]) | Out-Null }
  [U]::PostMessage($dlg, 0x0111, [IntPtr]1, [IntPtr]::Zero) | Out-Null
}

# Set-EditText sets an edit control's text and tells the window, as typing
# does (EN_CHANGE), so enabled states follow.
function Set-EditText($main, $edit, [int]$id, [string]$text) {
  [U]::SendMessage($edit, 0x000C, [IntPtr]::Zero, $text) | Out-Null
  [U]::PostMessage($main, 0x0111, [IntPtr]($id -bor (0x300 -shl 16)), $edit) | Out-Null
}

# Press-Keys brings the window to the front and sends keystrokes (SendKeys
# syntax: "%b" = Alt+B, "{ENTER}", "+{TAB}" = Shift+Tab).
function Press-Keys($hwnd, [string]$keys) {
  [U]::SetForegroundWindow($hwnd) | Out-Null
  Start-Sleep -Milliseconds 300
  [System.Windows.Forms.SendKeys]::SendWait($keys)
}

# Result-Line returns the result line of the operation screen.
function Result-Line($main) {
  [U]::Children($main) | Where-Object { [U]::Class($_) -eq "Static" -and [U]::IsWindowVisible($_) -and [U]::Text($_) -match "completed|failed|cancelled|not started" } | ForEach-Object { [U]::Text($_) }
}

# Shot saves a PNG of the window (including the invisible resize border).
function Shot($hwnd, [string]$path) {
  $r = New-Object U+RECT; [U]::GetWindowRect($hwnd, [ref]$r) | Out-Null
  $bmp = New-Object System.Drawing.Bitmap ($r.R - $r.L), ($r.B - $r.T)
  $g = [System.Drawing.Graphics]::FromImage($bmp); $hdc = $g.GetHdc(); [U]::PrintWindow($hwnd, $hdc, 2) | Out-Null; $g.ReleaseHdc($hdc)
  $bmp.Save($path, [System.Drawing.Imaging.ImageFormat]::Png)
  $g.Dispose(); $bmp.Dispose()
}
