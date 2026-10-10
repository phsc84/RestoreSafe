# RestoreSafe – Development setup guide (Windows)

This guide covers a complete Windows development setup for RestoreSafe.

## 1. Prerequisites

- Windows 10/11 (64-bit)
- Administrator rights for software installation
- Internet access for tools and dependencies

## 2. Install Go, Visual Studio Code, Git, and GitHub CLI

Recommended (via winget):

```powershell
winget install --id GoLang.Go -e
winget install --id Microsoft.VisualStudioCode -e
winget install --id Git.Git -e
winget install --id GitHub.cli -e
```

Alternative official installers:

- Go: https://go.dev/dl/
- Visual Studio Code: https://code.visualstudio.com/
- Git for Windows: https://git-scm.com/download/win
- GitHub CLI: https://cli.github.com/

Verify the installation:

```powershell
go version
code --version
git --version
gh --version
```

Sign in to GitHub once, so `gh` can open pull requests and read CI runs:

```powershell
gh auth login
```

## 3. VS Code setup

Open the project folder in VS Code (example: `C:\dev\RestoreSafe`).

Install the extensions using one of the following methods.

**A) Extensions view**

1. Open VS Code.
2. Click the Extensions icon in the left sidebar (or press `Ctrl+Shift+X`).
3. Search for `Go`.
4. Install the extension `Go` by `Go Team at Google`.
5. Optional: search for and install `GitLens`.

**B) Command Palette**

1. Press `Ctrl+Shift+P`.
2. Run `Extensions: Install Extensions`.
3. Search `Go` and install `Go Team at Google`.

After installing `Go`, if prompted to install Go tooling, click `Install All`.

Download the dependencies in the project root (this changes no file; `go mod tidy` would, and `build-release.bat` refuses an untidy `go.mod`):

```powershell
cd C:\dev\RestoreSafe
go mod download
```

### First 5 minutes in VS Code

1. Open the project folder (`File > Open Folder...`).
2. If prompted, click `Yes, I trust the authors`.
3. Open the integrated terminal (`Terminal > New Terminal`).
4. Confirm the tools are available:

   ```powershell
   go version
   git --version
   ```

5. Run a quick baseline check:

   ```powershell
   go build ./...
   ```

6. Open Source Control (`Ctrl+Shift+G`) and verify Git is active.
7. Optional: run `Ctrl+Shift+P` → `Go: Install/Update Tools` and install all recommended tools.

## 4. Configure Git (one-time per machine, see `.gitconfig` for default values)

```powershell
git config --global user.name "Your Name"
git config --global user.email "your.email@example.com"
git config --global init.defaultBranch main
git config --global core.autocrlf true
```

## 5. Create the GitHub repository and connect the local project

### Step A – Create the repository on GitHub

1. Sign in at https://github.com.
2. Create a new repository (for example: `RestoreSafe`).
3. Choose the visibility (public).
4. Choose the license (GPL-3.0).

### Step B – Connect the local project

If the local folder is not a Git repository yet:

```powershell
cd C:\dev\RestoreSafe
git init
git add .
git commit -m "Initial commit"
```

Add the remote and push:

```powershell
git remote add origin https://github.com/<user>/<repo>.git
git branch -M main
git push -u origin main
```

Authentication note: GitHub accepts no passwords over HTTPS. After `gh auth login`, run `gh auth setup-git` once, so Git uses the GitHub CLI's login.

## 6. Work with Git in VS Code

1. Open Source Control (`Ctrl+Shift+G`).
2. Review the changed files.
3. Stage files (`+` icon) or stage all.
4. Enter a commit message.
5. Commit (`Ctrl+Enter` in the message box).
6. Push / Pull via the status bar or the Command Palette (`Git: Push`, `Git: Pull`).

## 7. Git quick workflow (pull, commit, push)

The workflow of the repository (branches, commits, pull requests, CI, releases) is in [CONTRIBUTING.md](CONTRIBUTING.md). Work happens on `dev` (pushed directly; CI runs on every push), never on `main`, which changes only through a release pull request; the commands below are the daily basics on `dev`.

```powershell
cd C:\dev\RestoreSafe
git pull
git status
git add .
git commit -m "Short, clear description"
git push
```

Useful commands:

```powershell
git log --oneline --decorate --graph -20
git diff
git restore --staged <file>
git checkout -- <file>
```

## 8. Create the icon with Greenfish Icon Editor Pro

Goal: create `build\RestoreSafe.ico` for the Windows executable. The sources (SVG and PNG, dark and light variants) are in `assets\icon\`.

1. Open Greenfish Icon Editor Pro.
2. Import the SVG source from `assets\icon\`.
3. Icon > Generate Windows icon from image.
4. Include at least these sizes:
   - 16x16
   - 32x32
   - 48x48
   - 256x256
5. Keep the default settings:
   - Pad with transparency to keep aspect ratio
   - Dither method: Floyd-Steinberg
6. Save as `build\RestoreSafe.ico`.

The tool for the Windows version resources, `goversioninfo`, is pinned in `go.mod` like `staticcheck`, `govulncheck`, and `deadcode`; `build-release.bat` runs it with `go tool goversioninfo`, so there is nothing to install. Update a tool like any dependency: `go get -tool <module>@latest`.

`build-release.bat` embeds the icon, the application manifest (`build\RestoreSafe.manifest`), and the version information from `build\versioninfo.json`.

## 9. Update the Go version used by the application

Do this on `dev` (CONTRIBUTING.md section 1). CI takes the Go version from `go.mod` (`go-version-file`), so `go.mod` is the only place to change it.

### Step 1 – Upgrade Go

```powershell
winget upgrade --id GoLang.Go -e
go version
```

### Step 2 – Update `go.mod`

The `go` line names the exact version (e.g. `go 1.27.2`); there is no separate `toolchain` line.

```powershell
cd C:\dev\RestoreSafe
go mod edit -go=1.27.3
```

### Step 3 – Refresh the dependencies and the tools

```powershell
go get -u ./...
go get tool
go mod tidy
```

`go get -u ./...` updates the modules the program uses; `go get tool` updates the tools pinned in the `tool` block (`goversioninfo`, `staticcheck`, `govulncheck`, `deadcode`).

### Step 4 – Validate the project

Run the checks of CONTRIBUTING.md section 4, then `build-release.bat`.

### Step 5 – Record and commit

- CHANGELOG.md: an entry under **Dependencies** for the new Go version and for every updated module built into the exe (CONTRIBUTING.md section 5). Tool updates get none.
- For a new minor Go version (1.27 to 1.28): the Go badge at the top of the README and "Go 1.27 or later" under Development setup.

```powershell
git add go.mod go.sum CHANGELOG.md README.md
git commit -m "Update Go to 1.27.3"
git push
```

CI runs on the push; the update reaches `main` with the next release.

## 10. Recommended build workflow

```bat
cd C:\dev\RestoreSafe
.\build-release.bat
```

`build-release.bat` writes `RestoreSafe-<version>.zip` and its checksum `SHA256SUMS.txt` to `dist\`, and `RestoreSafe.exe` to `sandbox\`. It changes no tracked file: it fails instead when `go.mod` or `go.sum` is not tidy (run `go mod tidy` and commit). Two builds of the same commit give the same `RestoreSafe.exe`. It deletes older `RestoreSafe-*.zip` files in `dist\`, so keep release archives you want to keep elsewhere.

For manual testing between releases, build only the exe:

```bat
.\build-dev.bat
```

`build-dev.bat` writes `RestoreSafe.exe` to `sandbox\` with the same resources, flags, and version as `build-release.bat`, and leaves `dist\` as it is, so the archive of the last release isn't replaced by a build of `dev` with the same version. It doesn't check `go.mod` and `go.sum`.

A plain `go build` of `./cmd/restoresafe` needs `cmd\restoresafe\resource.syso`, which only the two scripts generate (ignored by Git). Without it the exe has no manifest, and RestoreSafe shows an error instead of its window. `go build ./...` alone only checks that everything compiles; it writes no exe.

Before a commit, run the checks of CONTRIBUTING.md section 4 (what each one does, and how to fix a failure, is in [CI.md](CI.md)):

```powershell
gofmt -l .
go vet ./...
go build ./...
go test ./...
go tool staticcheck ./...
go tool govulncheck ./...
```

`go test ./...` includes `internal/architecture`, which fails when a package imports one from a higher layer (order from bottom to top: `fsx`/`buildinfo`/`problem`, `security`, `config`/`logging`, `format`, `workflow`, `gui`, `cmd`). The project layout is described in the README (Development setup); the layer rules in the package documentation of `internal\architecture`, and how refactoring works in `docs\SPEC-refactoring.md`.

## 11. Manual testing

- `sandbox\` is the personal folder for manual tests (configurations, source and backup directories). It is ignored by Git.
- `scripts\gui-test\` contains the PowerShell UI automation used for the manual GUI checklist (`docs\GUI-TEST-CHECKLIST.md`); see its README.

### Throughput benchmarks

Two opt-in tests time backups and restores on a drive or network share: `TestThroughputBenchmarkBackup` (`internal\workflow\backup`) and `TestThroughputBenchmarkRestore` (`internal\workflow\restore`). `go test ./...` skips them unless `RESTORESAFE_BENCH_ROOT` is set.

```powershell
$env:RESTORESAFE_BENCH_ROOT = "M:\RestoreSafe-test"   # folder on the volume to test
$env:RESTORESAFE_BENCH_RUNS = "3"                     # timed runs, default 3
go test -p 1 -count=1 -v -timeout 60m -run TestThroughputBenchmark ./internal/workflow/backup ./internal/workflow/restore
Remove-Item Env:RESTORESAFE_BENCH_ROOT, Env:RESTORESAFE_BENCH_RUNS
```

- The source is `1-src-big` in the root: 4 files of 384 MB and 2000 files of 96 KB of random data (1.7 GB). The backup benchmark creates it if it is missing and keeps it for later runs; the restore benchmark needs it, so run the backup benchmark first.
- Each benchmark makes a warm-up run that is not counted, then the timed runs, and logs a `SUMMARY` line with the median time and MiB/s.
- `-p 1` runs the two packages one after the other; otherwise they run at the same time and slow each other down.
- They write `backup-N-*`, `restore-set-*` and `restore-N-*` folders in the root and delete them afterwards. The names are new on every run because on a network share a deleted folder can stay "pending deletion" while another program (Explorer, a virus scanner) still has it open; such a folder disappears once that program lets go.

### Go benchmarks

Small `testing.B` benchmarks cover the parts of the pipeline without a real drive: `EncryptStream` and `DecryptStream` (64 MiB), `BuildTar` (200 files, 25 MiB), manifest encode and decode (100,000 entries), and `Inventory` (500 sets). CI runs each once (`-benchtime 1x`) so that they keep working; the numbers mean something only when compared on one machine. Compare before and after a change with `benchstat`:

```powershell
go test -count=10 -run '^$' -bench . ./internal/security/cryptox ./internal/format/... > old.txt
# make the change, then the same command into new.txt
go run golang.org/x/perf/cmd/benchstat@latest old.txt new.txt
```

- Files that were just written are slow to open the first time, as the virus scanner checks them; `BuildTar` and `Inventory` show this. Run the benchmarks twice and compare the second runs.

### Fuzzing

Every parser of bytes that can come from a backup directory or a file has a fuzz target. What fuzzing does, how to run it locally, and what to do when a target fails is in [CI.md](CI.md) section 6.

## 12. Troubleshooting

- `go` command not found
  - Restart the terminal and run `go version` again.
- Git authentication fails
  - Run `gh auth status`; if needed `gh auth login` and `gh auth setup-git`.
  - Check the remote URL with `git remote -v`.
- VS Code does not detect Go
  - Confirm the `golang.go` extension is installed.
  - Restart VS Code.
  - Run `go env` in the terminal.
- Build fails after a Go update
  - Run `go mod tidy`, then `go build ./...`, and fix errors iteratively.

## 13. Publishing a release on GitHub

### Step 1 – Prepare the release

The example is version 2.1.0; replace it with the version you release. The steps follow CONTRIBUTING.md section 7.

1. On `dev`: in `CHANGELOG.md`, the section of the coming version is headed `## [2.1.0] - Unreleased`. Review it and replace `Unreleased` with today's date (`## [2.1.0] - 2026-12-01`).
2. Set the version in `build\versioninfo.json` (four places: `FileVersion` and `ProductVersion`, each as numbers and as text).
3. Commit, push, and wait for CI on `dev` to be green (`gh run list --branch dev --limit 1`).
4. Write the release notes for users into `dist\RELEASE-NOTES-2.1.0.md` (ignored by Git): the highlights, what users must know before updating, and the Download section (SmartScreen, Smart App Control, the optional checksum). The CHANGELOG section is the source; link to it for the full list.

### Step 2 – Merge `dev` into `main`

`main` holds the latest release and changes only through a pull request (direct pushes are rejected, see CONTRIBUTING.md section 1):

```powershell
cd C:\dev\RestoreSafe
gh pr create --base main --head dev --title "Release 2.1.0" --body "Merges the 2.1.0 release into main."
gh pr checks --watch
gh pr merge --merge
```

### Step 3 – Build from `main` and tag

Build from exactly the merge commit on `main`, and tag that commit:

```powershell
git fetch origin main:main
git switch main
.\build-release.bat
git tag -a v2.1.0 -m "Release version 2.1.0"
git push origin v2.1.0
```

### Step 4 – Create the release as a draft

```powershell
gh release create v2.1.0 --draft --title "RestoreSafe 2.1.0" --notes-file dist\RELEASE-NOTES-2.1.0.md dist\RestoreSafe-2.1.0.zip dist\SHA256SUMS.txt
```

Or on the web: https://github.com/phsc84/RestoreSafe/releases, "Draft a new release", select the tag, paste the notes, attach the ZIP and `SHA256SUMS.txt`, and click "Save draft".

### Step 5 – Test the draft, then publish

1. Open the draft on https://github.com/phsc84/RestoreSafe/releases and check the notes: links and the screenshot work once the tag is pushed.
2. Download the ZIP and `SHA256SUMS.txt` from the draft into an empty folder, check the checksum with the README's command, extract, and start RestoreSafe (SmartScreen may ask once; it stays silent when the file already has reputation or the extractor dropped the Mark of the Web).
3. Make one backup and one restore with a test configuration.
4. Publish: `gh release edit v2.1.0 --draft=false`, or "Publish release" on the web.

### Step 6 – Back to `dev`

```powershell
git switch dev
git merge main
git push
```

`git merge main` brings the release's merge commit into `dev` (a fast-forward when `dev` got no commits meanwhile). A commit made on `main` cannot be pushed, so never stay on it.

### Step 7 – Start the next refactoring round

Right after the release, on `dev`: replace the content of `docs\PLAN-refactoring.md` with the next round's plan, with the finished round's result as its first baseline and its carried-over items (SPEC-refactoring section 8, step 5).
