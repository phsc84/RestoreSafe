# Changelog

All notable changes to this project are documented in this file.

This project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [1.2.0] - 2026-03-14

### Added / Changed
- Updated Go toolchain version to 1.26.1.
- Added dynamic version display in interactive menu: version is now extracted from `versioninfo.json` at build time and displayed as "RestoreSafe v1.1.0" when the application starts.
- Updated `config-SAMPLE.yaml` to use forward-slash Windows path examples (`C:/...`) and added a note to avoid YAML escaping pitfalls.

### Fixed
- Improved config parse error guidance: invalid YAML path escape errors now include a hint explaining valid Windows path formats in YAML.

## [1.1.0] - 2026-03-02

### Added / Changed
- Startup mode simplified: removed CLI flags (`-backup`, `-restore`) and standardized operation via interactive menu / double-click flow.
- Improved startup error handling in `main`: introduced shared `exitWithError(...)` helper for cleaner and consistent fatal error behavior.
- Improved runtime I/O diagnostics architecture:
	- extracted shared stream progress logger into `internal/engine/progress.go`.
	- renamed helper to `logStreamProgress` for clearer intent.
	- clarified progress output labels to be operation-specific (`encrypted` during backup, `decrypted` during restore).
- Improved maintainability of restore password verification writer by simplifying internal state handling (`verifyWriter`).
- Improved developer readability of filename parsing by documenting `partFilePattern` capture groups in `internal/util/naming.go`.

### Fixed
- Fixed inconsistent menu error UX: backup failures now also wait for key press before returning.
- Fixed silent stdin read handling in `main` by checking read errors in both `getUserInput` and `waitForKeyPress`.
- Fixed potential silent close error in `SequentialReader.Read` (`internal/util/split.go`) when switching part files.

## [1.0.0] - 2026-03-01

### Added / Changed
- Initial stable release of RestoreSafe for Windows 64-bit.
- Encrypted backup creation for one or more configured source folders.
- Secure restore flow for encrypted backup archives.
- AES-256-GCM encryption for authenticated confidentiality and integrity.
- Argon2id key derivation for password-based protection.
- Optional YubiKey challenge-response (HMAC-SHA1, slot 2) as second factor.
- Encrypted metadata (file and folder names) in backup archives.
- Automatic archive splitting into numbered `.enc` parts.
- Deterministic backup naming scheme with date, random ID, and sequence number.
- Backup logging to per-run `.log` files.
- CLI support for backup (`-backup`) and restore (`-restore`) modes.
