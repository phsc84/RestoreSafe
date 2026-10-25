# Specification: Key derivation parameters

|          |                                                                                                   |
| -------- | ------------------------------------------------------------------------------------------------- |
| Status   | Agreed 2026-10-25. Defaults, limits and memory check decided and measured on the development PC (sections 4, 6); decisions K1–K4 in section 8. Not implemented: RestoreSafe 2.2.0 and MountSafe 1.0 implement it. Earlier drafts (0.1–0.7): git history. |
| Applies  | RestoreSafe (key sets, RestoreSafe SPEC-core 4.4) and MountSafe (vault header, MountSafe SPEC-core 5.1) |
| Origin   | Review of SPEC-refactoring 5.4, refactoring round after 2.1.0                                      |
| Sources  | RFC 9106 sections 4 and 7; OWASP Password Storage Cheat Sheet                                       |
| Copies   | Identical in RestoreSafe and MountSafe (`docs/SPEC-kdf.md` in each). A change to one is made to the other too. |

This document sets the Argon2id parameters with which RestoreSafe and MountSafe derive the key-encryption key of a slot, the limits they enforce, and why. It replaces the defaults of 512 MiB, 3 passes, 4 threads and the single set of bounds in `cryptox` (64 MiB–4 GiB, 2–20 passes, 1–255 threads).

## 1. Threat model

Neither tool is a login application. The attacker holds the encrypted files (a stolen drive, a copied NAS share, a cloud account) and guesses offline: no rate limit, no lockout, unlimited time, and any hardware they can buy or rent. Guidance for login servers (OWASP's minimums, "under one second", server load) does not apply; it led to the current parameters.

The defender pays the cost once per unlock: once per backup run, restore or verification, once per mount. A few seconds and several GiB of RAM are affordable.

The parameters decide the outcome only where an attacker can guess the input:

| Slot | Brute force possible? | Role of the parameters |
| ---- | --------------------- | ---------------------- |
| Password (mode 1) | Yes | Decisive, together with the password's strength. |
| Password + YubiKey (mode 2) | Only if the YubiKey and its PIN were stolen too: the hmac-secret needs both (RestoreSafe SPEC-core 4.4.3), and the YubiKey locks its PIN after 8 wrong tries | Decisive in that case. |
| YubiKey only (mode 3) | Only with the YubiKey and its PIN, which the YubiKey rate-limits | None: the hmac-secret has 256 random bits, as many as the master key V, which an attacker could search directly instead (RestoreSafe SPEC-core 4.4.1). |
| Recovery code | No: 125 random bits | A second line of defence: the code is the weakest secret of a key set, and each guess costs a full derivation (about 2²³ Argon2 blocks at the defaults, roughly 25–30 bits of extra work) and holds m of memory. This matters only if 125 bits ever come within reach. |

Every slot uses the same parameters, the YubiKey slot too, although it gains nothing from them: one way to build a slot keeps the code simple, it costs a few seconds per unlock, and it is what gives the recovery code its full parameters. A slot type may never get lighter parameters than the recovery slot.

## 2. The parameters

Argon2id fills a matrix of 1 KiB blocks. Every block is computed from the previous block in its row and one earlier, pseudo-randomly chosen block. All three parameters are inputs to this computation: changing one gives a different key, so they are stored with each slot and fixed for the life of a key set.

### 2.1 m, memory

The size of the matrix. The default of 4 GiB is 4,194,304 blocks.

- Every guess must hold the whole matrix while it runs. This is the main protection: it limits how many guesses an attacker can run in parallel on any hardware.
- An attacker can store fewer blocks and recompute the missing ones when they are needed (a time-memory trade-off). For Argon2id the best known trade-off saves at most a factor of about 2.1 at t=1 and 1.33 at t≥2 (RFC 9106 7.2).
- Filling more memory also takes longer. An attacker's cost per guess (memory held × time it is held) therefore grows roughly with the square of m, while the defender's unlock time grows linearly.

### 2.2 p, parallelism (lanes)

The matrix is split into p rows (lanes) that are computed at the same time. With p=4, 4 GiB become 4 lanes of 1 GiB each. The lanes are cut into 4 slices and synchronise after each slice; blocks can reference other lanes.

- `x/crypto/argon2` computes each lane in its own goroutine. On four or more cores, filling the memory takes roughly a quarter of the time.
- p changes neither the total memory nor the work per guess. **It only serves the defender:** the same memory is filled faster, so more memory fits into the time budget.
- The attacker gains nothing from it; they already run many guesses in parallel.
- p above the number of cores of the unlocking machine brings no speed-up. Because p is fixed in the key set, a restore machine with fewer cores than p unlocks correspondingly slower. RFC 9106 4 step 4 recommends p=4.

### 2.3 t, passes

How often the whole matrix is computed. Pass 1 fills it; each further pass recomputes every block, mixing the old value with newly referenced blocks.

- Time grows linearly with t; memory stays the same.
- Extra passes defend against time-memory trade-offs. That matters for Argon2i, whose memory access is predictable. Argon2id's access is mostly unpredictable, so the trade-off gains are small (2.1).

### 2.4 Why memory first, passes second

**The defender has a time budget.** Unlock time is roughly proportional to the number of blocks computed, m × t. The same budget can be spent in different ways (example: 2 seconds):

| Option | m | t | Blocks computed | Defender time |
| ------ | - | - | --------------- | ------------- |
| A | 4 GiB | 1 | 4 Gi | 2 s |
| B | 2 GiB | 2 | 4 Gi | 2 s |
| C | 1 GiB | 4 | 4 Gi | 2 s |

**The attacker pays for memory over time,** like renting RAM billed per GiB per second. Each guess holds its memory for the whole computation; with hardware as fast as the defender's, each guess takes the same 2 seconds:

| Option | RAM held | For | Cost per guess (GiB × s) | With the best known trade-off (2.1) |
| ------ | -------- | --- | ------------------------ | ----------------------------------- |
| A | 4 GiB | 2 s | 8 | 8 ÷ 2.1 ≈ 3.8 |
| B | 2 GiB | 2 s | 4 | 4 ÷ 1.33 ≈ 3.0 |
| C | 1 GiB | 2 s | 2 | 2 ÷ 1.33 ≈ 1.5 |

Extra passes reuse memory that is already allocated: they add time, not memory. Memory is the attacker's expensive resource, because it limits how many guesses run in parallel. One pass with the largest memory therefore turns the defender's time into the most attacker cost; this is what RFC 9106 7.3 states for Argon2id. Option A costs the attacker about 2.5 times as much as option C at the same unlock time.

**When t>1 makes sense.** Only when m cannot grow further, because the weakest machine that must unlock has no more RAM, and the time budget still has room (RFC 9106 4 step 10).

**t=2 at the same memory.** Going from t=1 to t=2 at fixed m doubles the time per guess for both sides, an even 1:1 exchange, unlike m. With the trade-off gains of 2.1 it is slightly better than 1:1: at 4 GiB, the attacker's cost per guess rises from 1 ÷ 2.1 ≈ 0.48 to 2 ÷ 1.33 ≈ 1.5, about three times, for twice the unlock time. That factor rests on the best known attack, which an attacker would first have to implement; ignoring it, the factor is exactly 2. Decision K1 uses this.

## 3. What raising each parameter does to an attacker

| Attacker's resource | Raising m | Raising t | Raising p |
| ------------------- | --------- | --------- | --------- |
| **GPUs, ASICs, FPGAs** (thousands of cores, limited memory per device) | **Strong protection.** A device runs only (its memory ÷ m) guesses at once; at 4 GiB a 24–80 GB GPU runs a handful, and most of its cores idle. Memory bandwidth becomes the limit. | Linear slowdown only; parallelism is unchanged. | None. |
| **Many CPUs, servers, cloud** | **Protects.** Cores are useless without m of RAM each; the attacker pays for RAM per parallel guess, and each guess holds it longer. | Linear slowdown. | None. |
| **Very large RAM** (one machine with hundreds of GB) | **Protects.** Guesses in parallel = RAM ÷ m; four times m means a quarter of the parallel guesses, each running longer. More RAM never makes the attack easier than at lower m. | Linear slowdown; the number of parallel guesses is unchanged. | None. |
| **Very fast single-thread CPU** | Protects linearly (more blocks to compute per guess), but the defender's CPU gains the same factor. | Linear slowdown, same as m but without the memory cost. | None; per guess the work is the same. |
| **Time-memory trade-off attacks** (store fewer blocks, recompute the rest) | Indirectly: more blocks to recompute. | **This is what t is for.** The best known gain drops from about 2.1 at t=1 to 1.33 at t≥2 (2.4). | None. |

**What none of the parameters defends against:**

- **Weak passwords.** A dictionary word or a reused, leaked password falls in few guesses at any cost per guess. Each additional random character multiplies the attacker's work by the alphabet size (roughly 60 to 90); going from 1 GiB to 4 GiB multiplies the cost per guess by roughly 4 to 16.
- **Malware on the user's machine:** keyloggers, a memory dump while a backup runs or a vault is mounted, screen capture.
- **A stolen YubiKey together with the password**, or an exposed recovery code.
- **Plaintext outside the encryption:** a mounted vault read by another process, restored files.

## 4. Defaults and limits

### 4.1 Defaults (new key sets and new vaults)

| Parameter | Default | Reason |
| --------- | ------- | ------ |
| m | **4 GiB** | Offline threat model; RFC 9106 4 steps 5–6: the largest memory the target machines can afford. Fits machines with 8 GB of RAM and more (4.4). |
| t | **2** (K1, decided 2026-10-24) | The second pass costs less than the first: on the development PC 4.35 s against 2.97 s for t=1 (section 6), because the first pass also includes getting 4 GiB of fresh memory from Windows. For about 1.4 s more, the attacker's cost per guess rises two- to threefold (2.4), and backups stay readable by RestoreSafe 2.0 and 2.1 (5.2). |
| p | **4** | RFC 9106 4 step 4; uses four cores to fill 4 GiB in the time budget (2.2). |

The unlock time with the defaults is measured on the weakest machine the user wants to restore on, and recorded in section 6. That machine is the development PC (a six-year-old entry-level AMD Ryzen, 16 GB): every other PC that restores is faster, so its numbers are the ones that count. Target: under 5 seconds on the development machine with other programs closed; met with 4.35 s. In everyday use, with less memory free, it takes 6–7 s (section 6, 4.4).

2 GiB with t=3 (3.13 s) was the alternative that always fits into the free memory of the development PC; by the cost model of 2.4 it costs an attacker about a third of the default per guess, so the default stays at 4 GiB.

### 4.2 Enrollment limits (what a new key set or vault should be created with)

| Parameter | Minimum | Maximum |
| --------- | ------- | ------- |
| m | 4 GiB | 32 GiB |
| t | 1 | 10 |
| p | 1 | 16 |

- The minimum equals the default (K2): Windows 11 PCs in use have at least 8 GiB, which is what the rule of 4.4 step 2 needs for 4 GiB, so no machine needs less. It replaces 64 MiB.
- MountSafe enforces these limits. RestoreSafe still creates keys with configured values that are outside them but within the read limits of 4.3, which include every value 2.0 and 2.1 accepted, and warns that they should be changed in `config.yaml` (5.1).
- **Values above the default are for machines with very large RAM only.** The memory is needed by **every machine that ever unlocks the key set**, not only by the one that creates it: a replacement laptop after the original was stolen, another PC that mounts the vault. A backup with 32 GiB keys cannot be restored on a 16 GB laptop in reasonable time. Before raising m, make sure every machine that may have to restore has at least twice that much RAM.
- Raise t only when m cannot grow further on the weakest machine and time is left (2.4).

### 4.3 Read limits (what a slot from disk is accepted with)

| Parameter | Minimum | Maximum |
| --------- | ------- | ------- |
| m | 64 MiB | 32 GiB |
| t | 1 | 20 |
| p | 1 | 255 |

- The read limits accept every key set written by any earlier version (64 MiB to 4 GiB, 2 to 20 passes) and every key set the enrollment limits allow.
- Their purpose is to refuse absurd values from a damaged or hostile header before any memory is allocated, not to judge strength. A key set below the enrollment minimum still opens; RestoreSafe's preflight says that it uses weaker parameters than the current defaults and offers **New keys + full backup**, unless `config.yaml` would create the new keys just as weak (5.1).
- The maximum of p stays at 255: the value must fit the `uint8` of `argon2.IDKey`.

### 4.4 Available memory

Windows limits allocations by the commit limit (RAM plus page file), not by physical RAM. Three cases:

- **m fits into the available physical memory:** the derivation runs at full speed.
- **m is larger than the available memory, but well below the total physical memory:** Windows makes room by moving memory that other programs are not using into the page file. Measured on the development PC (section 6): 4 GiB with t=2 took 5.9–7.3 s with 4.1 GiB available instead of 4.35 s, and the other programs did not feel slower afterwards. This is the everyday case on a 16 GB PC with a browser and an office suite open.
- **m comes close to the total physical memory, or exceeds what Windows can commit:** close to the total, the derivation itself is paged out, and Argon2's random memory access makes it much slower: on the development PC, 12 GiB of 13.9 GiB took 2.6 times as long as the smaller sizes predict (section 6), and closer to the total it gets worse. Above the commit limit (page file disabled, fixed or full), the Go runtime ends the process with an out-of-memory error that `recover` cannot catch.

Only the last case needs the user. Therefore, before every derivation:

1. Read the installed memory (`GetPhysicallyInstalledSystemMemory`), the total and available physical memory and the available commit (`GlobalMemoryStatusEx`: `ullTotalPhys`, `ullAvailPhys`, `ullAvailPageFile`). `ullTotalPhys` is lower than the installed memory by what the firmware and an integrated GPU reserve: a few hundred MiB without integrated graphics, several GiB with it (the development PC reports 13.9 GiB of 16 GiB, section 6), so the enrollment rule uses the installed memory; if that call fails, `ullTotalPhys` rounded up to the next GiB. `ullAvailPageFile` is only the commit available **now**: a system-managed page file (the Windows default) grows when a program asks for more, so it understates what a derivation can get (step 3).
2. **Enrollment:** if m is larger than half the installed memory, refuse: *"These key settings need 8 GiB of memory; this PC has 8 GiB. Use at most 4 GiB."* With the default of 4 GiB, a PC with 8 GiB installed passes.
3. **Unlock and enrollment, the memory can't be committed:** when m + 512 MiB is larger than `ullAvailPageFile`, the tool asks Windows to commit m + 512 MiB (`VirtualAlloc` with `MEM_RESERVE | MEM_COMMIT`) and releases it at once. The 512 MiB cover what the process needs besides the matrix: the Go runtime, and in RestoreSafe the manifest map (up to about 350 MB, RestoreSafe SPEC-core 5.3) and the chunk buffers. Windows grows the page file to satisfy the request where it can, so a successful probe shows that the memory can be had. It reserves nothing: Windows shrinks the page file again later (on the development PC it was back at its earlier size within the hour, section 6), and the derivation's own allocation grows it again. Only if this probe fails, refuse, since the process would end: *"Opening these keys needs 4 GiB of memory, but Windows can provide only 3.1 GiB right now (memory and page file). Close other programs and try again."* – **Try again** / **Cancel**. Comparing m with `ullAvailPageFile` alone would refuse the everyday case: the development PC reported 3.5 GiB available commit with programs open, and a 4 GiB derivation in that state succeeded while Windows grew the page file (section 6).
   - **Order at unlock:** the question of step 4, when it applies, comes before the probe. A damaged or hostile header can ask for up to 32 GiB (4.3); the user then sees "needs 32 GiB" and can cancel before the probe grows the page file by that much.
   - **What the probe can't prevent:** another program can take the memory between the probe and the derivation, and the process then ends as described above. This is accepted because every derivation comes before anything is written: a backup unlocks or enrolls before its first part file, a restore before it creates its destination folder, MountSafe before it mounts. Implementations keep that order.
4. **Unlock, m larger than the total physical memory minus 2 GiB:** ask: *"Opening these keys needs 4 GiB of memory; this PC has 5.9 GiB in all. Opening them can take very long. Close other programs and try again, or continue."* – **Try again** / **Continue** / **Cancel**. The 2 GiB margin is measured (section 6): on the development PC, 10 GiB (3.9 GiB below the total) still took about as long as the smaller sizes predict, 12 GiB (1.9 GiB below) 2.6 times as long.
5. **Unlock, m larger than the available physical memory** (and neither 3 nor 4): no question. The progress text says *"Unlocking… Windows is making room in memory; this takes a few seconds longer."*

## 5. Changes in RestoreSafe

### 5.1 Behaviour

- New key sets use the defaults of 4.1 unless `config.yaml` sets other values within the read limits of 4.3.
- Existing key sets are not changed; they are reused as before (RestoreSafe SPEC-core D5). When the current key set uses parameters below the enrollment minimum of 4.2, the Keys card and the preflight say so and offer **New keys + full backup…**; nothing is forced. While `config.yaml` itself sets values below 4.2 (`ARGON2_BELOW_RECOMMENDED`), new keys would be just as weak, so the offer is replaced by that warning's remedy: raise the values in `config.yaml` first; the offer appears once the configuration is reloaded with values within 4.2.
- **New keys above the default memory.** 2.0 and 2.1 capped `memory_mb` above 4096 to 4096 (`ARGON2_CAPPED`); this version uses the configured value, so a `memory_mb: 8192` that 2.1 ignored now takes effect. Whenever a backup creates new keys with m above the default, its preflight says what that means: *"New keys need 8 GiB of memory on every PC that restores these backups."* The release notes of 2.2.0 say so too.
- The memory check of 4.4 runs before every derivation.
- `config.yaml`: the argon2 settings keep their names; their documentation in `config-SAMPLE.yaml` says MiB (refactoring RF-98) and points to sections 2 and 3 of this spec. The argon2 values are used only when new keys are created, so they never stop RestoreSafe from starting, restoring or verifying:
  - **The old sample block.** `config-SAMPLE.yaml` of 2.0 and 2.1 set `time: 3`, `memory_mb: 512`, `threads: 4`, and most configurations are copies of it. It is treated like any other value below: valid, with the warning `ARGON2_BELOW_RECOMMENDED` for `memory_mb`. Nothing is ignored or rewritten behind the user's back; the warning tells them to raise it or remove the block to get the defaults of 4.1.
  - **Values outside 4.2 but within 4.3** (for example `memory_mb: 2048`, or `time: 15`) stay valid: new keys are created with them. The health check reports `ARGON2_BELOW_RECOMMENDED` (GUI spec 11.8), a warning naming the key, its value and the limit of 4.2, with the remedy to change it in `config.yaml`; the preflight of a backup that creates new keys repeats it.
  - **Values outside 4.3** load; the health check reports `ARGON2_OUT_OF_RANGE` (GUI spec 11.8) naming the key, its value and the limit. It blocks only a backup that creates new keys (the preflight says so, with the remedy); a backup that reuses the current key set, restore and verify are not affected. Values are never capped silently.

### 5.2 Compatibility

The format does not change: the slot's `kdf` object already carries `time`, `memory_kib` and `threads`. Only the accepted ranges change:

- Every backup of 2.0 and 2.1 opens unchanged (4.3 accepts their ranges).
- RestoreSafe 2.0 and 2.1 accept t from 2 to 20 and m up to 4 GiB. Backups whose keys use **t=1 or more than 4 GiB** need this version or later to restore; earlier builds reject them with *"Invalid Argon2 time …"* or *"Invalid Argon2 memory …"*.
- With the defaults at **t=2 and 4 GiB** (K1), 2.0 and 2.1 can still restore backups made with the defaults.
- The release notes say which applies; the README's restore advice already is to use the latest version.

### 5.3 Code

- `cryptox`: two sets of bounds, `Enroll…` and `Read…`, replacing `MinArgon…`/`MaxArgon…`; `ValidateArgon2Params` takes which set applies. `DefaultArgon2Params` = 4 GiB, t=2, p=4.
- Package config validates the argon2 values against the read set (outside: `ARGON2_OUT_OF_RANGE`) and compares them with the enrollment set (outside: `ARGON2_BELOW_RECOMMENDED`); it neither caps nor rejects (5.1).
- A memory check (4.4) in `fsx`, as in MountSafe (MountSafe SPEC-core, package table), called by unlock and enrollment.

## 6. Measurements

Measured 2026-10-24 with `tools/kdfbench` of MountSafe (`argon2.IDKey` as both tools call it, 32-byte key, median of 3 derivations; Go 1.27.2). Development PC: a six-year-old entry-level AMD Ryzen, 8 logical CPUs, 16 GB RAM (13.9 GiB usable). It is also the weakest machine that must restore (4.1), so there is no separate row for one.

Memory figures of the development PC on 2026-10-25 with everyday programs open (`Win32_PhysicalMemory`, `Win32_OperatingSystem`): 16 GiB installed, 13.9 GiB visible to Windows (the rest is reserved for the integrated graphics), 3.8 GiB physical memory available, 3.5 GiB commit available. The available commit is below the default of 4 GiB, so 4.4 step 3 probes instead of comparing. In this state, with the system-managed page file (`AutomaticManagedPagefile` true), a derivation with the default (4 GiB / 2 / 4) succeeded in 6.77 s with 3.4 GiB available, and Windows grew `C:\pagefile.sys` from 6,912 to 7,594 MiB (in use: 931 to 1,533 MiB) to provide the memory: the derivation gets more than `ullAvailPageFile` reports.

| Machine | RAM | m / t / p | Programs closed (≈ 7 GiB available) | Everyday (3.6–4.8 GiB available) |
| ------- | --- | --------- | ----------------------------------- | -------------------------------- |
| Development PC | 16 GB | 512 MiB / 3 / 4 (old default) | 0.64 s | 0.98 s |
| Development PC | 16 GB | 2 GiB / 2 / 4 | | 2.22 s |
| Development PC | 16 GB | 2 GiB / 3 / 4 | | 3.13 s |
| Development PC | 16 GB | 4 GiB / 1 / 4 | 2.97 s | |
| Development PC | 16 GB | **4 GiB / 2 / 4 (default)** | **4.35 s** | **5.9–7.3 s** (4.1 GiB available; 2 runs); **6.77 s** (3.4 GiB available, less commit than m; 2026-10-25) |
| Development PC | 16 GB | 8 GiB / 1 / 4 | 8.23 s (6.7 GiB available; 1 run, 2026-10-25) | |
| Development PC | 16 GB | 10 GiB / 1 / 4 | 9.36 s (8.2 GiB available; 1 run, 2026-10-25) | |
| Development PC | 16 GB | 12 GiB / 1 / 4 | **23.48 s** (10.7 GiB available; 1 run, 2026-10-25) | |

**Above the available memory** (the everyday column of the default): Windows moved idle memory of other programs into the page file; the derivation took up to 3 s longer, the other programs did not feel slower afterwards, and nothing aborted. This is why 4.4 asks only near the total physical memory or above the commit limit.

**Close to the total physical memory** (2026-10-25, programs closed, 13.9 GiB visible to Windows): 4 GiB with t=1 takes 2.97 s, about 0.74 s per GiB, which predicts 5.9 s for 8 GiB, 7.4 s for 10 GiB and 8.9 s for 12 GiB. 8 and 10 GiB took 8.23 s and 9.36 s: about 2 s more, the cost of paging out other memory, as in the everyday column. 12 GiB, 1.9 GiB below the total, took 23.48 s, 2.6 times the prediction: there the derivation's own memory is paged. This knee is what the 2 GiB margin of 4.4 step 4 is set at. No run aborted. Afterwards the page file was back at its size before the step 3 measurement (6,912 MiB, 1,919 MiB in use).

## 7. MountSafe

MountSafe uses the same defaults and limits from its first version (MountSafe SPEC-core 5.1). It has no older vaults, so its read limits can equal its enrollment limits. K1's compatibility argument does not apply to it, but its unlock-time argument does, so it uses t=2 as well. The memory check of 4.4 runs before Create vault and before every mount.

## 8. Decisions

| # | Question | Decision |
| - | -------- | -------- |
| K1 | t=1 (optimal per unit of time; new backups need this version to restore) or t=2 at 4 GiB (twice the unlock time, about three times the attacker cost, 2.0 and 2.1 can still restore; 2.4, 5.2)? | **Decided 2026-10-24: t=2.** 4.35 s on the development PC (section 6), within the budget, and the second pass costs only about 1.4 s. |
| K2 | Enrollment minimum 1 GiB, 2 GiB (RFC 9106's first recommended option), or 4 GiB? | **Decided 2026-10-25: 4 GiB**, equal to the default (first decided 2 GiB on 2026-10-24). Windows 11 PCs in use have at least 8 GiB, enough for 4 GiB by 4.4 step 2; a PC with less can't create keys, which is accepted. In RestoreSafe, configured values below it but within 4.3 stay valid, with a warning (5.1). |
| K3 | `password_min_length` stays 12, or rises with this change? | **Decided 2026-10-24: 12 stays**, in RestoreSafe and MountSafe. In mode 1 the password's strength and these parameters together set the attacker's cost (section 3); the new default raises the cost per guess about fiftyfold over the old one at the same password length (memory × time, 2.4: 8 times the memory, held about 7 times as long, section 6). |
| K4 | Release as 2.2.0 (format unchanged, ranges extended) or 3.0.0? | **Decided 2026-10-24: 2.2.0**, with the compatibility note of 5.2 in the release notes. The format does not change; every older backup opens; backups with the new default keys (4 GiB, t=2) also restore with 2.0 and 2.1, whose limits include 4096 MiB and t=2 (`cryptox` `MaxArgonMemoryKB`, `MinArgonTime`). |

## 9. Tests

In both tools unless marked; the memory figures of Windows come through an interface the tests replace, so no test needs a machine with a given amount of memory.

- **Bounds (4.2, 4.3):** each parameter at, below and above each limit; a slot from disk outside 4.3 is refused before anything is allocated (the test's allocation counter stays at zero); p above 255 can't reach `argon2.IDKey`.
- **Configuration (5.1, RestoreSafe):** values within 4.2 load without a finding; between 4.2 and 4.3, including the old sample block 3 / 512 / 4, they load, create keys with exactly those values and report `ARGON2_BELOW_RECOMMENDED`; outside 4.3 they load, report `ARGON2_OUT_OF_RANGE`, block a backup that creates new keys and leave a backup with the current keys, restore and verify alone; nothing is capped.
- **Offers (5.1, RestoreSafe):** weak current keys with a configuration within 4.2 offer **New keys + full backup…**; with a configuration below 4.2 they don't, and the remedy names the key; new keys above the default memory show the preflight line with the memory needed.
- **Memory check (4.4):** every step with faked figures: installed 8 GiB with `ullTotalPhys` 7.8 GiB passes enrollment at 4 GiB; installed memory unavailable falls back to `ullTotalPhys` rounded up; m above half the installed memory refuses enrollment; m + 512 MiB above `ullAvailPageFile` runs the probe, a succeeding probe goes on, a failing probe refuses with **Try again** / **Cancel**; m above the total minus 2 GiB asks before the probe runs; m above the available memory only changes the progress text.
- **Order (4.4 step 3):** each workflow (backup with and without enrollment, restore, verify; MountSafe: create, mount) derives before its first write; a test makes the derivation fail and checks that nothing was written.
- **Compatibility (5.2, RestoreSafe):** the format fixtures of 2.0.0 (512 MiB, t=3) open unchanged; a key set with the defaults stays within the bounds of 2.0 and 2.1 (4096 MiB, t ≥ 2), checked against those constants written into the test.
- **Slots (section 1):** every slot of a new key set, recovery slot included, carries the same parameters.
