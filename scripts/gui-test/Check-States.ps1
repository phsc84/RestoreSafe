# Check-States.ps1 - for each condition of spec 3.5 and 11.8: makes it with
# New-TestCondition.ps1 from a test setup of Smoke-BackupRestore.ps1, starts
# RestoreSafe on it, reads the hero's title and primary action, compares
# them with the expected values (those of the Go view tests), and saves a
# screenshot (spec 16.4).
#
# .\Check-States.ps1 -Exe ..\..\sandbox\RestoreSafe.exe -Config C:\...\smoke\config.yaml -Out C:\...\states
#
# Exit code 0 when every condition shows what it should.

param(
  [Parameter(Mandatory)][string]$Exe,
  [Parameter(Mandatory)][string]$Config,
  [Parameter(Mandatory)][string]$Out,
  [string]$Only = ""  # comma-separated conditions; all when empty
)
. "$PSScriptRoot\GuiDriver.ps1"

$backUp = "&Back up now…"
$expected = [ordered]@{
  Protected            = @("Your folders are protected", $backUp)
  Empty                = @("Create your first backup", $backUp)
  SkippedFiles         = @("* weren't backed up", $backUp)
  BaseMissing          = @("Ready to back up", $backUp)
  SourceMissing        = @("* can't be found", "Check &again")
  BackupDirUnreachable = @("The backup directory isn't reachable", "Check &again")
  IncompleteNewest     = @("Ready to back up", $backUp)
  VerifyFailed         = @("A backup of * is damaged", $backUp)
  Argon2Capped         = @("A key setting was too high and was capped", "&Edit config")
  NewKeysNeeded        = @("Your folders are protected", $backUp)
  Legacy1x             = @("Your folders are protected", $backUp)
  LeftoverTmp          = @("Your folders are protected", $backUp)
  FolderNotBackedUp    = @("* has no backup yet", $backUp)
}

New-Item -ItemType Directory -Force $Out | Out-Null
$failed = 0
foreach ($condition in $expected.Keys) {
  if ($Only -and ($Only -split ",") -notcontains $condition) { continue }
  $dir = Join-Path $Out $condition
  $cfg = & "$PSScriptRoot\New-TestCondition.ps1" -Config $Config -Condition $condition -Out $dir
  $p = Start-Process $Exe -ArgumentList "-config=`"$cfg`"" -PassThru
  try {
    $main = Wait-Until { Find-Window $p.Id "RestoreSafeMainWindow" } 20 "RestoreSafe window"
    $title = Wait-Until { $t = Text-Of $main HeroTitle; if ($t -and $t -notlike "Checking*") { $t } } 30 "the health check"
    Start-Sleep -Milliseconds 500
    $primary = Text-Of $main HeroPrimary
    Shot $main (Join-Path $Out "$condition.png")
    $want = $expected[$condition]
    $ok = ($title -like $want[0]) -and ($primary -eq $want[1])
    if (-not $ok) { $failed++ }
    "{0,-5} {1,-21} {2}  [{3}]" -f $(if ($ok) { "ok" } else { "FAIL" }), $condition, $title, $primary
    if (-not $ok) { "      expected: $($want[0])  [$($want[1])]" }
  } catch {
    "FAIL  {0,-21} {1}" -f $condition, $_
    $failed++
  } finally {
    Stop-Process $p -Force -ErrorAction SilentlyContinue
  }
}
if ($failed) { "$failed condition(s) failed"; exit 1 }
"all conditions ok"
exit 0
