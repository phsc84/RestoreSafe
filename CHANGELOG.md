# Changelog

All notable changes to this project are documented in this file.

This project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed
- `retention_keep` keeps at least 2 chains (0 still keeps all). Old chains are deleted right after a new backup; if the backup disk or NAS loses power at that moment, the new backup can be damaged, and with 1 chain no backup would be left. A `retention_keep: 1` is used as 2: Create backup and the Retention card on Settings show a warning until `config.yaml` says 2 or more.

### Fixed
- A source folder that is a junction or symbolic link (for example a Documents folder redirected to another drive) is backed up again. Until now, its backup was empty and reported as successful.
- Links inside a source folder (junctions, symbolic links, mount points), which RestoreSafe never follows, are no longer left out without a word: the log names each, and the result card counts them per folder. The Create backup window says how many a folder has, and how many of its cloud files (OneDrive, iCloud) are only online and are downloaded for the backup.
- When the log of a backup, restore or verification can't be saved in the backup directory, the result card now says so and names the copy in the TEMP folder, which **Show log** opens. Before, this was reported nowhere, and the copy in TEMP was the only one. A log that can't be created, or that misses lines, is reported the same way.

## [2.1.1] - 2026-10-19

### Fixed
- The log of a backup, restore or verification is written again to a backup directory on some network shares (for example a NAS). Since 2.1.0, the log there stayed empty, and its lines were left in a file in the TEMP folder.

## [2.1.0] - 2026-10-18

### Added
- **YubiKey page**, at the bottom of the sidebar while your configuration or your backups use a YubiKey (`Ctrl+4`): whether Windows and the connected YubiKey are ready (FIDO2, firmware), **Test YubiKey…** to check that it opens your backups, help with the PIN, a reset and FIDO2 (it starts Yubico Authenticator, or opens its download page), and technical details to copy into a bug report. It updates when a YubiKey is plugged in or pulled out.
- With a YubiKey, the Keys card on Create backup says how the first backup sets it up, and warns never to reset it.
- When your YubiKey doesn't unlock your keys at the start of a backup, for example because it was reset, the backup no longer just fails: **Try again**, or **Create new keys + full backup**, which shows the plan with new keys to start. The dialog says whether your older backups still open with your recovery code.
- The sidebar shows the item under the mouse.

### Changed
- A backup doesn't start while more than one YubiKey is connected: new keys would register the one you touch, and nothing would say which. Restore and verify work as before.
- With a YubiKey (`authentication_mode` 2 or 3), new keys always come with a recovery code, whatever `recovery_code` says, so a lost or broken YubiKey doesn't lose your backups.
- A setting RestoreSafe no longer uses no longer stops the start: it is ignored. At start, a dialog names such settings and the ones missing from `config.yaml`, and **Update config.yaml** removes the old ones and adds the missing ones with their defaults (a copy of the previous file is kept). **Not now** asks again at the next start. This replaces **Add to config.yaml** on the Settings page.
- A new recovery code stays on screen until you tick **I have stored my recovery code**: only then is **Continue** (formerly **I have stored it**) enabled, so a habitual click on the blue button doesn't lose the code.
- The windows that show the plan of a backup and of a restore are titled **Create backup** and **Restore backup**, like the pages they open from.
- A failed backup, restore or verification, and a backup set that fails its check after the backup, are logged as **ERROR** lines with the reason. They used to be warnings, and the reason of a failed run was missing from the log. **Warnings and errors** in the log window shows them.

### Removed
- The spare YubiKey and its setting `yubikey_spare`: a key set has one YubiKey, and the recovery code is the fallback. RestoreSafe offers to remove `yubikey_spare` from `config.yaml`. Backups whose keys have a spare YubiKey can no longer be opened.
- The developer tool `yubidiag`: the YubiKey page replaces it.

### Fixed
- Scrolling a page slowly no longer leaves controls painted over each other.
- A restore and a verification of the same backup, run at the same time in two windows, no longer lose lines of each other's log.
- A folder deleted while a backup runs no longer stops the backup or shows as a file that could not be read; like a deleted file, it is simply not in the backup.

### Dependencies
- `golang.org/x/crypto` 0.58.0 (was 0.57.0), `golang.org/x/sys` 0.49.0 (was 0.48.0).

## [2.0.0] - 2026-10-10

**Breaking change:** RestoreSafe 2.0 uses a new backup format. It cannot restore backups created by 1.x, and 1.x cannot restore 2.0 backups. Keep RestoreSafe 1.0.2 to restore your 1.x backups; 2.0 never modifies or deletes them. Start from the new `config-SAMPLE.yaml`. See "Updating from RestoreSafe 1.x to 2.0" in the README.

**Breaking change:** a 1.x `config.yaml` doesn't load if it contains `io_diagnostics`: the setting is gone, and RestoreSafe 2.0 refuses settings it doesn't know. Remove the line; `log_level: "debug"` now includes the I/O diagnostics.

### Added
- Windows application instead of the console menu. **Create backup** shows whether your folders are protected, **Restore backup** lists every backup run with its log, **Settings** shows the configuration and reloads it. Every backup, restore and verification shows its plan first, then its progress (with Cancel) and the result per folder. Works with the keyboard, screen readers, display scaling and high contrast.
- Differential backups, chosen automatically per folder and shown before the backup starts; **Full backup instead** forces a full backup. Settings in the new `differential` section.
- Restore and verify check every file against its SHA-256 checksum, from an encrypted manifest in every backup. Verify used to check only the archive structure.
- Restore of creation and modification times and of the read-only, hidden and system attributes.
- Spare YubiKey (`yubikey_spare`) and recovery code (`recovery_code`). **New keys + full backup…** changes the password or replaces a lost YubiKey; older backups keep opening with the old credentials.
- New settings: `exclude`, `on_unreadable_file`, `reminder_days`, `password_min_length`.
- The startup check reports incomplete backups, differentials whose full backup is missing, leftovers of interrupted backups, and 1.x backups.

### Changed
- The console menu is gone. `-config=<absolute path>` still selects another configuration.
- New file names: `[Directory]_ID_DATE_FULL-001.enc` and `[Directory]_ID_DATE_DIFFnnn-001.enc`; a full backup and its differentials share the ID. No more `.challenge` files.
- `retention_keep` counts backup chains (a full backup with its differentials); a chain is always deleted as a whole.
- A backup asks for the password once and needs one YubiKey touch.
- An interrupted backup never looks like a valid one.
- Two RestoreSafe windows with the same backup directory no longer get in each other's way.
- A restore refuses a destination that is a junction or symbolic link.
- Restoring to a network share is more than twice as fast.

### Removed
- The `io_diagnostics` setting: `log_level: "debug"` includes the I/O diagnostics of a backup.
- Local staging in the temp directory: it was slower in every measured case, and the temp directory no longer needs space for a copy of the backup.

### Dependencies
- Go 1.27.2 (was 1.26.7).
- `golang.org/x/crypto` 0.57.0 (was 0.55.0), `golang.org/x/sys` 0.48.0 (was 0.47.0).
- YAML: the maintained `go.yaml.in/yaml/v3` 3.0.5 instead of the archived `gopkg.in/yaml.v3`.
- `golang.org/x/term` is no longer used.

## [1.0.2] - 2026-08-22

### Changed
- Simplified Windows file and product version metadata by removing the unused build component from `versioninfo.json`.
- Update Go to 1.26.7

## [1.0.1] - 2026-07-10

### Changed
- Improved duplicate source directory backup names to read as `SourceDirectory__from__Drive_Parent`, with the drive letter first and single underscores between path segments.
- Update Go to 1.26.5

### Fixed
- Print backup staging cleanup before the success message, leaving the log file path as the final line.

## [1.0.0] - 2026-06-06

### Added
- Initial clean-reset release of RestoreSafe for Windows 64-bit.
- Interactive backup, restore, and verify workflows driven from the main menu.
- Encrypted backups for one or more configured source directories, written as deterministic split `.enc` part files.
- Restore workflow for selected backup sets, with safeguards for restore target paths and archive extraction.
- Verify workflow that checks backup completeness, decryptability, and TAR archive readability without restoring files.
- AES-256-GCM encryption with authenticated chunk ordering and end-of-stream validation.
- Argon2id key derivation with configurable and bounded time, memory, and thread parameters stored in each encrypted file header.
- Authentication modes for password-only, password + YubiKey, and YubiKey-only operation.
- YubiKey authentication using FIDO2 hmac-secret via the Windows WebAuthn API, with per-backup challenge files.
- Startup health checks for configuration, source and backup directories, temp storage, YubiKey readiness, and existing backup structure.
- Backup preflight checks for source size, target space, local staging needs, and maximum supported part count.
- Local staging for backup and restore when it helps avoid same-drive or same-share read/write contention.
- Optional post-backup verification via `verify_after_backup: true`.
- Retention cleanup via `retention_keep`, with transparent logging of removed backup parts, challenge files, and orphaned logs.
- Per-run log files that record the RestoreSafe version needed to identify the creating build.
- Deterministic backup naming with date, backup ID, part number, and alias disambiguation for duplicate source directory names.
- Configurable split size, log level, authentication mode, Argon2id parameters, retention count, and custom config path via `-config`.

### Security
- Encrypts file contents and archive metadata, including file and directory names.
- Authenticates encrypted stream chunk order and final-chunk markers to detect truncated or reordered encrypted data.
- Validates encrypted headers and Argon2id parameters before key derivation.
- Uses FIDO2 credentials and salts stored in `.challenge` files without storing passwords, FIDO2 PINs, or secret keys.
- Clears password buffers on password prompt return paths.
- Rejects unsafe restore paths and skips symlinks and other non-regular archive entries during restore.
