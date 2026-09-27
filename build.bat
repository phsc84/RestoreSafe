@echo off
REM ============================================================
REM  RestoreSafe build script
REM  Builds dist\RestoreSafe.exe and dist\RestoreSafe-<version>.zip
REM  The version is managed manually in build\windows\versioninfo.json
REM ============================================================

setlocal

set VERSIONINFO=build\windows\versioninfo.json
set DIST_DIR=dist

echo [BUILD] Load dependencies...
go mod tidy
if errorlevel 1 (
    echo [ERROR] go mod tidy failed
    exit /b 1
)

echo [BUILD] Generate resources (icon, manifest, version information)...
goversioninfo -64 -o cmd/resource.syso %VERSIONINFO%
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
go build -trimpath -ldflags="-s -w -H=windowsgui -X main.Version=%VERSION%" -o "%DIST_DIR%\RestoreSafe.exe" ./cmd
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

echo.
echo [OK] Successfully compiled: %CD%\%DIST_DIR%\RestoreSafe.exe
echo [OK] Successfully created: %CD%\%DIST_DIR%\%ZIP_NAME%
echo.

endlocal
