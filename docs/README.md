# Documentation

| Document | Covers | Status |
|---|---|---|
| [CONTRIBUTING.md](CONTRIBUTING.md) | Branches, commits, pull requests, CHANGELOG, rules of the code, releases | Living |
| [GUIDE-development.md](GUIDE-development.md) | Setting up a Windows machine, building, updating Go, benchmarks, publishing a release step by step | Living |
| [GUIDE-ci.md](GUIDE-ci.md) | The CI jobs: what each checks, how to run it locally, how to fix a failure | Living |
| [GUIDE-gui-test.md](GUIDE-gui-test.md) | Manual tests of the window before a release | Living |
| [SPEC-core.md](SPEC-core.md) | Container format 2, keys, manifest, what backup, restore, verify and retention do, configuration, security | Living; the format part is frozen for 2.x |
| [SPEC-gui.md](SPEC-gui.md) | The window: pages, dialogs, requirement IDs (`OV-1`, `BP-3`, ...), the workflow interface the window needs | Living |
| [SPEC-ci.md](SPEC-ci.md) | The CI runner `tools/ci`: command line, jobs and their steps, GitHub annotations, its tests, the pre-push hook; for whoever changes it (using CI: GUIDE-ci.md) | Living |
| [SPEC-refactoring.md](SPEC-refactoring.md) | How a refactoring round works: when, standing constraints, baseline, review checklist, item format, template | Living |
| [PLAN-refactoring.md](PLAN-refactoring.md) | The refactoring round after 2.0.0: RF-40 and RF-65 to RF-97, its result and what it carries over; replaced by the next round's plan after 2.1.0 | Done 2026-10-15 |
| [PLAN-yubikey-page.md](PLAN-yubikey-page.md) | The YubiKey page of SPEC-gui 10.2 for 2.1.0, in six phases: diagnosis in `yubikey`, the YK-11 session with the real key, `keycheck`, view, window, docs; `cmd/yubidiag` goes | Agreed 2026-10-15 |

The names say what kind of document it is: `GUIDE-` how to do something (set up a machine, use CI, test the window), `SPEC-` what the program or a tool must do (living, kept up to date with the code), `PLAN-` work in progress. [CONTRIBUTING.md](CONTRIBUTING.md), the rules of the project, keeps the name GitHub recognises, which links it where pull requests and issues are opened.

An implementation plan is deleted once its work is released; the refactoring plan keeps its name and gets the next round's content. Git history keeps both: the GUI redesign plan and the plan of the round of 2.0 are at the tag `v2.0.0` (`git show v2.0.0:docs/PLAN-gui-redesign.md`).

Code comments refer to sections as "core spec 6.1" or "GUI spec 6.1", and to requirements as "GUI spec OV-8".

The layer rules of the code are in the package documentation of `internal/architecture`, and its test enforces them.
