# Specification: CI runner

| | |
|---|---|
| Status | Agreed 2026-10-14 (decisions in section 11) |
| Plan item | RF-87 (tooling) in [PLAN-refactoring.md](PLAN-refactoring.md), phase 5 |
| Replaces | `scripts/ci/*.sh` and the inline shell of [.github/workflows/ci.yml](../.github/workflows/ci.yml) |
| Keeps | The five jobs, their names and the ruleset; `scripts/ci/gui-smoke.ps1` and `scripts/gui-test/` |
| Dependencies | None new: Go standard library only, the tools already pinned in `go.mod` |

## 1. Purpose

Today the checks of CI are defined in three places: shell inside `ci.yml` (gofmt, `go test | tee`, benchmarks, `pipefail`), four bash scripts with awk and grep in `scripts/ci/`, and the "Locally" columns of [CI.md](CI.md), which repeat them as single commands. Running CI locally means typing those commands one by one; the coverage floors need Git Bash because Windows PowerShell 5.1 writes redirected output in UTF-16.

This spec replaces all of that with one Go program, `tools/ci`, that runs a job the same way on a developer's machine and on the GitHub runner:

```powershell
go run ./tools/ci fast        # static and test, before a push
go run ./tools/ci all         # what CI runs, except the GUI smoke test
go run ./tools/ci gui         # the GUI smoke test (takes over mouse and keyboard)
```

`ci.yml` keeps the triggers, the runners, checkout, Go setup and artifact upload; each job's checks become one `go run ./tools/ci <job>` step. Moving to another CI service later means rewriting that thin YAML, not the checks.

**Goals**

1. One command per job, identical locally and in CI.
2. No bash, awk or grep: Go is the only requirement (plus a gcc for `race`, and Windows PowerShell for `gui`, as today).
3. Test output read as `go test -json` events instead of text patterns.
4. No check becomes weaker (section 4 maps each check of today to the runner).

## 2. Constraints

1. **Job IDs and names stay.** The keys `static`, `test`, `race`, `fuzz`, `gui` and their `name:` values in `ci.yml` do not change, so [release-branch.json](../.github/rulesets/release-branch.json) needs no change and is not re-applied.
2. **Standard library only.** `tools/ci` imports nothing outside the standard library and no package of the module. External tools are called through `go tool` (staticcheck, govulncheck, deadcode, as pinned in `go.mod`), `go`, `gofmt`, `gcc` (for the check in 6.3 only) and `powershell.exe` (for `gui` only).
3. **The project's rules apply to the runner too.** It is vetted, staticchecked and dead-code checked like the rest; its logic package has a coverage floor (section 8); it is in `internal/architecture` (section 3.2).
4. **No behaviour of RestoreSafe changes.** Only tooling and docs change; nothing for the CHANGELOG's user-facing sections.
5. **Delete, don't keep** (SPEC-refactoring constraint 6). The `.sh` scripts and the inline shell go in the commit that switches `ci.yml` to the runner.

## 3. Layout

### 3.1 Packages

| Path | Package | Holds |
|---|---|---|
| `tools/ci/` | `main` | Flags, job dispatch, running commands, console output. Thin: no parsing. |
| `tools/ci/check/` | `check` | Everything that can be tested without running a command: reading `go test -json` events, coverage floors, the deadcode filter, fuzz target lists, the summary, GitHub annotations and their escaping. |
| `tools/ci/check/testdata/` | | Recorded `go test -json` streams, floors files and deadcode outputs for the tests (section 8). |

`tools/` and not `cmd/`: `cmd/` holds the programs that ship (`restoresafe`, `yubidiag`).

### 3.2 Architecture test

**The problem.** `TestEveryPackageBelongsToALayer` in `internal/architecture` requires every package of the module to be in one of the layers (`fsx` ... `cmd`) or in `testSupport`. `tools/ci` and `tools/ci/check` are in neither, so `go test ./...` would fail as soon as they exist.

**Why not a layer.** A layer is part of RestoreSafe and may use the layers below it. The runner is not part of the program: it checks the code from outside, by running `go` commands, and needs none of RestoreSafe's packages. Putting it in a layer (e.g. next to `cmd`) would allow it to import `internal/...`, and would allow `cmd` to import it.

**Change.**

1. A third list next to `layers` and `testSupport`: `devTools = []string{"tools"}`. `TestEveryPackageBelongsToALayer` accepts a package under `tools` like one in `testSupport`.
2. A new test `TestDevToolsStandAlone` enforces the separation both ways: a package under `tools` imports no package of the module, and no package outside `tools` imports one under `tools`.
3. The package documentation of `internal/architecture` gets one sentence: `tools` holds development tools, outside the layers, separate from the program in both directions.

`TestImportsPointDownward` needs no change: a package under `tools` imports only the standard library, which is in no layer, so it never looks like an import upwards.

## 4. Today's checks and where they go

| Job | Check today | Where today | In the runner |
|---|---|---|---|
| static | `gofmt -l .` is empty | inline bash | 6.1 step 1 |
| static | `go vet ./...` | ci.yml | 6.1 step 2 |
| static | `go build -trimpath ./...` with `CGO_ENABLED=0` | ci.yml | 6.1 step 3 |
| static | `go tool staticcheck ./...` | ci.yml | 6.1 step 4 |
| static | `go tool govulncheck ./...` | ci.yml | 6.1 step 5 |
| static | deadcode, both passes (RF-86) | `static-deadcode.sh` | 6.1 step 6 |
| test | `go test -count=1 -coverprofile ./...` | inline bash, `tee` | 6.2 step 1, with `-json` |
| test | failure annotation | `shared-annotate-failures.sh` | 6.2 step 1, from the events |
| test | coverage floors | `test-coverage-floors.sh` | 6.2 step 2 |
| test | every benchmark once | ci.yml | 6.2 step 3 |
| test | total coverage | inline bash, `tail` | 6.2 step 4 |
| race | `go test -race -timeout 45m ./...` with `CGO_ENABLED=1` | inline bash | 6.3 |
| race | failure annotation | `shared-annotate-failures.sh` | 6.3, from the events |
| fuzz | every target 30 s | `fuzz-targets.sh` | 6.4 |
| gui | smoke test and state check | `gui-smoke.ps1` | 6.5, calls the same script |

## 5. Command line

```
go run ./tools/ci [flags] <job or group>...
```

| Argument | Runs |
|---|---|
| `static`, `test`, `race`, `fuzz`, `gui` | that job |
| `fast` | `static test` |
| `all` | `static test race fuzz` |

| Flag | Default | Meaning |
|---|---|---|
| `-out <dir>` | `sandbox/ci` | Directory for logs and artifacts; each job writes to `<out>/<job>/`, emptied at the job's start. Relative to the module root. |
| `-fuzztime <duration>` | `30s` | Time per fuzz target. |
| `-h` | | Usage, with the jobs and groups. |

| Requirement | |
|---|---|
| CI-1 | The runner finds the module root (`go env GOMOD`) and runs every command there, so it works from any directory of the repository. |
| CI-2 | Jobs run one after the other, in the order given (a group expands in place; a job named twice runs once). Every job runs even when an earlier one failed. |
| CI-3 | Within a job, every step runs even when an earlier step failed, except where a step needs an earlier one's output and that output is missing (then the step is reported as not run). The job fails when any step failed. |
| CI-4 | The runner sets `CGO_ENABLED` for each command it starts (`0` everywhere, `1` for the race tests) and does not depend on the caller's environment for it. |
| CI-5 | Exit code 0: every job passed or was skipped. 1: a check failed. 2: the runner could not run (unknown job, no `go.mod`, a command not found, an unreadable floors file). |
| CI-6 | At the end, a summary: one line per job with `PASS`, `FAIL` or `SKIP`, its duration, and for `FAIL` the failed steps; then the path of `-out`. |
| CI-7 | `gui` is never part of a group; it runs only when named. |
| CI-8 | No step changes a tracked file. Files written outside `-out` are only those `go test -fuzz` writes into `testdata/fuzz/` for a failing input, as today. |

## 6. Jobs

Each step prints a header with its name before it starts. A command's output is shown as it comes (streamed), except where 6.2 says otherwise.

### 6.1 static

| Step | Command | Fails when |
|---|---|---|
| 1. gofmt | `gofmt -l .` | it lists a file; the files are printed |
| 2. vet | `go vet ./...` | exit code ≠ 0 |
| 3. build | `go build -trimpath ./...`, `CGO_ENABLED=0` | exit code ≠ 0 |
| 4. staticcheck | `go tool staticcheck ./...` | exit code ≠ 0 |
| 5. govulncheck | `go tool govulncheck ./...` | exit code ≠ 0 |
| 6. deadcode | `go tool deadcode -test ./...`, then `go tool deadcode ./...` | see below |

Step 6, as `static-deadcode.sh` does today (RF-86):

- the first pass fails on any line it prints ("functions nothing calls");
- the second pass fails on any line that is not under `internal/testutil/` or `internal/workflow/interact/interacttest/` (either path separator) and not already printed by the first pass ("production functions only tests call");
- a non-zero exit code of either command fails the step.

### 6.2 test

| Step | Does | Fails when |
|---|---|---|
| 1. go test | `go test -count=1 -json -coverprofile=<out>/test/cover.out ./...`, `CGO_ENABLED=0`; the event stream is saved to `<out>/test/test.json` | a package or test fails, or a package does not build |
| 2. floors | compares each package's coverage with `scripts/ci/test-coverage-floors.txt` | see below |
| 3. benchmarks | `go test -count=1 -run '^$' -bench . -benchtime 1x ./...`, `CGO_ENABLED=0` | exit code ≠ 0 |
| 4. total | `go tool cover -func=<out>/test/cover.out`, last line printed | never |

**Console output of step 1.** What `go test ./...` without `-v` prints: the package lines (`ok`, `FAIL`, `?`) as they come, the full output of each failing test, and build errors (`build-output` events). Passing tests print nothing. This keeps a log of a full run short.

**Reading the events.** A failure is an event with `"Action":"fail"`: with `Test` set for a test (subtests included, e.g. `TestX/case`), without it for a package (a panic outside a test, `TestMain`, a timeout), and with `FailedBuild` for a build failure. A test's output is the `output` events with the same `Package` and `Test`. A package's coverage is the `output` event of that package matching `coverage: <n>% of statements` (it is printed for failing packages too).

**Floors file.** One package per line: path relative to the module (`internal/fsx`), whitespace, a number. Lines starting with `#` and blank lines are ignored; CRLF and LF are both accepted. Any other line is an error (exit 2), where the awk script ignored it silently.

**Floors check.** For each package in the file: below its floor fails ("<pkg>: 84.9 % is below its floor of 85 %"); not in the events fails ("<pkg>: not measured (floor 85 %)"); otherwise one line with coverage and floor. Packages are listed sorted. A package at exactly its floor passes. Packages without a floor are not checked.

### 6.3 race

`go test -count=1 -race -timeout 45m -json ./...`, `CGO_ENABLED=1`, events saved to `<out>/race/race.json`, console output and failures as in 6.2 step 1. A data race shows as a failing test whose output contains `WARNING: DATA RACE`; the report is that test's output, no extra pattern needed.

Before it starts, the runner looks for `gcc` in `PATH` and prints its version line. Without a gcc: in CI (`GITHUB_ACTIONS=true`) the job fails; locally it is skipped with the reason "no gcc in PATH (see CI.md section 5)" (decision 1).

### 6.4 fuzz

1. **List.** `go test -list '^Fuzz' -json ./...`; a target is an `output` event of a package whose text (trimmed) starts with `Fuzz` and is a Go identifier. No target found is a failure: the project has fuzz targets (CI.md section 6), so an empty list means the listing broke.
2. **Fuzz** each target, one after the other: `go test -run '^$' -fuzz '^<name>$' -fuzztime <fuzztime> <package>`, `CGO_ENABLED=0`, output streamed and copied to `<out>/fuzz/fuzz.log`, headed `== <name> (<package>)` as today. Plain output, not `-json`: fuzzing reports a crash in text.
3. A failing target fails the job; the others still run. When the output contains `Failing input written to <path>`, the summary lists that path.

### 6.5 gui

Runs `powershell.exe -NoProfile -ExecutionPolicy Bypass -File scripts\ci\gui-smoke.ps1 -Work <out>\gui\work` and passes its exit code on. The script, its screenshots (`<out>\gui\work\shots`, `...\states`) and `scripts/gui-test/` stay as they are.

Locally (not `GITHUB_ACTIONS`), the runner prints "The GUI smoke test takes over mouse and keyboard. Starting in 5 s, Ctrl+C to cancel." and waits 5 seconds. On a system other than Windows, the job fails with exit 2.

## 7. GitHub integration

The runner knows it runs in GitHub Actions by `GITHUB_ACTIONS=true`. Only then:

| Requirement | |
|---|---|
| CI-20 | Each step's output is wrapped in `::group::<job>: <step>` and `::endgroup::`, so the log is folded by step. |
| CI-21 | Each failure becomes an `::error title=<title>::<message>` annotation: per failing test (title `<package> <test>`, message its output, at most 40 lines), per package below or missing its floor, per deadcode pass with findings, per unformatted-file list, per failing fuzz target, and one per other failed step (title the step, message the last 40 lines of its output). |
| CI-22 | At most 10 error annotations per job run (GitHub shows no more per step); when there are more, the 10th says "and <n> more failures, see the log". |
| CI-23 | In annotation messages, `%`, CR and LF are escaped as `%25`, `%0D`, `%0A`; in titles also `:` and `,` (`%3A`, `%2C`). |

Locally the same failures are printed as plain text in the summary (CI-6).

### 7.1 ci.yml after the change

Every job has this shape; only `name`, `timeout-minutes` and the artifact differ:

```yaml
  test:
    name: "test: tests and coverage"
    runs-on: windows-latest
    steps:
      - uses: actions/checkout@v7
      - uses: actions/setup-go@v7
        with:
          go-version-file: go.mod
      - run: go run ./tools/ci -out "${{ runner.temp }}/ci" test
      - uses: actions/upload-artifact@v7
        with:
          name: coverage
          path: ${{ runner.temp }}/ci/test/cover.out
```

- `defaults.run.shell: bash` goes; the steps run in the runner's default shell.
- Artifacts: `coverage` from `ci/test/cover.out` (always); `fuzz-failures` from `internal/**/testdata/fuzz/` (on failure, unchanged); `gui-screenshots` from `ci/gui/work/shots` and `ci/gui/work/states` (on failure).
- Triggers, permissions, runners and timeouts stay.

## 8. Tests of the runner

`tools/ci/check` is tested from recorded inputs in `testdata/`, recorded with the Go version of `go.mod`:

| Area | Cases |
|---|---|
| Events | all pass; a failing test; a failing subtest; a panic in a test; a package failing outside a test; a build failure; a data race; coverage of a passing and of a failing package; a package without test files |
| Console output | for the cases above, the printed text matches a golden file |
| Floors file | comments, blank lines, CRLF, a malformed line (error), a duplicate package (error) |
| Floors check | above, exactly at, below the floor; not measured; output sorted |
| Deadcode filter | both passes empty; findings in pass 1 only; test-only functions inside and outside the test-support packages, with `/` and `\` |
| Fuzz list | several packages, a package without targets, a line that is not an identifier |
| Annotations | escaping of message and title; the limit of 10 |
| Groups | `fast`, `all`, a job named twice, an unknown job |

- `tools/ci/check` gets a coverage floor of 90 in `test-coverage-floors.txt`.
- `tools/ci` (`main`) gets no floor: it runs commands.
- No fuzz target: the runner reads output of `go`, not bytes from a backup directory (CI.md section 6).

## 9. Docs

In the commit that switches `ci.yml`:

- **CI.md**: section 1 table, column "Scripts" becomes "Runner" (`go run ./tools/ci <job>`); the "Locally" columns and examples of sections 3 to 7 use the runner, and keep the single commands only where they help to narrow a failure (one test, one fuzz target, one package's coverage); the Git Bash note of section 4 goes; section 8 "Names" describes `tools/ci` instead of the script prefixes (`gui-smoke.ps1` and `test-coverage-floors.txt` keep their names).
- **CONTRIBUTING.md** section 4 and **DEVELOPMENT.md** section 10: the list of six commands becomes `go run ./tools/ci fast`, with a pointer to CI.md.
- **DEVELOPMENT.md**, with phase 4: the pre-push hook (section 11.1), how to turn it on, and how to skip it once.
- **docs/README.md**: a row for this spec.
- **PLAN-refactoring.md**: the item and its state.

## 10. Phases

| Phase | Content | Done when |
|---|---|---|
| 1. Runner | `tools/ci`, `tools/ci/check` with its tests, the architecture change (3.2), the floor (8). `ci.yml` and the scripts unchanged. | CI passes; locally on Windows, `go run ./tools/ci all` and `gui` give the same pass and fail as the scripts on the same commit. |
| 2. Switch | `ci.yml` as in 7.1, the four `.sh` deleted, docs (section 9), in one commit. | CI passes with all five jobs under their old names; the PR view into `main` shows them as required checks without a ruleset change. |
| 3. Proof | On a throwaway branch with a pull request into `dev`, one commit per check that breaks it: an unformatted file, a vet finding, a failing test, a floor one above the current coverage, an unused function, a data race in a test. | Each turns the right job red with a readable annotation; the branch and PR are deleted unmerged. |
| 4. Pre-push hook | `.githooks/pre-push` and its docs (section 11.1). | A push with a failing check is stopped locally; the time of `fast` on the development machine is recorded in the plan. |

## 11. Decisions

Decided by the owner, 2026-10-14:

1. **Race without a gcc, locally: skipped.** The summary shows `SKIP` with the reason, exit code 0. The development machine has no gcc; the race detector runs on the GitHub runner on every push and stays a required check for `main`. On the runner, a missing gcc fails the job (6.3).
2. **Pre-push hook: yes, tried out.** Commits stay unchecked; pushes are checked, so pushing less often (several commits at once) keeps the wait small. If the wait turns out to be a nuisance, the hook is dropped again; that is a change of DEVELOPMENT.md only.

### 11.1 The pre-push hook

| Requirement | |
|---|---|
| CI-30 | `.githooks/pre-push` runs `go run ./tools/ci fast` from the repository root and passes its exit code on; a non-zero code stops the push. It is a two-line `sh` script (`#!/bin/sh`, `exec go run ./tools/ci fast`), which Git for Windows runs with its own `sh`. |
| CI-31 | `.gitattributes` gets `.githooks/* text eol=lf`, so the hook keeps LF line endings on Windows. |
| CI-32 | The hook is turned on per clone with `git config core.hooksPath .githooks` (Git does not take hooks from a clone by itself). DEVELOPMENT.md says so, and that `git push --no-verify` skips it once, e.g. for a docs-only push. |
| CI-33 | DEVELOPMENT.md notes that the hook checks the working tree, not the pushed commits: uncommitted changes are checked along with them. |

## 12. Not part of this spec

- Moving to another forge or CI service, mirroring the repository, self-hosted runners.
- Changing triggers, schedules or which checks are required (e.g. fuzzing only on pull requests into `main`).
- Running jobs in parallel locally.
- `build-release.bat`, `build-dev.bat`, releases, `scripts/apply-rulesets.ps1`, `scripts/gui-test/`.
- A job summary page (`GITHUB_STEP_SUMMARY`); annotations and the log are enough for now.

## 13. Acceptance criteria

1. `go run ./tools/ci all` on a clean checkout of `dev` passes on the development machine with a gcc, and fails with a non-zero exit code when any check of section 4 fails.
2. The five CI jobs keep their names, pass, and the ruleset is unchanged.
3. No `.sh` file is left in the repository; `ci.yml` has no shell logic beyond one `go run ./tools/ci` per job.
4. A failing test, a package below its floor and a deadcode finding each show as an annotation on the pull request (phase 3).
5. `tools/ci/check` is at or above its floor; `go test ./...`, staticcheck and both deadcode passes report nothing for `tools/`.
6. CI takes no longer than before (about 7 to 8 minutes per run, fuzzing the longest job).
