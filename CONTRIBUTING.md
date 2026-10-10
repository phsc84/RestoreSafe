# Contributing to RestoreSafe

How changes get from an idea into a release: branches, commits, pull requests, the checks CI runs, and the rules every change keeps. How to set up a machine and build is in the README ("Development setup").

## 1. Branches

| Branch | Holds | Rules |
|---|---|---|
| `main` | The latest release (now 2.0.0) | Changes only at a release, through a pull request from `dev` (section 7). GitHub shows `main` by default. |
| `dev` | The work towards the next release: features, fixes, refactoring rounds | Committed and pushed to directly; CI runs on every push. Keep it building and green, so a release can be made from it at any time. |
| A branch for larger work, e.g. `v3` or `feature-<name>` | A next major version or a feature that takes longer than one release | Created from `dev` only when needed; bring it up to date with `git merge dev` regularly, and merge it back into `dev` with a pull request when it is done. Then delete it. |

An urgent fix while `dev` holds work that is not ready for a release goes on a short branch from `main` (e.g. `fix-2.0.1`), into `main` with a pull request, and is released from there; then merge `main` into `dev`. Without unreleased work on `dev`, the fix simply goes through `dev`.

GitHub enforces this for `main` with the ruleset in [.github/rulesets/release-branch.json](.github/rulesets/release-branch.json), for everyone including the owner:

- a direct push is rejected; changes arrive through a pull request only;
- a pull request can be merged only when the three CI jobs are green;
- the branch cannot be force-pushed or deleted;
- no approval is required (GitHub does not let anyone approve their own pull request).

The JSON file is the definition. To change a rule, edit the file, commit it, and run `powershell -File scripts\apply-rulesets.ps1`, which replaces the ruleset on GitHub with the file's content (it needs `gh` logged in with admin rights). A change made in the GitHub settings instead is overwritten by the next run of the script. `dev` and the other branches are not protected.

Stay on `dev` for daily work. To bring your local `main` up to date without leaving it, run `git fetch origin main:main` (it only moves `main` forward, so it cannot lose anything). Switch to `main` (`git switch main`) only to build exactly what is released, and switch back afterwards: a commit made on `main` cannot be pushed.

A pull request lives on GitHub, not in Git: its commits are those of its branch. See them with `gh pr list --state all`, `gh pr view <number>`, and `gh pr checks <number>`.

## 2. Commits

- **One purpose per commit.** A commit that renames something does not also fix a bug. Small commits are easy to review, to find with `git log`, and to revert.
- **Every commit builds and passes the tests** (`go build ./...`, `go test ./...`). A series of commits may leave an improvement unfinished, never something broken.
- **The message says what and why.** The first line is a short summary in the imperative or as a statement ("Tests no longer swap os.Stdout"), at most about 72 characters. After a blank line, the body explains why the change was needed and anything a reviewer would not see in the diff. Cite plan items as "refactoring 2.0 RF-10" and spec sections as "2.0 spec 6.1" or "GUI spec OV-8".
- **Replaced code goes in the same commit.** No commented-out code, no "old" copies, no compatibility shims inside the program.
- **Commits written with an AI assistant** end with its `Co-Authored-By:` line.

## 3. Pull requests

A pull request (PR) asks to merge one branch into another. GitHub shows its commits, the combined diff, and the result of CI for its newest commit. There are three kinds:

- `dev` into `main`: a release (section 7);
- a branch for larger work into `dev`, when that work is done;
- an urgent fix into `main` (section 1).

1. Push the branch: `git push -u origin <branch>`.
2. Open the PR, on GitHub or with `gh pr create --base <target>`. Open it as a **draft** (`--draft`) while the work goes on: CI runs on every push, but the PR cannot be merged by mistake.
3. The description says what the PR changes and, for plan work, which items it covers.
4. Mark it ready (`gh pr ready`) when the work is done and CI is green.
5. Merge it on GitHub with **Merge pull request** (or `gh pr merge --merge`). The button is enabled once every check is green. Only merge commits are allowed: squashing would fold the small commits of section 2 into one, and rebasing would rewrite them.

## 4. CI (GitHub Actions)

GitHub Actions runs the checks of [.github/workflows/ci.yml](.github/workflows/ci.yml) on a fresh Windows machine ("runner") provided by GitHub. Nothing runs on your computer; the runner starts empty every time, which also shows what depends on a developer's machine.

### When it runs

- On every push to a branch with an open pull request; the result shows on the PR.
- On every push to `main` and `dev`; for `main` that means after every merge.

A push to any other branch without a PR runs nothing.

### What it does

Five jobs run in parallel, each on its own runner; together they take about eight minutes, the fuzzing job the longest.

| Job | Steps | Fails when |
|---|---|---|
| Format, vet, build, static analysis | `gofmt -l .`; `go vet ./...`; the build in the release configuration (`CGO_ENABLED=0`); `go tool staticcheck ./...`; `go tool govulncheck ./...`; `go tool deadcode -test ./...` | a file is not gofmt-formatted, vet or staticcheck reports a finding, the build fails, or the code calls a function with a known vulnerability. deadcode only reports unreachable functions and never fails the job. |
| Tests and coverage | `go test -coverprofile ./...`; the coverage floor of each package ([scripts/ci/coverage-floors.txt](scripts/ci/coverage-floors.txt)); the total coverage; every benchmark once (`-benchtime 1x`); uploads `cover.out` as an artifact | a test or benchmark fails, or a package falls below its floor |
| Race detector | `go test -race ./...` with cgo and the runner's gcc | a test fails, or two goroutines access the same data without synchronisation |
| Fuzzing | every fuzz target for 30 seconds ([scripts/ci/fuzz.sh](scripts/ci/fuzz.sh)); on failure uploads the failing inputs as the `fuzz-failures` artifact | a fuzz target finds an input that breaks its property |
| GUI smoke test | builds RestoreSafe.exe with its resources and runs `Smoke-BackupRestore.ps1` and `Check-States.ps1` of [scripts/gui-test](scripts/gui-test/README.md) on the runner's desktop ([scripts/ci/gui-smoke.ps1](scripts/ci/gui-smoke.ps1)); on failure uploads the screenshots as the `gui-screenshots` artifact | a step of the smoke test fails, or a status of the Create backup page shows the wrong title or action |

The release build stays `CGO_ENABLED=0`; only the race detector needs cgo.

### Reading a result

- On the pull request: the list of checks at the bottom of the **Conversation** tab, or the **Checks** tab. A red check has **Details**, which opens the log of that job.
- All runs of the repository: the **Actions** tab. Each run belongs to one commit; each job shows every step with its full output.
- When a test fails or a race is found, the job adds an **annotation** with the failing lines, shown at the top of the run and on the PR, so the long log is rarely needed.
- In the terminal: `gh pr checks <PR number>`, `gh run list`, `gh run view <run id> --log-failed`.

### Running the same checks locally

Before pushing, run what CI runs (the tools are pinned in `go.mod`; nothing to install):

```powershell
gofmt -l .
go vet ./...
go build ./...
go test ./...
go tool staticcheck ./...
go tool govulncheck ./...
```

The race detector needs a MinGW-w64 gcc in `PATH` (e.g. from [winlibs.com](https://winlibs.com/)), then `$env:CGO_ENABLED = "1"; go test -race ./...`. Without one, CI runs it.

### Benchmarks and fuzzing

The `testing.B` benchmarks (`bench_test.go` in cryptox, archive, manifest, and catalog) run only once in CI, so that they keep working; their numbers mean something only when compared on one machine, before and after a change:

```powershell
go test -count=10 -run '^$' -bench . ./internal/security/cryptox ./internal/format/... > old.txt
# make the change, then the same command into new.txt
go run golang.org/x/perf/cmd/benchstat@latest old.txt new.txt
```

Every parser of bytes that can come from a backup directory or a file has a fuzz target (`func FuzzX(f *testing.F)`). `go test` runs their seed inputs and the inputs saved under `testdata/fuzz/`. To fuzz locally: `bash scripts/ci/fuzz.sh 30` (every target, seconds each), or one target longer with `go test -run '^$' -fuzz '^FuzzOpen$' -fuzztime 5m ./internal/format/container`. When a target fails, Go saves the input under the package's `testdata/fuzz/<target>/` (in CI: the `fuzz-failures` artifact). Commit it with the fix: it stays a regression test.

### Coverage floors

[scripts/ci/coverage-floors.txt](scripts/ci/coverage-floors.txt) lists the minimum coverage per package, in % of statements. A floor is raised when a package's coverage has risen for good. It is never lowered just to let a change through; a lower floor needs a reason in the refactoring plan.

### Updating the actions

The workflow uses GitHub's own actions (`actions/checkout`, `actions/setup-go`, `actions/upload-artifact`) at a major version (`@v7`). When GitHub warns about an outdated version (e.g. the Node.js runtime), read the action's release notes on its GitHub page (e.g. github.com/actions/checkout/releases) and update the major version.

## 5. CHANGELOG.md

[CHANGELOG.md](CHANGELOG.md) is written for users. A change they notice adds an entry under the unreleased version in the same commit: a new or changed feature, a changed text or behaviour, a fixed bug. Keep entries short and say what changes for the user; explanations and details go in the README. Internal changes (refactoring, tests, CI) get no entry.

A dependency of the program also gets an entry, under **Dependencies**: a new Go version, and a module added, updated, replaced or removed that is built into `RestoreSafe.exe`. For a tool that protects encrypted backups, technical users want to know which Go and crypto versions a release ships with. Tools that only build or check the code (the `tool` block of `go.mod`) get no entry.

At a release, the unreleased section gets its version number and date.

## 6. Rules of the code

- **Layers.** Imports point downward only: `cmd` → `gui` → `workflow` → `format` → `config`/`logging` → `security` → `fsx`/`buildinfo`. The full rules are in the package documentation of [internal/architecture](internal/architecture/doc.go); its test fails on a violation and on a package that belongs to no layer. A new package is added to that test in the same commit.
- **Imports.** The module is `github.com/phsc84/restoresafe`. Import blocks group the standard library, the module's own packages, and other modules, as `go run golang.org/x/tools/cmd/goimports -local github.com/phsc84/restoresafe -w .` sorts them.
- **The backup format of 2.x is frozen.** Every backup written by 2.0.0 must restore with every later 2.x version.
- **Behaviour is preserved** unless a change says otherwise and adds a CHANGELOG entry: texts, the log file format, the order of questions.
- **No unattended operation.** RestoreSafe never schedules backups, runs in the background, or stores credentials.
- **Errors the user sees are `problem.Error`s**: what happened, and what the user does about it, e.g. `problem.Errorf("Failed to read backup header: %w.", err).WithRemedy("Check that the backup file is complete.")`. Never write "Remedy:" into an error text: the GUI shows the remedy from its field, and logs get "<message> Remedy: <remedy>" from `Error()`. The texts are sentences for the user, so staticcheck's ST1005 is turned off in [staticcheck.conf](staticcheck.conf).

The standing constraints of refactoring work are in [docs/SPEC-refactoring.md](docs/SPEC-refactoring.md) section 2; the specifications are listed in [docs/README.md](docs/README.md).

## 7. Releases

1. On `dev`: give the unreleased section of CHANGELOG.md its version and date, set the version in `build/versioninfo.json`, push, and wait for CI to be green.
2. Merge `dev` into `main` with a pull request (`gh pr create --base main --head dev --title "Release 2.1.0"`); CI checks the release state once more before `main` changes.
3. Update the local `main` (`git fetch origin main:main`), switch to it (`git switch main`), and run `build.bat`: it writes `dist/RestoreSafe-<version>.zip` and `dist/SHA256SUMS.txt`.
4. Tag that commit of `main` with an annotated tag (`git tag -a v2.1.0 -m "Release version 2.1.0"`, `git push origin v2.1.0`).
5. Create the GitHub release as a **draft** with release notes written for users, the ZIP, and `SHA256SUMS.txt`. Download the ZIP from the draft, check it, and run it once; then publish.
6. Switch back to `dev` and bring it up to date with the merge (`git switch dev`, `git merge main`).
