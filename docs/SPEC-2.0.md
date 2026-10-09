# Specification: RestoreSafe 2.0

| | |
|---|---|
| Status | Implemented for 2.0.0 (unreleased): format and workflows on `v2`, the window on `gui-redesign` ([SPEC-gui.md](SPEC-gui.md)). Hardware test of the YubiKey paths pending. |
| Target release | RestoreSafe 2.0.0 |
| Compatibility | **Breaking.** 2.0 cannot read 1.x backups; 1.x cannot read 2.0 backups. |

Main topics:

- Differential backups (sections 6-9).
- Container format 2 with embedded manifest (sections 4-5).
- Key sets: spare YubiKey and recovery code (section 4.4).
- Restore of timestamps and attributes (section 7.6).
- Exclude patterns, unreadable-file policy, and a password minimum (sections 6.4-6.6).
- Graphical user interface replacing the console menu: specified separately in [SPEC-gui.md](SPEC-gui.md).

## 1. Goals and non-goals

### 1.1 Goals

1. **Bullet-proof.** A restore either produces exactly the recorded state of the source directory or fails loudly. No silent partial restores, no silently missing or stale files.
2. **Self-contained.** RestoreSafe.exe alone creates, verifies, and restores full and differential backups. No external tools, no local state database, no files outside the backup directory.
3. **User friendly.** The user chooses "Create backup"; RestoreSafe decides between full and differential, explains the decision in the preflight summary, and lets the user override it with one key.
4. **No single point of failure in the credentials.** Losing one YubiKey or forgetting the password does not have to mean losing all backups (spare YubiKey, recovery code).

### 1.2 Not part of 2.0 (see section 13 for optional enhancements)

- Content-based change detection (hashing every source file on every run).
- Block-level (sub-file) deltas.
- Incremental chains (differential based on another differential).
- Rename/move detection and deduplication.
- Single-file/folder restore, recovery mode for damaged backups, compression, Volume Shadow Copy.
- Reading 1.x backups.

### 1.3 Permanently out of scope

- **Unattended or scheduled backups.** Running without a user present would require storing the password (or an equivalent secret) on the machine. RestoreSafe never stores credentials; every backup, restore, and verify requires the user to authenticate interactively.

## 2. Terminology

| Term | Meaning |
|---|---|
| Backup set | All part files produced for one source directory in one run. |
| Full backup | A backup set that contains every file of the source directory. |
| Differential backup | A backup set that contains only files that are new or changed since its base full backup, plus a complete manifest of the source state. |
| Base | The full backup a differential depends on. |
| Chain | One full backup plus all differential backups based on it. |
| Chain ID | The 6-character ID of a chain's full backup. Every file of the chain carries it in its name. |
| Differential number | Sequence number `001`-`999` of a differential within its chain. |
| Run ID | Random 6-character ID of one backup run; names the log file. A full backup's chain ID is the run ID of the run that created it. |
| Restore point | Any backup set. Restoring a full needs only that full; restoring a differential needs the differential and its base. |
| Manifest | Encrypted list of every entry (file/directory) in the source state at backup time, embedded in the backup set. |
| Key set | A random master key plus one or more **slots**, each holding a copy of the master key encrypted for one way to unlock it. Stored in every set header. |
| Slot | One way to unlock a key set: password, password + YubiKey, YubiKey, or recovery code. |
| Enrollment | Creating a new key set (entering a new password, registering YubiKeys, generating a recovery code). |

## 3. Key design decisions

| # | Decision | Rationale |
|---|---|---|
| D1 | Differential, not incremental. | A restore never needs more than two backup sets (full + one differential). Losing one differential never breaks another restore point. |
| D2 | Every backup set carries an encrypted **manifest** section with a SHA-256 per file. | Enables change detection without reading the base's data, records deletions, and allows end-to-end hash verification on every restore and verify. |
| D3 | The manifest is embedded in the `.enc` part files (second encrypted section + trailer), not a separate file and not inside the TAR. | Hidden from the user, cannot be lost or overwritten independently. The trailer allows reading the manifest without decrypting the (possibly very large) data section. |
| D4 | Key sets with slots (like BitLocker/LUKS) instead of deriving the data key directly from the credential. | Enables a spare YubiKey and a recovery code without weakening encryption. YubiKey challenges live in the header; `.challenge` files are removed. |
| D5 | One key set is reused for all backups in the backup directory until the user creates new keys. | One password entry and at most one YubiKey touch per backup run (1.x: two prompts per run). A spare YubiKey is only needed at enrollment. A differential and its base always share a key set, so restoring a differential needs one unlock. |
| D6 | Change detection compares size, last-write time, and NTFS change time for exact equality. | Fast (metadata only). Equality (not "newer than") makes it immune to clock changes. NTFS change time is updated on every write and attribute change and cannot be reset by ordinary applications, closing the "tool preserved the modification time" gap. |
| D7 | Retention operates on chains. | A full is never deleted while a kept differential depends on it. |
| D8 | Part files are written under temporary names and renamed only after the set is complete. | An interrupted backup never looks like a valid backup set or a usable base. |
| D9 | Manifest format: JSON Lines, Go standard library only. | Streamable (constant memory to write), strictly typed, debuggable, no new dependency. |
| D10 | Every file of a chain carries the chain ID in its name, placed before the date; differentials are numbered within the chain. | Sorting by name keeps a chain together. Deleting a full "because a newer differential exists" is visibly wrong, since the differential has the same ID. |
| D11 | The manifest records each file's position in the data section and its Windows metadata; the header reserves a compression field. | Costs almost nothing now, and allows single-file restore, a recovery mode, and compression later without another format break. |

## 4. On-disk format (container format 2)

### 4.1 Files in the backup directory

```text
[Documents]_ABC123_2026-09-01_FULL-001.enc       full backup, part 1
[Documents]_ABC123_2026-09-01_FULL-002.enc       full backup, part 2
[Documents]_ABC123_2026-09-12_DIFF001-001.enc    differential 1 of chain ABC123
[Documents]_ABC123_2026-09-26_DIFF002-001.enc    differential 2 of chain ABC123
[Documents]_DEF456_2026-10-01_FULL-001.enc       next chain
2026-09-01_ABC123.log
2026-09-12_KLM456.log
2026-09-26_XYZ789.log
2026-10-01_DEF456.log
```

- Full: `[DirectoryName]_CHAINID_YYYY-MM-DD_FULL-PART.enc`
- Differential: `[DirectoryName]_CHAINID_YYYY-MM-DD_DIFFNNN-PART.enc`
  - The chain ID comes before the date so that sorting by name (the Windows Explorer default) always keeps all files of a chain together. Within a chain, files sort by date. Chains among themselves sort by their (random) ID, not chronologically; RestoreSafe's restore list shows them chronologically.
  - `YYYY-MM-DD` is the date of that backup (full: creation date of the full; differential: creation date of the differential).
  - `NNN` is the differential number within the chain: the highest existing number of the chain + 1, never reused, even after retention deleted older differentials. A chain that has reached `DIFF999` gets a new full (rule in 6.1).
  - `PART` is the part number, as in 1.x.
- Log: `YYYY-MM-DD_RUNID.log`. One log per run, as in 1.x. A run can create fulls (chain ID = run ID) and differentials of older chains (chain ID different from run ID), so each set header records the `run_id` of the run that wrote it (4.3); retention uses it to find logs that no longer belong to any set.
- In progress: `<final name>.tmp` (see 4.7).
- No `.challenge` files (challenges are part of the key set in the header).

Parse regex: `^\[(.+?)\]_([A-Z0-9]{6})_(\d{4}-\d{2}-\d{2})_(FULL|DIFF(\d{3}))-(\d{3})\.enc$`.

The set header (4.3) repeats directory name, date, chain ID, type, and differential number; any mismatch between file name and header is a hard error. The header is authenticated (4.5), so renaming files cannot attach a differential to a different full.

A new run ID is regenerated until it does not occur as chain ID or run ID of any existing set in the backup directory, so a chain ID always identifies exactly one full per directory.

The README explains the rule in one sentence: *all files with the same ID belong together; a `DIFF` file is useless without the `FULL` files of the same ID.*

1.x files (`..._ID-001.enc`, `.challenge`) do not match the 2.0 pattern. They are never modified or deleted by 2.0 (see 11).

### 4.2 Logical stream

The part files `001..N` of a set, concatenated in order, form one logical byte stream:

```text
+--------------------+  offset 0
| Set header         |  plaintext, authenticated via AAD
+--------------------+
| Data section       |  encrypted chunk stream, plaintext = TAR
+--------------------+
| Manifest section   |  encrypted chunk stream, plaintext = JSON Lines
+--------------------+
| Trailer (64 bytes) |  plaintext, fixed size, last 64 bytes of the stream
+--------------------+
```

Part boundaries are arbitrary byte offsets (split writer behavior is unchanged). Readers use a multi-part `io.ReaderAt` to address the logical stream; the trailer may span two parts.

### 4.3 Set header

```text
[6]  magic "RSBKP\x00"
[1]  format version = 2
[1]  reserved = 0
[4]  header JSON length L (big-endian, 1 <= L <= 65536)
[L]  header JSON (UTF-8, single object)
```

Header JSON fields (all required unless noted; unknown fields are rejected):

| Field | Type | Meaning |
|---|---|---|
| `set_type` | `"full"` \| `"diff"` | Backup type. |
| `chain_id` | string | 6-char chain ID (for a full, its own ID). |
| `diff_number` | u32 | Differential only: `1`-`999`, matches file name. |
| `run_id` | string | Run ID of the run that wrote the set (links to the log file). |
| `set_nonce` | b64 (32 bytes) | Random per set; makes every `header_hash`, and therefore every set key, unique. |
| `directory_name` | string | Directory name as used in the file name. |
| `date` | string | `YYYY-MM-DD`, matches file name. |
| `created_utc` | string | RFC 3339 timestamp. |
| `app_version` | string | RestoreSafe version that wrote the set. |
| `base_date` | string | Differential only: `date` of the base full (the full is identified by `chain_id` + `directory_name`). |
| `base_manifest_sha256` | hex string | Differential only: SHA-256 of the base's decrypted manifest bytes. |
| `chunk_size` | u32 | Plaintext chunk size, must equal 8 MiB. |
| `compression` | string | Must be `"none"` in 2.0. Reserved for 13.4; readers reject any other value with a "created by a newer RestoreSafe version" error. |
| `key_set` | object | See 4.4. |

`header_hash = SHA-256(all header bytes including magic, version, reserved, length)`.

The header is plaintext so that listing, health check, retention, chain grouping, and slot selection work without the password. It reveals directory name, dates, IDs, backup type, differential number (all visible in file names anyway), and the key set's slot types and labels. File names and contents are only in the encrypted sections.

### 4.4 Key sets and slots

A key set is created at enrollment (6.3) and copied unchanged into the header of every set written with it.

```json
"key_set": {
  "id": "hex(16 random bytes)",
  "created_utc": "2026-09-01T08:12:44Z",
  "auth_mode": 2,
  "slots": [
    {"type": "password_yubikey", "label": "YubiKey 1", "kdf": {...}, "challenge": {...}, "nonce": "b64", "wrapped": "b64"},
    {"type": "password_yubikey", "label": "YubiKey 2 (spare)", "kdf": {...}, "challenge": {...}, "nonce": "b64", "wrapped": "b64"},
    {"type": "recovery", "label": "Recovery code", "kdf": {...}, "check": "b64", "nonce": "b64", "wrapped": "b64"}
  ]
}
```

Slot types per authentication mode:

| `auth_mode` | Regular slots | Optional |
|---|---|---|
| 1 Password | 1 × `password` | `recovery` |
| 2 Password + YubiKey | `password_yubikey` per registered YubiKey (1 or 2) | `recovery` |
| 3 YubiKey only | `yubikey` per registered YubiKey (1 or 2) | `recovery` |

Slot fields:

- `kdf`: `{"alg":"argon2id","salt":b64(32),"time":u32,"memory_kib":u32,"threads":u32}`, validated against the existing Argon2 bounds (from config at enrollment).
- `challenge`: YubiKey slots only; the existing `ChallengeData` (credential ID, hmac-secret salt, checksum).
- `check`: recovery slot only; see 4.4.2.
- `nonce`, `wrapped`: AES-256-GCM encryption of the master key (below).

#### 4.4.1 Key hierarchy

```text
V         = 32 random bytes                                   // key set master key, created at enrollment
secret    = password | password || hmac-secret | hmac-secret | recovery code bytes   (by slot type)
KEK       = Argon2id(secret, slot.kdf.salt, slot.kdf.params)  // 32 bytes
wrapped   = AES-256-GCM(KEK, slot.nonce, V, AAD = "RestoreSafe v2 slot" || key_set.id || slot index (u32) || slot.type)
data_key  = HKDF-SHA256(V, salt = header_hash, info = "RestoreSafe v2 data")
man_key   = HKDF-SHA256(V, salt = header_hash, info = "RestoreSafe v2 manifest")
```

- Unlocking tries only the slot(s) matching the factor the user provides, so each unlock costs one Argon2 derivation (as in 1.x).
- A wrong password or wrong YubiKey fails GCM authentication of the slot, so the credential is validated immediately from the header, before any data is read.
- New slots can only be created by someone who knows V. An attacker with write access to the backup directory can destroy backups but cannot add a slot they control to an existing key set.
- HKDF comes from Go's standard `crypto/hkdf`; no new dependency.

#### 4.4.2 Recovery code

- 25 random characters (125 bits) in Crockford Base32 (no I, L, O, U), followed by a 5-character checksum group (first bits of SHA-256 over the 25 characters), displayed as 6 groups of 5: `7KQ2M-X9D4T-...-CHECK`.
- Input is case-insensitive and ignores spaces and dashes. The checksum catches typos before the slow Argon2 derivation.
- `check` in the slot stores the checksum group only (not secret; it's derivable from the code).
- The recovery code alone unlocks the key set, **in every mode including 2FA**. The enrollment screen says so explicitly and tells the user to store the code in a password manager or offline (paper, safe), never next to the backups.

#### 4.4.3 Multiple YubiKey slots

All YubiKey slots of a key set share one hmac-secret salt: the spare YubiKey is registered with YubiKey 1's salt. This costs no security, because each YubiKey's hmac-secret output is keyed by its own credential secret, so the outputs still differ. Validation rejects key sets whose YubiKey slots use different salts.

To unlock, RestoreSafe sends one WebAuthn hmac-secret request with all YubiKey credential IDs in the allow list and the shared salt. The YubiKey that is plugged in answers, and the response identifies which credential was used; RestoreSafe then unwraps that slot. This behavior must be confirmed on real hardware. If it isn't reliable, the fallback is that the user picks the YubiKey by its slot label ("YubiKey 1" / "YubiKey 2 (spare)") before the touch.

Registering the spare passes YubiKey 1's credential ID as exclude list. Windows reports `NTE_EXISTS` (0x8009000F) when the connected YubiKey already holds it, and RestoreSafe asks for the spare again.

### 4.5 Encrypted sections

Each section is a chunk stream identical to format 1 (`[1] flags | [4] length | [length] GCM ciphertext`, 8 MiB plaintext chunks, final-chunk flag, counter nonce) with two changes:

- Each section uses its own key (`data_key`, `man_key`), so the counter nonce restarts at 0 per section without (key, nonce) reuse.
- `AAD = header_hash (32) || section_id (1: 0x01 data, 0x02 manifest) || chunk_index (8) || flags (1)`.

Consequences: any modification of the header (e.g. `chain_id`, `set_type`, a slot) fails authentication of every chunk; sections cannot be swapped between each other or between sets.

All ciphertext chunks except the last in a section have the same size (5 + 8 MiB + 16 bytes), so the position of any chunk can be computed. Together with the file positions in the manifest (5.1), this makes random access possible (13.5, 13.6).

### 4.6 Trailer

Last 64 bytes of the logical stream, big-endian:

```text
[8]  magic "RSTRL\x00\x02\x00"
[8]  data section offset
[8]  data section length
[8]  manifest section offset
[8]  manifest section length
[4]  part count N
[4]  reserved = 0
[16] first 16 bytes of SHA-256 over the preceding 48 bytes
```

The trailer is a locator and a completeness marker, not a security boundary: a tampered trailer can only point to bytes that then fail authentication. Without the password, RestoreSafe checks: trailer magic and checksum valid, part count equals the number of part files present, `header + data + manifest + 64 == total size of all parts`, sections adjacent and in bounds.

### 4.7 Atomic finalization

1. Write all parts as `<name>.enc.tmp` in the backup directory.
2. After the trailer is written, sync and close the last part.
3. Rename parts `.tmp` -> `.enc` in ascending order.
4. A set is considered complete only if the checks of 4.6 pass. A set with a missing or invalid trailer is reported as incomplete, is never offered as a restore point, and is never used as a base or as the source of the current key set.
5. At the start of every backup run (under the backup lock), leftover `*.enc.tmp` files matching the 2.0 naming pattern are deleted and logged.

## 5. Manifest

### 5.1 Format

JSON Lines, UTF-8, `\n` line endings, one object per line. Line 1 is the header object, followed by one entry per line in walk order, and a footer as last line.

Header line:

```json
{"manifest_version":1,"set_type":"diff","chain_id":"ABC123","diff_number":2,"directory_name":"Documents","source_path":"C:/Users/phs/Documents","hash_alg":"sha256","exclude":["node_modules","*.tmp"]}
```

Entry lines (short keys keep the manifest small at high file counts):

```json
{"p":"Projects","t":"d","m":1727340000000000000,"b":1720000000000000000,"a":0}
{"p":"Projects/report.docx","t":"f","s":48213,"m":1727340000123456700,"c":1727340000123456700,"b":1720000000000000000,"a":0,"h":"9f86d0...","o":"D","off":1536}
{"p":"Photos/img001.jpg","t":"f","s":2849123,"m":1725180000000000000,"c":1725180000000000000,"b":1725180000000000000,"a":1,"h":"2c26b4...","o":"F"}
{"p":"Mail/archive.pst","t":"s","r":"The process cannot access the file because it is being used by another process."}
```

| Key | Meaning |
|---|---|
| `p` | Path relative to the source directory, `/` separators, same validation rules as TAR entry names. |
| `t` | `"f"` regular file, `"d"` directory, `"s"` skipped file (6.5). Other types (symlinks, junctions, devices) are not recorded, as in 1.x. |
| `s` | File size in bytes. |
| `m` | Last-write time, UTC, nanoseconds since Unix epoch (NTFS 100 ns precision). |
| `c` | NTFS change time, same unit. `0` if unavailable (e.g. FAT/exFAT sources). |
| `b` | Creation time, same unit. |
| `a` | Windows attributes restored by 2.0: bit mask of `READONLY` (0x1), `HIDDEN` (0x2), `SYSTEM` (0x4). |
| `h` | Lowercase hex SHA-256 of the file content as written into the TAR. |
| `o` | Origin of the content: `"F"` = data section of the chain's full backup, `"D"` = data section of this differential. In a full, always `"F"`. |
| `off` | Only when the content is in **this** set's data section: byte offset of the file's TAR header in the plaintext TAR stream. |
| `x` | Optional, `1` = stale: the file could not be read in this run; the entry refers to the older content in the full backup (6.5). |
| `v` | Optional, `1` = this set's data section contains a void TAR entry for this path that must be skipped (6.5). |
| `r` | Skipped files only: reason (OS error text). |

Directories and skipped files are never TAR entries; the TAR contains only regular file content.

Footer line:

```json
{"end":true,"entries":15234,"files":14002,"dirs":1231,"skipped":1,"stale":0,"total_bytes":32212254720,"data_bytes":1073741824}
```

`total_bytes` = sum of file sizes in the restore point (exact restore size). `data_bytes` = sum of file sizes in this set's own data section.

### 5.2 Validation rules (reader)

Hard error on any of: unknown `manifest_version`, header line fields disagreeing with the set header, missing footer, footer counts not matching the entries, duplicate path, duplicate path under case-insensitive comparison (would collide on NTFS), path failing TAR path validation, entry whose parent directory entry is missing, invalid hash length, negative size, unknown attribute bits, origin other than `"F"` (full) or `"F"`/`"D"` (differential), `off` present without the content being in this set, `x` in a full backup.

### 5.3 Size and memory

About 250-300 bytes per entry: 15,000 files ≈ 5 MB, 1,000,000 files ≈ 300 MB. The manifest is written and read as a stream: the entries are held in memory, the serialized manifest never as a whole. Differential creation and differential restore hold one map `path -> entry` in memory (15,000 files ≈ 6 MB; 1,000,000 files ≈ 350 MB). See 13.3 for the constant-memory alternative.

## 6. Backup workflow

### 6.1 Automatic type selection (per source directory, before the password prompt)

First, the **current key set** is determined: the key set of the newest complete set in the backup directory (by `created_utc`), if it matches the configuration (`authentication_mode`, `yubikey_spare`, `recovery_code`). Otherwise a new key set is needed, which means a full backup for every directory (reason shown in the preflight, e.g. "New keys needed: spare YubiKey enabled in config.yaml").

If the backup directory contains no complete 2.0 backup set (first run, or the user deleted all backups), the preflight says: *"No existing keys found in the backup directory: new keys will be created."* Whenever new keys are created, the preflight also states that YubiKey registrations and any recovery code from earlier keys do not work for the new backups (they still open older backups that carry the old key set).

Then, per directory, a differential is chosen when **all** of the following hold, otherwise a full:

| Check | Source (no password needed) | Reason text shown when failing |
|---|---|---|
| A complete full backup of this directory exists (newest full = candidate base). | Headers + trailers | "No full backup found" / "Latest full backup is incomplete" |
| The base uses the current key set. | Base header | "New keys needed" |
| Differentials are enabled (`differential.enabled`). | Config | "Differential backups disabled" |
| Base age < `differential.full_backup_interval_days`. | Base header `created_utc` | "Full backup is 31 days old (limit 30)" |
| Newest differential of the chain: data section length < `differential.max_size_percent` % of the base's data section length. Without an earlier differential this check passes. | Trailers | "Last differential was 57 % of the full backup (limit 50 %)" |
| The chain has fewer than 999 differentials. | File names + headers | "Chain ABC123 reached the maximum of 999 differentials" |

All checks need no password, so the complete plan is known and shown in the preflight before anything is written.

The size check compares the encrypted data section lengths from the trailers, which include TAR and encryption overhead; for real data this is negligible, for tiny test sources it is not. Differential numbers count every differential of the chain, complete or incomplete, so a number is never reused.

After the credentials are entered, the base is opened and its manifest is decrypted and validated (5.2). If that fails, the directory gets a full backup instead, and the run shows a warning naming the base and the reason; a differential is never written on top of a base that cannot be read.

### 6.2 Preflight and override

```text
Source directory(s):
  [OK] C:/Users/phs/Documents
          → Differential backup (base: full 2026-09-01 ABC123, 25 days old)
  [OK] C:/Users/phs/Pictures
          → Full backup (reason: full backup is 31 days old (limit 30))
...
Keys          : existing keys, created 2026-09-01, password + YubiKey (2 YubiKeys), recovery code

Start backup now? [Y] yes / [F] full backup / [K] new keys + full backup / [N] cancel:
```

- `F` forces a full backup for every directory with the current key set (e.g. to start fresh chains). It is offered only when at least one differential is planned.
- `K` creates a new key set (enrollment, 6.3) and forces a full backup for every directory. Use it to change the password, replace a lost YubiKey, or get a new recovery code. Creating new keys needs no old credentials; older chains keep their old key set and remain restorable with the old credentials.
- There is no "force differential": when the automatic rules choose full, a differential would violate a configured limit or has no valid base.

**Needed space.** The exact changes of a differential are known only after the password is entered (the full backup's manifest is encrypted), so the preflight estimates them from the directory listing: files whose last-write or creation time is at or after the full backup's creation count as changed. It shows `about <changed> (files changed since the full backup); up to <all files> if everything is stored again`. The estimate misses files moved or renamed since the full backup (they keep their times but get a new path) and files whose times a tool set back; the differential stores them anyway.

- The free-space check of the backup directory fails only when even the estimate does not fit. When only the estimate fits, the preflight shows a warning; if the space then runs out, the backup stops and removes the unfinished set (4.7).
- Choosing `F` or `K` when the estimate was used checks the free space again against all files, before anything is written.

### 6.3 Credentials

**Existing key set** (the normal case):
- Modes 1/2: prompt the password **once** (no confirmation needed; the slot validates it). Wrong password: up to 3 attempts, as in 1.x.
- Modes 2/3: one YubiKey touch (4.4.3).
- The unlocked key set is used for every directory in the run, full and differential alike.

**Enrollment** (no usable key set, configuration changed, or `K`):
1. Modes 1/2: new password + confirmation; minimum length enforced (6.6).
2. Modes 2/3: register YubiKey 1 and derive its secret (two Windows prompts, as in 1.x).
3. If `yubikey_spare: true`: *"Remove YubiKey 1 and insert your spare YubiKey."* Register and derive (two prompts). Registration passes YubiKey 1's credential ID as exclude list, so registering the **same** YubiKey twice is refused by the key itself; RestoreSafe reports this and asks again for the spare.
4. If `recovery_code: true`: show the code with the warning of 4.4.2; the user stores it (copy button or paper) before the backup starts. There is no retype step.
5. Generate V, create all slots, and check each slot unwraps with the in-memory KEK before anything is written.

The preflight states exactly which prompts will appear (password, number of YubiKey prompts, recovery code).

### 6.4 Exclude patterns

```yaml
exclude:
  - "node_modules"
  - "*.tmp"
  - "~$*"
  - "Thumbs.db"
  - "/Cache"
```

- Applies to all source directories. Matching is case-insensitive (Windows semantics) with `path.Match` syntax (`*`, `?`, `[...]`).
- A pattern without `/` matches a file or directory **name** at any depth. A pattern containing `/` is anchored at the source directory root; a leading `/` is optional (`/Cache`, `Projects/*/build`).
- A trailing `/` matches directories only (`logs/`). Backslashes are treated as `/`.
- A matching directory is skipped including its whole subtree.
- Excluded entries are not recorded in the manifest. The active pattern list is recorded in the manifest header, and the count of excluded entries is logged per directory.
- Changing the list does not force a full: newly excluded files disappear from the next differential (like deletions); newly included files appear as new files.
- Invalid patterns are rejected at startup (config validation), not during the backup.

### 6.5 Unreadable files

`on_unreadable_file: fail | skip` (default `fail`).

A file is unreadable when it cannot be opened (e.g. locked by another process, access denied), a read fails, or its size changes while it is being copied. A directory is unreadable when it cannot be listed.

A file or directory deleted while the backup is running is not unreadable: it is simply not part of the backup, like a file deleted before the backup started. The count is logged per directory; this applies to both settings, so temporary files vanishing mid-run never abort a backup.

- `fail`: abort the backup of that directory with a clear message naming the file (1.x behavior). No set is created for that directory.
- `skip`:
  - Full backup: record the file as `t:"s"` with the reason. An unreadable directory is recorded as `t:"s"` and its content is not backed up.
  - Differential backup, file present in the base: keep the base's entry (`o:"F"`) and mark it stale (`x:1`); the restore point contains the older version.
  - Differential backup, file not in the base: record as `t:"s"`.
  - If the failure happens after the file's TAR header was written, the remaining bytes are zero-padded and the entry gets `v:1` so restore skips the void TAR entry.
  - The console summary and log list every skipped and stale file; the run ends with "completed with warnings".
  - Retention is skipped for a directory whose newest set contains skipped or stale entries, so an older backup that still contains those files is never deleted as a consequence.

### 6.6 Password minimum

When a new key set is created in modes 1 and 2, the password must have at least `password_min_length` characters (default 12). The value is configurable but never below 8; lower values are rejected at startup so a typo cannot allow trivially short passwords. Characters are counted, not bytes. The rule only applies to enrollment; unlocking an existing key set accepts whatever password was set.

### 6.7 Creating a full backup

As today (walk -> TAR -> encrypt -> split), plus:
- The TAR producer computes SHA-256 of every file's bytes as they are copied into the TAR and emits a manifest entry per entry (including `off`). The manifest is held in memory only (about 300 bytes per entry, see 5.3) and never written to disk in plaintext; after the data section is finished, it is encrypted as the manifest section.
- Exactly `hdr.Size` bytes are copied per file (`io.CopyN`); a size change during the copy is an unreadable file (6.5).

### 6.8 Creating a differential backup

1. Load and validate the base manifest (5.2) into a map.
2. Walk the source in the same order as a full backup. For each entry:
   - Directory: record in the manifest.
   - File present in base with identical `s`, `m`, and `c`: record with the base's `h`, `b`, `a`, and `o = "F"`. Not read.
   - Otherwise (new or changed): stream into the TAR, hash while copying, record with `o = "D"` and `off`.
   - Unreadable: see 6.5.
3. Paths in the base but not in the walk are deleted; they simply do not appear in the new manifest.
4. Header gets `chain_id` (= base's chain ID), `diff_number` (highest existing number of the chain + 1), `run_id`, `base_date`, `base_manifest_sha256`, and the base's key set.
5. If nothing changed, a differential is still written (empty TAR + manifest). It is a valid, cheap restore point and documents that the run happened.

### 6.9 Post-backup verification and retention

- `verify_after_backup: true` verifies each new set per section 8. For a differential, only its own data section is checked (every new or changed file against its hash); the base was verified when it was written and is not re-read.
- Unchanged rule: if verification fails, retention is skipped.

## 7. Restore workflow

### 7.1 Selection

Restore points are listed per backup run, newest run first, as in 1.x; every complete set is a restore point. Differentials are marked:

```text
Available backups:
  - Backup ID: XYZ789 / Timestamp (local): 2026-09-26 18:02:11 CEST
    - Documents_ABC123_2026-09-26_DIFF002 (differential: full backup ABC123 + changes)
    - Pictures_XYZ789_2026-09-26_FULL
  - Backup ID: ABC123 / Timestamp (local): 2026-09-01 17:45:03 CEST
    - Documents_ABC123_2026-09-01_FULL
```

The selection accepts `.` (newest run), a run ID (every set of that run), or a set name. Incomplete sets are not listed. A differential whose full backup is missing or incomplete is rejected in the preflight with the reason.

Possible later improvement: a per-directory view with restore sizes and entry counts (needs the password to read the manifests) and markers for restore points with skipped or stale files.

### 7.2 Preflight

Lists every set required (for a differential: `→ with full backup <name> (parts: N)` below the differential) with part count and completeness status. The space estimate adds the full backup's size for a differential, because both are read. The destination must not exist (unchanged).

### 7.3 Credentials

One unlock per distinct key set in the selection (normally one). The user authenticates with the regular factor (password and/or YubiKey) or chooses `[R] use recovery code` when the key set has a recovery slot.

### 7.4 Restoring a full

1. Decrypt the manifest.
2. Create all directories listed in the manifest.
3. Stream the data section, extract every file whose manifest entry has content in this set, and hash every file while writing. Skip TAR entries marked `v:1`.
4. Final steps (7.6, 7.7).

### 7.5 Restoring a differential

1. Decrypt the differential's manifest (target state). Decrypt the base's manifest; check `SHA-256(base manifest) == base_manifest_sha256`, that `chain_id`, `directory_name`, and `base_date` match the base header, and that both use the same key set. One unlock (7.3) opens both sets.
2. Create all directories listed in the target manifest.
3. Stream the base's data section: extract only files whose target entry has `o = "F"`; skip everything else (superseded or deleted files are never written). For each extracted file, the hash must equal the target entry's `h`.
4. Stream the differential's data section: extract every file with a target entry `o = "D"` and a matching hash; skip TAR entries marked `v:1`. Any other TAR entry is a hard error.
5. Final steps (7.6, 7.7).

### 7.6 Timestamps and attributes

After each file's hash is confirmed: set creation and last-write time (`SetFileTime`), then attributes (`SetFileAttributes`; read-only last). After all files are written: set directory times and attributes, deepest directories first (writing files changes the parent directory's times). Uses `golang.org/x/sys/windows`, already a dependency.

Still not restored (README "Known limitations"): symlinks/junctions, permissions/ACLs/ownership, last-access time, alternate data streams, EFS encryption and NTFS compression flags.

### 7.7 Final checks

- Every file entry of the target manifest was written exactly once with a matching hash and size.
- No TAR entry was ignored except those explicitly skipped (superseded in 7.5 step 3, `v:1`).
- Skipped and stale files are listed in the console and log: *"3 files are not in this restore point (they could not be read during backup)"*, *"1 file is restored in an older version from 2026-09-01"*.
- On any failure: stop, report which entries are affected, leave the partially restored destination in place, and state clearly that the restore is **incomplete** (the log lists the missing/invalid paths). The destination is never presented as a successful restore.

## 8. Verify workflow

Verify performs the restore algorithm of section 7 with all writes replaced by hashing to `io.Discard`. A full verify therefore checks decryption, TAR structure, **and every file hash** against the manifest (stronger than 1.x). Verifying a differential verifies the complete restore point (base + differential). The recovery code can be used, as in restore.

## 9. Retention

- `retention_keep` = number of **chains** to keep per directory (0 = keep all). Chains are ordered by their full's `created_utc`.
- Deleting a chain deletes its full and all its differentials.
- A log file is deleted when no remaining set has its run ID as `run_id` (header).
- `differential.retention_keep_differentials` (0 = keep all) limits differentials within **each** kept chain; the oldest (lowest numbers) are deleted first. The newest differential is always kept, so differential numbers are never reused. The two settings are independent: either one alone enables cleanup.
- Incomplete sets (no valid trailer) older than the newest complete set of the same directory are deleted and logged. Incomplete sets newer than that are kept and reported (possibly a crash that should be investigated).
- Retention never deletes files that do not match the 2.0 naming pattern.
- Unchanged: retention is skipped when post-backup verification failed or when any set's metadata cannot be read.
- New: retention is skipped for a directory whose newest set contains skipped or stale files (6.5).

## 10. Security considerations

- File names, file contents, and all metadata of backed-up files are only in encrypted sections.
- Plaintext header content: directory name, dates, IDs, backup type, key set ID, slot types and labels, Argon2 parameters, YubiKey credential IDs and hmac salts. None of these is secret.
- Header, slots, and sections are authenticated; any tampering fails decryption rather than producing wrong data.
- The recovery code is a full alternative credential: it bypasses the YubiKey in mode 2. It is optional and off by default.
- The key set is reused until the user creates new keys (`K`). Compromise of the password (mode 1) exposes all backups using that key set, as with 1.x when the same password was used. `K` is the documented response to a suspected compromise.
- No credentials or keys are ever written to disk outside the encrypted slot format. Unattended operation is permanently out of scope (1.3).

## 11. Startup health check and migration

- Reads the header and trailer of every set (no password). Reports: incomplete sets, differentials with missing/incomplete base, header/file-name mismatches, unexpected `.tmp` files.
- A differential whose full is missing is reported with the chain ID, e.g. *"[Documents] differentials DIFF001-DIFF004 of chain ABC123 cannot be restored: the full backup `[Documents]_ABC123_2026-09-01_FULL-*.enc` is missing. Remedy: Restore the FULL files of ABC123 from your copy, or delete the DIFF files of ABC123."*
- Shows the current key set summary (created date, slot types) and whether the configuration requires new keys at the next backup.
- If 1.x files are found (`[Name]_DATE_ID-SEQ.enc`, `.challenge`): warn once per start: *"RestoreSafe 1.x backups found. RestoreSafe 2.0 cannot restore them. Keep RestoreSafe 1.0.2 to restore these files; they are never modified or deleted by 2.0."* This is a warning, not a blocking error.

## 12. Configuration

New and changed keys (the other 1.x keys keep their meaning, except the removed `io_diagnostics` below):

```yaml
# Number of backup chains (a full backup plus its differential backups) kept per
# source directory. 0 = keep all.
retention_keep: 3

# Files and directories to leave out of every backup. See README for the syntax.
# Default: none
exclude: []

# What to do when a file cannot be read (e.g. locked by another program):
# "fail" = abort the backup of that directory (default)
# "skip" = continue, list the file as skipped, and mark the backup with a warning
on_unreadable_file: "fail"

# Modes 2 and 3: register a second YubiKey as spare when new keys are created.
# Either YubiKey can then restore every backup made with those keys.
# Default: false
yubikey_spare: false

# Create a recovery code when new keys are created. The code can restore every
# backup made with those keys, even without password or YubiKey. Store it offline.
# Default: false
recovery_code: false

# Minimum password length (in characters) when new keys are created.
# Default: 12  |  Minimum: 8  |  Maximum: 256
password_min_length: 12

differential:
  # Create differential backups when a valid full backup exists.
  # false = every run creates a full backup.
  enabled: true
  # Create a new full backup when the latest full backup is at least this many days old.
  # Range 1-365. Default: 30
  full_backup_interval_days: 30
  # Create a new full backup when the latest differential backup has reached this
  # percentage of the full backup size. Range 1-100. Default: 50
  max_size_percent: 50
  # Differential backups kept per chain (oldest deleted first). 0 = keep all.
  retention_keep_differentials: 0
```

Changing `authentication_mode`, `yubikey_spare`, or `recovery_code` requires new keys; the next backup announces this in the preflight and creates full backups.

Removed: `io_diagnostics`. `log_level: "debug"` includes the I/O diagnostics of a backup (progress every 2 seconds, a warning when no data moves for 10 seconds, write calls and part sizes). 2.0 doesn't promise to load a 1.x configuration file: a file that still contains `io_diagnostics` fails with an unknown key (GUI spec 11.6), and users start from the new `config-SAMPLE.yaml`.

## 13. Optional future enhancements (not part of 2.0)

### 13.1 Content-based change detection

`differential.change_detection: metadata | content`. In `content` mode every source file is hashed and compared against the base's `h`, so changes that leave size and both timestamps untouched (raw disk writes, deliberate timestamp forging) are also detected. Costs one full read of the source per differential. No format change.

### 13.2 Block-level differentials

For large files that change partially (VM images, databases, PST files): store changed fixed-size blocks (or content-defined chunks) instead of the whole file. Requires per-block hashes in the base manifest (e.g. `"bl":[...]` per 4 MiB block) and a new entry kind for "patch against base block list". Would raise `manifest_version`.

### 13.3 Constant-memory comparison

Sort manifest entries by a component-wise path order that matches the walk order and compare base and source as a streaming merge instead of a map. Only needed for multi-million-file sources.

### 13.4 Compression

Compress data and manifest sections before encryption, skipping already-compressed file types. Uses the reserved `compression` header field. Needs a fast codec to not slow down large backups (Go's standard `compress/flate` is slower than the current pipeline).

### 13.5 Single-file and folder restore

Browse or search a restore point's manifest and restore selected paths. Uses `off` and the fixed chunk size to decrypt only the needed chunks.

### 13.6 Recovery mode for damaged backups

When a chunk fails authentication (bit rot, bad sector), restore all files that do not overlap the damaged chunk(s) and list exactly which files are lost, instead of stopping at the first error. Uses `off`, `s`, and the fixed chunk size.

### 13.7 Volume Shadow Copy

Back up from a VSS snapshot so open/locked files are captured consistently. Requires administrator rights; would complement `on_unreadable_file`.

### 13.8 Per-source exclude patterns

Allow `exclude` per source directory in addition to the global list.

## 14. Error handling principles

- Every error message names the affected set (`[Directory] CHAINID DATE FULL|DIFFNNN`) and ends with a `Remedy:`, as in 1.x.
- Never fall back silently: when a differential is impossible, the preflight says so and why, and the run writes a full.
- A differential is never written if its base cannot be fully validated (header, trailer, manifest decryption, manifest validation).
- The plaintext manifest exists only in memory; it is never written to a temp file.
- Key material (password, hmac-secret, KEK, V, derived keys, recovery code) is zeroed after use, as 1.x does for passwords.
- The existing backup lock serializes backup runs; restore/verify stay read-only on the backup directory.

## 15. Test plan

### 15.1 Unit tests

- Header: encode/decode, every field validated, unknown fields rejected, length bounds, `compression` other than `"none"` rejected, bit flip in any header byte causes authentication failure of the sections.
- Key sets: each slot type unwraps with the correct credential and fails with a wrong one; slot swap/reorder fails (AAD); modified slot fails; recovery code format, checksum, normalization (case, dashes, spaces); spare registration with the same YubiKey refused (mocked FIDO2); key material zeroed.
- Trailer: checksum, bounds, part-count mismatch, trailer spanning two parts.
- Sections: key separation (data key cannot decrypt manifest), section swap between sets fails, truncation of each section detected.
- Manifest: round trip, every rule in 5.2, fuzz tests for the header JSON and manifest parsers.
- Change detection: new, changed size, changed mtime only, changed ctime only (mtime reset via `SetFileTime`), attribute change only, unchanged, deleted file, deleted directory, new empty directory, case-only rename, FAT source (`c = 0`).
- Exclude patterns: name vs rooted patterns, case-insensitivity, directory subtree, invalid pattern rejected at startup, pattern change between full and differential.
- Unreadable files: locked file with `fail` and `skip`, failure after TAR header (void entry), stale entry in differential, retention skipped.
- Password minimum: shorter than `password_min_length` rejected, characters counted (not bytes), config values below 8 rejected, only at enrollment.
- Type selection: every rule in 6.1 including boundary values and key set mismatch.
- Retention: chains, keep-differentials, incomplete sets, 1.x files untouched, logs kept while any set references their run ID, skipped/stale rule.
- Naming: parse/format round trip, differential numbers never reused after retention, a DIFF file renamed to another chain ID is rejected (file name/header mismatch), and a DIFF whose header `chain_id` was edited fails authentication.

### 15.2 Round-trip tests (restore result compared byte-for-byte and structurally with the source, including timestamps and attributes)

- Full -> restore.
- Full -> modify (add, change, delete, delete directory, empty directory, rename, case-only rename, attribute change) -> differential -> restore differential == modified source; restore full == original source.
- Two differentials on the same base; each restores its own state.
- Differential with no changes.
- Restore with recovery code; restore with the spare YubiKey slot (mocked FIDO2).
- YubiKey modes 2/3 with mocked FIDO2 functions: one touch per backup run and per restore of a differential.
- Read-only and hidden files and directories restored with their attributes.

### 15.3 Failure tests

- Missing base, base from another chain (renamed files), missing/extra/truncated part of base or differential, wrong password during backup, interrupted backup (process killed before rename) -> set not listed and not usable as a base or key set source, `.tmp` cleanup.
- Source file changes size during backup with `fail` (clean abort) and `skip` (void entry, warning).

### 15.4 Scale test

Synthetic source with 50,000 files (mixed sizes) and ~5 GB: measure differential creation time with 1 % changed files, peak memory, and manifest size; assert memory stays below a defined budget.

## Appendix A. README draft: "How your backups are locked"

Audience: RestoreSafe users, not cryptography experts. This draft was adopted into README.md ("How your backups are locked") in phase 7, adjusted to the final prompts; the README is now the maintained version. Everything below the line is the original draft.

---

### How your backups are locked

#### In short

- Your backups are encrypted with a **master key** that RestoreSafe creates at random.
- The master key is stored inside every backup, but only in **locked boxes**. Each box opens with one of your unlock methods: your password, your YubiKey, your spare YubiKey, or your recovery code.
- To restore, you only need to open **one** box. Any of your unlock methods works.

#### The picture

```text
Every backup file contains:

  Box 1: master key, locked with your password + YubiKey
  Box 2: master key, locked with your password + spare YubiKey    (optional)
  Box 3: master key, locked with your recovery code               (optional)

Open any one box  ->  master key  ->  your files
```

The boxes are not secret. Someone who steals your backup files also has the boxes, but they still need one of your unlock methods to open one. Without it, the backup is useless to them.

#### Your unlock methods

| `authentication_mode` | You unlock with | Optional extras |
|---|---|---|
| `1` | Password | Recovery code |
| `2` | Password + YubiKey | Spare YubiKey, recovery code |
| `3` | YubiKey | Spare YubiKey, recovery code |

Turn the extras on in `config.yaml` with `yubikey_spare: true` and `recovery_code: true`.

#### What you will see

**Your first backup (key setup).** RestoreSafe creates your keys:

1. You choose a password (at least 12 characters) and enter it twice.
2. You register your YubiKey (two Windows Security prompts).
3. With `yubikey_spare: true`: RestoreSafe asks you to swap in your spare YubiKey and register it too (two more prompts). Accidentally inserting the first YubiKey again is detected and refused.
4. With `recovery_code: true`: RestoreSafe shows your recovery code once. Copy it into your password manager or write it down.
5. Then every source directory gets a full backup.

**Every backup after that.** Enter your password once and/or touch your YubiKey once. RestoreSafe reuses your keys automatically, for differential **and** new full backups, so your spare YubiKey can stay in its safe place.

**Restore and verify.** Enter your password and/or touch whichever of your YubiKeys you have. If you have a recovery code, you can choose `[R] use recovery code` instead.

#### When RestoreSafe creates new keys

New keys mean: a new master key, new boxes, and a new full backup of every source directory. This happens when:

- you run your first backup, or the backup directory contains no RestoreSafe 2.0 backup anymore (for example because you deleted all backups);
- you change `authentication_mode`, `yubikey_spare`, or `recovery_code` in `config.yaml`;
- you press `[K]` in the backup summary, to change your password, replace a lost YubiKey, or get a new recovery code.

The backup summary always tells you in advance when new keys will be created and why.

**Important:** new keys come with new unlock methods. Your old password, old YubiKey registrations, and old recovery code do **not** open backups made with the new keys. They still open your older backups, until retention deletes them.

#### What to keep where

| Item | Keep it | Never |
|---|---|---|
| Password | In your head or a password manager | In a file next to your backups |
| YubiKey | With you | - |
| Spare YubiKey | At a different, safe place (home safe, trusted person) | In the same bag as your main YubiKey |
| Recovery code | On paper, in a safe place | Next to your backups, or unencrypted on your computer |

The recovery code opens your backups **on its own**, even in password + YubiKey mode. Treat it like the key to a safe.

#### What if ...

| Situation | What to do |
|---|---|
| I lost my YubiKey. | Restore with your spare YubiKey or your recovery code. Then press `[K]` at your next backup to create new keys with a new YubiKey, so you have a spare again. Without spare and recovery code, backups locked with that YubiKey cannot be restored by anyone. |
| I forgot my password. | Restore with your recovery code; it works alone, no password or YubiKey needed. Then press `[K]` at your next backup to set a new password. Without a recovery code, the backups cannot be restored by anyone. |
| I want to change my password. | Press `[K]` at the next backup. Your older backups keep opening with the old password. |
| Someone stole my backup drive. | Without your unlock methods, they cannot read anything. If you think your password or recovery code was exposed too, press `[K]` at your next backup and delete the old backups once the new ones are in place. |
| I deleted all backups. | The next backup creates new keys, like the first time. |
