# RestoreSafe

RestoreSafe is a standalone Windows 64-bit backup tool that backs up your folders into encrypted, split archive files, with password protection and optional YubiKey 2FA. Restore your backups anytime using the same secure password or YubiKey authentication.

## Table of Contents

- [Features](#features)
- [Installation & Configuration](#installation--configuration)
- [Usage](#usage)
- [Naming scheme of created files](#naming-scheme-of-created-files)
- [YubiKey setup](#yubikey-setup)

## Features

### Core
- Backs up one or more source folders into split, encrypted `.enc` archive files
- Restores selected backup sets to a chosen destination
- Verifies backup integrity (decryption + archive readability) without restoring
- Retention policy: automatically keeps only the newest N backup sets per source folder

### Security
- AES-256-GCM encryption (content and metadata/file names)
- Argon2id key derivation
- Password-only, password + YubiKey 2FA, or YubiKey-only authentication modes

### Reliability
- Local staging: when source and target share the same drive/share (e.g. NAS), parts are written to local TEMP first, then moved
- Startup health check: validates folders, temp access, YubiKey CLI, and structural integrity of existing backups at launch
- Streaming pipeline: no intermediate temp files, low CPU/RAM footprint

### Usability
- Portable, standalone `.exe` — no runtime dependencies
- Interactive menu; custom config path via `-config` flag
- Per-run log files; configurable log level
- Backup split size configurable; supports multiple source folders with automatic alias disambiguation

## Installation & Configuration

### Requirements

- Windows 64-bit

### First time usage

1. [Download](https://github.com/phsc84/RestoreSafe/releases) the latest version of RestoreSafe.exe and store it in any directory on your computer.
2. [Download](https://github.com/phsc84/RestoreSafe/releases) `config-SAMPLE.yaml`, rename it to `config.yaml` and put it into the same directory as RestoreSafe.exe.
   
   By default, RestoreSafe loads config.yaml from the same directory as the executable. Use the `-config` flag to point to a different config file (always use an absolute path). This is useful when managing multiple backup configurations.
   
   Create a `.bat` file to launch RestoreSafe with the desired config:
   
   ```bat
   @echo off
   "C:\Tools\RestoreSafe\RestoreSafe.exe" -config="D:\Configs\home-backup.yaml"
   pause
   ```
3. Edit `config.yaml` and at least set parameters `source_folders` and `target_folder`.
   
   Set `retention_keep` to keep only the newest N backup sets per source folder. Older backup and log files are deleted automatically.

   Authentication mode comparison

   | Setting | Password prompt | YubiKey required | Description |
   |---|---|---|---|
   | `authentication_mode: 1` | Yes | No | Standard password-only backup |
   | `authentication_mode: 2` | Yes | Yes | Password + YubiKey two-factor |
   | `authentication_mode: 3` | No | Yes | Password-less, key-in-hand authentication |

   In `authentication_mode: 3` physical possession of the YubiKey is the sole authentication factor. Keep your YubiKey safe - anyone with the YubiKey and the `.challenge` file can restore the backup.

### Updating

[Download](https://github.com/phsc84/RestoreSafe/releases) the latest version of RestoreSafe.exe and replace the existing version on your computer.

If updating to a new major version (v1.x.x -> v2.x.x), please also download `config-SAMPLE.yaml`, rename it to `config.yaml` and set the parameters according to your previous `config.yaml`.

This is not needed when updating to a new minor version (v1.0.x -> v1.1.x) or a new bugfix version (v1.0.1 -> v1.0.2).

## Usage

### Create a backup
Double-click RestoreSafe.exe, choose **Backup** from the menu, confirm the preflight summary, and enter your password (and touch the YubiKey if enabled).

### Restore a backup
Double-click RestoreSafe.exe, choose **Restore** from the menu, select the backup set(s) and destination folder, then enter your password (and touch the YubiKey if enabled).

### Verify a backup
Double-click RestoreSafe.exe, choose **Verify** from the menu, and select the backup set(s) to check. RestoreSafe confirms all parts are present, decryptable, and form a readable archive — without writing any files to disk.

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
   The automatically generated `.challenge` file(s) must be stored together with the corresponding `.enc` file(s). The `.challenge` files do not contain secret keys, but are required for restore when YubiKey mode is enabled.

3. If several configured source folders have the same folder name (for example all end with `Documents`), RestoreSafe keeps the folder name and adds an extra alias derived from the remaining path and the drive letter. Only this added alias part is adjusted. The source folder name itself stays unchanged.
   In the added alias part, every character outside `a-zA-Z0-9` is encoded as UTF-8 hex bytes in the form `~XX~`:

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

### Installation & configuration

1. Install YubiKey Manager: [YubiKey Manager Downloads](https://www.yubico.com/support/download/yubikey-manager/) (includes the `ykman` CLI tool)

   Compatibility note: RestoreSafe supports only YubiKey v5 hardware.
2. Open the YubiKey Manager GUI > Applications > OTP > Configure slot 2 with HMAC-SHA1 challenge-response.
3. Verify the YubiKey Manager CLI tool (`ykman`) is available:
   RestoreSafe auto-detects `ykman` on PATH and also in the default Windows install directory: `C:\Program Files\Yubico\YubiKey Manager\ykman.exe`.

   Optional PATH check in **PowerShell**:
   ```powershell
   where.exe ykman
   ```
   If `where.exe` shows no result, RestoreSafe can still work as long as `ykman.exe` exists in the default install directory above.
4. Set `authentication_mode` in `config.yaml`: `2` for password + YubiKey (2FA), or `3` for password-less YubiKey-only mode.
5. Insert and touch the YubiKey when prompted during backup or restore.
