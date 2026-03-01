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

echo [BUILD] Compile RestoreSafe.exe...
set GOOS=windows
set GOARCH=amd64
set CGO_ENABLED=0

go build -trimpath -ldflags="-s -w" -o RestoreSafe.exe ./cmd
if errorlevel 1 (
    echo [ERROR] Compilation failed
    exit /b 1
)

echo.
echo [OK] RestoreSafe.exe successfully created.
echo.

endlocal
