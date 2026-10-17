# Plan: The YubiKey page

| | |
|---|---|
| Status | Agreed 2026-10-15; phases 1 to 5 done, phase 6 next |
| Implements | [SPEC-gui.md](SPEC-gui.md) 10.2 (YK-1 to YK-11) and decision 10 |
| Release | 2.1.0 |
| Branch | `dev` |
| Scope | The YubiKey page, its sidebar item and links, `yubikey.Diagnose` and `yubikey.ProbeCapability`, the package `workflow/keycheck`, and the removal of `cmd/yubidiag`. **No change to the backup format of 2.x**, to the keys, or to what backup, restore and verify do. |
| Hardware | One YubiKey, which may be reset. The cases that need a second key or a FIDO-only Security Key are tested with fakes only (section 4). |

## 1. Goals

1. **Every YK requirement is covered.** Section 3 maps each requirement to the phase that implements it. The spec's test rows "YubiKey diagnosis and test" and "YubiKey view" (16) are the tests' checklist.
2. **YK-11 is answered early,** with the real key, before the view is written (phase 2), so the hero variants and texts are built once.
3. **The build and tests pass after every phase.** A phase may leave the page missing, never broken. Each phase ends with gofmt, vet, staticcheck and the full `go test -cover` with the floors checked locally, and a commit. `deadcode` (without `-test`) fails CI until phase 5: `Diagnose`, `ProbeCapability`, `keycheck` and the view have no production caller before the window uses them. So phases 1 to 4 stay local, and the first push comes after phase 5, with every job of `go run ./tools/ci fast` passing.
4. **Delete, don't keep.** `cmd/yubidiag` goes in phase 1, when its code moves to `yubikey`. Until the page exists, the hardware test of phase 1 does its job.
5. **No secret leaves `keycheck`.** The derived secret is zeroed in every path, and the technical details never hold a PIN, secret, salt or credential ID. Tests prove both.

## 2. Decisions

| # | Decision | Rationale |
|---|---|---|
| 1 | WebAuthn failures become a typed error, `yubikey.HRESULTError{Op, Code}`, wrapped by the existing `problem` errors. It has `Cancelled()` for `NTE_USER_CANCELLED` (0x80090036) and `HRESULT_FROM_WIN32(ERROR_CANCELLED)` (0x800704C7), until phase 2 shows which one Windows returns, plus whatever else phase 2 finds. | `keycheck` must tell cancel from failure and show the HRESULT (YK-7, YK-9). Today the HRESULT exists only inside a formatted string. The messages that restore and backup show stay the same. |
| 2 | `Diagnose` reads the HID usage page, product ID and USB device version from the device's hardware IDs (`HID_DEVICE_UP:F1D0_U:0001`, `HID\VID_1050&PID_0407&REV_0543&MI_01`). The interfaces of one key are grouped by their USB parent (`CM_Get_Parent`). The device is opened only to learn whether write access is granted. (Changed in phase 1 from `HidP_GetCaps`, which needs an open handle.) | YK-4: FIDO is found by usage page `0xF1D0`, also on single-function keys without `MI_xx`. Hardware IDs need no handle, so the FIDO interface, which Windows may not open without administrator rights, is still found. |
| 3 | Enumeration, attributes, capabilities, registry and WebAuthn version are function variables in `yubikey`, replaced in tests, like `fido2MakeCredFn` today. | The spec's test row asks for an injected HID enumeration. It follows the package's existing pattern, without a new interface. |
| 4 | A manual hardware test, `internal/security/yubikey/hardware_test.go` with build tag `yubikeyhw`, prints `Diagnose` and runs the WebAuthn cases of phase 2, step by step. It is kept after phase 2. | It replaces `yubidiag` until the page exists, and it remains the way to answer YK-11 again for new firmware or a new Windows build. The build tag keeps it out of CI and coverage. |
| 5 | Device changes come from `WM_DEVICECHANGE` with `DBT_DEVNODES_CHANGED`, which Windows sends to every top-level window without registration. A 500 ms timer restarts on each one (YK-3). | No `RegisterDeviceNotification` handle to manage. Diagnose is cheap, and a check after an unrelated USB device is harmless. If phase 2 shows that the message isn't sent for a YubiKey, `RegisterDeviceNotification` for the HID interface class takes its place. |
| 6 | **Open Windows Update…** in the Windows error hero (figure 10.4) opens `ms-settings:windowsupdate`. | The figure shows the button, but no requirement names its target. Phase 4 adds the sentence to YK-8. |
| 7 | "After a backup that created new keys" (YK-6) is detected as a change of `Keys.Created` between two snapshots. | The snapshot is already taken after every backup. No new signal from the backup workflow is needed. |
| 8 | The test result lives in the app (UI thread), not in `keycheck`: `keycheck.Run` returns a `Result`, and the page keeps the last one. | "Kept in memory for the session" (YK-6), cleared by app events (device change, Reload, new keys) that only the app sees. |

## 3. Requirements by phase

| Req | What | Phase |
|---|---|---|
| YK-1 | Sidebar item at the bottom below a line; visibility from `authentication_mode` and `Keys.YubiKeyInBackups`; switch to Settings when it disappears; `Ctrl+4`; one tab stop | 3 (data), 4 (view), 5 (widget, app) |
| YK-2 | Page order: title, Check again, hero, Checks, guide, details; own verdict, never changes 3.5 or the Create backup hero | 4, 5 |
| YK-3 | Automatic checks: on show, Check again, F5, Reload, 500 ms after a device change; worker goroutine; stale results dropped; "Checking your YubiKey…" | 1 (`Diagnose`), 5 |
| YK-4 | Checks card: Windows, Connected (usage page, product IDs, firmware, no model names), Your keys | 1, 4 |
| YK-5 | Test: Match over all YubiKey slots of the current key set, or Capability; secret zeroed after its length check | 1 (`ProbeCapability`), 3 (`keycheck`) |
| YK-6 | During the test: "Follow the Windows Security prompt", actions disabled, one worker slot, not during an operation; result per session and when it is cleared | 4, 5 |
| YK-7 | Results to hero variants; cancel hint; HRESULT in details | 3, 4 |
| YK-8 | Setup guide: four steps, warning, footnote and link, Open Windows settings…, when it is expanded, strings in `view/strings.go` | 4, 5 |
| YK-9 | Technical details: versions, interfaces, registry, last test, WebAuthn trace; no secrets; Copy details | 1, 3, 4, 5 |
| YK-10 | Retired 2026-10-16 (owner's review): no links to the page; the sidebar is the way there | — |
| YK-11 | Confirm with the real key, record in the spec, adapt variants and texts | 2 |
| 14 | `cmd/yubidiag` and `RESTORESAFE_FIDO2_DEBUG` removed; no `GetConsoleWindow` fallback (13.4) | 1 |

## 4. Hardware coverage with one YubiKey

| Case | How | Where |
|---|---|---|
| Registered key answers (Match) | Credential made in the session, then an assertion with it | Phase 2, phase 6 with a real backup |
| Key without the requested credential | Assertion with a random credential ID: the key can't open it, as an unregistered key can't | Phase 2 |
| Cancel | Cancel in the PIN dialog | Phase 2, phase 6 |
| Wrong PIN, soft block, blocked PIN | Wrong PINs: 3 in a row need a replug, 8 in all block the PIN | Phase 2 |
| Fresh key without a PIN | After the reset that unblocks the PIN, register before setting a PIN | Phase 2 |
| FIDO2 turned off | Turned off with Yubico Authenticator (run as administrator), then turned on again | Phase 2, optional |
| `bcdDevice` versus firmware | `Diagnose` compared with the firmware Yubico Authenticator shows; one data point only | Phase 2 |
| Two keys connected | Not possible with one key | Fakes in `yubikey` and `view` tests only |
| FIDO-only Security Key (no `MI_xx`) | Not possible | Fakes in `yubikey` tests only |

## 5. Phases

### Phase 1: `yubikey` diagnosis, test calls, and the end of `yubidiag`

- [x] `HRESULTError` (decision 1), returned through the existing errors of `realMakeCredential` and `realGetHmacSecret`. Restore and backup still show the same texts; a test checks `errors.As` through the wrapping. `CheckAvailability` is gone: yubidiag was its only caller, and `Diagnosis.APIErr` holds the same error.
- [x] `Diagnose() Diagnosis` with the seams of decision 3 (`hidDevicesFn`, `hidOpenFn`, `hidDriversFn`, `webauthnVersionFn`):
  - WebAuthn API version, the minimum, and the error of `CheckAvailability`;
  - per Yubico HID interface: path, usage page, function (FIDO, OTP, other), product ID, USB device version, and write access or the open error;
  - the registry driver entries of `VID_1050`.

  No prompt, no write access kept open. Moved from `cmd/yubidiag`.
- [x] `Diagnosis` helpers the view needs: FIDO present, number of keys (grouped by device instance, not by interface), and the functions per key from the product ID.
- [x] `ProbeCapability() error`: a non-resident registration plus one hmac-secret with a random salt; the secret zeroed after its length check. It returns no credential ID.
- [x] `dialogParent`: the `GetConsoleWindow` fallback goes (13.4); 0 means no owner window.
- [x] `DebugOutput` comment: set only by the YubiKey test.
- [x] Tests (the spec's row "YubiKey diagnosis and test", `yubikey` part): FIDO by usage page with and without `MI_xx`, no Yubico device, two YubiKeys, an interface that can't be opened, WebAuthn missing and too old, `ProbeCapability` zeroing with fakes. The `yubikey` floor (62) holds or rises.
- [x] `hardware_test.go` with tag `yubikeyhw` (decision 4): prints the `Diagnose` report, and runs one case per `-run` name for phase 2.
- [x] Remove `cmd/yubidiag`, its line in [internal/architecture/doc.go](../internal/architecture/doc.go), and the README section "YubiKey diagnostic tool". In the README's folder table, `cmd/restoresafe` stays the only command.

### Phase 2: YK-11 with the real key (owner and Claude together)

Done 2026-10-16 with the owner's YubiKey 5 (firmware 5.7.4), which was reset on purpose. The answers are in SPEC-gui YK-11:
- Every refusal ends in the same `0x800704C7` (`ERROR_CANCELLED`): an unknown credential, wrong PINs, the soft block, the blocked PIN, and a cancel in either dialog. So there is no "doesn't open your backups" variant. The cancel hero gets a second hint (a PIN entered wrong too often), and the guide opens after a cancel (YK-7, YK-8).
- A reset key without a PIN gets one during the registration, so the guide's step 2 says so.
- `bcdDevice` equals the firmware. The Connected line shows it, and firmware below 5.2 gets the new hero "Error: firmware too old" (YK-4, figure 10.4).
- A WebAuthn call without a parent window fails with `NTE_INVALID_PARAMETER` (`0x80090027`); 13.4 is corrected. The hardware tests use the foreground window.
- Capability asks for the PIN twice with two touches (figure 10.3); Match asks for the PIN once with one touch.
- `HRESULTError` needed no new codes: `Cancelled()` already accepts `0x800704C7`.

Moved to phase 5: whether `DBT_DEVNODES_CHANGED` arrives when the key is plugged in (decision 5). It needs a window, which the hardware tests don't have.

### Phase 2b: No spare YubiKey (owner's decision, 2026-10-16)

The spare YubiKey goes completely, as if it had never existed. The owner is the only user with a YubiKey, so there is no migration, no reason for new keys and no compatibility text. The fallback for a lost YubiKey is the recovery code, which is always created in modes 2 and 3. Format 2 is unchanged; the reader is only stricter. The release stays 2.1.0.

- [x] `yubikey`: `RegisterSpare`, `ErrAlreadyRegistered`, the exclude list and `NTE_EXISTS` go. `DeriveFIDO2SecretAny` becomes `DeriveFIDO2Secret(ChallengeData)` with one credential, and `answeringCredential` goes.
- [x] `container`: exactly one YubiKey slot in modes 2 and 3; the shared-salt rule and `YubiKeyCount` go; the label is "YubiKey".
- [x] `config`: `yubikey_spare` goes. New: a list of retired settings, reported with a generic message ("Config file has a setting that is no longer used: 'yubikey_spare' (line N)." Remedy: remove the line). A recovery code is always created in modes 2 and 3; `recovery_code` matters only in mode 1.
- [x] `catalog`: new-keys reasons without the spare; `recovery_code` compared only in mode 1.
- [x] `unlock`, `backup` (key creation, preflight), `interact` and `interacttest`, `gui/flow`, `gui` (questions), `gui/view` (credentials, create page, settings, strings), `health` (`SpareYubiKey`): everything about the spare goes. The texts say "your YubiKey", never "any of".
- [x] Settings (ST-8) and config-SAMPLE.yaml: "With a YubiKey, a recovery code is always created, so a lost YubiKey doesn't lose your backups."
- [x] Tests follow; the 2.0.0 format fixtures still pass (none has a spare). `container` and `unlock` got tests of their own to stay at their floors, and `stepTitle` went: with one numbered step, no dialog says "Step n of N" any more.
- [x] Docs:
  - SPEC-core: 1, 3 D4/D5, 4.4, 4.4.3, 6.1, 6.3, 12 and 15.
  - SPEC-gui: decision 11, the credential dialogs, ST-8, and YK-4, YK-5, YK-7, YK-8 and figures 10.2 to 10.4 without the spare.
  - README, GUIDE-gui-test (CR-1, CR-2), `scripts/ci/gui-smoke.ps1`, CHANGELOG.

### Phase 3: Data — `health` and `keycheck`

- [x] `health.KeysSummary.YubiKey` (the current key set has a YubiKey slot) and `YubiKeyInBackups` (any set's key set has one). Tests in `health`; floor 95 holds.
- [x] New package `internal/workflow/keycheck`:
  - `Run(ks *container.KeySet, now func() time.Time) Result`. Match when `ks` has a YubiKey slot, with its challenge as unlock uses it; Capability otherwise. No context: a WebAuthn call can't be cancelled from outside its dialog.
  - `Result{Kind, Time, Cancelled, HRESULT, Err, Trace}`.
  - `DebugOutput` set to a buffer for the duration and reset afterwards. The test uses the one worker slot (12.2).
- [x] `keycheck` calls `yubikey` through function variables, faked in its tests: Match and Capability, cancel, the HRESULTs of phase 2, the secret zeroed in every path, the trace captured and `DebugOutput` reset, and no salt or credential ID in the trace or the `Result`.
- [x] `keycheck` in the layer list of [internal/architecture/doc.go](../internal/architecture/doc.go) (workflow layer); the architecture test passes.
- [x] Coverage floor for `internal/workflow/keycheck` in [scripts/ci/test-coverage-floors.txt](../scripts/ci/test-coverage-floors.txt), at its measured value: 100.

### Phase 4: View — `gui/view`

- [x] `view.YubiKeyVisible(cfg, snapshot)` (YK-1). `view.Navigation` gets the item in phase 5, with the sidebar that can hide it: a fourth item now would show a page that doesn't exist yet.
- [x] `view.YubiKeyPageOf(snapshot, YubiKeyState{Diagnosis, Test, Testing, Busy, AppVersion, WindowsVersion}, now) YubiKeyPage`:
  - every hero variant of figure 10.4, including "Error: firmware too old" (firmware from `Key.Revision` below 5.2), and the cancel hero with both hints;
  - the Checks lines, including "No FIDO2 function" and firmware below 5.2;
  - the test button: shown when, and enabled when;
  - guide expanded or collapsed (also after a cancel);
  - the technical details text, with the RestoreSafe and Windows versions passed in.
- [x] Every text in `view/strings.go` (3.6), with the time in the existing format ("tested today, 08:52").
- [x] "Check your YubiKey" on the Keys card of `view.CreatePage` and on Keys and unlocking of `view.Settings`, while the item is visible (YK-10).
- [x] YK-8 in SPEC-gui gets the target of Open Windows Update… (decision 6).
- [x] Tests (the spec's row "YubiKey view"): visibility for each `authentication_mode` with and without YubiKey sets, every hero variant, the Checks lines, guide expansion, actions disabled during a test or an operation, and details free of secret, salt and credential ID (by construction: neither `Diagnosis` nor `keycheck.Result` holds one, which the `keycheck` tests check). Clearing the result on a device change, Reload and new keys lives in `gui` (decision 8) and is done in phase 5. Floor raised to 92 (92.1 %). The tests found one bug, fixed: with Windows too old and no YubiKey, the Connected line crashed.

### Phase 5: Window — `gui/widget`, `gui/win32`, `gui`

- [x] `widget.Sidebar`: hidden items, a bottom group anchored to the bottom edge below a line; arrow keys skip hidden items; UI Automation names and selection state as before.
- [x] `win32`: `WM_DEVICECHANGE` and `DBT_DEVNODES_CHANGED`, and the `ShellExecute` helpers if not already there. Check with the real key that the message arrives when the key is plugged in and pulled out (moved from phase 2); if it doesn't, use `RegisterDeviceNotification` (decision 5). `ms-settings:` and `https:` links go through the existing `ShellExecute` wrapper.
- [x] `gui/yubikeypage.go`: the page from `view.YubiKeyPage`. Hero, Checks card and guide card are drawn like the other cards (decision 7 of the GUI redesign: text in standard controls). Technical details is a read-only rich edit in Consolas, with **Copy details** through `win32.CopyText`.
- [x] App (`gui/yubikey.go`):
  - `Ctrl+4`;
  - F5 on this page runs `Diagnose` besides the health check;
  - the debounce timer, and `Diagnose` on a worker goroutine with a request number so stale results are dropped (YK-3);
  - the test on a worker goroutine in place of an operation: no operation, restore or reload starts while it runs, and it doesn't start while one runs (YK-6); "Follow the Windows Security prompt";
  - result kept and cleared (decision 8);
  - visibility recomputed after each snapshot and Reload.
- [x] The links of YK-10 select the page.
- [x] The GUI smoke test (`scripts/ci/gui-smoke.ps1`) still passes (2026-10-17: backups, restore, verify and all 13 conditions). Its configuration uses password mode, so the YubiKey item stays hidden: the test covers the sidebar with a hidden item, not the page; the page is checked by hand in phase 6.
- [x] Found while wiring the test: the health check can log WebAuthn details while the test collects its trace. `yubikey.DebugOutput` became `SetDebugOutput`, which serializes the writes; `keycheck` reads the trace only after switching it off.
- [x] Owner's review of the first build (2026-10-16), all fixed: (1) slow scrolling left controls painted over each other on every page; the panel now repaints after each scroll step; (2) the sidebar had no hover effect; (3) the Keys card of Create backup says how the first backup sets up the YubiKey, carries the reset warning (moved from the guide), and its "YubiKey connected" follows the key, because a device change takes a new snapshot; (4) the Checks card has a FIDO2 line and, under Connected, which YubiKeys work; (5) the guide is a how-to: set or change the PIN (Windows Settings), turn on FIDO2 (starts Yubico Authenticator as administrator, or opens its download page), test, register; (6) Technical details is an expander, and Copy details shows only while it is open.
- [x] Owner's second review (2026-10-16), all done: the Check your YubiKey links went (YK-10 retired); Set up your YubiKey became **YubiKey help** with the PIN and FIDO2 parts only, opening on its own when FIDO2 is off or a test didn't pass; Connected shows the number of keys and the firmware only (OTP and smart card don't matter to RestoreSafe), so `yubikey.FunctionsOf` went; the icon, label and text of a check share the first line; YubiKey help and Technical details have a heading that opens and closes them, the new `widget.Expander`.
- [x] Owner's third review (2026-10-16): plugging in and out confirmed on Create backup and the YubiKey page (decision 5 holds, no `RegisterDeviceNotification` needed). Technical details no longer widen the page when a scroll bar appears. YubiKey help covers the reset in Windows Settings. Decision 12: no backup with more than one YubiKey connected (health `YUBIKEYS_CONNECTED`, backup preflight issue), red on the YubiKey page.
- [x] Owner's fourth review (2026-10-17): YubiKey help is always shown. Technical details rebuilt from the ground up: sections from the overview to the raw data (SUMMARY, YUBIKEY n, LAST TEST, WINDOWS), labels and values in one column, error codes with their meaning, no word wrap (figure 10.5). `yubikey.Key` carries its USB device and interfaces.

### Phase 6: Manual test, docs, release notes

- [ ] [GUIDE-gui-test.md](GUIDE-gui-test.md): a section for the YubiKey page, with the cases of section 4 that one key can do, and a note on what needs a second key or a Security Key.
- [ ] Session with the real key, through the page:
  - not connected, then plug in (the page updates);
  - Capability without key sets;
  - a backup in a test directory registers the key, then Match answers "YubiKey 1";
  - cancel;
  - Copy details, pasted and read for secrets;
  - pull the key out during the test;
  - Reload;
  - password-only configuration with older YubiKey sets (item visible);
  - Narrator names on the page.
- [ ] Screenshots of figures 10.2 and 10.3 for the visual review against 3.3. The README gets a YubiKey page paragraph in its user part (it replaces the yubidiag section removed in phase 1). Whether a screenshot goes in the README is decided then.
- [ ] [SPEC-gui.md](SPEC-gui.md): header "Planned" updated, and 10.2 matches what was built.
- [ ] [CHANGELOG.md](../CHANGELOG.md): 2.1.0 entry for the YubiKey page and the removal of `yubidiag`.
- [ ] This plan's status set to done. It is deleted after the 2.1.0 release ([docs/README.md](README.md)).

## Open questions

None at the start. Phase 2 may raise some; they are answered here before phase 4.

## Done when

- YK-1 to YK-11 implemented, and YK-11's answers recorded in the spec.
- The tests of the spec's rows "YubiKey diagnosis and test" and "YubiKey view" exist and pass; every package at its floor, `keycheck` with its own.
- `cmd/yubidiag`, `RESTORESAFE_FIDO2_DEBUG` and the `GetConsoleWindow` fallback are gone; `git grep -i yubidiag` finds only the specs' history notes and this plan.
- The manual session of phase 6 passed, and CI passes on all jobs.
