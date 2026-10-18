# README-Screenshots.ps1 - saves the five screenshots of the README to
# docs\images, cropped to the visible frame and scaled (0.667 at 150 %
# display scaling):
#
#   create-backup-running.png  the full backup, about halfway through a folder
#   create-backup-window.png   the Create backup window of the differentials
#   create-backup.png          the Create backup page after them
#   restore-backup.png         the Restore backup page, newest run selected
#   restore-backup-window.png  the Restore backup window on that run
#
# Use a password-only test configuration (authentication_mode 1) whose
# backup directory doesn't exist yet, with one source folder large enough
# for the progress to be caught (the README demo has 1.4 GB of pictures).
# Each source folder gets a copy of its newest file for the differentials;
# the copies are removed at the end. The Restore backup window shows
# "Restore" next to config.yaml as destination, which must not exist;
# nothing is restored.
#
# .\README-Screenshots.ps1 -Exe ..\..\sandbox\RestoreSafe.exe -Config C:\...\config.yaml `
#     -Password "correct horse battery" [-OutDir ..\..\docs\images] [-Scale 0.667]

param(
  [Parameter(Mandatory)][string]$Exe,
  [Parameter(Mandatory)][string]$Config,
  [Parameter(Mandatory)][string]$Password,
  [string]$OutDir = "$PSScriptRoot\..\..\docs\images",
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

# Save hides the access-key underlines and focus rectangles that the
# driver's keystrokes turned on (WM_CHANGEUISTATE, UIS_SET, UISF_HIDEFOCUS |
# UISF_HIDEACCEL), as a mouse user sees the window, and saves it.
function Save($hwnd, [string]$name) {
  [U]::SendMessage($hwnd, 0x0127, [IntPtr]0x30001, [IntPtr]::Zero) | Out-Null
  Start-Sleep -Milliseconds 300
  Save-Shot $hwnd (Join-Path $OutDir $name) $Scale
}

# Halfway returns true while the progress card shows a folder between 40 %
# and 80 % of its progress bar (PBM_GETPOS, PBM_GETRANGE).
function Halfway($main) {
  $bar = [U]::Children($main) | Where-Object { [U]::Class($_) -eq "msctls_progress32" -and [U]::IsWindowVisible($_) } | Select-Object -First 1
  if (-not $bar) { return $false }
  $range = [U]::SendMessage($bar, 0x0407, [IntPtr]::Zero, [IntPtr]::Zero).ToInt64()
  if ($range -le 0) { return $false }
  $f = [U]::SendMessage($bar, 0x0408, [IntPtr]::Zero, [IntPtr]::Zero).ToInt64() / $range
  $f -ge 0.4 -and $f -le 0.8
}

# Settled waits until the health check after an operation has finished.
function Settled($main) {
  Wait-Until { (Text-Of $main HeroTitle) -and (Text-Of $main HeroTitle) -notlike "Checking*" } 60 "health check" | Out-Null
  Start-Sleep -Seconds 1
}

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
  Settled $main

  # The full backup, with new keys; the screenshot while it runs.
  Click-Control $main HeroPrimary 60
  $plan = Wait-Until { Find-Window $p.Id "RestoreSafePlan" } 20 "Create backup window"
  Click-Control $plan PlanStart 120
  Answer-Credentials $p.Id $Password { Halfway $main }
  Save $main "create-backup-running.png"
  Finish $main
  Settled $main

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
  Save $plan "create-backup-window.png"
  Click-Control $plan PlanStart
  Answer-Credentials $p.Id $Password { Find-Control $main RunDone -Enabled }
  Finish $main
  Settled $main
  Save $main "create-backup.png"

  # The Restore backup page with the newest run selected.
  Go-Page $main 1
  # The check after an operation rebuilds the list and can drop a selection
  # made just before it: select again until the action is enabled.
  $list = Wait-Until { Find-Control $main BackupsList } 10 "Backups list"
  Wait-Until { try { Select-ListItem $list 0 } catch {}; Find-Control $main BackupsRestore -Enabled } 20 "a backup selected" | Out-Null
  Start-Sleep -Seconds 1
  Save $main "restore-backup.png"

  # The Restore backup window on it.
  Click-Control $main BackupsRestore
  $win = Wait-Until { Find-Window $p.Id "RestoreSafeRestore" } 10 "Restore backup window"
  Set-Text (Wait-Until { Find-Control $win RestoreDest } 10 "destination") $restoreTo
  Wait-Until { Find-Control $win RestoreStart -Enabled } 30 "choices checked" | Out-Null
  Start-Sleep -Seconds 1
  Save $win "restore-backup-window.png"
  Click-Control $win RestoreCancel
} finally {
  Stop-Process $p -Force -ErrorAction SilentlyContinue
  if ($copies) { Remove-Item $copies -ErrorAction SilentlyContinue }
}
