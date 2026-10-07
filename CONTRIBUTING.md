# Contributing to RestoreSafe

How changes get from an idea into a release: branches, commits, pull requests, the checks CI runs, and the rules every change keeps. How to set up a machine and build is in the README ("Development setup").

## 1. Branches

| Branch | Holds | Rules |
|---|---|---|
| `main` | The latest release (now 1.0.2) | Changes only at a release: `v2` is merged into `main` as soon as 2.0.0 is released. GitHub shows `main` by default. |
| `v2` | The development of 2.x | Always builds and passes CI, so a release can be made from it at any time. Changes only through pull requests. |
| Work branches, e.g. `refactor-2.0` | One piece of work: a refactoring round, a feature, a fix | Branched from `v2`, merged back with a pull request, then brought up to date with `v2` (or deleted when the work is done). |

A refactoring round has one branch, `refactor-<release>`, and one pull request per phase of its plan ([docs/SPEC-refactoring.md](docs/SPEC-refactoring.md)).

Keep your local branches current: after a merge on GitHub, `git switch v2` and `git pull`; then `git switch <work branch>` and `git merge v2`.

## 2. Commits

- **One purpose per commit.** A commit that renames something does not also fix a bug. Small commits are easy to review, to find with `git log`, and to revert.
- **Every commit builds and passes the tests** (`go build ./...`, `go test ./...`). A series of commits may leave an improvement unfinished, never something broken.
- **The message says what and why.** The first line is a short summary in the imperative or as a statement ("Tests no longer swap os.Stdout"), at most about 72 characters. After a blank line, the body explains why the change was needed and anything a reviewer would not see in the diff. Cite plan items as "refactoring 2.0 RF-10" and spec sections as "2.0 spec 6.1" or "GUI spec OV-8".
- **Replaced code goes in the same commit.** No commented-out code, no "old" copies, no compatibility shims inside the program.
- **Commits written with an AI assistant** end with its `Co-Authored-By:` line.

## 3. Pull requests

A pull request (PR) asks to merge a work branch into `v2`. GitHub shows its commits, the combined diff, and the result of CI for its newest commit.

1. Push the work branch: `git push -u origin <branch>`.
2. Open the PR, on GitHub or with `gh pr create --base v2`. Open it as a **draft** (`--draft`) while the work goes on: CI runs on every push, but the PR cannot be merged by mistake.
3. The description says what the PR changes and, for plan work, which items it covers.
4. Mark it ready (`gh pr ready`) when the work is done and CI is green.
5. Merge it on GitHub with **Merge pull request**. Merge only when every check is green.

## 4. CI (GitHub Actions)

GitHub Actions runs the checks of [.github/workflows/ci.yml](.github/workflows/ci.yml) on a fresh Windows machine ("runner") provided by GitHub. Nothing runs on your computer; the runner starts empty every time, which also shows what depends on a developer's machine.

### When it runs

- On every push to a branch with an open pull request; the result shows on the PR.
- On every push to `main` and `v2`, which in practice means after every merge.

A push to a work branch without a PR runs nothing.

### What it does

Three jobs run in parallel, each on its own runner; together they take about three minutes.

| Job | Steps | Fails when |
|---|---|---|
| Format, vet, build, static analysis | `gofmt -l .`; `go vet ./...`; the build in the release configuration (`CGO_ENABLED=0`); `go tool staticcheck ./...`; `go tool govulncheck ./...`; `go tool deadcode -test ./...` | a file is not gofmt-formatted, vet or staticcheck reports a finding, the build fails, or the code calls a function with a known vulnerability. deadcode only reports unreachable functions and never fails the job. |
| Tests and coverage | `go test -coverprofile ./...`; the coverage floor of each package ([scripts/ci/coverage-floors.txt](scripts/ci/coverage-floors.txt)); the total coverage; uploads `cover.out` as an artifact | a test fails, or a package falls below its floor |
| Race detector | `go test -race ./...` with cgo and the runner's gcc | a test fails, or two goroutines access the same data without synchronisation |

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

### Coverage floors

[scripts/ci/coverage-floors.txt](scripts/ci/coverage-floors.txt) lists the minimum coverage per package, in % of statements. A floor is raised when a package's coverage has risen for good. It is never lowered just to let a change through; a lower floor needs a reason in the refactoring plan.

### Updating the actions

The workflow uses GitHub's own actions (`actions/checkout`, `actions/setup-go`, `actions/upload-artifact`) at a major version (`@v7`). When GitHub warns about an outdated version (e.g. the Node.js runtime), read the action's release notes on its GitHub page (e.g. github.com/actions/checkout/releases) and update the major version.

## 5. CHANGELOG.md

[CHANGELOG.md](CHANGELOG.md) is written for users. A change they notice adds an entry under the unreleased version in the same commit: a new or changed feature, a changed text or behaviour, a fixed bug. Internal changes (refactoring, tests, CI) get no entry. At a release, the unreleased section gets its version number and date.

## 6. Rules of the code

- **Layers.** Imports point downward only: `cmd` → `gui` → `workflow` → `format` → `config`/`logging` → `security` → `fsx`/`buildinfo`. The full rules are in the package documentation of [internal/architecture](internal/architecture/doc.go); its test fails on a violation and on a package that belongs to no layer. A new package is added to that test in the same commit.
- **The backup format of 2.x is frozen.** Every backup written by 2.0.0 must restore with every later 2.x version.
- **Behaviour is preserved** unless a change says otherwise and adds a CHANGELOG entry: texts, the log file format, the order of questions.
- **No unattended operation.** RestoreSafe never schedules backups, runs in the background, or stores credentials.
- **Error texts are sentences for the user**, with a remedy ("... Remedy: ..."), so staticcheck's ST1005 is turned off in [staticcheck.conf](staticcheck.conf).

The standing constraints of refactoring work are in [docs/SPEC-refactoring.md](docs/SPEC-refactoring.md) section 2; the specifications are listed in [docs/README.md](docs/README.md).

## 7. Releases

1. On `v2`: give the unreleased section of CHANGELOG.md its version and date, set the version in `build/versioninfo.json`, commit, push, and wait for CI to be green.
2. Run `build.bat`: it writes `dist/RestoreSafe-<version>.zip` and `dist/SHA256SUMS.txt`.
3. Tag the commit with an annotated tag (`git tag -a v2.0.0 -m "Release version 2.0.0"`, `git push origin v2.0.0`) and publish a GitHub release for the tag, with the notes of the CHANGELOG section, the ZIP, and `SHA256SUMS.txt`.
4. Merge `v2` into `main`, so `main` holds the latest release.
