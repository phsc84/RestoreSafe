# Changelog

All notable changes to this project are documented in this file.

This project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Added password-less YubiKey-only mode. Authentication is now configured via a single `authentication_mode` key in `config.yaml` with three numeric options: `1` (default, password only), `2` (password + YubiKey HMAC-SHA1 second factor), and `3` (YubiKey only, no password). The challenge file written by a YubiKey-only backup is marked with a `NOPW:` prefix so that restore and verify detect the mode without relying on `config.yaml`. Backup and restore/verify preflight summaries display the resolved authentication label.
- Added CLI flags `-backup`, `-restore`, and `-verify` to run operations directly without opening the interactive menu.
- Added safety guard for unattended backup: non-interactive `-backup` now requires `authentication_mode: 3` and exits with an error otherwise.
- Added automatic newest-run resolution in non-interactive `-restore` and `-verify` modes (no backup picker).
- Added CLI flag `-config` to load `config.yaml` from a custom location; if omitted, RestoreSafe still uses `config.yaml` in the application folder.
- Added automatic startup health check. RestoreSafe now runs a non-interactive diagnostic pass on launch and reports configuration, source/target folder access, temp directory access, YubiKey CLI availability, and structural issues in existing backup/challenge files before showing the main menu.
- Added interactive verify mode for existing backups. Verification checks selected backup sets for missing parts, validates decryption with password and optional YubiKey challenge-response, and confirms that the decrypted stream is a readable TAR archive without restoring files.
- Added backup and restore completion summaries showing processed folders, total part files created/processed, log file location, and whether warnings occurred.
- Added a simple retention policy via `retention_keep` in `config.yaml`: after a successful backup, RestoreSafe keeps only the newest N backup sets per source folder, deletes older encrypted part/challenge files, and removes orphan `.log` files only when no backup parts remain for the same backup run.
- Added unit and integration tests for config validation, TAR verification, health/retention helpers, backup/restore selection logic, and backup/restore round-trip behavior.

### Changed
- Refactored internal code structure into clearer package boundaries to improve maintainability and testability.
- Improved backup preflight output: RestoreSafe now shows estimated total source size, free target disk space, and a warning when estimated size likely exceeds currently free target space.
- Improved restore/verify backup selection and ID handling: backup sets are grouped by `date + ID`, support date filtering, include a quick `newest` shortcut, and when the same backup ID exists on multiple dates RestoreSafe warns and automatically uses the newest date.
- Changed restore authentication detection so it no longer depends on `config.yaml` alone: YubiKey requirement is inferred from backup-side challenge files when available.
- Improved duplicate source-folder handling: when multiple configured sources share the same basename, RestoreSafe appends a full path-derived alias (including drive hint) to backup naming. Every non-alphanumeric character in that alias is encoded as UTF-8 hex (`~XX~`, for example `-` -> `~2D~`, `_` -> `~5F~`, space -> `~20~`), and true identical source-path duplicates are warned and skipped.
- Improved user-facing messaging across config, backup, restore, verify, health check, YubiKey, and low-level crypto/split/logging/retention/archive paths: messages use clearer punctuation and include concrete remediation steps (for example forward-slash path hints, missing part/challenge guidance, and permission checks).

### Fixed
- Unified TAR path validation rules between verify and restore flows and removed unused restore dead code.

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
