# Specification: Refactoring rounds

| | |
|---|---|
| Status | Living; first used for [PLAN-refactoring-2.0.md](PLAN-refactoring-2.0.md) |
| Applies to | Every refactoring round of RestoreSafe: structure, dead and legacy code, performance, security hardening, tests, tooling, build, and docs |
| Not for | New features and changes to what the program does for the user; those get their own spec |

## 1. Purpose and cadence

Technical debt grows with every feature. A refactoring round pays it down on purpose instead of in passing: measure the code, review it against a fixed checklist, write down what is worth changing, change it in a safe order, and measure again.

This document says how a round works. Each round has its own plan, `docs/PLAN-refactoring-<release>.md`, named after the release it follows. The plan holds the findings and their state; this document holds what stays the same from round to round.

**When to start a round:**

- after each major or minor release (2.0.0, 2.1.0, ...), once the release fixes are out;
- earlier when a signal says so: CI got noticeably slower, a coverage floor was lowered to let a change through, `deadcode` or staticcheck report new findings, a file passed the size limits of section 5.1, or a feature took much longer than it should because of the code around it.

At most one round is open at a time. A round that can't finish before the next release is cut down to what it finished; the rest moves to the next round (section 8).

## 2. Standing constraints

Every round keeps these. A change that would break one is out of scope, not a trade-off. A round's plan may add constraints, never remove one.

1. **The backup format of the current major version is frozen.** Every backup written by any x.0.0 release restores unchanged with every later x.y build. This includes fields that look like leftovers. The format-fixture tests (`internal/format/testdata/`) enforce it; a round never edits existing fixtures, it only adds new ones.
2. **Behaviour is preserved.** The GUI's texts, the log file format, the order of questions, and the exit behaviour stay the same unless an item says otherwise and names the user-visible change for the CHANGELOG.
3. **The layer rules stay and get stricter, never looser.** `internal/architecture` is the reference; a new package is added to its test in the same commit.
4. **No unattended operation.** No item may introduce scheduling, background backups, or stored credentials (SPEC-2.0 section 1.3).
5. **The build and all tests pass after every commit.** A phase may leave an improvement unfinished, never something broken.
6. **Delete, don't keep.** Replaced code goes in the same commit. No compatibility shims inside the program, no commented-out code, no "old" copies of files.
7. **Measure before claiming.** A performance item records the numbers before and after; a "simplification" that adds lines or indirection needs a reason in the item.

## 3. A round, step by step

1. **Branch.** `refactor-<release>` from the release branch, after the release.
2. **Baseline.** Run the measurements of section 4 and copy the previous round's baseline table into the new plan with a column for each round, so trends are visible.
3. **Review.** Go through the checklist of section 5, area by area. Write down only findings with evidence: a file and line, or a tool's output. "Could be nicer" without a cost is not a finding.
4. **Carry over.** Add the items the previous round moved on (section 8), re-checked: some will have gone away by themselves.
5. **Write the plan** from the template in section 9: items (section 6), phases (section 7), open questions, and what is not part of the round.
6. **Decide.** The owner answers the open questions and confirms the plan; its status becomes "Agreed <date>".
7. **Work** phase by phase. Mark each item done in the plan in the commit that finishes it ("Done in <commit>"), or dropped with a reason.
8. **Close** the round (section 8).

## 4. Baseline

Run on a clean checkout of the round's starting commit and record the results in the plan. Once CI runs these (refactoring 2.0 RF-1), its output for that commit is the baseline. The tools are pinned in `go.mod` (refactoring 2.0 RF-2); until then, use `go run <module>@<version>`.

| Measure | Command | Record |
|---|---|---|
| Size | `git ls-files \| wc -l`; Go lines with `wc -l` over `git ls-files '*.go'` | files, Go lines, the 5 largest production files |
| Toolchain and dependencies | `go version`; `go list -m all`; `go list -m -u all` | Go version, direct dependencies, which have updates |
| Vet | `go vet ./...` | findings |
| Tests and coverage | `go test -count=1 -coverprofile=cover.out ./...`; `go tool cover -func=cover.out` | pass or fail, total and per-package coverage |
| Races | `go test -race ./...` with `CGO_ENABLED=1` and a gcc | pass or fail |
| Static analysis | `go tool staticcheck ./...` (project configuration) | findings by check |
| Dead code | `go tool deadcode -test ./...` | unreachable functions |
| Vulnerabilities | `go tool govulncheck -show verbose ./...` | called, imported, and required-only findings |
| Formatting | `gofmt -l .` | files listed |
| Fuzzing | each fuzz target for 30 s | crashes |
| Throughput | the opt-in benchmarks of DEVELOPMENT.md section 11, SSD and network share | median MiB/s, backup and restore |
| Markers | `git grep -n -E "TODO\|FIXME\|XXX\|HACK"` | count and where |
| Docs | the index in `docs/README.md` against the files in `docs/` | missing or stale documents |

## 5. Review checklist

For each area: what to look at, and how to find it. Not every point yields an item every round; every point is looked at.

### 5.1 Structure and modularity

- Layering: `internal/architecture` passes, and no rule was loosened since the last round (`git log -p internal/architecture`).
- Package boundaries: a package that every change touches, or two packages that always change together (`git log --name-only` since the last round).
- File size: production files over 500 lines, and in `internal/gui` over 500 lines per page file.
- Function size and signatures: functions over ~80 lines; more than 6 parameters, or several parameters of the same type in a row; `context.Context` kept in a struct instead of passed first.
- Global state: package-level variables that hold state (not constants, tables or lazy DLL procs).
- Duplication: the same sequence of steps in two workflows, or two places that compute the same fact (the UI must show what the workflow does, by construction).
- Plans and specs: target structures in plans that the code no longer matches.

### 5.2 Dead and legacy code

- `deadcode -test` and staticcheck U1000 findings. Struct fields that only keep a Windows ABI layout are not dead; they carry a `//lint:ignore U1000` with the reason and a layout test.
- Wrapper functions in `gui/win32` with no caller.
- Conversions that undo each other (a value turned into flags and back).
- Wording and comments of an older version: file kinds, formats or screens that no longer exist (e.g. 1.x `.challenge` files, the first GUI).
- Protocols that rely on text: code that parses or matches text meant for people (prompts, log lines) to decide what to do.
- Package documentation that describes steps the package no longer performs.

### 5.3 Performance and efficiency

- Profile the throughput benchmarks (`-cpuprofile`, `-memprofile`) and list the top functions in the plan before proposing any change.
- Allocations per chunk, per file, or per manifest entry in the backup and restore paths (`testing.B` with `b.ReportAllocs`).
- Memory that grows with the number of files (manifest, maps) against the limits in SPEC-2.0 section 5.3.
- Blocking calls on the UI thread; snapshot and file-system work that can hang on a network drive.
- Compare with the previous round's throughput; a drop of more than 10 % is a P1 finding.

### 5.4 Security hardening

- Untrusted input: every parser of bytes from the backup directory or the configuration (header, key set, trailer, chunk framing, manifest, file names, challenge JSON, recovery code, config.yaml) has a fuzz target and bounds on what it allocates.
- Restore safety: paths from a backup can't leave the destination (lexical check, reparse points, `O_EXCL`).
- Secrets: passwords, recovery codes and derived keys live in `[]byte` and are zeroed; no secret passes through a `string`, a log line, an error text, or the clipboard history.
- Concurrency between RestoreSafe processes: what the backup-directory lock covers, and what happens without it.
- Windows specifics: DLL search order, file and directory permissions of what RestoreSafe writes, the application manifest.
- Cryptography: algorithms, KDF bounds and defaults against current guidance (OWASP, RFC 9106); a change to defaults is a feature with its own spec, not a refactoring item.
- Dependencies and supply chain: `govulncheck`, tool versions pinned, release checksums (and signing, if decided).
- Documentation of what is plaintext and what is encrypted matches the format.

### 5.5 Quality assurance and testing

- Per-package coverage against the floors in CI; packages that decide whether damage is detected (format, restorepoint) first.
- Tests that are flaky, slow (over 10 s per package), sleep, depend on today's date, or touch process-global state (`os.Stdout`, environment, working directory) while running in parallel.
- The fault-injection matrix (damaged, missing, truncated parts; full disk; cancel in every phase) has a test per row.
- The format fixtures restore bit-exact.
- The GUI scripts in `scripts/gui-test` still find every control (`ids_test.go`) and run.
- The manual checklist (`docs/GUI-TEST-CHECKLIST.md`) has no row that a script could cover instead.

### 5.6 Tooling, build and CI

- CI runs every measurement of section 4 that is cheap enough, and fails on regressions.
- Go version and dependencies: update to the current Go release and dependency versions (`go get -u ./...`, then `go mod tidy`), unless an item says why not.
- `build.bat` (or its successor) changes no tracked file and gives the same exe for the same commit.
- Line endings: `.gitattributes` covers every file type in the repository.

### 5.7 Documentation

- README, CHANGELOG, `config-SAMPLE.yaml`, and the specs against the code: defaults, option names, screenshots, statuses.
- `docs/README.md` lists every document with its status; finished plans are deleted after their release.
- Code comments cite specs as "2.0 spec n" or "GUI spec n"; a cited section still says what the comment claims.

## 6. Items

Every finding becomes one item in the plan:

```markdown
**RF-n (P1|P2|P3) Short title.** Evidence: what is wrong, with file:line links or tool output.
Change: what to do (where the work is, not a full design).
Acceptance: how to see it is done (a command, a test, a measurement).
```

- **P1** fixes a defect, a security gap, or something that blocks the other work.
- **P2** removes real cost: duplication, legacy protocol, measurable slowness, missing tests on code that decides correctness.
- **P3** is cleanup worth doing while the code is open anyway.

IDs restart at RF-1 in every round and are grouped by area (e.g. RF-1x tests, RF-5x security), leaving gaps for items found later. Outside its plan, cite an item with its round: "refactoring 2.0 RF-26". An item is never renumbered; a dropped item keeps its ID with "Dropped: <reason>".

## 7. Phases

- **Tooling first.** CI, formatting and broken test tooling come before anything that needs them to be checked.
- **Safety net before change.** Tests that pin current behaviour (fixtures, missing coverage on code the round will change) come before the change.
- **Release fixes early, large diffs late.** User-visible fixes go in a phase that can ship with the next patch release; renames and moves that touch many files come last, so they don't conflict with everything else.
- **Each phase is a series of small commits,** each building and passing, each with one purpose. A phase ends with the baseline commands of section 4 run again and their results added to the plan.

## 8. Closing a round

A round is done when every P1 and P2 item is done or dropped with a reason, and the round's own "Done when" list holds. Then:

1. Run the baseline (section 4) once more and record it as the round's result in the plan.
2. Copy the unfinished P3 items, with their evidence re-checked, into a "Carried over" section at the end of the plan; the next round starts from there.
3. Update this document with what the round taught: a new checklist point for a kind of finding the checklist missed, a new standing constraint, a better command.
4. Mark the plan "Done <date>" in `docs/README.md`. Delete it once the next release is out: git history keeps it, and the next round's plan carries its baseline and carried-over items forward.

## 9. Template for a round's plan

```markdown
# Plan: Refactoring round after <release>

| | |
|---|---|
| Status | Proposed <date> |
| Follows | [SPEC-refactoring.md](SPEC-refactoring.md) |
| Baseline | `<branch>` at `<commit>` |
| Branch | `refactor-<release>` |
| Scope | <areas; what is explicitly not touched> |

## 1. Purpose
<state of the code in two sentences; what this round is after>

## 2. Baseline (measured <date>)
<the table of section 4, one column per round>

## 3. Constraints
The standing constraints of SPEC-refactoring section 2 apply. <added constraints, if any>

## 4.-n. Items by area
<RF items in the format of section 6>

## Phases
| Phase | Items | When | Why then |

## Open questions
<decisions for the owner; the plan works with either answer>

## Not part of this round

## Done when

## Carried over (filled in when the round closes)
```
