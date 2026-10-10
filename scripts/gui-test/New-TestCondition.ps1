# New-TestCondition.ps1 - copies a test setup made by Smoke-BackupRestore.ps1
# (its config.yaml and backup directory, after at least two backups) to
# -Out and turns the copy into one of the conditions of spec 3.5 and 11.8.
# The condition names are those of the Go fixtures
# (internal/testutil/scenario). The original setup is not changed.
#
# .\New-TestCondition.ps1 -Config C:\...\smoke\config.yaml -Condition BaseMissing -Out C:\...\cond\BaseMissing
#
# It prints the path of the new config.yaml. Overdue has no variant: a
# backup's date is in its encrypted header (use a backup from earlier days).

param(
  [Parameter(Mandatory)][string]$Config,
  [Parameter(Mandatory)][ValidateSet("Protected", "Empty", "SkippedFiles", "BaseMissing", "SourceMissing", "BackupDirUnreachable",
    "IncompleteNewest", "VerifyFailed", "Argon2Capped", "NewKeysNeeded", "Legacy1x", "LeftoverTmp", "FolderNotBackedUp")][string]$Condition,
  [Parameter(Mandatory)][string]$Out
)
$ErrorActionPreference = "Stop"

$text = Get-Content $Config -Raw -Encoding UTF8
if ($text -notmatch '(?m)^backup_directory:\s*"?([^"\r\n]+)"?\s*$') { throw "no backup_directory in $Config" }
$source = $Matches[1].Trim()
if (Test-Path $Out) { Remove-Item -Recurse -Force $Out }
$backups = Join-Path $Out "backups"
New-Item -ItemType Directory -Force $backups | Out-Null
if ($Condition -ne "Empty") { Copy-Item -Path (Join-Path $source "*") -Destination $backups -Recurse }
$text = $text -replace '(?m)^backup_directory:.*$', ('backup_directory: "' + ($backups -replace '\\', '/') + '"')

# The newest run's log and the sets it wrote (from its set facts).
function Newest-Log { Get-ChildItem $backups -Filter "*.log" | Sort-Object LastWriteTime | Select-Object -Last 1 }
function Sets-Of($log) {
  Select-String -Path $log.FullName -Pattern '"kind":"set".*"set":"([^"]+)"' | ForEach-Object { $_.Matches[0].Groups[1].Value }
}
function Add-Fact($log, [string]$json) {
  Add-Content -Path $log.FullName -Value ("[{0}] FACT  - {1}" -f (Get-Date -Format "yyyy-MM-dd HH:mm:ss"), $json) -Encoding UTF8
}
# Parts-Of returns the part files of a set name "Folder_CHAIN_DATE_TYPE".
function Parts-Of([string]$set) {
  if ($set -notmatch '^(.+)_([A-Z0-9]{6})_(\d{4}-\d{2}-\d{2})_(FULL|DIFF\d{3})$') { throw "not a set name: $set" }
  $prefix = "[{0}]_{1}_{2}_{3}-" -f $Matches[1], $Matches[2], $Matches[3], $Matches[4]
  Get-ChildItem $backups -Filter "*.enc" | Where-Object { $_.Name.StartsWith($prefix) } | Sort-Object Name
}
function Add-Source([string]$path) {
  $script:text = $script:text -replace '(?m)^source_directories:\s*$', ("source_directories:`n  - `"" + ($path -replace '\\', '/') + '"')
}

switch ($Condition) {
  "SkippedFiles" {
    $log = Newest-Log; $set = Sets-Of $log | Select-Object -First 1
    Add-Fact $log ('{"kind":"set","result":"warnings","set":"' + $set + '","skipped":2}')
  }
  "VerifyFailed" {
    $log = Newest-Log; $set = Sets-Of $log | Select-Object -First 1
    Add-Fact $log ('{"kind":"verify","result":"failed","set":"' + $set + '","error":"checksum mismatch (test condition)"}')
  }
  "BaseMissing" {
    $diff = Get-ChildItem $backups -Filter "*_DIFF*-001.enc" | Select-Object -First 1
    if (-not $diff) { throw "no differential: run Smoke-BackupRestore.ps1 with at least two backups" }
    if ($diff.Name -notmatch '^\[(.+)\]_([A-Z0-9]{6})_') { throw "unexpected part name $($diff.Name)" }
    $fullPrefix = "[{0}]_{1}_" -f $Matches[1], $Matches[2]
    Get-ChildItem $backups -Filter "*_FULL-*.enc" | Where-Object { $_.Name.StartsWith($fullPrefix) } | Remove-Item
  }
  "IncompleteNewest" {
    $set = Sets-Of (Newest-Log) | Select-Object -First 1
    $part = Parts-Of $set | Select-Object -Last 1
    $fs = [System.IO.File]::Open($part.FullName, "Open", "ReadWrite")
    $fs.SetLength([long]($fs.Length / 2)); $fs.Close()
  }
  "SourceMissing" { Add-Source (Join-Path $Out "Missing\Photos") }
  "BackupDirUnreachable" {
    $used = (Get-PSDrive -PSProvider FileSystem).Name
    $letter = [char[]]"QRSTUVWXYZ" | Where-Object { $used -notcontains [string]$_ } | Select-Object -First 1
    $text = $text -replace '(?m)^backup_directory:.*$', "backup_directory: `"$($letter):/RestoreSafe-test`""
  }
  "Argon2Capped" {
    # Raise memory_mb in an existing argon2 block; a second block would make
    # the configuration invalid instead of capped.
    if ($text -match '(?m)^\s+memory_mb:') { $text = $text -replace '(?m)^(\s+memory_mb:).*$', '$1 99999' }
    else { $text += "`nargon2:`n  memory_mb: 99999`n" }
  }
  "NewKeysNeeded" {
    if ($text -match '(?m)^recovery_code:\s*true') { $text = $text -replace '(?m)^recovery_code:\s*true', 'recovery_code: false' }
    else { $text = ($text -replace '(?m)^recovery_code:.*$', '') + "`nrecovery_code: true`n" }
  }
  "Legacy1x" { Set-Content -LiteralPath (Join-Path $backups "[Documents]_2025-01-01_OLD001-001.enc") -Value "1.x data" }
  "LeftoverTmp" { Set-Content -LiteralPath (Join-Path $backups "[Documents]_ZZZ999_2025-01-01_FULL-001.enc.tmp") -Value "partial" }
  "FolderNotBackedUp" {
    $music = Join-Path $Out "Music"
    New-Item -ItemType Directory -Force $music | Out-Null
    Set-Content -Path (Join-Path $music "song.txt") -Value "la la"
    Add-Source $music
  }
}

$cfg = Join-Path $Out "config.yaml"
[System.IO.File]::WriteAllText($cfg, $text, (New-Object System.Text.UTF8Encoding $false))
$cfg
