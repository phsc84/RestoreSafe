@echo off
REM ============================================================
REM  RestoreSafe Build Script (simple)
REM  Creates RestoreSafe ZIP and test executable
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

echo [BUILD] Prepare build directory...
set TEST_DIR=test
if not exist %TEST_DIR%\ (
    mkdir %TEST_DIR%
)

echo [BUILD] Compile RestoreSafe.exe...
set GOOS=windows
set GOARCH=amd64
set CGO_ENABLED=0

go build -trimpath -ldflags="-s -w -X main.Version=%VERSION%" -o "%TEST_DIR%\RestoreSafe.exe" ./cmd
if errorlevel 1 (
    echo [ERROR] Compilation failed
    exit /b 1
)

set ZIP_NAME=RestoreSafe-%VERSION%.zip

echo [BUILD] Delete old ZIP archives...
for %%f in (RestoreSafe-*.zip) do (
    del /f /q "%%f"
    if errorlevel 1 (
        echo [WARN] Could not delete "%%f"
    )
)

echo [BUILD] Create %ZIP_NAME% ...
powershell -NoProfile -Command "Compress-Archive -Path '%TEST_DIR%\RestoreSafe.exe','config-SAMPLE.yaml' -DestinationPath '%ZIP_NAME%' -Force"
if errorlevel 1 (
    echo [ERROR] Failed to create %ZIP_NAME%
    exit /b 1
)

echo.
echo [OK] Successfully compiled: %CD%\%TEST_DIR%\RestoreSafe.exe
echo [OK] Successfully created: %CD%\%ZIP_NAME%
echo.

endlocal
