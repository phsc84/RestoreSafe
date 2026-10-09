# Documentation

| Document | Covers | Status |
|---|---|---|
| [SPEC-2.0.md](SPEC-2.0.md) | Container format 2, keys, manifest, what backup, restore, verify and retention do, configuration, security | Living; the format part is frozen for 2.x |
| [SPEC-gui.md](SPEC-gui.md) | The window: pages, dialogs, requirement IDs (`OV-1`, `BP-3`, ...), the workflow interface the window needs | Living |
| [GUI-TEST-CHECKLIST.md](GUI-TEST-CHECKLIST.md) | Manual tests of the window before a release | Living |
| [PLAN-gui-redesign.md](PLAN-gui-redesign.md) | Implementation plan of SPEC-gui.md | Done 2026-10-09; deleted after the 2.0.0 release |
| [SPEC-refactoring.md](SPEC-refactoring.md) | How a refactoring round works: when, standing constraints, baseline, review checklist, item format, template | Living |
| [PLAN-refactoring.md](PLAN-refactoring.md) | The current refactoring round; now the round of 2.0: findings RF-1 to RF-64 and their phases, result and carried-over items in its sections 17 and 18 | Done 2026-10-09; the next round replaces it right after the 2.0.0 release |

An implementation plan is deleted once its work is released; the refactoring plan keeps its name and gets the next round's content. Git history keeps both.

Code comments refer to sections as "2.0 spec 6.1" or "GUI spec 6.1", and to requirements as "GUI spec OV-8".

The layer rules of the code are in the package documentation of `internal/architecture`, and its test enforces them.
