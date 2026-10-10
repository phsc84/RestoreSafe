@echo off
REM ============================================================
REM  RestoreSafe development build
REM  Compiles sandbox\RestoreSafe.exe for manual testing, with
REM  the same resources, flags and version as build.bat. It
REM  doesn't touch dist\: the release archive and its checksum
REM  are left as they are. Releases are built with build.bat.
REM ============================================================

setlocal

set VERSIONINFO=build\versioninfo.json
set SANDBOX_DIR=sandbox

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

if not exist %SANDBOX_DIR%\ (
    mkdir %SANDBOX_DIR%
)

echo [BUILD] Compile RestoreSafe.exe...
set GOOS=windows
set GOARCH=amd64
set CGO_ENABLED=0

REM The same command as in build.bat; keep them equal.
go build -trimpath -ldflags="-s -w -H=windowsgui -X github.com/phsc84/restoresafe/internal/buildinfo.Version=%VERSION%" -o "%SANDBOX_DIR%\RestoreSafe.exe" ./cmd/restoresafe
if errorlevel 1 (
    echo [ERROR] Compilation failed
    exit /b 1
)

echo.
echo [OK] Successfully compiled: %CD%\%SANDBOX_DIR%\RestoreSafe.exe
echo.

endlocal
