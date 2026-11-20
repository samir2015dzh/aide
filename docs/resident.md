# Staying small while waiting

Aide stays on the owner's PC all day: the desktop app in the notification
area, aided (the owner role and this PC's machine) under its logon task, and
her. Most of that time nothing runs. This page says what each part does to
cost little then, and how it is measured. The owner's target for the tray
state (the window closed, nothing running) is **105 MB** of private working
set (Task Manager's Memory column) for all of them together, agents not
counted; the 2026-11-20 baseline on Aide 0.1.0 was 143.8 MB.

Nothing here changes a protocol or a stored format. Android has its own
resident service and is not covered.

## 1. The tray state

### 1.1 No page

The main window and the settings, update and about windows are destroyed
when closed, not hidden: their renderer process goes with them. The tray
panel's page goes 30 s after the panel hides (`main/tray.ts`). In the tray
state the app runs its main process, Chromium's GPU process and its network
service, and no page. `e2e/tray.spec.ts` checks that no page is left.

### 1.2 Idle memory given back

When power saving starts (§2: no window, no session, Her idle, for 30 s),
aided asks Windows to take every page it can from the working sets of
aided itself, her, and the desktop app's processes: its main process, which
names itself on the Watch it keeps for notifications (the `Aide-App-Pid`
header), and that process's descendants (GPU, utilities). The call is
`SetProcessWorkingSetSize(h, -1, -1)`, what `EmptyWorkingSet` does.

What it is, plainly: the pages move to Windows' standby list. They are still
in memory and come back at a soft fault for whatever touches them again; in
the meantime they count as available memory for any other program. The
private working set then shows what the processes actually touch while they
wait, not what they once touched. It is done once, as saving starts; what a
process touches afterwards stays in its working set. aided logs the working
sets before and after (`power saving: idle memory given back`).

### 1.3 Tried, or left for later

- Killing Chromium's GPU process in the tray state: Chromium counts it as a
  crash and, after a few, falls back to software rendering for good. Not done.
- Turning hardware acceleration off: smaller GPU process, slower window, and
  it takes a restart. Left as a possible option for the owner, not a default.
- Running the network service in the browser process: one process fewer,
  untested on the Electron in use. Not done.

## 2. Power saving (Windows)

### 2.1 When

aided saves power while all of these hold, for 30 s:

- no session running, preparing, waiting for an answer or queued;
- her does nothing (no turn, sub-agent or routine run: `Machine.Busy`);
- no window of the app holds a call open. The desktop app's gateway marks
  every call of a page with `Aide-Viewer: window`; its main process's own
  Watch has no mark.

It ends at once when one of them stops holding: a window opening (its first
call), a session about to start (the machine's dispatch wakes it before the
agent starts), her starting work (seen within 2 s while saving).

### 2.2 What

aided and her get `SetProcessInformation(ProcessPowerThrottling)` with
execution speed throttling on (Windows 10 1709 and later; Windows 11 shows it
as efficiency mode, EcoQoS) and below normal priority. Ending it gives both
back to the system's choice and normal priority, and also sets back the
processes in her's job, since a child started while her ran below normal
takes that priority.

Agents never run slowed: every process tree aided starts (agents, her) is
created with `NORMAL_PRIORITY_CLASS`, whatever aided's priority then. The
service's supervisor (`aidedw run --supervise`), which only waits on the
daemon, is throttled and below normal for good, and starts the daemon at
normal priority; the daemon sets itself to normal when it starts.

### 2.3 Self-report

Each change is a line of aided's log: `power saving starts` / `power saving
ends`, with the reason, the API result and the priority class. `aided status`
prints a `power` line, and the machine's control socket answers `GET /power`
with the state as JSON:

```json
{"supported":true,"saving":true,"since":"2026-11-20T09:12:03+08:00",
 "reason":"no session running, Her idle, no window open",
 "priority":"below normal","her_pid":4242}
```

Checking it: on Windows 10 (no efficiency mode column) Task Manager's
Details tab shows `aidedw.exe` and `her.exe` at Below normal priority while
`aided status` says saving; on Windows 11 the Efficiency mode column says
Yes. With a window open or a session running, neither holds.

## 3. Sessions wait while memory is low

Before an agent starts, the machine reads the system's free memory
(`GlobalMemoryStatusEx`; `/proc/meminfo` on Linux). Below max(1 GiB, 8% of
the total), the queued sessions stay queued: each says so once (a status
event, `waiting for memory: 300 MB of 16384 MB free (needs 1638 MB)`, and the
progress phase `queued_low_memory`, which the app shows as 排队中 ·
系统内存不足，内存释放后自动开始), and memory is read again every 5 s.
Above 25% more than the line they start, in the order they were queued.
`AIDE_MEMORY_RESERVE_MB` sets the reserve; `0` turns the check off.

The concurrency limits are unchanged: `aided run --max-sessions` (the
machine's `MaxConcurrent`) defaults to 0, no limit; a grant's
`max_concurrent_sessions` defaults to 0, no limit.

## 4. Wakeups while waiting

What aided's timers do while nothing runs and no window is open, and what
changed:

| Timer | Before | Now, while waiting |
|---|---|---|
| Machine database WAL checkpoint | every 200 ms | backs off to every 10 s while nothing is written; 200 ms again at the next write |
| Screen comparison of a Watch (`screenTick`) | every 1 s, one per Watch; the app's main process keeps one for good | 1 s for a window; 5 s for any other Watch. The relay connecting or dropping kicks the screens, so the status is as quick as before |
| Machines' liveness (`livenessEvery`) | every 2 s | every 10 s while no window looks; at once when one comes, then every 2 s |
| Pending machine names | a store read every 3 s | none once nothing is pending (nothing asks for it since approvals went) |
| Relay connection watch (`pkg/transport`) | every 2 s per connection, a ping after 15 s of quiet | unchanged: it notices the PC waking from sleep |
| Session janitor | every 10 s | unchanged (cheap) |
| Remote machine info, tunnel pings | every 30 s / 15 s per followed machine | unchanged: they keep other machines' state current |
| Her waiting messages | every 30 s | unchanged |
| Work stats | every quarter hour | unchanged |

In the desktop app's main process, the only repeating timers are the update
check (every 4 h) and a check of whether the network is up (every 60 s).

The two `aidedw.exe` processes Task Manager shows are the service's
supervisor (about 0.4 MB, waiting on its child) and the daemon itself.

The Go runtime's memory limit is 96 MiB by default (`GOMEMLIMIT` overrides
it, `off` for none): the collector works harder as the heap nears it; above
it the daemon only collects more often, it is never stopped for it. 64 MiB
was the starting point; under load it made the collector thrash, so the
owner's allowance of 96 MiB is used (`go run ./bench -sessions 300
-concurrency 100 -commands 4 -output-bytes 65536 -deltas 50`, Linux, one
run each):

| GOMEMLIMIT | GC cycles | aided avg CPU | aided peak RSS | StartSession p50 / p99 | sessions/s |
|---|---|---|---|---|---|
| off | 76 | 43% | 141 MiB | 7 / 47 ms | 73 |
| 96 MiB | 94 | 50% | 123 MiB | 19 / 58 ms | 65 |
| 64 MiB | 355 | 78% | 90 MiB | 57 / 254 ms | 54 |

An idle daemon's heap (a few MB) is far under either: the limit only
matters under load.

## 5. Measuring

The baseline procedure is the handover repository's `RESIDENT-BASELINE.md`
(scenarios A–D, `measure/resident-baseline.ps1`). In CI, the Windows desktop
suite's `tray` spec closes the window, waits 45 s, and samples the app's
processes' private working sets ten times; it prints the table (and adds it
to the job summary) and fails over its budget (`trayBudgetMB`, or
`AIDE_TRAY_BUDGET_MB`). A GitHub runner has no GPU, so its numbers are not
alexwork's; the budget guards against growth, the target is checked on the
owner's PC.
