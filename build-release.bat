@echo off
REM ============================================================
REM  RestoreSafe release build
REM  Brings the local main up to date with origin/main, switches to
REM  it, and builds there: dist\RestoreSafe-<version>.zip with
REM  dist\SHA256SUMS.txt, a copy of release-notes\<version>.md as
REM  dist\RELEASE-NOTES-<version>.md, and the compiled RestoreSafe.exe
REM  in sandbox\ for manual testing. Then it switches back to the
REM  branch it started on, also when the build fails.
REM  It needs a clean working tree, changes no tracked file, and the
REM  same commit gives the same RestoreSafe.exe.
REM  The version is managed manually in build\versioninfo.json
REM ============================================================

REM git switch replaces this file while it runs, and cmd reads a
REM batch file from disk line by line. So the build runs from a copy
REM in %TEMP%; without call, the copy takes over and this file is not
REM read again.
if /i not "%~1"=="--from-temp" (
    copy /y "%~f0" "%TEMP%\RestoreSafe-build-release.bat" >nul
    if errorlevel 1 (
        echo [ERROR] Could not copy build-release.bat to %TEMP%
        exit /b 1
    )
    "%TEMP%\RestoreSafe-build-release.bat" --from-temp "%~dp0."
)

setlocal
cd /d "%~2"
if errorlevel 1 (
    echo [ERROR] Could not change to the repository "%~2"
    exit /b 1
)

set VERSIONINFO=build\versioninfo.json
set DIST_DIR=dist
set SANDBOX_DIR=sandbox
set RELEASE_BRANCH=main

echo [GIT] Check the working tree...
git rev-parse --is-inside-work-tree >nul 2>&1
if errorlevel 1 (
    echo [ERROR] "%CD%" is not a Git working tree
    exit /b 1
)
REM go build stamps uncommitted and untracked files as vcs.modified
REM into the exe, and git switch would carry them over to main.
for /f "delims=" %%i in ('git status --porcelain') do (
    echo [ERROR] The working tree is not clean: commit or stash your changes first
    exit /b 1
)

set START_BRANCH=
for /f "delims=" %%i in ('git branch --show-current') do set START_BRANCH=%%i
if not defined START_BRANCH (
    echo [ERROR] HEAD is detached: switch to a branch first
    exit /b 1
)

echo [GIT] Update %RELEASE_BRANCH% from origin...
REM fetch into the branch that is checked out is refused; pull instead.
if "%START_BRANCH%"=="%RELEASE_BRANCH%" (
    git pull --ff-only origin %RELEASE_BRANCH%
) else (
    git fetch origin %RELEASE_BRANCH%:%RELEASE_BRANCH%
)
if errorlevel 1 (
    echo [ERROR] Could not bring %RELEASE_BRANCH% up to date with origin/%RELEASE_BRANCH%
    exit /b 1
)

REM pull --ff-only accepts a local main that is ahead of origin/main;
REM the release is exactly what origin/main holds, nothing else.
set LOCAL_COMMIT=
set REMOTE_COMMIT=
for /f "delims=" %%i in ('git rev-parse refs/heads/%RELEASE_BRANCH%') do set LOCAL_COMMIT=%%i
for /f "delims=" %%i in ('git rev-parse refs/remotes/origin/%RELEASE_BRANCH%') do set REMOTE_COMMIT=%%i
if not defined REMOTE_COMMIT (
    echo [ERROR] origin/%RELEASE_BRANCH% is unknown: check the remote origin
    exit /b 1
)
if not "%LOCAL_COMMIT%"=="%REMOTE_COMMIT%" (
    echo [ERROR] %RELEASE_BRANCH% has commits that origin/%RELEASE_BRANCH% doesn't: push them through a pull request, or reset %RELEASE_BRANCH% to origin/%RELEASE_BRANCH%
    exit /b 1
)

if not "%START_BRANCH%"=="%RELEASE_BRANCH%" (
    echo [GIT] Switch from %START_BRANCH% to %RELEASE_BRANCH%...
    git switch %RELEASE_BRANCH%
    if errorlevel 1 (
        echo [ERROR] Could not switch to %RELEASE_BRANCH%
        exit /b 1
    )
)

call :build
set BUILD_RESULT=%errorlevel%

if not "%START_BRANCH%"=="%RELEASE_BRANCH%" (
    echo [GIT] Switch back to %START_BRANCH%...
    git switch %START_BRANCH%
    if errorlevel 1 (
        echo [ERROR] Could not switch back to %START_BRANCH%
        exit /b 1
    )
)

exit /b %BUILD_RESULT%

REM ------------------------------------------------------------
REM  The build of the checked-out commit. exit /b returns to the
REM  caller above, which switches back to the start branch.
REM ------------------------------------------------------------
:build
echo [BUILD] Check dependencies...
REM Only the success line goes to stdout; mismatches still print on stderr.
go mod verify >nul
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
set VERSION=
for /f "delims=" %%i in ('powershell -NoProfile -Command "(Get-Content '%VERSIONINFO%' | ConvertFrom-Json).StringFileInfo.ProductVersion"') do set VERSION=%%i
if not defined VERSION (
    echo [WARN] Could not extract version, using fallback
    set VERSION=dev
)
echo [BUILD] Version: %VERSION%

set NOTES=release-notes\%VERSION%.md
if not exist %NOTES% (
    echo [ERROR] %NOTES% is missing: write the release notes first
    exit /b 1
)

echo [BUILD] Check tag v%VERSION%...
REM A tag of this version on another commit means the version was not
REM raised: the build would replace the released archive in dist\.
for /f "delims=" %%i in ('git rev-parse HEAD') do set BUILD_COMMIT=%%i
set TAG_COMMIT=
for /f "delims=" %%i in ('git rev-list -n 1 refs/tags/v%VERSION% 2^>nul') do set TAG_COMMIT=%%i
if defined TAG_COMMIT if not "%TAG_COMMIT%"=="%BUILD_COMMIT%" (
    echo [ERROR] Tag v%VERSION% marks another commit: set the new version in %VERSIONINFO% first
    exit /b 1
)

echo [BUILD] Prepare %DIST_DIR% directory...
if not exist %DIST_DIR%\ (
    mkdir %DIST_DIR%
)

echo [BUILD] Compile RestoreSafe.exe...
set GOOS=windows
set GOARCH=amd64
set CGO_ENABLED=0

REM -H=windowsgui: a window application; no console window opens.
REM build-dev.bat has the same command; keep them equal.
go build -trimpath -ldflags="-s -w -H=windowsgui -X github.com/phsc84/restoresafe/internal/buildinfo.Version=%VERSION%" -o "%DIST_DIR%\RestoreSafe.exe" ./cmd/restoresafe
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

echo [BUILD] Copy %NOTES% to %DIST_DIR%...
for %%f in (%DIST_DIR%\RELEASE-NOTES-*.md) do (
    del /f /q "%%f"
    if errorlevel 1 (
        echo [WARN] Could not delete "%%f"
    )
)
copy /y "%NOTES%" "%DIST_DIR%\RELEASE-NOTES-%VERSION%.md" >nul
if errorlevel 1 (
    echo [ERROR] Failed to copy %NOTES% to %DIST_DIR%
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
echo [OK] Release notes: %CD%\%DIST_DIR%\RELEASE-NOTES-%VERSION%.md
echo [OK] Successfully compiled: %CD%\%SANDBOX_DIR%\RestoreSafe.exe
if defined TAG_COMMIT (
    echo [OK] Tag v%VERSION% marks the built commit %BUILD_COMMIT%
) else (
    echo [NEXT] Tag the built commit:
    echo        git tag -a v%VERSION% %BUILD_COMMIT% -m "Release version %VERSION%"
    echo        git push origin v%VERSION%
)
echo.
exit /b 0
