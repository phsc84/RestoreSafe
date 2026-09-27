# Screenshot.ps1 - starts RestoreSafe, waits, and saves a screenshot of its
# window, cropped to the visible frame and optionally scaled (e.g. 0.667 for
# the README screenshots, taken at 150 %).
#
# .\Screenshot.ps1 -Exe ..\..\dist\RestoreSafe.exe -ExeArgs '-config="C:\path\config.yaml"' -Out home.png [-Wait 5] [-Scale 0.667]

param(
  [Parameter(Mandatory)][string]$Exe,
  [string]$ExeArgs = "",
  [Parameter(Mandatory)][string]$Out,
  [int]$Wait = 5,
  [double]$Scale = 1.0
)
. "$PSScriptRoot\GuiDriver.ps1"
Add-Type @"
using System; using System.Runtime.InteropServices;
public static class Dwm {
  [StructLayout(LayoutKind.Sequential)] public struct R { public int L, T, Rt, B; }
  [DllImport("dwmapi.dll")] public static extern int DwmGetWindowAttribute(IntPtr h, int a, out R r, int s);
  [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h, out R r);
}
"@

$p = if ($ExeArgs) { Start-Process $Exe -ArgumentList $ExeArgs -PassThru } else { Start-Process $Exe -PassThru }
try {
  $main = Wait-Until { Find-Window $p.Id "RestoreSafeMainWindow" } 20 "RestoreSafe window"
  Start-Sleep -Seconds $Wait
  $raw = [System.IO.Path]::GetTempFileName() + ".png"
  Shot $main $raw

  # The window rectangle includes an invisible resize border; crop to the
  # visible frame (DWMWA_EXTENDED_FRAME_BOUNDS = 9).
  $wr = New-Object Dwm+R; [Dwm]::GetWindowRect($main, [ref]$wr) | Out-Null
  $fr = New-Object Dwm+R; [Dwm]::DwmGetWindowAttribute($main, 9, [ref]$fr, 16) | Out-Null
  $src = [System.Drawing.Image]::FromFile($raw)
  $crop = New-Object System.Drawing.Rectangle ($fr.L - $wr.L), ($fr.T - $wr.T), ($fr.Rt - $fr.L), ($fr.B - $fr.T)
  $w = [int]($crop.Width * $Scale); $h = [int]($crop.Height * $Scale)
  $dst = New-Object System.Drawing.Bitmap $w, $h
  $g = [System.Drawing.Graphics]::FromImage($dst)
  $g.InterpolationMode = [System.Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
  $g.DrawImage($src, (New-Object System.Drawing.Rectangle 0, 0, $w, $h), $crop, [System.Drawing.GraphicsUnit]::Pixel)
  $dst.Save($Out, [System.Drawing.Imaging.ImageFormat]::Png)
  $g.Dispose(); $dst.Dispose(); $src.Dispose(); Remove-Item $raw
  "Saved $Out (${w}x${h})"
} finally {
  Stop-Process $p -Force -ErrorAction SilentlyContinue
}
