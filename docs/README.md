# Documentation

| Document | Covers | Status |
|---|---|---|
| [CONTRIBUTING.md](CONTRIBUTING.md) | Branches, commits, pull requests, CHANGELOG, rules of the code, releases | Living |
| [GUIDE-development.md](GUIDE-development.md) | Setting up a Windows machine, building, updating Go, benchmarks, publishing a release step by step | Living |
| [GUIDE-ci.md](GUIDE-ci.md) | The CI jobs: what each checks, how to run it locally, how to fix a failure | Living |
| [GUIDE-gui-test.md](GUIDE-gui-test.md) | Manual tests of the window before a release | Living |
| [SPEC-core.md](SPEC-core.md) | Container format 2, keys, manifest, what backup, restore, verify and retention do, configuration, security | Living; the format part is frozen for 2.x |
| [SPEC-gui.md](SPEC-gui.md) | The window: pages, dialogs, requirement IDs (`OV-1`, `BP-3`, ...), the workflow interface the window needs | Living |
| [SPEC-kdf.md](SPEC-kdf.md) | Argon2id parameters of the key sets: threat model, defaults, enrollment and read limits, memory check; identical copy in MountSafe | Agreed 2026-10-25, not implemented |
| [SPEC-ci.md](SPEC-ci.md) | The CI runner `tools/ci`: command line, jobs and their steps, GitHub annotations, its tests, the pre-push hook; for whoever changes it (using CI: GUIDE-ci.md) | Living |
| [SPEC-refactoring.md](SPEC-refactoring.md) | How a refactoring round works: when, standing constraints, baseline, review checklist, item format, template | Living |
| [PLAN-refactoring.md](PLAN-refactoring.md) | The refactoring round after 2.1.0: the points carried over from the round after 2.0.0 and what 2.1.0 added; items RF-98 to RF-126 from the review | Reviewed 2026-10-24 and 2026-10-25, except performance |

The names say what kind of document it is: `GUIDE-` how to do something (set up a machine, use CI, test the window), `SPEC-` what the program or a tool must do (living, kept up to date with the code), `PLAN-` work in progress. [CONTRIBUTING.md](CONTRIBUTING.md), the rules of the project, keeps the name GitHub recognises, which links it where pull requests and issues are opened.

Documents in `docs/` cover what the project relies on: the rules, the specifications, and the guides every contributor or every release needs. A tool or script for a single task has a README next to its code, which says how to build and run it: [scripts/gui-test](../scripts/gui-test/README.md) (the scripted window tests), [tools/locktest](../tools/locktest/README.md) (the backup lock after network drops).

An implementation plan is deleted once its work is released; the refactoring plan keeps its name and gets the next round's content. Git history keeps both: the GUI redesign plan and the plan of the round of 2.0 are at the tag `v2.0.0` (`git show v2.0.0:docs/PLAN-gui-redesign.md`).

Code comments refer to sections as "core spec 6.1" or "GUI spec 6.1", and to requirements as "GUI spec OV-8".

The layer rules of the code are in the package documentation of `internal/architecture`, and its test enforces them.
