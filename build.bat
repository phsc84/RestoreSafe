@echo off
REM ============================================================
REM  RestoreSafe Build Script (simple)
REM  Creates RestoreSafe.exe for Windows 64-bit
REM  Version is managed manually in versioninfo.json
REM ============================================================

setlocal

echo [BUILD] Load dependencies...
go mod tidy
if errorlevel 1 (
    echo [ERROR] go mod tidy failed
    exit /b 1
)

echo [BUILD] Generate resources (icon + versioninfo)...
goversioninfo -64 -o cmd/resource.syso versioninfo.json
if errorlevel 1 (
    echo [ERROR] goversioninfo failed
    exit /b 1
)

echo [BUILD] Extract version from versioninfo.json...
for /f "delims=" %%i in ('powershell -NoProfile -Command "(Get-Content versioninfo.json | ConvertFrom-Json).StringFileInfo.ProductVersion"') do set VERSION=%%i
if not defined VERSION (
    echo [WARN] Could not extract version, using fallback
    set VERSION=dev
)
echo [BUILD] Version: %VERSION%

echo [BUILD] Compile RestoreSafe.exe...
set GOOS=windows
set GOARCH=amd64
set CGO_ENABLED=0

go build -trimpath -ldflags="-s -w -X main.Version=%VERSION%" -o RestoreSafe.exe ./cmd
if errorlevel 1 (
    echo [ERROR] Compilation failed
    exit /b 1
)

set ZIP_NAME=RestoreSafe-%VERSION%.zip

echo [BUILD] Create %ZIP_NAME% ...
powershell -NoProfile -Command "Compress-Archive -Path 'RestoreSafe.exe','assets\ykman.exe','config-SAMPLE.yaml' -DestinationPath '%ZIP_NAME%' -Force"
if errorlevel 1 (
    echo [ERROR] Failed to create %ZIP_NAME%
    exit /b 1
)

echo [BUILD] Extract %ZIP_NAME% to test directory...
if not exist test\ (
    mkdir test
)
powershell -NoProfile -Command "Expand-Archive -Path '%ZIP_NAME%' -DestinationPath 'test' -Force"
if errorlevel 1 (
    echo [ERROR] Failed to extract %ZIP_NAME% to test directory
    exit /b 1
)

echo.
echo [OK] RestoreSafe.exe successfully compiled.
echo [OK] %ZIP_NAME% successfully created and extracted to test directory.
echo.

endlocal
