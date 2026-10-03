# Smoke-BackupRestore.ps1 - backs up twice through the plan dialog (a full,
# then differentials), restores one folder of the newest run through the
# restore wizard, verifies the newest run from the Restore backup page, and
# compares the restored folder with its source (spec 16.4).
#
# Use a test configuration with authentication_mode 1 (password only); the
# script cannot answer YubiKey prompts. It answers new-key setup (password
# twice) and a recovery code (reads it from the dialog and types it back).
#
# .\Smoke-BackupRestore.ps1 -Exe ..\..\sandbox\RestoreSafe.exe -Config C:\...\config.yaml `
#     -Password "correct horse battery" -RestoreTo C:\...\restored [-ScreenshotDir C:\...\shots]
#
# Exit code 0 when every step succeeded and the restored files match.

param(
  [Parameter(Mandatory)][string]$Exe,
  [Parameter(Mandatory)][string]$Config,
  [Parameter(Mandatory)][string]$Password,
  [Parameter(Mandatory)][string]$RestoreTo,
  [string]$ScreenshotDir = "",
  [int]$Backups = 2
)
. "$PSScriptRoot\GuiDriver.ps1"

if (Test-Path $RestoreTo) { throw "RestoreTo must not exist yet: $RestoreTo" }
$shotNo = 0
function Snap($hwnd, [string]$name) {
  if ($ScreenshotDir) {
    New-Item -ItemType Directory -Force $ScreenshotDir | Out-Null
    $script:shotNo++
    Shot $hwnd (Join-Path $ScreenshotDir ("{0:D2}-{1}.png" -f $script:shotNo, $name))
  }
}
# Result waits for the result card in $window and returns its title.
function Result($window) {
  Wait-Until { Find-Control $window RunDone -Enabled } 600 "result" | Out-Null
  Text-Of $window RunTitle
}

$failed = $false
$p = Start-Process $Exe -ArgumentList "-config=`"$Config`"" -PassThru
try {
  $main = Wait-Until { Find-Window $p.Id "RestoreSafeMainWindow" } 20 "RestoreSafe window"
  Wait-Until { (Text-Of $main HeroTitle) -and (Text-Of $main HeroTitle) -notlike "Checking*" } 60 "health check" | Out-Null
  Snap $main "overview"

  # Backups through the plan dialog.
  for ($i = 1; $i -le $Backups; $i++) {
    Click-Control $main HeroPrimary 60
    $plan = Wait-Until { Find-Window $p.Id "RestoreSafePlan" } 20 "plan dialog"
    Wait-Until { Find-Control $plan PlanStart -Enabled } 120 "backup plan" | Out-Null
    Snap $plan "plan-$i"
    Click-Control $plan PlanStart
    Answer-Credentials $p.Id $Password { Find-Control $main RunDone -Enabled }
    $r = Result $main; Snap $main "backup-$i"; "Backup $($i): $r"
    $o = Test-Overlaps $main "backup result"; if ($o) { $o; $failed = $true }
    if ($r -notlike "Backup finished*") { $failed = $true }
    Click-Control $main RunDone
  }

  # Restore of the newest run's first folder through the wizard.
  Go-Page $main 1
  $list = Wait-Until { Find-Control $main BackupsList } 10 "Backups list"
  Select-ListItem $list 0
  Wait-Until { Find-Control $main BackupsRestore -Enabled } 20 "a backup selected" | Out-Null
  Snap $main "backups"
  Click-Control $main BackupsRestore
  $wiz = Wait-Until { Find-Window $p.Id "RestoreSafeWizard" } 10 "restore wizard"
  Click-Control $wiz WizardNext
  Click-Control $wiz WizardNext
  $edit = Wait-Until { Find-Control $wiz WizardDest } 10 "destination"
  Set-Text $edit $RestoreTo
  Wait-Until { Find-Control $wiz WizardNext -Enabled } 20 "destination checked" | Out-Null
  Snap $wiz "wizard-destination"
  Click-Control $wiz WizardNext
  Wait-Until { (Text-Of $wiz WizardNext) -eq "&Restore…" -and (Find-Control $wiz WizardNext -Enabled) } 60 "restore plan" | Out-Null
  Snap $wiz "wizard-check"
  Click-Control $wiz WizardNext
  Answer-Credentials $p.Id $Password { Find-Control $wiz RunDone -Enabled }
  $r = Result $wiz; Snap $wiz "restore-result"; "Restore:  $r"
  $o = Test-Overlaps $wiz "restore result"; if ($o) { $o; $failed = $true }
  if ($r -notlike "Restore finished*") { $failed = $true }
  Click-Control $wiz RunDone

  # Verification of the newest run.
  Go-Page $main 1
  $list = Wait-Until { Find-Control $main BackupsList } 10 "Backups list"
  Select-ListItem $list 0
  Click-Control $main BackupsVerify
  Click-TaskButton $p.Id "Verify…"
  Answer-Credentials $p.Id $Password { Find-Control $main RunDone -Enabled }
  $r = Result $main; Snap $main "verify-result"; "Verify:   $r"
  $o = Test-Overlaps $main "verify result"; if ($o) { $o; $failed = $true }
  if ($r -notlike "Verification finished*") { $failed = $true }
  Click-Control $main RunDone
} catch {
  "FAILED: $_"
  $failed = $true
} finally {
  Stop-Process $p -Force -ErrorAction SilentlyContinue
}

# Compare the restored folder with its source.
$sources = Select-String -Path $Config -Pattern '^\s*-\s*"(.+)"\s*$' | Where-Object { $_.Line -notmatch '^\s*#' } |
  ForEach-Object { $_.Matches[0].Groups[1].Value }
$compared = 0
foreach ($src in $sources) {
  $name = Split-Path $src -Leaf
  $dst = Join-Path $RestoreTo $name
  if (-not (Test-Path $dst)) { continue }  # one folder is restored
  $compared++
  $srcFiles = Get-ChildItem -Recurse -File $src | ForEach-Object { $_.FullName.Substring($src.Length) }
  $diff = foreach ($rel in $srcFiles) {
    $b = Join-Path $dst $rel
    if (-not (Test-Path $b) -or (Get-FileHash (Join-Path $src $rel)).Hash -ne (Get-FileHash $b).Hash) { $rel }
  }
  if ($diff) { "Compare: $name differs: $($diff -join ', ')"; $failed = $true } else { "Compare: $name identical ($($srcFiles.Count) files)" }
}
if ($compared -eq 0) { "Compare: nothing was restored"; $failed = $true }

if ($failed) { exit 1 }
exit 0
