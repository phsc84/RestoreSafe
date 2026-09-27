# Smoke-BackupRestore.ps1 - runs a backup, a restore of the newest backup run,
# and a verification through the RestoreSafe window, and compares the
# restored files with the sources.
#
# Use a test configuration with authentication_mode 1 (password only); the
# script cannot answer YubiKey prompts. It answers new-key setup (password
# twice) and a recovery code (reads it from the dialog and types it back).
#
# .\Smoke-BackupRestore.ps1 -Exe ..\..\dist\RestoreSafe.exe -Config C:\...\config.yaml `
#     -Password "correct horse battery" -RestoreTo C:\...\restored [-ScreenshotDir C:\...\shots]
#
# Exit code 0 when every step succeeded and the restored files match.

param(
  [Parameter(Mandatory)][string]$Exe,
  [Parameter(Mandatory)][string]$Config,
  [Parameter(Mandatory)][string]$Password,
  [Parameter(Mandatory)][string]$RestoreTo,
  [string]$ScreenshotDir = ""
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
# Answer-Credentials answers the questions after a start: new or existing
# password, recovery code shown at key setup, and the unlock method (regular).
function Answer-Credentials($procId, $main) {
  $end = (Get-Date).AddSeconds(120)
  while ((Get-Date) -lt $end) {
    if (Find-Button $main "&Back to start") { return }
    $dlg = Find-Window $procId "RestoreSafeInputDialog"
    if ($dlg) {
      $edits = @([U]::Children($dlg) | Where-Object { [U]::Class($_) -eq "Edit" })
      if ([U]::Text($dlg) -like "*recovery code*" -and $edits.Count -eq 0) {
        # Key setup shows the recovery code: read it, then confirm it.
        $script:recoveryCode = ([U]::Children($dlg) | Where-Object { [U]::Class($_) -eq "Static" } | ForEach-Object { [U]::Text($_) } |
          Where-Object { $_ -match '^[0-9A-Z-]+\r\n[0-9A-Z-]+$' } | Select-Object -First 1) -replace "`r`n", "-"
        Snap $dlg "recovery-code"
        [U]::PostMessage($dlg, 0x0111, [IntPtr]1, [IntPtr]::Zero) | Out-Null
      } elseif ([U]::Text($dlg) -like "*recovery code*") {
        Fill-Dialog $dlg @($script:recoveryCode)
      } else {
        Fill-Dialog $dlg (@($Password) * $edits.Count)
      }
      Start-Sleep -Milliseconds 800
      continue
    }
    $task = [U]::TopLevel($procId) | Where-Object { [U]::Class($_) -eq "#32770" } | Select-Object -First 1
    if ($task) { Press-Keys $task "{ENTER}"; Start-Sleep -Milliseconds 800; continue }  # unlock method: regular credentials
    Start-Sleep -Milliseconds 300
  }
  throw "timeout waiting for the result screen"
}

$failed = $false
$p = Start-Process $Exe -ArgumentList "-config=`"$Config`"" -PassThru
try {
  $main = Wait-Until { Find-Window $p.Id "RestoreSafeMainWindow" } 20 "RestoreSafe window"
  Wait-Until { Find-Button $main "Create &backup" } 60 "health check" | Out-Null
  Snap $main "home"

  # Backup as planned.
  Click (Find-Button $main "Create &backup")
  Wait-Until { Find-Button $main "&Start backup" } 120 "backup preflight" | Out-Null
  Snap $main "backup-preflight"
  Click (Find-Button $main "&Start backup")
  Answer-Credentials $p.Id $main
  $r = Result-Line $main; Snap $main "backup-result"; "Backup:  $r"
  if ($r -notmatch "completed") { $failed = $true }

  # Restore of the newest backup run.
  Click (Find-Button $main "&Back to start")
  Click (Wait-Until { Find-Button $main "&Restore backup" } 60 "Restore enabled")
  Wait-Until { Find-Button $main "&Restore selected" } 30 "selection" | Out-Null
  Snap $main "selection"
  Click (Find-Button $main "&Restore selected")
  Wait-Until { Find-Button $main "&Browse..." } 10 "destination" | Out-Null
  $edit = [U]::Children($main) | Where-Object { [U]::Class($_) -eq "Edit" -and [U]::IsWindowVisible($_) } | Select-Object -First 1
  Set-EditText $main $edit 202 $RestoreTo
  Click (Wait-Until { Find-Button $main "&Next" } 10 "Next enabled")
  Wait-Until { Find-Button $main "&Start restore" } 60 "restore preflight" | Out-Null
  Snap $main "restore-preflight"
  Click (Find-Button $main "&Start restore")
  Answer-Credentials $p.Id $main
  $r = Result-Line $main; Snap $main "restore-result"; "Restore: $r"
  if ($r -notmatch "completed") { $failed = $true }

  # Verification of the newest backup run.
  Click (Find-Button $main "&Back to start")
  Click (Wait-Until { Find-Button $main "&Verify backup" } 60 "Verify enabled")
  Click (Wait-Until { Find-Button $main "&Verify selected" } 30 "selection")
  Click (Wait-Until { Find-Button $main "&Start verification" } 60 "verify preflight")
  Answer-Credentials $p.Id $main
  $r = Result-Line $main; Snap $main "verify-result"; "Verify:  $r"
  if ($r -notmatch "completed") { $failed = $true }
} catch {
  "FAILED: $_"
  $failed = $true
} finally {
  Stop-Process $p -Force -ErrorAction SilentlyContinue
}

# Compare the restored files with the sources of the configuration.
$sources = Select-String -Path $Config -Pattern '^\s*-\s*"(.+)"\s*$' | Where-Object { $_.Line -notmatch '^\s*#' } |
  ForEach-Object { $_.Matches[0].Groups[1].Value }
foreach ($src in $sources) {
  $name = Split-Path $src -Leaf
  $dst = Join-Path $RestoreTo $name
  if (-not (Test-Path $dst)) { continue }  # only the sets of the newest run are restored
  $srcFiles = Get-ChildItem -Recurse -File $src | ForEach-Object { $_.FullName.Substring($src.Length) }
  $diff = foreach ($rel in $srcFiles) {
    $b = Join-Path $dst $rel
    if (-not (Test-Path $b) -or (Get-FileHash (Join-Path $src $rel)).Hash -ne (Get-FileHash $b).Hash) { $rel }
  }
  if ($diff) { "Compare: $name differs: $($diff -join ', ')"; $failed = $true } else { "Compare: $name identical ($($srcFiles.Count) files)" }
}

if ($failed) { exit 1 }
exit 0
