# RestoreSafe

RestoreSafe is a standalone Windows 64-bit backup tool that creates encrypted, split archives from your folders and restores them securely using a password (with optional YubiKey 2FA).

## Table of Contents

- [Features](#features)
- [Requirements](#requirements)
- [Installation](#installation)
- [Configuration](#configuration)
- [Usage](#usage)
	- [Create a backup](#create-a-backup)
	- [Restore a backup](#restore-a-backup)
- [Build](#build)
- [Naming scheme](#naming-scheme)
- [YubiKey setup](#yubikey-setup)

## Features

RestoreSafe archives one or more source folders and writes encrypted, split backup files to a target directory.

**Security architecture**

- Encryption: AES-256-GCM (authenticated encryption)
- Key derivation: Argon2id (64 MB memory, 3 iterations)
- Optional 2FA: YubiKey (HMAC-SHA1, slot 2)
- Both file content and metadata (file/folder names) are encrypted
- Streaming processing: no temporary backup files and no fixed RAM limit

## Requirements

- Windows 64-bit
- No .NET runtime
- No Java runtime
- No additional system dependencies

## Installation

1. Copy `RestoreSafe.exe` and `config.yaml` to the same directory.
2. Edit `config.yaml` (source folders, target folder, options).

## Configuration

Use the sample file `config-SAMPLE.yaml` as a reference.

## Usage

### Create a backup

Double-click `RestoreSafe.exe` and follow the prompts.

Command-line usage:

```bat
C:\path\to\RestoreSafe>RestoreSafe.exe -backup
```

Files created in the target folder:

```text
[Documents]_2024-01-15_ABC123-001.enc
[Documents]_2024-01-15_ABC123-002.enc   (if folder size > split_size_mb)
[Pictures]_2024-01-15_ABC123-001.enc
2024-01-15_ABC123.log
[Documents]_2024-01-15_ABC123.challenge  (YubiKey mode only)
```

### Restore a backup

Double-click `RestoreSafe.exe` and follow the prompts.

Command-line usage:

```bat
C:\path\to\RestoreSafe>RestoreSafe.exe -restore
```

## Build

Requirement: Go 1.26+

```bat
.\build.bat
```

This creates `RestoreSafe.exe` (standalone Windows 64-bit executable).

## Naming scheme

```text
[SourceFolderName]_YYYY-MM-DD_ID-Seq.enc
```

| Placeholder | Meaning |
|---|---|
| SourceFolderName | Name of the source folder (without path) |
| YYYY-MM-DD | Backup date |
| ID | Random 6-character identifier (A-Z, 0-9) |
| Seq | 3-digit part number (001, 002, ...) |

Log file:

```text
YYYY-MM-DD_ID.log
```

## YubiKey setup

1. Install YubiKey Manager: [YubiKey Manager Downloads](https://www.yubico.com/support/download/yubikey-manager/)
2. Open Applications > OTP > Long Touch (Slot 2) > Configure (required for HMAC-SHA1 challenge-response).
3. Set `yubikey_enable: true` in `config.yaml`.
4. Insert and touch the YubiKey when prompted during backup or restore.

> **Important**
> The `.challenge` file must be stored together with the corresponding `.enc` files.
> It does not contain secret keys, but it is required for restore when YubiKey mode is enabled.
