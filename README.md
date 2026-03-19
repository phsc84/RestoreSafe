# RestoreSafe

RestoreSafe is a standalone Windows 64-bit backup tool that backs up your folders into encrypted, split archive files, with password protection and optional YubiKey 2FA. Restore your backups anytime using the same secure password or YubiKey authentication.

## Table of Contents

- [Features](#features)
- [Requirements](#requirements)
- [Usage](#usage)
  - [Installation & Configuration](#installation--configuration)
  - [Create a backup](#create-a-backup)
  - [Restore a backup](#restore-a-backup)
  - [Verify a backup](#verify-a-backup)
  - [Command-line flags for scheduled / unattended operation](#command-line-flags-for-scheduled--unattended-operation)
- [Naming scheme of created files](#naming-scheme-of-created-files)
- [YubiKey setup](#yubikey-setup)

## Features

### General architecture

- Portable: no installation needed
- Standalone: no dependencies -> no .NET runtime, no Java runtime, no other system dependencies
- Lean code: no overloaded GUI frameworks, concentrates on core functionality
- Streaming processing: no temporary backup files, very low CPU & RAM requirements

### Security architecture

- Encryption: AES-256-GCM (authenticated encryption)
- Key derivation: Argon2id (64 MB memory, 3 iterations)
- Optional 2FA: YubiKey (HMAC-SHA1, slot 2)
- Optional YubiKey-only mode: no password required, physical possession of the YubiKey is the sole authentication factor
- Both file content and metadata (file/folder names) are encrypted

## Requirements

- Windows 64-bit

## Usage

### Installation & Configuration

**First time usage**

1. [Download](https://github.com/phsc84/RestoreSafe/releases) the latest version of RestoreSafe.exe and store it in any directory on your computer.
2. [Download](https://github.com/phsc84/RestoreSafe/releases) `config-SAMPLE.yaml`, rename it to `config.yaml` and put it into the same directory as RestoreSafe.exe.
3. Edit `config.yaml` (at least parameters `source_folders` and `target_folder` have to be set, all other options may remain default).

Recommended: set `retention_keep` in `config.yaml` to keep only the newest N backup sets per source folder.
Older backup part/challenge files are deleted automatically, and logs are removed only when no backup parts remain for the same backup run (date + ID).

**Updating**

[Download](https://github.com/phsc84/RestoreSafe/releases) the latest version of RestoreSafe.exe and replace the existing version on your computer.

> **Important**
>
> If updating to a new major version (v1.x.x -> v2.x.x), please also download `config-SAMPLE.yaml`, rename it to `config.yaml` and edit it according to your previous `config.yaml`.
>
> This is not needed when updating to a new minor version (v1.0.x -> v1.1.x) or a new bugfix version (v1.0.1 -> v1.0.2).

At startup, RestoreSafe automatically runs a health check. It validates configured source folders, target folder and temp directory access, optional YubiKey CLI availability, and the structural integrity of existing backup parts and challenge files.

### Create a backup

Double-click `RestoreSafe.exe` and follow the prompts.
Before backup starts, RestoreSafe shows a preflight summary including estimated total source size, free target disk space, and checks that all source folders are accessible.

### Restore a backup

Double-click `RestoreSafe.exe` and follow the prompts.
The backup picker groups backups by backup set - a group of files created in one backup run, identified by `date + ID` - and supports date filtering via `YYYY-MM-DD` plus a quick `newest` shortcut for the most recent backup set.
If a backup ID exists on multiple dates, ID-based selection warns and automatically uses the newest date.
When the backup folder and restore target are on the same drive/share and your temp directory is on a different local drive, RestoreSafe automatically stages the selected backup parts in temp storage first to reduce same-share read/write contention.

### Verify a backup

Double-click `RestoreSafe.exe`, choose `Verify backup`, and follow the prompts.
Verify mode checks that all selected backup parts are present, decryptable with the provided password (and YubiKey if required), and readable as a complete archive without restoring any files to disk.
The same backup picker groups backups by backup set (`date + ID`) and supports date filtering via `YYYY-MM-DD` plus a quick `newest` shortcut for the most recent backup set.
If a backup ID exists on multiple dates, ID-based selection warns and automatically uses the newest date.

### Command-line flags for scheduled / unattended operation

RestoreSafe can be run from a batch file or Windows Task Scheduler using command-line flags.

**Unattended mode**

| Flag | Description |
|---|---|
| `-backup` | Run unattended backup and exit (requires `authentication_mode: 3`). |
| `-restore` | Run unattended restore for the newest backup run and exit. |
| `-verify` | Run unattended verify for the newest backup run and exit. |

- The "Start now?" confirmation is skipped automatically.
- Exits with code `0` on success or `1` on failure, making it suitable for batch files and scheduled tasks.
- In `-backup` mode, `authentication_mode` must be `3` (YubiKey only). Insert the YubiKey before the scheduled task runs.
- In `-restore` and `-verify` modes, the newest backup run is selected automatically and the start confirmation is skipped.
- `-restore` and `-verify` may still require authentication input for the selected backup set (password and/or YubiKey touch). Selection is unattended, but fully unattended end-to-end execution depends on the selected backup set.

**Custom config file path**

| Flag | Description |
|---|---|
| `-config="<absolute-path>"` | Load config from a custom absolute path. |
| `-config="<absolute-path>" -backup` | Custom config path combined with unattended backup. |

- By default RestoreSafe loads `config.yaml` from the application folder.
- Combined with `-backup`, operation runs unattended (requires `authentication_mode: 3`).
- Combined with `-restore` or `-verify`, operation auto-selects the newest backup set and skips the start confirmation, but authentication prompts may still appear depending on the selected backup set.

**Example batch files**

Example batch file for scheduled / unattended backup:

```bat
@echo off
cd /d "%~dp0"
RestoreSafe.exe -backup
if %errorlevel% neq 0 (
  echo Backup failed with exit code %errorlevel% >> backup-error.log
)
```

Example batch file using a custom config location (`-config=`), interactive mode:

```bat
@echo off
cd /d "%~dp0"
RestoreSafe.exe -config="D:/RestoreSafe/config.yaml"
```

Example batch file using a custom config location (`-config=`), unattended mode:

```bat
@echo off
cd /d "%~dp0"
RestoreSafe.exe -config="D:/RestoreSafe/config.yaml" -backup
if %errorlevel% neq 0 (
  echo Backup failed with exit code %errorlevel% >> backup-error.log
)
```

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

Examples for common cases:

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

Examples **without** special characters in that added alias part:

```text
C:\RootA\Documents -> [Documents__RootA-C]_2026-01-15_ABC123-001.enc
D:\RootB\Documents -> [Documents__RootB-D]_2026-01-15_ABC123-001.enc
```

Examples **with** special characters in that added alias part:

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

1. Install YubiKey Manager: [YubiKey Manager Downloads](https://www.yubico.com/support/download/yubikey-manager/) (includes the `ykman` CLI tool)
  - Compatibility note: RestoreSafe supports only YubiKey v5 hardware.
2. Open the YubiKey Manager GUI > Applications > OTP > Configure slot 2 with HMAC-SHA1 challenge-response.
3. Verify the YubiKey Manager CLI tool (`ykman`) is available:
  - RestoreSafe auto-detects `ykman` on PATH and also in the default Windows install directory: `C:\Program Files\Yubico\YubiKey Manager\ykman.exe`.
  - Optional PATH check in **PowerShell (Windows)**:

```powershell
where.exe ykman
```

  If `where.exe` shows no result, RestoreSafe can still work as long as `ykman.exe` exists in the default install directory above.
4. Set `authentication_mode` in `config.yaml`: `2` for password + YubiKey (2FA), or `3` for password-less YubiKey-only mode.
5. Insert and touch the YubiKey when prompted during backup or restore.

### YubiKey mode comparison

| Setting | Password prompt | YubiKey required | Best for |
|---|---|---|---|
| `authentication_mode: 1` | Yes | No | Standard password-only backup |
| `authentication_mode: 2` | Yes | Yes | Password + YubiKey two-factor |
| `authentication_mode: 3` | No | Yes | Password-less, key-in-hand authentication |

> **Important**
>
> In mode `3`, physical possession of the YubiKey is the sole authentication factor.
> Keep your YubiKey safe - anyone with the YubiKey and the `.challenge` file can restore the backup.
