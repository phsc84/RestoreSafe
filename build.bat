@echo off
REM ============================================================
REM  RestoreSafe build script
REM  Builds dist\RestoreSafe-<version>.zip with dist\SHA256SUMS.txt
REM  and moves the compiled RestoreSafe.exe to sandbox\ for manual
REM  testing. It changes no tracked file, and the same commit gives
REM  the same RestoreSafe.exe.
REM  The version is managed manually in build\versioninfo.json
REM ============================================================

setlocal

set VERSIONINFO=build\versioninfo.json
set DIST_DIR=dist
set SANDBOX_DIR=sandbox

echo [BUILD] Check dependencies...
go mod verify
if errorlevel 1 (
    echo [ERROR] go mod verify failed: the module cache does not match go.sum
    exit /b 1
)
REM -diff changes nothing; it fails when go.mod or go.sum is not tidy.
go mod tidy -diff
if errorlevel 1 (
    echo [ERROR] go.mod or go.sum is not tidy: run go mod tidy and commit the change
    exit /b 1
)

echo [BUILD] Generate resources (icon, manifest, version information)...
go tool goversioninfo -64 -o cmd/restoresafe/resource.syso %VERSIONINFO%
if errorlevel 1 (
    echo [ERROR] goversioninfo failed
    exit /b 1
)

echo [BUILD] Extract version from %VERSIONINFO%...
for /f "delims=" %%i in ('powershell -NoProfile -Command "(Get-Content '%VERSIONINFO%' | ConvertFrom-Json).StringFileInfo.ProductVersion"') do set VERSION=%%i
if not defined VERSION (
    echo [WARN] Could not extract version, using fallback
    set VERSION=dev
)
echo [BUILD] Version: %VERSION%

echo [BUILD] Prepare %DIST_DIR% directory...
if not exist %DIST_DIR%\ (
    mkdir %DIST_DIR%
)

echo [BUILD] Compile RestoreSafe.exe...
set GOOS=windows
set GOARCH=amd64
set CGO_ENABLED=0

REM -H=windowsgui: a window application; no console window opens.
go build -trimpath -ldflags="-s -w -H=windowsgui -X RestoreSafe/internal/buildinfo.Version=%VERSION%" -o "%DIST_DIR%\RestoreSafe.exe" ./cmd/restoresafe
if errorlevel 1 (
    echo [ERROR] Compilation failed
    exit /b 1
)

set ZIP_NAME=RestoreSafe-%VERSION%.zip

echo [BUILD] Delete old ZIP archives in %DIST_DIR%...
for %%f in (%DIST_DIR%\RestoreSafe-*.zip) do (
    del /f /q "%%f"
    if errorlevel 1 (
        echo [WARN] Could not delete "%%f"
    )
)

echo [BUILD] Create %ZIP_NAME%...
powershell -NoProfile -Command "Compress-Archive -Path '%DIST_DIR%\RestoreSafe.exe','config-SAMPLE.yaml' -DestinationPath '%DIST_DIR%\%ZIP_NAME%' -Force"
if errorlevel 1 (
    echo [ERROR] Failed to create %ZIP_NAME%
    exit /b 1
)

echo [BUILD] Write SHA256SUMS.txt...
REM The format of sha256sum: "<hash>  <file>", one line, LF.
powershell -NoProfile -Command "$h = (Get-FileHash -Algorithm SHA256 '%DIST_DIR%\%ZIP_NAME%').Hash.ToLower(); [IO.File]::WriteAllText('%CD%\%DIST_DIR%\SHA256SUMS.txt', $h + '  %ZIP_NAME%' + [char]10)"
if errorlevel 1 (
    echo [ERROR] Failed to write SHA256SUMS.txt
    exit /b 1
)

echo [BUILD] Move RestoreSafe.exe to %SANDBOX_DIR%...
if not exist %SANDBOX_DIR%\ (
    mkdir %SANDBOX_DIR%
)
move /y "%DIST_DIR%\RestoreSafe.exe" "%SANDBOX_DIR%\RestoreSafe.exe" >nul
if errorlevel 1 (
    echo [ERROR] Failed to move RestoreSafe.exe to %SANDBOX_DIR%
    exit /b 1
)

echo.
echo [OK] Successfully created: %CD%\%DIST_DIR%\%ZIP_NAME%
echo [OK] Checksum: %CD%\%DIST_DIR%\SHA256SUMS.txt
echo [OK] Successfully compiled: %CD%\%SANDBOX_DIR%\RestoreSafe.exe
echo.

endlocal
