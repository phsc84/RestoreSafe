# gui-smoke.ps1 - runs the GUI smoke test and the state check of
# scripts\gui-test on a fresh setup in $Work: two source folders, a
# password-only configuration with a recovery code, and RestoreSafe.exe built
# from this checkout. Needs an interactive desktop (UI Automation).
#
# .\scripts\ci\gui-smoke.ps1 -Work C:\...\gui-smoke
#
# Exit code 0 when both pass; screenshots are in $Work\shots and $Work\states.
param([Parameter(Mandatory)][string]$Work)
$ErrorActionPreference = "Stop"
$repo = Resolve-Path (Join-Path $PSScriptRoot "..\..")

if (Test-Path $Work) { Remove-Item -Recurse -Force $Work }
New-Item -ItemType Directory -Force $Work | Out-Null
$exe = Join-Path $Work "RestoreSafe.exe"
Push-Location $repo
try {
  # The manifest in the resources selects Common Controls 6, which the
  # window needs; resource.syso is generated, as by build.bat.
  go tool goversioninfo -64 -o cmd/restoresafe/resource.syso build/versioninfo.json
  if ($LASTEXITCODE -ne 0) { throw "goversioninfo failed" }
  go build -trimpath -ldflags="-H=windowsgui" -o $exe ./cmd/restoresafe
  if ($LASTEXITCODE -ne 0) { throw "go build failed" }
} finally { Pop-Location }

$docs = Join-Path $Work "src\Docs"
$pics = Join-Path $Work "src\Pics"
New-Item -ItemType Directory -Force (Join-Path $docs "letters"), $pics, (Join-Path $Work "backups") | Out-Null
Set-Content -Encoding ascii (Join-Path $docs "notes.txt") "RestoreSafe GUI smoke test"
Set-Content -Encoding ascii (Join-Path $docs "letters\dear.txt") "Dear RestoreSafe"
[IO.File]::WriteAllBytes((Join-Path $pics "photo.bin"), [byte[]](1..255 * 400))

$w = $Work -replace '\\', '/'
$config = Join-Path $Work "config.yaml"
@"
source_directories:
  - "$w/src/Docs"
  - "$w/src/Pics"
backup_directory: "$w/backups"
split_size_mb: 1
retention_keep: 3
differential:
  enabled: true
  full_backup_interval_days: 30
  max_size_percent: 50
  retention_keep_differentials: 0
exclude: []
on_unreadable_file: "fail"
log_level: "info"
verify_after_backup: true
reminder_days: 7
authentication_mode: 1
yubikey_spare: false
recovery_code: true
password_min_length: 12
argon2:
  time: 2
  memory_mb: 64
  threads: 1
"@ | Set-Content -Encoding utf8 $config

# Diagnostics when the window doesn't show: the session, the process, its
# windows, and a screenshot of the whole desktop.
function Show-Diagnostics {
  . (Join-Path $gui "GuiDriver.ps1")
  Write-Host "Session $((Get-Process -Id $PID).SessionId), user interactive: $([Environment]::UserInteractive)"
  $p = Start-Process $exe -ArgumentList "-config=`"$config`"" -PassThru
  Start-Sleep -Seconds 15
  $p.Refresh()
  if ($p.HasExited) { Write-Host "RestoreSafe exited with code $($p.ExitCode)" }
  else {
    Write-Host "RestoreSafe runs; main window '$($p.MainWindowTitle)' $($p.MainWindowHandle)"
    foreach ($h in [U]::TopLevel($p.Id)) {
      Write-Host "  window class $([U]::Class($h)), text '$([U]::Text($h))'"
      foreach ($c in [U]::Children($h)) { if ([U]::Text($c)) { Write-Host "    $([U]::Text($c))" } }
    }
    Stop-Process -Id $p.Id -Force
  }
  Add-Type -AssemblyName System.Windows.Forms, System.Drawing
  $b = [System.Windows.Forms.Screen]::PrimaryScreen.Bounds
  Write-Host "Screen $($b.Width) x $($b.Height)"
  $bmp = New-Object System.Drawing.Bitmap $b.Width, $b.Height
  [System.Drawing.Graphics]::FromImage($bmp).CopyFromScreen($b.Location, [System.Drawing.Point]::Empty, $b.Size)
  New-Item -ItemType Directory -Force (Join-Path $Work "shots") | Out-Null
  $bmp.Save((Join-Path $Work "shots\desktop.png"), [System.Drawing.Imaging.ImageFormat]::Png)
}

$gui = Join-Path $repo "scripts\gui-test"
& (Join-Path $gui "Smoke-BackupRestore.ps1") -Exe $exe -Config $config -Password "correct horse battery" `
  -RestoreTo (Join-Path $Work "restored") -ScreenshotDir (Join-Path $Work "shots")
if ($LASTEXITCODE -ne 0) {
  Write-Host "Smoke-BackupRestore failed"
  try { Show-Diagnostics } catch { Write-Host "Diagnostics failed: $_" }
  exit 1
}
& (Join-Path $gui "Check-States.ps1") -Exe $exe -Config $config -Out (Join-Path $Work "states")
if ($LASTEXITCODE -ne 0) { Write-Host "Check-States failed"; exit 1 }
exit 0
