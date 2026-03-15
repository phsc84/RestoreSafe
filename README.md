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

Before backup starts, RestoreSafe shows a preflight summary including estimated total source size, free target disk space, and source reachability checks.

### Restore a backup

Double-click `RestoreSafe.exe` and follow the prompts.

The backup picker groups backups by backup set (`date + ID`) and supports date filtering via `YYYY-MM-DD` plus a quick `newest` shortcut for the most recent backup set.

If a backup ID exists on multiple dates, ID-based selection warns and automatically uses the newest date.

### Verify a backup

Double-click `RestoreSafe.exe`, choose `Verify backup`, and follow the prompts.

Verify mode checks that all selected backup parts are present, decryptable with the provided password (and YubiKey if required), and readable as a complete TAR archive without restoring any files to disk.

The same backup picker groups backups by backup set (`date + ID`) and supports date filtering via `YYYY-MM-DD` plus a quick `newest` shortcut for the most recent backup set.

If a backup ID exists on multiple dates, ID-based selection warns and automatically uses the newest date.

## Naming scheme of created files

### Backup files

Each backup file name follows this pattern:

`[FolderName]_YYYY-MM-DD_ID-001.enc`

What this means in plain words:

- `FolderName`: the source folder name
- `YYYY-MM-DD`: the backup date
- `ID`: a short code for one backup run
- `001`, `002`, ...: part number when a backup is split into multiple files

Basic examples:
```text
[Documents]_2026-01-15_ABC123-001.enc
[Pictures]_2026-01-15_ABC123-001.enc
```

Examples for common if-cases:

1. If one folder is larger than `split_size_mb`, it is split into multiple parts:

```text
[Documents]_2026-01-15_ABC123-001.enc
[Documents]_2026-01-15_ABC123-002.enc
[Documents]_2026-01-15_ABC123-003.enc
```

2. If YubiKey mode is enabled, a matching `.challenge` file is created per folder:

```text
[Documents]_2026-01-15_ABC123.challenge
[Pictures]_2026-01-15_ABC123.challenge
```

> **Important**
>
> The automatically generated `.challenge` file(s) must be stored together with the corresponding `.enc` file(s).
> The `.challenge` files do not contain secret keys, but are required for restore when YubiKey mode is enabled.

3. If several configured source folders have the same folder name (for example all end with `Documents`), RestoreSafe keeps the folder name and adds an extra alias derived from the remaining path and the drive letter. Only this added alias part is adjusted. The source folder name itself stays unchanged. In the added alias part, every character outside `a-zA-Z0-9` is encoded as UTF-8 hex bytes in the form `~XX~`:

Examples **WITHOUT** special characters in that added alias part:

```text
C:\RootA\Documents -> [Documents__RootA-C]_2026-01-15_ABC123-001.enc
D:\RootB\Documents -> [Documents__RootB-D]_2026-01-15_ABC123-001.enc
```

Examples **WITH** special characters in that added alias part:

```text
C:\Root A\Documents -> [Documents__Root~20~A-C]_2026-01-15_ABC123-001.enc
C:\Root-A\Documents -> [Documents__Root~2D~A-C]_2026-01-15_ABC123-001.enc
C:\Root_A\Documents -> [Documents__Root~5F~A-C]_2026-01-15_ABC123-001.enc
C:\Root.A\Documents -> [Documents__Root~2E~A-C]_2026-01-15_ABC123-001.enc
C:\Root~A\Documents -> [Documents__Root~7E~A-C]_2026-01-15_ABC123-001.enc
```

Result: aliases remain deterministic and distinct across special characters.

4. If the exact same source folder appears twice in `config.yaml`, RestoreSafe warns and skips the duplicate entry:

```text
[WARN] C:\Work\Documents -> identical duplicate of C:\Work\Documents; this entry will be skipped
```

Result: only one backup file set is written for that path.

### Log files

Naming structure: `YYYY-MM-DD_ID.log`

Sample:
```text
2026-01-15_ABC123.log
```

### Quick reference

| Name part | Meaning |
|---|---|
| FolderName | Name of the source folder |
| YYYY-MM-DD | Backup date |
| ID | Short backup run code (6 characters, A-Z and 0-9) |
| 001 / 002 / ... | File part number when the backup is split |

## YubiKey setup

1. Install YubiKey Manager: [YubiKey Manager Downloads](https://www.yubico.com/support/download/yubikey-manager/)
2. Open Applications > OTP > Long Touch (Slot 2) > Configure (required for HMAC-SHA1 challenge-response).
3. Set `yubikey_enable: true` in `config.yaml`.
4. Insert and touch the YubiKey when prompted during backup or restore.
