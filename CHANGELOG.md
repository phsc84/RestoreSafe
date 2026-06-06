# Changelog

All notable changes to this project are documented in this file.

This project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
