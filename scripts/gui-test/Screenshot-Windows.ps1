# Screenshot-Windows.ps1 - saves the README screenshots of the Create backup
# and Restore backup windows: makes a full backup, changes the source
# folders, saves the next backup's Create backup window (differentials),
# makes that backup, and saves the Restore backup window on it.
#
# Use a password-only test configuration (authentication_mode 1) whose
# backup directory doesn't exist yet. Each source folder gets a copy of its
# newest file for the differential; the copies are removed at the end. The
# Restore backup window shows "Restore" next to config.yaml as destination,
# which must not exist; nothing is restored.
#
# .\Screenshot-Windows.ps1 -Exe ..\..\sandbox\RestoreSafe.exe -Config C:\...\config.yaml `
#     -Password "correct horse battery" -OutDir ..\..\docs\images [-Scale 0.667]

param(
  [Parameter(Mandatory)][string]$Exe,
  [Parameter(Mandatory)][string]$Config,
  [Parameter(Mandatory)][string]$Password,
  [Parameter(Mandatory)][string]$OutDir,
  [double]$Scale = 0.667
)
. "$PSScriptRoot\GuiDriver.ps1"

function Setting([string]$pattern) {
  Select-String -Path $Config -Pattern $pattern | Where-Object { $_.Line -notmatch '^\s*#' } | ForEach-Object { $_.Matches[0].Groups[1].Value }
}
$sources = @(Setting '^\s*-\s*"(.+)"\s*$')
$backupDir = Setting '^\s*backup_directory:\s*"(.+)"\s*$'
$restoreTo = Join-Path (Split-Path $Config) "Restore"
if (Test-Path $backupDir) { throw "The backup directory must not exist yet: $backupDir" }
if (Test-Path $restoreTo) { throw "The restore destination must not exist: $restoreTo" }
New-Item -ItemType Directory -Force $OutDir | Out-Null
$OutDir = (Resolve-Path $OutDir).Path

# Finish waits for the result card, prints its title, and closes it.
function Finish($main) {
  Wait-Until { Find-Control $main RunDone -Enabled } 600 "result" | Out-Null
  "Result: " + (Text-Of $main RunTitle)
  Click-Control $main RunDone
}

$copies = @()
$p = Start-Process $Exe -ArgumentList "-config=`"$Config`"" -PassThru
try {
  $main = Wait-Until { Find-Window $p.Id "RestoreSafeMainWindow" } 20 "RestoreSafe window"
  Wait-Until { (Text-Of $main HeroTitle) -and (Text-Of $main HeroTitle) -notlike "Checking*" } 60 "health check" | Out-Null

  # The full backup, with new keys.
  Click-Control $main HeroPrimary 60
  $plan = Wait-Until { Find-Window $p.Id "RestoreSafePlan" } 20 "Create backup window"
  Click-Control $plan PlanStart 120
  Answer-Credentials $p.Id $Password { Find-Control $main RunDone -Enabled }
  Finish $main

  # Changes for the differentials: a copy of each folder's newest file.
  foreach ($src in $sources) {
    $f = Get-ChildItem -Recurse -File $src | Sort-Object LastWriteTime | Select-Object -Last 1
    $copy = Join-Path $f.DirectoryName ($f.BaseName + " (copy)" + $f.Extension)
    Copy-Item $f.FullName $copy
    $copies += $copy
  }

  # The differentials: the Create backup window shows DIFF 1 for each folder.
  Click-Control $main HeroPrimary 60
  $plan = Wait-Until { Find-Window $p.Id "RestoreSafePlan" } 20 "Create backup window"
  Wait-Until { Find-Control $plan PlanStart -Enabled } 120 "plan" | Out-Null
  Start-Sleep -Seconds 1
  Save-Shot $plan (Join-Path $OutDir "create-backup-window.png") $Scale
  Click-Control $plan PlanStart
  Answer-Credentials $p.Id $Password { Find-Control $main RunDone -Enabled }
  Finish $main

  # The Restore backup window on the newest run.
  Go-Page $main 1
  # The check after an operation rebuilds the list and can drop a selection
  # made just before it: select again until the action is enabled.
  $list = Wait-Until { Find-Control $main BackupsList } 10 "Backups list"
  Wait-Until { try { Select-ListItem $list 0 } catch {}; Find-Control $main BackupsRestore -Enabled } 20 "a backup selected" | Out-Null
  Click-Control $main BackupsRestore
  $win = Wait-Until { Find-Window $p.Id "RestoreSafeRestore" } 10 "Restore backup window"
  Set-Text (Wait-Until { Find-Control $win RestoreDest } 10 "destination") $restoreTo
  Wait-Until { Find-Control $win RestoreStart -Enabled } 30 "choices checked" | Out-Null
  Start-Sleep -Seconds 1
  Save-Shot $win (Join-Path $OutDir "restore-backup-window.png") $Scale
  Click-Control $win RestoreCancel
} finally {
  Stop-Process $p -Force -ErrorAction SilentlyContinue
  if ($copies) { Remove-Item $copies -ErrorAction SilentlyContinue }
}
