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
io_diagnostics: false
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

$gui = Join-Path $repo "scripts\gui-test"
& (Join-Path $gui "Smoke-BackupRestore.ps1") -Exe $exe -Config $config -Password "correct horse battery" `
  -RestoreTo (Join-Path $Work "restored") -ScreenshotDir (Join-Path $Work "shots")
if ($LASTEXITCODE -ne 0) { Write-Host "Smoke-BackupRestore failed"; exit 1 }
& (Join-Path $gui "Check-States.ps1") -Exe $exe -Config $config -Out (Join-Path $Work "states")
if ($LASTEXITCODE -ne 0) { Write-Host "Check-States failed"; exit 1 }
exit 0
