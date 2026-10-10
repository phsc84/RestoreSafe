# Screenshot.ps1 - starts RestoreSafe, waits, and saves a screenshot of its
# window, cropped to the visible frame and optionally scaled (e.g. 0.667 for
# the README screenshots, taken at 150 %).
#
# .\Screenshot.ps1 -Exe ..\..\sandbox\RestoreSafe.exe -ExeArgs '-config="C:\path\config.yaml"' -Out home.png [-Wait 5] [-Scale 0.667]

param(
  [Parameter(Mandatory)][string]$Exe,
  [string]$ExeArgs = "",
  [Parameter(Mandatory)][string]$Out,
  [int]$Wait = 5,
  [double]$Scale = 1.0
)
. "$PSScriptRoot\GuiDriver.ps1"

$p = if ($ExeArgs) { Start-Process $Exe -ArgumentList $ExeArgs -PassThru } else { Start-Process $Exe -PassThru }
try {
  $main = Wait-Until { Find-Window $p.Id "RestoreSafeMainWindow" } 20 "RestoreSafe window"
  Start-Sleep -Seconds $Wait
  Save-Shot $main $Out $Scale
} finally {
  Stop-Process $p -Force -ErrorAction SilentlyContinue
}
