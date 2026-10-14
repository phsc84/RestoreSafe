# CI

What CI checks, how to run the same checks locally, and what to do when a job fails. The branch and pull request rules around it are in [CONTRIBUTING.md](CONTRIBUTING.md).

## 1. Overview

GitHub Actions runs the checks of [.github/workflows/ci.yml](../.github/workflows/ci.yml) on a fresh Windows machine ("runner") provided by GitHub. Nothing runs on your computer; the runner starts empty every time, which also shows what depends on a developer's machine.

### When it runs

- On every push to a branch with an open pull request; the result shows on the PR.
- On every push to `main` and `dev`; for `main` that means after every merge.

A push to any other branch without a PR runs nothing.

### The jobs

Five jobs run in parallel, each on its own runner; together they take about eight minutes, the fuzzing job the longest. A pull request into `main` can be merged only when all five are green ([.github/rulesets/release-branch.json](../.github/rulesets/release-branch.json), CONTRIBUTING.md section 1).

| ID | Job (as GitHub shows it) | Checks | Runner | Section |
|---|---|---|---|---|
| `static` | static: format, vet, build, analysis | formatting, `go vet`, the release build, staticcheck, known vulnerabilities, dead code | `go run ./tools/ci static` | 3 |
| `test` | test: tests and coverage | `go test`, the coverage floor of each package, every benchmark once | `go run ./tools/ci test` | 4 |
| `race` | race: race detector | `go test -race` | `go run ./tools/ci race` | 5 |
| `fuzz` | fuzz: fuzzing | every fuzz target for 30 seconds | `go run ./tools/ci fuzz` | 6 |
| `gui` | gui: smoke test | backups, a restore and a verification through the real window, and every status of the Create backup page | `go run ./tools/ci gui` | 7 |

The ID is the job's key in `ci.yml` and the prefix of its name (section 8). The release build stays `CGO_ENABLED=0`; only the race detector needs cgo.

### Running CI locally

Each job is one run of the CI runner, `tools/ci` ([SPEC-ci.md](SPEC-ci.md)), the same on your machine and on GitHub's:

```powershell
go run ./tools/ci fast        # static and test, before a push
go run ./tools/ci all         # what CI runs, except the GUI smoke test
go run ./tools/ci gui         # the GUI smoke test (takes over mouse and keyboard)
go run ./tools/ci test race   # any jobs, in this order
```

It ends with a summary (`PASS`, `FAIL` or `SKIP` per job, and the steps that failed) and writes its logs and artifacts to `sandbox\ci\<job>\` (`-out` changes that; `-fuzztime` the time per fuzz target). Exit code 0 when every job passed or was skipped, 1 when one failed; the runner prints "exit status 2" when it could not run at all (no `go.mod`, a command not found, an unreadable floors file). Without a gcc, `race` is skipped locally (section 5).

### Reading a result

- On the pull request: the list of checks at the bottom of the **Conversation** tab, or the **Checks** tab. A red check has **Details**, which opens the log of that job.
- All runs of the repository: the **Actions** tab. Each run belongs to one commit; each job shows every step with its full output.
- Each failure becomes an **annotation**, shown at the top of the run and on the PR, so the long log is rarely needed: a failing test or race with its output, a package below its coverage floor, deadcode findings, unformatted files, a failing fuzz target, any other failed step with its last lines. At most ten per job; the tenth says how many more failed. The log is folded by step.
- Fuzzing, the GUI smoke test and the tests upload files as **artifacts** (failing inputs, screenshots, `cover.out`). They are listed at the bottom of the run's summary page.
- In the terminal:

```powershell
gh run list --branch dev --limit 5           # the newest runs, with their ids
gh pr checks <PR number>                     # the checks of a pull request
gh run view <run id> --log-failed            # the log of the failed steps only
gh run download <run id> -n <artifact> -D sandbox\ci-<artifact>   # an artifact
```

## 2. Fixing a failure: the routine

The same steps for every job; sections 3 to 7 add what is particular to each.

1. **Find the failing step.** Open the job's log, or `gh run view <run id> --log-failed`. Read the annotation first, if there is one.
2. **Reproduce it locally** with `go run ./tools/ci <job>`, or with the narrower command in the job's section. A failure you can reproduce is understood once it is fixed; one you can't, isn't.
3. **Fix the cause in the code**, not the check: no skipped test, no lowered floor, no disabled staticcheck check, no longer timeout just to get green. If the check itself is wrong, fix the check, and say why in the commit message.
4. **Run the job's command again**, then push. For a commit on `dev`, the fix goes in a new commit on `dev`; for a pull request, a new commit on its branch.
5. **Re-run without a change only for a runner fault** (the runner lost its network, GitHub reports an outage, a download failed): `gh run rerun <run id> --failed`. A test that fails only sometimes is a bug in the code or the test; re-running it until it is green hides it.

A job can turn red on `dev` without a change of yours: govulncheck learns of new vulnerabilities (section 3), and the fuzzer can find an input that no run found before (section 6). Fix it like any other failure; a release cannot be merged into `main` until it is green.

## 3. static: format, vet, build, analysis

`go run ./tools/ci static` runs these steps:

| Step | Fails when | Narrower, to fix it |
|---|---|---|
| gofmt | a file is not formatted | `gofmt -l .` lists the files; `gofmt -w .` formats them |
| vet | vet reports a finding | `go vet ./internal/<package>` |
| build | the code does not build with `CGO_ENABLED=0 -trimpath` | `go build ./...` |
| staticcheck | staticcheck reports a finding | `go tool staticcheck ./internal/<package>` |
| govulncheck | the code calls a function with a known vulnerability | `go tool govulncheck -show verbose ./...` |
| deadcode | a function has no caller, or a production function has only tests as callers (outside `testutil` and `interacttest`) | the step prints the functions |

The tools are pinned in the `tool` block of `go.mod`; nothing to install.

- **staticcheck.** Each finding names its check (e.g. `SA4006`); [staticcheck.dev/docs/checks](https://staticcheck.dev/docs/checks) explains it. Fix the code. The checks that are turned off, and why, are in [staticcheck.conf](../staticcheck.conf); turning off another one is a change of the project's rules and needs its reason there. An ignored error that is meant to be ignored gets a `//nolint:errcheck // <why>` comment, as in `internal/config/keys.go`.
- **govulncheck.** The report names the vulnerability (`GO-2026-…`), the module, the version that fixes it, and the call path from RestoreSafe's code to the vulnerable function.
  - In the standard library: update Go to the fixed version (`go mod edit -go=<version>`, see "Maintenance" in section 8), and an entry under **Dependencies** in CHANGELOG.md.
  - In a module: `go get <module>@<fixed version>`, `go mod tidy`, and an entry under **Dependencies** in CHANGELOG.md (CONTRIBUTING.md section 5).
  - Without a fixed version yet: avoid the vulnerable call if the code can, or wait for the fix; the job stays red until then.
- **deadcode.** Delete the function, or give it its caller: a function and its first caller go in the same commit. A helper that only tests need goes into a `_test.go` file of the package that uses it, or into `testutil` when several packages need it (RF-70).

## 4. test: tests and coverage

`go run ./tools/ci test` runs these steps:

| Step | Fails when | Narrower, to fix it |
|---|---|---|
| go test | a test fails, or a package does not build | one test, see below |
| floors | a package falls below its floor in [scripts/ci/test-coverage-floors.txt](../scripts/ci/test-coverage-floors.txt), or is not measured | see "Coverage floors" below |
| benchmarks | a benchmark fails | see "A failing benchmark" below |
| total | never; prints the total | |

The job uploads `cover.out` as the `coverage` artifact (locally it is in `sandbox\ci\test\`, with `test.json`, the events of `go test -json`). The console shows what `go test ./...` without `-v` shows: the package lines, the output of each failing test, and build errors. `go test` also runs every input saved under `testdata/fuzz/` (section 6).

**A failing test.** The annotation shows the failing test and its lines. Run it alone, with its output:

```powershell
go test -count=1 -v -run '^TestName$' ./internal/workflow/backup
```

`-count=1` stops Go from using a cached result. A test that passes locally but fails in CI depends on something of your machine: a path, a file that exists only there, the time zone, the speed, a YubiKey. The runner has none of that; the test must not need it.

**Coverage floors.** A floor is the minimum coverage of a package, in % of statements, one line per package in `test-coverage-floors.txt` (`#` starts a comment; a line the runner cannot read stops it). The `floors` step of `go run ./tools/ci test` checks them; coverage counts only the package's own tests, not tests of other packages that call it.

- **Below its floor:** a change added code without tests, or removed tests. Add tests for the new code. To see which lines are not covered: `go test -coverprofile=c.out ./internal/format/naming`, then `go tool cover -html=c.out`.
- **Not measured:** the package was renamed, moved or deleted. Change its line in `test-coverage-floors.txt` in the same commit.
- A floor is raised when a package's coverage has risen for good. It is never lowered just to let a change through; a lower floor needs a reason in the refactoring plan ([PLAN-refactoring.md](PLAN-refactoring.md)).

**A failing benchmark** is a broken benchmark, not a slow one: CI runs each once and does not compare numbers. Run it alone with `go test -run '^$' -bench '^BenchmarkName$' -benchtime 1x ./internal/security/cryptox`.

The benchmarks (`bench_test.go` in cryptox, archive, manifest, and catalog) mean something only when compared on one machine, before and after a change:

```powershell
go test -count=10 -run '^$' -bench . ./internal/security/cryptox ./internal/format/... > old.txt
# make the change, then the same command into new.txt
go run golang.org/x/perf/cmd/benchstat@latest old.txt new.txt
```

## 5. race: race detector

`go run ./tools/ci race`: `go test -race ./...` with cgo and the runner's gcc, with a timeout of 45 minutes. It fails when a test fails, or when two goroutines access the same data without synchronisation and one of them writes ("data race").

**Locally** it needs a MinGW-w64 gcc in `PATH` (e.g. from [winlibs.com](https://winlibs.com/)). Without one, the runner skips the job ("no gcc in PATH"); push and let CI run it. On GitHub's runner, a missing gcc fails the job. One test with the race detector:

```powershell
$env:CGO_ENABLED = "1"
go test -count=1 -race -run '^TestName$' ./internal/workflow/job
Remove-Item Env:CGO_ENABLED
```

**Reading a race report.** The annotation shows `WARNING: DATA RACE` with two stack traces: the access that found the race, and the earlier access by another goroutine, each with the line. Below them, where each goroutine was started. The data both access is the bug.

- Fix it by synchronising the access (a `sync.Mutex`, a channel, `sync/atomic`), or by not sharing the data. Not by removing the test, `t.Parallel()`, or the goroutine.
- A race is found only when both accesses happen in a run, so a race can show once and not again. It is still a race; the report has the two lines.
- A test that fails under `-race` but not without it, without a race report, is usually too slow: the race detector makes code several times slower. A test that waits a fixed time should wait for the event instead.

## 6. fuzz: fuzzing

Fuzzing tests a parser with random inputs. Starting from the seed inputs of a target, the fuzzer changes bytes, cuts and repeats parts, thousands of times per second, and fails when the target finds an input that breaks its property: a panic, a hang, or a check of the target (e.g. "decoding what was encoded gives the same").

Every parser of bytes that can come from a backup directory or a file has a fuzz target (`func FuzzX(f *testing.F)`): the container header, trailer and set, the chunk stream, the manifest and its paths, part and log file names, YubiKey challenge data, recovery codes, and `config.yaml`. A new parser of such bytes gets a target in the same commit. `go run ./tools/ci fuzz` finds every target by itself (`go test -list`) and fuzzes each for 30 seconds; it fails when it finds none, since that means the listing broke.

**Locally:**

```powershell
go run ./tools/ci -fuzztime 10s fuzz                                            # every target, 10 s each
go test -run '^$' -fuzz '^FuzzOpen$' -fuzztime 5m ./internal/format/container  # one target, longer
```

**When a target fails:**

1. **Find the target and the input.** The log shows `== FuzzOpen (github.com/phsc84/restoresafe/internal/format/container)` before the target, then the failure, and `Failing input written to testdata/fuzz/FuzzOpen/<name>`; the summary lists the input too.
2. **Get the input.** It is in the `fuzz-failures` artifact: `gh run download <run id> -n fuzz-failures -D sandbox\fuzz-failures`. Copy the file into the package's `testdata/fuzz/<target>/` (e.g. `internal/format/container/testdata/fuzz/FuzzOpen/<name>`).
3. **Reproduce it.** `go test -run '^FuzzOpen/<name>$' ./internal/format/container` runs only that input and fails like CI.
4. **Fix the parser**: it returns an error for the input instead of panicking, hanging, or reading past its data. If the target's property was wrong instead (it expected something the format does not promise), fix the target.
5. **Check.** The command of step 3 passes; fuzz the target a few minutes longer locally, as one bug often hides another.
6. **Commit the fix and the input together.** The input stays in `testdata/fuzz/`, and `go test` runs it from then on as a regression test.

A failure without a saved input (the fuzzer reports that its process hung or ran out of memory) is reproduced by fuzzing that target locally until it fails; Go then saves the input.

## 7. gui: smoke test

[scripts/ci/gui-smoke.ps1](../scripts/ci/gui-smoke.ps1) builds `RestoreSafe.exe` with its resources, makes a fresh setup (two source folders, a password-only configuration with a recovery code), and runs two scripts of [scripts/gui-test](../scripts/gui-test/README.md) on the runner's desktop:

- `Smoke-BackupRestore.ps1`: two backups through the Create backup window, a restore of one folder through the wizard, a verification; compares the restored folder with its source and checks that no label lies over a control.
- `Check-States.ps1`: makes each condition of GUI spec 3.5 and 11.8 (backup missing, directory unreachable, damaged set, ...), starts RestoreSafe, and compares the title and primary action of the Create backup page with the expected ones.

It fails when a step of the smoke test fails, or a condition shows the wrong title or action. On failure it uploads the screenshots as the `gui-screenshots` artifact: `shots\` with one screenshot per smoke test step (and `desktop.png` when the window did not show), `states\` with one per condition.

**Locally**, with hands off mouse and keyboard while it runs (the runner waits 5 seconds first, so you can cancel with Ctrl+C):

```powershell
go run ./tools/ci gui
```

It runs `gui-smoke.ps1` with `-Work sandbox\ci\gui\work` and writes only there; the screenshots are in `shots\` and `states\` below it. To check only some conditions, run `Check-States.ps1` with `-Only` on that setup (see its README).

**Finding the cause.** The log names the failing step; for a condition it prints the title and action it found, and below them the expected ones. Then look at the screenshot of that step.

- **A text or title changed on purpose.** Update the expected values in `$expected` of `Check-States.ps1` in the same commit; they are those of the Go view tests (`internal/gui/view`), which change with the code.
- **A control cannot be found.** The scripts find controls by their ID (`$Ids` in `GuiDriver.ps1`). A changed or new ID goes into that table; `internal/gui/ids_test.go` fails when the table and the code differ, so `go test` shows this first.
- **The window did not show.** The log prints diagnostics: whether RestoreSafe runs or exited with an error code, its windows, and `shots\desktop.png`. A missing `resource.syso` (the manifest) shows an error instead of the window.
- **A step waited too long.** `Wait-Until` gives each step a timeout (30 seconds by default). A step that is slow on the runner but not locally has become slower in the code; find out why before raising the timeout.

## 8. Maintenance

- **Actions.** The workflow uses GitHub's own actions (`actions/checkout`, `actions/setup-go`, `actions/upload-artifact`) at a major version (`@v7`). When GitHub warns about an outdated version (e.g. the Node.js runtime), read the action's release notes on its GitHub page (e.g. github.com/actions/checkout/releases) and update the major version.
- **Go version.** CI takes it from `go.mod` (`go-version-file`), so the `go` line of `go.mod` is the only place to change it: `go mod edit -go=<version>`, then `go get -u ./...`, `go get tool` and `go mod tidy`.
- **Names.** Each job has a short ID: its key in `ci.yml` (`static`, `test`, `race`, `fuzz`, `gui`), and the argument of `go run ./tools/ci`. The job's `name:` starts with the ID and a colon (`test: tests and coverage`). The two files the runner reads in `scripts/ci/` start with the ID of their job: `test-coverage-floors.txt` and `gui-smoke.ps1`.
- **The runner.** `tools/ci` (package `main`) runs the commands; `tools/ci/check` holds what can be tested without running one (the `go test -json` events, floors, the deadcode filter, fuzz lists, annotations), tested from recorded streams in its `testdata/`, with a coverage floor of its own. It imports nothing of RestoreSafe, and RestoreSafe imports nothing of it (`internal/architecture`). After a Go update, record the streams again if the format of `go test -json` changed (SPEC-ci section 8).
- **A new job.** Give it an ID and a name as above. Add it to the runner (`check.Jobs` and a method of the runner), to `ci.yml`, to the table in section 1 and a section of its own here, and its name to the required checks in `release-branch.json`; then apply the ruleset (CONTRIBUTING.md section 1). The name in the ruleset must be the job's `name:` exactly; renaming a job means changing the ruleset in the same commit and applying it.
