# Changelog

All notable changes to this project are documented in this file.

This project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [2.0.0] - Unreleased

**Breaking change:** RestoreSafe 2.0 uses a new backup format. It cannot restore backups created by 1.x, and 1.x cannot restore 2.0 backups. Keep RestoreSafe 1.0.2 to restore your 1.x backups; 2.0 never modifies or deletes them. Start from the new `config-SAMPLE.yaml`. See "Updating from RestoreSafe 1.x to 2.0" in the README.

### Added
- Windows application instead of the console menu, built around the question "are my folders protected?":
  - **Create backup** page: the status (protected, warning, error, or no backup yet) with the action that fixes a problem, the folders with their newest backup and the type of the next one, the backup directory and its free space, and the keys. The backups are checked at start, after every operation, with **Refresh** or F5 (which also reads `config.yaml` again), and when you return after five minutes.
  - **Backup plan** before every backup: full or differential per folder and why, the space, how you unlock, and what retention removes if the backup succeeds.
  - Progress with steps and speed (a backup on Create backup, a restore or verification on Restore backup), also on the taskbar button; Cancel at any time (completed backup sets are kept, the unfinished one is removed); a result card that says what happened per folder.
  - **Restore backup** page: every backup run with its sets, types, sizes, and status (complete, verified, skipped files, incomplete, damaged), the log of the selected run, and Restore and Verify for a folder or, through the date of a run, all its folders.
  - **Restore wizard** on the backup selected on the Restore backup page: which folders, where to, and a last check; it checks the destination folders and the free space while you type.
  - **Settings** page: the configuration in words, **Edit config.yaml**, and **Reload** (no restart needed after a change).
  - Operable by keyboard (access keys, Ctrl+1 to Ctrl+3, Ctrl+B, F5) and readable by screen readers; follows the display scaling of each monitor and Windows high contrast.
- Differential backups: RestoreSafe chooses automatically between a full and a differential backup per source directory and shows the decision and reason before the backup starts. A differential stores only files that are new or changed since the last full backup (size, modification time, and NTFS change time are compared; unchanged files are not read).
- **Full backup instead** in the backup plan forces full backups.
- Restore and verify of differential backups; a differential is restored together with its full backup, and the link between them is checked before any file is written.
- Encrypted manifest in every backup: restore and verify check every file against its SHA-256 checksum.
- Restore of creation and modification times and of the read-only, hidden, and system attributes.
- Keys with multiple unlock methods: optional spare YubiKey (`yubikey_spare`) and recovery code (`recovery_code`). Either registered YubiKey unlocks the backups; the recovery code unlocks them without password or YubiKey. The recovery code is shown once, with a **Copy** button for your password manager (kept out of the Windows clipboard history).
- **New keys + full backup…** in the backup plan creates new keys (e.g. to change the password or replace a lost YubiKey); older backups keep opening with the old credentials.
- `password_min_length` (default 12, at least 8) for new passwords.
- `reminder_days` (default 7, 0 = off): the Create backup page reminds you when the newest backup is older. RestoreSafe checks it only while it is open.
- `exclude` patterns for files and directories to leave out of backups.
- `on_unreadable_file: skip` backs up everything else when a file cannot be read and lists the file as a warning; older backups of that directory are kept.
- `differential` configuration section (`enabled`, `full_backup_interval_days`, `max_size_percent`, `retention_keep_differentials`).
- The startup health check reports incomplete backups, differentials whose full backup is missing, leftovers of interrupted backups, 1.x backups, and the state of the keys.

### Changed
- RestoreSafe is a window application; the console menu is gone. `-config=<absolute path>` still selects another configuration (e.g. in a shortcut).
- The needed space of a differential is estimated from the files changed since its full backup (moved or renamed files are not in the estimate); the free-space check refuses a backup only when even the estimate does not fit, and warns when only the estimate fits.
- New file names: `[Directory]_ID_DATE_FULL-001.enc` and `[Directory]_ID_DATE_DIFFnnn-001.enc`. All files of a chain (a full backup and its differentials) share the ID.
- `.challenge` files are no longer created; the YubiKey data is stored inside the backup files.
- A backup run asks for the password once (no confirmation) and needs one YubiKey touch; the password is entered twice and the YubiKey registered only when new keys are created.
- `retention_keep` counts backup chains; a chain is always deleted as a whole.
- Backup parts are written as `.tmp` files and renamed only when the backup is complete, so an interrupted backup never looks like a valid one.
- Verify checks every file's checksum instead of only the archive structure.
- Restored files are written in 1 MB blocks instead of 32 KB; restoring to a network share is more than twice as fast.
- Update Go to 1.27.1
- YAML parsing uses the maintained `go.yaml.in/yaml/v3` module instead of the archived `gopkg.in/yaml.v3`.

### Removed
- Local staging in the temp directory. Backup and restore now always read and write the backup directory directly: staging was slower in every measured case (on a network share, backups took 57% and restores 14% longer), and the temp directory no longer needs free space for a copy of the backup.

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
