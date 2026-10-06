# Documentation

| Document | Covers | Status |
|---|---|---|
| [SPEC-2.0.md](SPEC-2.0.md) | Container format 2, keys, manifest, what backup, restore, verify and retention do, configuration, security | Living; the format part is frozen for 2.x |
| [SPEC-gui.md](SPEC-gui.md) | The window: pages, dialogs, requirement IDs (`OV-1`, `BP-3`, ...), the workflow interface the window needs | Living |
| [GUI-TEST-CHECKLIST.md](GUI-TEST-CHECKLIST.md) | Manual tests of the window before a release | Living |
| [PLAN-gui-redesign.md](PLAN-gui-redesign.md) | Implementation plan of SPEC-gui.md | Nearly finished; deleted after the 2.0.0 release |
| [SPEC-refactoring.md](SPEC-refactoring.md) | How a refactoring round works: when, standing constraints, baseline, review checklist, item format, template | Living |
| [PLAN-refactoring-2.0.md](PLAN-refactoring-2.0.md) | The refactoring round after 2.0: findings RF-1 to RF-64 and their phases | Proposed |

Plans are deleted once their work is released; git history keeps them.

Code comments refer to sections as "2.0 spec 6.1" or "GUI spec 6.1", and to requirements as "GUI spec OV-8".

The layer rules of the code are in the package documentation of `internal/architecture`, and its test enforces them.
