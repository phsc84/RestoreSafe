# Contributing to RestoreSafe

How changes get from an idea into a release: branches, commits, pull requests, the checks CI runs, and the rules every change keeps. How to set up a machine and build is in the README ("Development setup"); what CI checks and how to fix a failure in [CI.md](CI.md).

## 1. Branches

| Branch | Holds | Rules |
|---|---|---|
| `main` | The latest release (now 2.0.0) | Changes only at a release, through a pull request from `dev` (section 7). GitHub shows `main` by default. |
| `dev` | The work towards the next release: features, fixes, refactoring rounds | Committed and pushed to directly; CI runs on every push. Keep it building and green, so a release can be made from it at any time. |
| A branch for larger work, e.g. `v3` or `feature-<name>` | A next major version or a feature that takes longer than one release | Created from `dev` only when needed; bring it up to date with `git merge dev` regularly, and merge it back into `dev` with a pull request when it is done. Then delete it. |

An urgent fix while `dev` holds work that is not ready for a release goes on a short branch from `main` (e.g. `fix-2.0.1`), into `main` with a pull request, and is released from there; then merge `main` into `dev`. Without unreleased work on `dev`, the fix simply goes through `dev`.

GitHub enforces this for `main` with the ruleset in [.github/rulesets/release-branch.json](../.github/rulesets/release-branch.json), for everyone including the owner:

- a direct push is rejected; changes arrive through a pull request only;
- a pull request can be merged only when all five CI jobs are green;
- the branch cannot be force-pushed or deleted;
- no approval is required (GitHub does not let anyone approve their own pull request).

The JSON file is the definition. To change a rule, edit the file, commit it, and run `powershell -ExecutionPolicy Bypass -File scripts\apply-rulesets.ps1` (Windows blocks scripts by default; the bypass applies to that run only), which replaces the ruleset on GitHub with the file's content (it needs `gh` logged in with admin rights). A change made in the GitHub settings instead is overwritten by the next run of the script. `dev` and the other branches are not protected.

Stay on `dev` for daily work. To bring your local `main` up to date without leaving it, run `git fetch origin main:main` (it only moves `main` forward, so it cannot lose anything). Switch to `main` (`git switch main`) only to build exactly what is released, and switch back afterwards: a commit made on `main` cannot be pushed.

A pull request lives on GitHub, not in Git: its commits are those of its branch. See them with `gh pr list --state all`, `gh pr view <number>`, and `gh pr checks <number>`.

## 2. Commits

- **One purpose per commit.** A commit that renames something does not also fix a bug. Small commits are easy to review, to find with `git log`, and to revert.
- **Every commit builds and passes the tests** (`go build ./...`, `go test ./...`). A series of commits may leave an improvement unfinished, never something broken.
- **The message says what and why.** The first line is a short summary in the imperative or as a statement ("Tests no longer swap os.Stdout"), at most about 72 characters. After a blank line, the body explains why the change was needed and anything a reviewer would not see in the diff. Cite plan items as "refactoring 2.0 RF-10" and spec sections as "core spec 6.1" or "GUI spec OV-8".
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

GitHub Actions runs five jobs on every push to `main` and `dev` and to a branch with an open pull request: `static` (format, vet, build, static analysis), `test` (tests and coverage), `race` (race detector), `fuzz` (fuzzing), and `gui` (GUI smoke test). A pull request into `main` can be merged only when all five are green.

Before pushing, run what CI runs (the tools are pinned in `go.mod`; nothing to install):

```powershell
gofmt -l .
go vet ./...
go build ./...
go test ./...
go tool staticcheck ./...
go tool govulncheck ./...
```

What each job checks, how to read a result, and how to fix a failure, job by job, is in [CI.md](CI.md).

## 5. CHANGELOG.md

[CHANGELOG.md](../CHANGELOG.md) is written for users. A change they notice adds an entry under the unreleased version in the same commit: a new or changed feature, a changed text or behaviour, a fixed bug. Keep entries short and say what changes for the user; explanations and details go in the README. Internal changes (refactoring, tests, CI) get no entry.

A dependency of the program also gets an entry, under **Dependencies**: a new Go version, and a module added, updated, replaced or removed that is built into `RestoreSafe.exe`. For a tool that protects encrypted backups, technical users want to know which Go and crypto versions a release ships with. Tools that only build or check the code (the `tool` block of `go.mod`) get no entry.

At a release, the unreleased section gets its version number and date.

## 6. Rules of the code

- **Layers.** Imports point downward only: `cmd` → `gui` → `workflow` → `format` → `config`/`logging` → `security` → `fsx`/`buildinfo`. The full rules are in the package documentation of [internal/architecture](../internal/architecture/doc.go); its test fails on a violation and on a package that belongs to no layer. A new package is added to that test in the same commit.
- **Imports.** The module is `github.com/phsc84/restoresafe`. Import blocks group the standard library, the module's own packages, and other modules, as `go run golang.org/x/tools/cmd/goimports -local github.com/phsc84/restoresafe -w .` sorts them.
- **The backup format of 2.x is frozen.** Every backup written by 2.0.0 must restore with every later 2.x version.
- **Behaviour is preserved** unless a change says otherwise and adds a CHANGELOG entry: texts, the log file format, the order of questions.
- **No unattended operation.** RestoreSafe never schedules backups, runs in the background, or stores credentials.
- **Errors the user sees are `problem.Error`s**: what happened, and what the user does about it, e.g. `problem.Errorf("Failed to read backup header: %w.", err).WithRemedy("Check that the backup file is complete.")`. Never write "Remedy:" into an error text: the GUI shows the remedy from its field, and logs get "<message> Remedy: <remedy>" from `Error()`. The texts are sentences for the user, so staticcheck's ST1005 is turned off in [staticcheck.conf](../staticcheck.conf).

The standing constraints of refactoring work are in [SPEC-refactoring.md](SPEC-refactoring.md) section 2; the specifications are listed in [README.md](README.md).

## 7. Releases

1. On `dev`: give the unreleased section of CHANGELOG.md its version and date, set the version in `build/versioninfo.json`, push, and wait for CI to be green.
2. Merge `dev` into `main` with a pull request (`gh pr create --base main --head dev --title "Release 2.1.0"`); CI checks the release state once more before `main` changes.
3. Update the local `main` (`git fetch origin main:main`), switch to it (`git switch main`), and run `build.bat`: it writes `dist/RestoreSafe-<version>.zip` and `dist/SHA256SUMS.txt`.
4. Tag that commit of `main` with an annotated tag (`git tag -a v2.1.0 -m "Release version 2.1.0"`, `git push origin v2.1.0`).
5. Create the GitHub release as a **draft** with release notes written for users, the ZIP, and `SHA256SUMS.txt`. Download the ZIP from the draft, check it, and run it once; then publish.
6. Switch back to `dev` and bring it up to date with the merge (`git switch dev`, `git merge main`).
