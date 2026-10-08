# Changelog

All notable changes to this project are documented in this file.

This project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [2.0.0] - Unreleased

**Breaking change:** RestoreSafe 2.0 uses a new backup format. It cannot restore backups created by 1.x, and 1.x cannot restore 2.0 backups. Keep RestoreSafe 1.0.2 to restore your 1.x backups; 2.0 never modifies or deletes them. Start from the new `config-SAMPLE.yaml`. See "Updating from RestoreSafe 1.x to 2.0" in the README.

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
