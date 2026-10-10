# Documentation

| Document | Covers | Status |
|---|---|---|
| [CONTRIBUTING.md](CONTRIBUTING.md) | Branches, commits, pull requests, CHANGELOG, rules of the code, releases | Living |
| [DEVELOPMENT.md](DEVELOPMENT.md) | Setting up a Windows machine, building, updating Go, benchmarks, publishing a release step by step | Living |
| [CI.md](CI.md) | The CI jobs: what each checks, how to run it locally, how to fix a failure | Living |
| [SPEC-core.md](SPEC-core.md) | Container format 2, keys, manifest, what backup, restore, verify and retention do, configuration, security | Living; the format part is frozen for 2.x |
| [SPEC-gui.md](SPEC-gui.md) | The window: pages, dialogs, requirement IDs (`OV-1`, `BP-3`, ...), the workflow interface the window needs | Living |
| [GUI-TEST-CHECKLIST.md](GUI-TEST-CHECKLIST.md) | Manual tests of the window before a release | Living |
| [SPEC-refactoring.md](SPEC-refactoring.md) | How a refactoring round works: when, standing constraints, baseline, review checklist, item format, template | Living |
| [PLAN-refactoring.md](PLAN-refactoring.md) | The current refactoring round: the round after 2.0.0, items from RF-65 on, and RF-40 carried over | Started 2026-10-10 |

An implementation plan is deleted once its work is released; the refactoring plan keeps its name and gets the next round's content. Git history keeps both: the GUI redesign plan and the plan of the round of 2.0 are at the tag `v2.0.0` (`git show v2.0.0:docs/PLAN-gui-redesign.md`).

Code comments refer to sections as "core spec 6.1" or "GUI spec 6.1", and to requirements as "GUI spec OV-8".

The layer rules of the code are in the package documentation of `internal/architecture`, and its test enforces them.
