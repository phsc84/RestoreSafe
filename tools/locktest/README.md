# locktest

`locktest` measures how the backup lock behaves on a network share when the network drops: a byte-range lock with `LockFileEx` on the first byte of a file opened with sharing, as RestoreSafe locks `restoresafe.lock` (core spec 14). It was written for refactoring item RF-126 and measured the Mac share on 2026-10-25 (results below). Run it again when the backup directory moves to another NAS or SMB server: what happens after a drop depends on the server.

It is a development tool, not part of RestoreSafe: it imports nothing of the program (`internal/architecture`).

## Build

From the repository root:

```powershell
go build -o sandbox\locktest.exe ./tools/locktest
```

`sandbox\` is ignored by Git, and Windows Sandbox can map it (below).

## Commands

```text
locktest hold [-shared] <file>   take the lock, write a holder line, keep it until Ctrl+C
locktest probe <file>            try to take the lock every 5 s and report each change
```

- **hold** opens `<file>` (creates it if missing), takes the exclusive lock, or the shared lock with `-shared` (a restore's or verification's), and, for the exclusive lock, writes a holder line (computer, process ID, time) at offset 512. Every 5 s it reads one byte beyond the lock and prints `handle fails: …` and `handle works again` when that changes. Ctrl+C prints `Ctrl+C: closing the handle` and `handle closed, error: …` with their times, then ends.
- **probe** opens `<file>` every 5 s, tries an exclusive lock and releases it at once. It prints a line only when the result changes, with the time and the seconds since it started: `open failed: …` (the share is unreachable), `locked: … | holder line readable: "…"` (or `not readable`), or `FREE: the lock could be taken`.

## Setup

Use a test folder on the share, never the real backup directory, and a new file name for each scenario (the tool never empties the file, so lines and locks of an earlier scenario would mix in). Open three PowerShell windows and set in each:

```powershell
$L = 'C:\dev\RestoreSafe\sandbox\locktest.exe'
$D = 'M:\RestoreSafe-test\locktest'   # a folder on the share
```

Window C is the network clock; use the server's IP address, since name lookups are slow during a drop:

```powershell
$ip = '10.10.10.10'; $last = $null
while ($true) { $ok = Test-Connection $ip -Count 1 -Quiet
  if ($ok -ne $last) { '{0:HH:mm:ss} reachable: {1}' -f (Get-Date), $ok; $last = $ok }
  Start-Sleep 1 }
```

Cut the network by pulling the cable or turning Wi-Fi off on the PC, not with `Disable-NetAdapter`: switching the adapter off can reset the connection instead of dropping it silently (MountSafe core spec 6.6), which is another case.

## Scenarios

Windows A (hold), B (probe) and C (clock). Stop the probe before cutting the network and start it again as soon as C shows `reachable: True`, so its seconds count from the reconnect; in the scenarios where A ends during the drop, start it right after that instead (its opens fail and leave nothing behind while the share is unreachable).

| # | What | Steps | What it tells |
|---|---|---|---|
| 0 | Baseline | A: `& $L hold "$D\lock0"`; B: `& $L probe "$D\lock0"` shows `locked` with the holder line; Ctrl+C in A; B shows `FREE`. Again with `hold -shared` on `lock0s` | The probe reads the holder line while the lock is held; how fast a closed lock is free |
| 1 | Exclusive, kept open | A: `hold` on `lock1`; B: `locked`, Ctrl+C; cut for 1–2 min; A prints `handle fails`; reconnect; B: probe for 5 min; Ctrl+C in A | Whether the lock comes back with the handle (B never `FREE` until A ends) |
| 2 | Exclusive, closed during the drop | A: `hold` on `lock2`; B: `locked`, Ctrl+C; cut; after `handle fails`, Ctrl+C in A; B: probe at once; reconnect about 10 s later; B until `FREE` | How long the server keeps the lock of a handle closed during a drop (a run that ends with a network error) |
| 3, 4 | Shared | 1 and 2 with `hold -shared` on `lock3`, `lock4` | The same for the shared lock |
| 5 | Holder disappears | A runs in Windows Sandbox on the UNC path (below); B on the PC: `locked`; close the Sandbox window and note the time; B until `FREE` | How long the server keeps the lock of a client that is switched off or crashes |

## Windows Sandbox as a second client

Windows Sandbox is a separate Windows with its own SMB connection, so the server sees it as a second PC. Turn it on once (PowerShell as administrator, then restart Windows):

```powershell
Enable-WindowsOptionalFeature -Online -FeatureName Containers-DisposableClientVM -All
```

Start it with `sandbox\` mapped to `C:\sandbox`, from the repository root:

```powershell
@"
<Configuration>
  <MappedFolders>
    <MappedFolder><HostFolder>$((Resolve-Path sandbox).Path)</HostFolder><SandboxFolder>C:\sandbox</SandboxFolder><ReadOnly>false</ReadOnly></MappedFolder>
  </MappedFolders>
</Configuration>
"@ | Set-Content sandbox\sandbox.wsb
Start-Process sandbox\sandbox.wsb
```

In the Sandbox, open PowerShell, connect to the share with the server's account (the Sandbox has neither your credentials nor your drive letters) and use the UNC path:

```powershell
net use \\10.10.10.10\Data_SSD /user:<user>
C:\sandbox\locktest.exe hold \\10.10.10.10\Data_SSD\RestoreSafe-test\locktest\lock5
```

Closing the Sandbox window ends that Windows at once, like a power cut; everything in it is gone, the mapped `sandbox\` stays.

## Results on the Mac share (2026-10-25)

| Case | Result |
|---|---|
| Drop of a few seconds | Hidden by Windows; the handle failed only about 30 s after the cut |
| Kept open, drop of 1–2 min (exclusive and shared) | The handle works again about 5 s after the reconnect, and the lock is held throughout |
| Closed during a drop of 54 s to 2 min 47 s (exclusive and shared) | No orphan: free at the probe's first try after the reconnect |
| Holder disappeared (Sandbox closed) | Held 2 min 22 s, holder line readable throughout |

The details are in refactoring plan RF-126; core spec 14 is what RestoreSafe does with them.
