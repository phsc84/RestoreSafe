# RestoreSafe

RestoreSafe is a standalone Windows 64-bit backup tool that securely encrypts and splits folder archives, with password protection and optional YubiKey 2FA. Restore your backups anytime using the same secure password or YubiKey authentication.

## Table of Contents

- [Features](#features)
- [Requirements](#requirements)
- [Usage](#usage)
  - [Installation & Configuration](#installation--configuration)
  - [Create a backup](#create-a-backup)
  - [Restore a backup](#restore-a-backup)
  - [Verify a backup](#verify-a-backup)
- [Naming scheme of created files](#naming-scheme-of-created-files)
- [YubiKey setup](#yubikey-setup)

## Features

### General architecture

- Portable: no installation needed
- Standalone: no dependencies -> no .NET runtime, no Java runtime, no other system dependencies
- Lean code: no overloaded GUI frameworks, concentrate on core functionality
- Streaming processing: no temporary backup files, very low CPU & RAM requirements

### Security architecture

- Encryption: AES-256-GCM (authenticated encryption)
- Key derivation: Argon2id (64 MB memory, 3 iterations)
- Optional 2FA: YubiKey (HMAC-SHA1, slot 2)
- Both file content and metadata (file/folder names) are encrypted

## Requirements

Windows 64-bit

## Usage

### Installation & Configuration

**First time usage**

1. [Download](https://github.com/phsc84/RestoreSafe/releases) the latest version of RestoreSafe.exe and store it in any directory on your computer.
2. [Download](https://github.com/phsc84/RestoreSafe/releases) `config-SAMPLE.yaml`, rename it to `config.yaml` and put it into the same directory as RestoreSafe.exe.
3. Edit `config.yaml` (at least parameters `source folders` and `target folder` have to be set, all other options may remain default).

Recommended: set `retention_keep` in `config.yaml` to keep only the newest N backup sets per source folder.
Older backup part/challenge files are deleted automatically, and logs are removed only when no backup parts remain for the same backup run (date + ID).

**Updating**

[Download](https://github.com/phsc84/RestoreSafe/releases) the latest version of RestoreSafe.exe and replace the existing version on your computer.

> **Important**
>
> If updating to a new major version (v1.x.x -> v2.x.x), please also download `config-SAMPLE.yaml`, rename it to `config.yaml` and edit it according to your previous `config.yaml`.
>
> This won’t be needed when updating to a new minor version (v1.0.x -> v1.1.x) or a new bugfix version (v1.0.1 -> v1.0.2).

At startup, RestoreSafe automatically runs a non-interactive health check. It validates configured source folders, target folder and temp directory access, optional YubiKey CLI availability, and the structural integrity of existing backup parts and challenge files.

### Create a backup

Double-click `RestoreSafe.exe` and follow the prompts.

### Restore a backup

Double-click `RestoreSafe.exe` and follow the prompts.

If a backup ID exists on multiple dates, ID-based selection restores only the newest date and asks for confirmation first.

### Verify a backup

Double-click `RestoreSafe.exe`, choose `Verify backup`, and follow the prompts.

Verify mode checks that all selected backup parts are present, decryptable with the provided password (and YubiKey if required), and readable as a complete TAR archive without restoring any files to disk.

If a backup ID exists on multiple dates, ID-based selection verifies only the newest date and asks for confirmation first.

## Naming scheme of created files

### Backup files

Naming structure: `[SourceFolderName]_YYYY-MM-DD_ID-Seq.enc`

Samples:
```text
[Documents]_2026-01-15_ABC123-001.enc
[Documents]_2026-01-15_ABC123-002.enc   (if source folder size > split_size_mb)
[Pictures]_2026-01-15_ABC123-001.enc
[Documents]_2026-01-15_ABC123.challenge  (YubiKey mode only)
[Pictures]_2026-01-15_ABC123.challenge  (YubiKey mode only)
[Documents__7B3FA4C1]_2026-01-15_ABC123-001.enc  (auto-alias for duplicate source basenames)
```

> **Important**
>
> The automatically generated `.challenge` file(s) must be stored together with the corresponding `.enc` file(s).
> The `.challenge` files do not contain secret keys, but are required for restore when YubiKey mode is enabled.

### Log files

Naming structure: `YYYY-MM-DD_ID.log`

Sample:
```text
2026-01-15_ABC123.log
```

### Description

| Placeholder | Meaning |
|---|---|
| SourceFolderName | Name of the source folder (without path) |
| YYYY-MM-DD | Backup date |
| ID | Backup ID, random 6-character identifier (A-Z, 0-9) |
| Seq | Sequence number, ascending 3-digit number (001, 002, ...) |

## YubiKey setup

1. Install YubiKey Manager: [YubiKey Manager Downloads](https://www.yubico.com/support/download/yubikey-manager/)
2. Open Applications > OTP > Long Touch (Slot 2) > Configure (required for HMAC-SHA1 challenge-response).
3. Set `yubikey_enable: true` in `config.yaml`.
4. Insert and touch the YubiKey when prompted during backup or restore.
