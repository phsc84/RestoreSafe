# Refactoring plan: project structure

| | |
|---|---|
| Status | Agreed 2026-09-27; done (phases 1-5) |
| Branch | `v2` (after GUI phase G7, commit `9f2f21a`) |
| Scope | Folder and package structure only. No change in behavior, file formats, or the user interface. |

## 1. Why

The layout grew in the order features were built:

- `internal/` holds 17 packages side by side; window code, the backup format, and the workflows are not distinguishable.
- `util` (13 files) and `operation` (8 files) are grab bags; `security` mixes cryptography, YubiKey/WebAuthn, and terminal prompts that only tests still use.
- `ui` mixes the contract between workflows and frontends with a frontend (`ui.Console`, now test-only), and its name is one letter away from `gui`, although it is the opposite: the contract, not a user interface.
- `cmd/main.go` does not follow the one-folder-per-program convention that `cmd/yubidiag/` already follows.
- `assets/` mixes build inputs, README screenshots, and untracked icon sources hidden by exception rules in `.gitignore`.
- `test/` is both the build output and a personal sandbox; the release ZIP lands in the repository root.

## 2. Decisions

| # | Decision |
|---|---|
| 1 | Grouped layout: `internal/<group>/<package>`, at most two levels below `internal/`. |
| 2 | Groups: `gui` (the frontend), `workflow` (what the user starts, including `workflow/interact`, the contract with a frontend), `format` (the backup format on disk), `security` (key material: cryptography, recovery code, and YubiKey). |
| 3 | No `util`, `common`, or `helpers` package or group. `config`, `logging`, `fsx`, and `buildinfo` stay directly under `internal/`: small, unrelated, and named after what they contain. A `util` folder would recreate the grab bag one level up. |
| 4 | `security` is a group of three packages that handle key material and unlock methods: `cryptox`, `recovery`, `yubikey`. Having them in one folder scopes security reviews and makes changes there visible in a diff. |
| 5 | Unit tests stay next to the code (`*_test.go`). Only end-to-end tests (`internal/e2e`) and shared fixtures (`internal/testutil`) have their own packages. |
| 6 | The terminal password and line input is removed; tests that feed real stdin switch to scripted input. The `golang.org/x/term` dependency goes away. |
| 7 | A test enforces the dependency direction between the groups (section 4). |
| 8 | Icon sources (SVG/PNG, today untracked) are tracked in `assets/icon/`. |
| 9 | `dev_setup.txt` becomes `docs/DEVELOPMENT.md` and stays private (listed in `.gitignore`). |
| 10 | Build output goes to `dist/` (ignored). The personal test folder `test/` is renamed to `sandbox/` (ignored). |
| 11 | The 1.x console screenshot is kept for history, with the other screenshots in `docs/images/`. |
| 12 | The PowerShell GUI automation used for the manual checklist is kept in `scripts/gui-test/`. |
| 13 | Every move uses `git mv`, so file history is kept. |
| 14 | The contract between the workflows and a frontend is `workflow/interact`, not `ui`: `ui` is one letter away from `gui` but means the opposite. It lives in the workflow group because the workflows define what they need (Go defines interfaces on the consumer side). `interface` is not possible (a Go keyword) and would be too generic. Its test helper is `interacttest`, like `net/http/httptest`. |

Deliberately not done: no `pkg/` (RestoreSafe is not a library), no `src/`, no splitting of files that are fine as they are.

## 3. Target structure

```text
RestoreSafe/
├── cmd/
│   ├── restoresafe/            main.go, main_test.go          → RestoreSafe.exe
│   └── yubidiag/               main.go                        → developer tool, not released
├── internal/
│   ├── gui/                    window, screens, dialogs, bridge between worker and UI thread
│   │   └── win32/              Win32 wrapper (only gui uses it)
│   ├── workflow/
│   │   ├── interact/           contract with a frontend: UI, Report, Progress, Result, ErrCancelled (today ui)
│   │   │   └── interacttest/   scripted UI for tests: Script (today ui.Console)
│   │   ├── backup/             backup workflow, keys and enrollment, planning, preflight, retention
│   │   ├── restore/            restore workflow
│   │   ├── verify/             verify workflow
│   │   ├── health/             startup health check (today startup)
│   │   ├── unlock/             unlocking key sets
│   │   ├── staging/            local staging
│   │   ├── restorepoint/       decrypt pipeline, restoring and verifying a restore point
│   │   └── job/                what every run shares: logger setup, progress, cancellation, preflight rows, source checks
│   ├── format/
│   │   ├── archive/            TAR build and extraction, Windows file metadata
│   │   ├── container/          set header, key set, sections, trailer, parts, multi-part writer
│   │   ├── manifest/           manifest format
│   │   ├── catalog/            inventory of the backup directory, selection
│   │   ├── naming/             backup IDs, backup entries, part and log file names
│   │   └── setio/              writing a backup set
│   ├── security/
│   │   ├── cryptox/            encryption, Argon2, key sealing, subkeys, random bytes, ZeroBytes
│   │   ├── recovery/           recovery code: generate, parse, checksum
│   │   └── yubikey/            FIDO2 hmac-secret through Windows WebAuthn
│   ├── config/                 config.yaml, exclude patterns
│   ├── logging/                run log files
│   ├── fsx/                    disk space, paths and volumes, directory checks, file copy, backup lock, I/O counters
│   ├── buildinfo/              application version stamped by the build
│   ├── architecture/           dependency-direction test (section 4)
│   ├── testutil/               shared test fixtures
│   └── e2e/                    end-to-end tests of the workflows
├── build/windows/              RestoreSafe.ico, RestoreSafe.manifest, versioninfo.json
├── assets/icon/                icon sources (SVG, PNG, dark and light variants)
├── docs/
│   ├── SPEC-restoresafe-2.0.md, SPEC-restoresafe-gui.md, GUI-TEST-CHECKLIST.md, REFACTORING-PLAN.md
│   ├── DEVELOPMENT.md          private (ignored)
│   └── images/                 README screenshots (v2.0.0 and the 1.x one)
├── scripts/gui-test/           PowerShell UI automation and screenshot helpers, with a README
├── dist/                       build output: RestoreSafe.exe, RestoreSafe-x.y.z.zip (ignored)
├── sandbox/                    personal manual test folder (ignored)
└── build.bat  config-SAMPLE.yaml  README.md  CHANGELOG.md  LICENSE  go.mod  go.sum  .gitignore  .gitattributes
```

`config-SAMPLE.yaml` stays in the root: it ships in the ZIP and users look for it there.

## 4. Dependency direction

Imports point downward only. The layers, from top to bottom (a package may import its own layer and every layer below it):

```text
cmd             restoresafe, yubidiag
gui             gui, gui/win32
workflow        interact, backup, restore, verify, health, unlock, staging, restorepoint, job
format          archive, container, manifest, catalog, naming, setio
config, logging config → security/cryptox (Argon2 bounds); logging → buildinfo
security        cryptox, recovery, yubikey (recovery and yubikey → cryptox)
fsx, buildinfo  no internal imports
```

`format` imports `config` (exclude patterns in `archive` and `setio`, the authentication mode in `container`, the configuration in `catalog`), so `config` sits below `format`, not beside it.

Additional rules:

- No workflow package imports `backup`, `restore`, `verify`, or `health`; only the frontend starts workflows.
- `workflow/interact` (and `interacttest`) imports no other workflow package: the contract does not depend on workflow code.
- `gui/win32` has no internal imports.
- `testutil`, `e2e`, and `architecture` are test support: exempt from the layers, never imported by production code.

`internal/architecture/architecture_test.go` enforces all of this on the production imports reported by `go list`, and fails for a package that belongs to no layer.

## 5. Where today's code goes

| Today | Target |
|---|---|
| `cmd/main.go`, `cmd/main_test.go` | `cmd/restoresafe/` |
| `internal/gui` | `internal/gui` (unchanged) |
| `internal/win32` | `internal/gui/win32` |
| `internal/ui` (`ui.go`, `report.go`, `progress.go`, `result.go`) | `internal/workflow/interact` (package `interact`) |
| `internal/ui/console.go` + tests | `internal/workflow/interact/interacttest` (`Console` becomes `Script`) |
| `internal/backup`, `restore`, `verify` | `internal/workflow/backup`, `restore`, `verify` |
| `internal/startup` | `internal/workflow/health` (`startup.CheckHealth` becomes `health.Check`, `HealthCheckResult` becomes `health.Result`) |
| `internal/operation/unlock.go` | `internal/workflow/unlock` (`unlock.Options`, `KeySet`, `KeySets`, `MasterKeys`; `PasswordFailurePrefix` moved here from `runtime.go`) |
| `internal/operation/staging.go` | `internal/workflow/staging` (`staging.Plan`, `PlanLocal`, `Scope`, `NewScope`, `CreateDir`, `CleanupDir`) |
| `internal/operation/restorepoint.go`, `decrypt_pipeline.go` | `internal/workflow/restorepoint` (`ProcessRestorePoint` becomes `restorepoint.Process`) |
| `internal/operation/progress.go`, `runtime.go`, `preflight.go`, `source_validation.go` | `internal/workflow/job` (named `job`, not `run`: "run" is the domain term for one backup run and a common variable name) |
| `internal/archive`, `container`, `manifest`, `catalog`, `setio` | `internal/format/...` |
| `internal/util/naming.go` | `internal/format/naming` |
| `internal/util/split.go` | `internal/format/container` (the multi-part writer; `container`'s own tests use it, and `setio` builds on `container`, so it cannot live in `setio`) |
| `internal/util/config.go`, `exclude.go` | `internal/config` |
| `internal/util/logging.go` | `internal/logging` |
| `internal/util/version.go` | `internal/buildinfo` (`util.AppVersion` becomes `buildinfo.Version`; the container imported `util` only for it) |
| `internal/util/counting.go`, `disk.go`, `file_copy.go`, `file_system.go`, `format.go`, `lock.go`, `path.go` | `internal/fsx` |
| `internal/security/crypto.go`, `ZeroBytes` | `internal/security/cryptox` |
| `internal/security/recovery.go` | `internal/security/recovery` (`recovery.Code`, `recovery.Generate`, `recovery.Parse`) |
| `internal/security/fido2.go` | `internal/security/yubikey` |
| `internal/security/prompt.go`: `ReadPasswordConfirmed`, `ErrPasswordEmpty`, `ErrPasswordMismatch` | `internal/workflow/interact` |
| `internal/security/prompt.go`: terminal `ReadPassword`, `ReadLine` | removed |
| `internal/testutil`, `internal/e2e` | unchanged |
| (new) | `internal/architecture` (dependency-direction test) |
| `assets/RestoreSafe.ico`, `assets/RestoreSafe.manifest`, `versioninfo.json` | `build/windows/` |
| `assets/Screenshot_*.png` | `docs/images/` |
| `assets/RestoreSafe_dark*`, `assets/RestoreSafe_light*` (untracked) | `assets/icon/` (tracked) |
| `dev_setup.txt` (untracked) | `docs/DEVELOPMENT.md` (ignored) |
| `test/` (untracked) | `sandbox/` (ignored) |
| Build output in `test/`, ZIP in the root | `dist/` |

## 6. Phases

Each phase ends with `go build ./...`, `go vet ./...`, `go test ./...` green, a build through `build.bat`, and a commit.

1. **Repository root.** `build/windows/`, `docs/images/` (README links), `assets/icon/`, `scripts/gui-test/`, `dist/` output in `build.bat` (resource paths in `versioninfo.json` and `build.bat`), `test/` → `sandbox/`, `dev_setup.txt` → `docs/DEVELOPMENT.md`, simplified `.gitignore`.
2. **Entry point.** `cmd/main.go` → `cmd/restoresafe/`; `build.bat` builds `./cmd/restoresafe` and generates `cmd/restoresafe/resource.syso`.
3. **Foundation packages.** Split `util` into `config`, `logging`, `fsx`, `buildinfo`, `format/naming`, and `split.go` into `container`; split `security` into `security/cryptox`, `security/recovery`, and `security/yubikey`; rename `ui` to `workflow/interact` and move `ReadPasswordConfirmed` there; switch the stdin-based tests to scripted input and remove the terminal input and `golang.org/x/term`.
4. **Workflows and frontend.** Split `operation` into `workflow/unlock`, `staging`, `restorepoint`, `job`; move `backup`, `restore`, `verify`, `startup` under `workflow/`; `interact.Console` → `interact/interacttest.Script`; `win32` → `gui/win32`.
5. **Format and checks.** Move `archive`, `container`, `manifest`, `catalog`, `setio` under `format/`; add the dependency-direction test; update the package table in the GUI spec (the 2.0 spec has none), the README development section, and `docs/DEVELOPMENT.md`; mark this plan as done.

## 7. Notes

- Package names avoid clashes with the standard library and with common variable names: `cryptox` (not `crypto`, which is a standard library package; not `cipher`, which `cryptox` itself imports as `crypto/cipher`; not `secret`, `key`, or `keys`, which are frequent variable names), following the `fsx` scheme. `recovery` needs one local variable of the same name renamed.
- After phase 1, `sandbox/` keeps its content; configuration files in it that point to paths inside the old `test/` folder need their paths updated by hand.
- The GUI automation scripts depend on control texts and window classes of the GUI; they are tools for the manual checklist, not part of `go test`.
