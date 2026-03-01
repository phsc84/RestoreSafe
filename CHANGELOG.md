# Changelog

All notable changes to this project are documented in this file.

This project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added / Changed
- 

### Fixed
- 

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
