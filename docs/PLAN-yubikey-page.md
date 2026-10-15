# Plan: The YubiKey page

| | |
|---|---|
| Status | Agreed 2026-10-15; not started |
| Implements | [SPEC-gui.md](SPEC-gui.md) 10.2 (YK-1 to YK-11) and decision 10 |
| Release | 2.1.0 |
| Branch | `dev` |
| Scope | The YubiKey page, its sidebar item and links, `yubikey.Diagnose` and `yubikey.ProbeCapability`, the package `workflow/keycheck`, and the removal of `cmd/yubidiag`. **No change to the backup format of 2.x**, to the keys, or to what backup, restore and verify do. |
| Hardware | One YubiKey, which may be reset. The cases that need a second key or a FIDO-only Security Key are tested with fakes only (section 4). |

## 1. Goals

1. **Every YK requirement is covered.** Section 3 maps each requirement to the phase that implements it. The spec's test rows "YubiKey diagnosis and test" and "YubiKey view" (16) are the tests' checklist.
2. **YK-11 is answered early,** with the real key, before the view is written (phase 2), so the hero variants and texts are built once.
3. **The build and tests pass after every phase.** A phase may leave the page missing, never broken. Each phase ends with gofmt, vet, staticcheck, `deadcode` and the full `go test -cover` with the floors checked locally, then a push and green CI.
4. **Delete, don't keep.** `cmd/yubidiag` goes in phase 1, when its code moves to `yubikey`. Until the page exists, the hardware test of phase 1 does its job.
5. **No secret leaves `keycheck`.** The derived secret is zeroed in every path, and the technical details never hold a PIN, secret, salt or credential ID. Tests prove both.

## 2. Decisions

| # | Decision | Rationale |
|---|---|---|
| 1 | WebAuthn failures become a typed error, `yubikey.HRESULTError{Op, Code}`, wrapped by the existing `problem` errors. It has `Cancelled()` for `NTE_USER_CANCELLED` (0x80090036) and `HRESULT_FROM_WIN32(ERROR_CANCELLED)` (0x800704C7), until phase 2 shows which one Windows returns, plus whatever else phase 2 finds. | `keycheck` must tell cancel from failure and show the HRESULT (YK-7, YK-9). Today the HRESULT exists only inside a formatted string. The messages that restore and backup show stay the same. |
| 2 | `Diagnose` reads the HID usage page with `HidD_GetPreparsedData` and `HidP_GetCaps` on a handle opened with zero access. | YK-4: FIDO is found by usage page `0xF1D0`, also on single-function keys without `MI_xx`. A zero-access open works for every interface, including the FIDO one that Windows doesn't open for writing without administrator rights. |
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
| YK-4 | Checks card: Windows, Connected (usage page, product IDs, no model names), Your keys, spare line | 1, 4 |
| YK-5 | Test: Match over all YubiKey slots of the current key set, or Capability; secret zeroed after its length check | 1 (`ProbeCapability`), 3 (`keycheck`) |
| YK-6 | During the test: "Follow the Windows Security prompt", actions disabled, one worker slot, not during an operation; result per session and when it is cleared | 4, 5 |
| YK-7 | Results to hero variants; cancel hint; HRESULT in details | 3, 4 |
| YK-8 | Setup guide: four steps, warning, footnote and link, Open Windows settings…, when it is expanded, strings in `view/strings.go` | 4, 5 |
| YK-9 | Technical details: versions, interfaces, registry, last test, WebAuthn trace; no secrets; Copy details | 1, 3, 4, 5 |
| YK-10 | "Check your YubiKey" links on the Keys card (OV-6) and on Keys and unlocking (ST-8) | 4, 5 |
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
| Spare answers, spare not tested, two keys connected | Not possible with one key | Fakes in `keycheck` and `view` tests only |
| FIDO-only Security Key (no `MI_xx`) | Not possible | Fakes in `yubikey` tests only |

## 5. Phases

### Phase 1: `yubikey` diagnosis, test calls, and the end of `yubidiag`

- [ ] `HRESULTError` (decision 1), returned through the existing errors of `realMakeCredential` and `realGetHmacSecret`. Restore and backup still show the same texts; a test checks `errors.As` through the wrapping.
- [ ] `Diagnose() Diagnosis` with the seams of decision 3:
  - WebAuthn API version, the minimum, and the error of `CheckAvailability`;
  - per Yubico HID interface: path, usage page, function (FIDO, OTP, other), product ID, USB device version, and write access or the open error;
  - the registry driver entries of `VID_1050`.

  No prompt, no write access kept open. Moved from `cmd/yubidiag`.
- [ ] `Diagnosis` helpers the view needs: FIDO present, number of keys (grouped by device instance, not by interface), and the functions per key from the product ID.
- [ ] `ProbeCapability() error`: a non-resident registration plus one hmac-secret with a random salt; the secret zeroed after its length check. It returns no credential ID.
- [ ] `dialogParent`: the `GetConsoleWindow` fallback goes (13.4); 0 means no owner window.
- [ ] `DebugOutput` comment: set only by the YubiKey test.
- [ ] Tests (the spec's row "YubiKey diagnosis and test", `yubikey` part): FIDO by usage page with and without `MI_xx`, no Yubico device, two YubiKeys, an interface that can't be opened, WebAuthn missing and too old, `ProbeCapability` zeroing with fakes. The `yubikey` floor (62) holds or rises.
- [ ] `hardware_test.go` with tag `yubikeyhw` (decision 4): prints the `Diagnose` report, and runs one case per `-run` name for phase 2.
- [ ] Remove `cmd/yubidiag`, its line in [internal/architecture/doc.go](../internal/architecture/doc.go), and the README section "YubiKey diagnostic tool". In the README's folder table, `cmd/restoresafe` stays the only command.

### Phase 2: YK-11 with the real key (owner and Claude together)

Before the reset: **the key must not be the only way into backups that matter.** Either a password, recovery code or another key opens them, or they are test backups. The reset in step 5 makes the key forget every registration for good.

1. `Diagnose` report: the usage pages and paths of the key's interfaces, and whether `DBT_DEVNODES_CHANGED` arrives when it is plugged in (decision 5). Compare the USB device version with the firmware that Yubico Authenticator shows.
2. Capability (registration and hmac-secret) with the key's PIN: passes. Record the number of prompts and touches, for the text "PIN and two touches".
3. Assertion with a random credential ID: record the HRESULT and what Windows shows. Then cancel in the PIN dialog and record that HRESULT.
4. Wrong PINs: record the HRESULT after the first wrong PIN, after 3 (soft block), and after 8 (blocked).
5. Reset in Windows Settings (Accounts > Sign-in options > Security key > Reset). Without a PIN, run Capability and record whether Windows asks for a new PIN. If it does, set one there; if not, set it in Windows Settings.
6. Optional: turn FIDO2 off over USB with Yubico Authenticator, then record `Diagnose` ("No FIDO2 function") and whether Windows Settings can turn it on. Turn it on again.
7. Record the answers in SPEC-gui YK-11, as a short "Confirmed 2026-…" paragraph with the Windows build and firmware. Update YK-4, YK-7, YK-8 and figure 10.4 where the answers change them:
   - a distinct HRESULT for "no credential" makes the variant "This YubiKey doesn't open your backups" live;
   - a distinct HRESULT for a blocked PIN gets its own variant, pointing to the warning;
   - a PIN asked during registration changes step 2 of the guide;
   - a firmware match in `bcdDevice` makes the Connected line show the firmware and flag versions below 5.2.
8. Adapt `HRESULTError` with the codes found, and add a test for each.

### Phase 3: Data — `health` and `keycheck`

- [ ] `health.KeysSummary.YubiKeySlots` (labels of the current key set's YubiKey slots, in slot order) and `YubiKeyInBackups` (any set's key set has a YubiKey slot). Tests in `health`; floor 95 holds.
- [ ] New package `internal/workflow/keycheck`:
  - `Run(ctx, ks *container.KeySet, now func() time.Time) Result`. Match when `ks` has YubiKey slots, with their challenges in slot order as unlock builds them; Capability otherwise.
  - `Result{Kind, Slot, Time, Cancelled, HRESULT, Err, Trace}`.
  - `DebugOutput` set to a buffer for the duration and reset afterwards. The test uses the one worker slot (12.2).
- [ ] `keycheck` calls `yubikey` through function variables, faked in its tests: Match and Capability, the slot label of the answering credential, cancel, the HRESULTs of phase 2, the secret zeroed in every path, the trace captured and `DebugOutput` reset, and no salt or credential ID in the trace or the `Result`.
- [ ] `keycheck` in the layer list of [internal/architecture/doc.go](../internal/architecture/doc.go) (workflow layer); the architecture test passes.
- [ ] Coverage floor for `internal/workflow/keycheck` in [scripts/ci/test-coverage-floors.txt](../scripts/ci/test-coverage-floors.txt), at its measured value (target 90).

### Phase 4: View — `gui/view`

- [ ] `view.Navigation` with the YubiKey item and its visibility (YK-1); the selection falls back to Settings when the item disappears.
- [ ] `view.YubiKeyPage(snapshot, cfg, diagnosis, test, busy) YubiKeyPage`:
  - every hero variant of figure 10.4, plus those phase 2 adds;
  - the Checks lines, including "No FIDO2 function", and the spare line;
  - the test button: shown when, and enabled when;
  - guide expanded or collapsed;
  - the technical details text, with the RestoreSafe and Windows versions passed in.
- [ ] Every text in `view/strings.go` (3.6), with the time in the existing format ("tested today, 08:52").
- [ ] "Check your YubiKey" on the Keys card of `view.CreatePage` and on Keys and unlocking of `view.Settings`, while the item is visible (YK-10).
- [ ] YK-8 in SPEC-gui gets the target of Open Windows Update… (decision 6).
- [ ] Tests (the spec's row "YubiKey view"): visibility for each `authentication_mode` with and without YubiKey sets, every hero variant, the Checks and spare lines, guide expansion, actions disabled during a test or an operation, and details free of secret, salt and credential ID (checked against the fakes' values). Clearing on device change, Reload and new keys is tested in phase 5 if it lives in `gui`, or here if it fits into a `view` helper. Floor 91 holds.

### Phase 5: Window — `gui/widget`, `gui/win32`, `gui`

- [ ] `widget.Sidebar`: hidden items, a bottom group anchored to the bottom edge below a line; arrow keys skip hidden items; UI Automation names and selection state as before.
- [ ] `win32`: `WM_DEVICECHANGE` and `DBT_DEVNODES_CHANGED`, and the `HidP` and `ShellExecute` helpers if not already there. `ms-settings:` and `https:` links go through the existing `ShellExecute` wrapper.
- [ ] `gui/yubikeypage.go`: the page from `view.YubiKeyPage`. Hero, Checks card and guide card are drawn like the other cards (decision 7 of the GUI redesign: text in standard controls). Technical details is a read-only rich edit in Consolas, with **Copy details** through `win32.CopyText`.
- [ ] App:
  - `Ctrl+4`;
  - F5 on this page runs `Diagnose` besides the health check;
  - the debounce timer, and `Diagnose` on a worker goroutine with a request number so stale results are dropped (YK-3);
  - the test on the worker slot, with operations and Check again disabled meanwhile (YK-6), and "Follow the Windows Security prompt";
  - result kept and cleared (decision 8);
  - visibility recomputed after each snapshot and Reload.
- [ ] The links of YK-10 select the page.
- [ ] The GUI smoke test (`scripts/ci/gui-smoke.ps1`) still passes. If the item is visible in its configuration, it opens the page once; the page must render without a YubiKey.

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
