# Native Startup and Idle Measurement

Use this opt-in source-tree harness on an exclusively operated, disposable native host. It observes the installed candidate through its own canonical installer, public CLI and cgroup v2 counters. It does not install a candidate, update configuration, drop page caches, remove images, qualify enforcement or restart a host with live user workloads. See [Native Installation](INSTALL.md) for lifecycle ownership and [Fresh-Archive Qualification](QUALIFICATION.md) for memory, storage and network acceptance.

## Run

Copy `measure_native.py` from the source checkout to the measurement host. The selected installed release must contain `native_install.py`, `qualify_native.py` and the CLI. Python 3.12 and root are required. Run the observer outside `axern.slice`, such as from an SSH session. Do not run another qualification, measurement, lifecycle command or workload client concurrently; the measurement lock excludes other instances of this harness, not public API users or administrators.

```bash
sudo python3 /absolute/path/to/measure_native.py \
  --release /opt/axern/releases/EXPECTED_SHA256_FROM_TRUSTED_BUILD_RECEIPT \
  --expected-archive EXPECTED_SHA256_FROM_TRUSTED_BUILD_RECEIPT \
  --output-dir /absolute/path/to/new-full-start-observation \
  --image docker.io/library/python@sha256:EXACT_IMAGE_DIGEST \
  --scenario full-start
```

Choose `--scenario node-restart` to time only a daemon restart followed by the canonical fresh-serving barrier. It requires an already running installation and no active user Runs. Supporting services retain their current invocations. This is an idle restart measurement, not a test of live Allocation recovery.

`full-start` accepts an installed, fully stopped host, including its first startup. It checks that all native services have no main process, the local runsc inventory is empty and the native node cgroup has no surviving process. An absent, not-yet-created runtime/cgroup directory is accepted only in this stopped case. If services are already running, it first checks every public Run inventory page and the empty local sandbox domain, then uses the installed helper's complete stop before starting the clock. A partially running installation whose public inventory cannot be verified fails before stopping anything. After startup the public inventory must again contain only terminal Runs.

Every run verifies the installed payload hashes and expected archive identity before timing. This warms executable and archive pages. The report includes the installed source revision, gVisor pin, archive digest and SHA-256 of the actual measurement script. The harness is source-run and is not added to the native payload by this change.

## Timing Definitions

All elapsed durations use the host monotonic clock. Unit timestamps are systemd monotonic microseconds converted to the same clock. UTC timestamps are not subtracted to calculate performance.

| Report field | Meaning |
| --- | --- |
| `startup.systemctl_return_seconds` | Return of the explicit `systemctl restart` command in `node-restart`. It includes stop/start jobs and the unit's `ExecStartPost` local readiness checks. It is `null` for `full-start`: the unchanged canonical helper owns its internal `systemctl` command, whose exact return is not separately observable. |
| `startup.systemd_active_seconds` | Latest `ActiveEnterTimestampMonotonic` among the services restarted by this scenario, relative to the operation start. This includes applicable `ExecStartPost` checks; active alone does not prove authenticated serving. |
| `startup.authenticated_ready_seconds` | Return of the installed canonical `start` helper. Its barrier requires all native services active and authenticated capability evidence for the current host boot and current node invocation. For node restart, this includes the explicit restart followed by the helper check. |
| `first_workload.environment_create_seconds` | Separate public Environment creation after readiness; not part of startup or submit-to-marker time. |
| `first_workload.accepted_seconds` | Public Run submission return, relative to submission start. |
| `first_workload.running_observed_seconds` | First public `running` observation. |
| `first_workload.marker_observed_seconds` | First complete, unique nonce marker obtained through public Run logs, relative to submission start. This includes scheduling, any image retrieval, process startup, polling and log transport. It is an observed upper bound, not an intrinsic guest boot timestamp. |
| `first_workload.ready_to_marker_seconds` / `startup_to_marker_seconds` | End-to-end observed intervals including intervening inventory checks and Environment creation. |

The first workload requests 256 MiB memory and 128 MiB ephemeral storage, prints one unique marker from Python and sleeps for at most 120 seconds. The observer cancels its own Run after collecting that marker, verifies terminal/runtime/socket/cgroup cleanup through the installed qualification helper, then deletes its own Environment. This verifies the ability to execute a real command, not workload completion or resource-limit enforcement. No caller-owned Run or Environment is cancelled or deleted.

## Cold and Warm Conditions

`full-start` is a **cold process start**: all platform processes start anew. Persistent PostgreSQL data and local images remain. On an installation never started before, it also includes first bootstrap/migration work. The harness does not pre-start a stopped installation or pre-pull its workload image. Record separately whether this is the first image use on a fresh host or a subsequent use of an already populated image cache.

`node-restart` is a **warm platform, cold node process** observation: supporting services, persistent data and caches remain. Its untimed preflight establishes fresh authenticated readiness before restarting the idle node. Neither scenario claims a cold page cache; payload verification itself warms pages. A fresh VM or reboot should be recorded as an external preparation condition, not inferred from these scenario names. Do not compare a first bootstrap/image download directly with a cached restart without retaining those labels and all individual results. Use new output directories for repetitions and retain failed observations; the harness never retries an entire measurement to obtain a faster result.

## Idle Accounting

After owned workload cleanup and an empty inventory check, the observer waits five seconds, then samples for at least 60 seconds at approximately one-second intervals. It does not disable routine internal maintenance or capability probes; their resource use is part of idle platform cost. A user sandbox appearing during the window, a service invocation change, a boot/cgroup identity change or a decreasing CPU counter invalidates the observation.

| Metric | Accounting boundary |
| --- | --- |
| `groups.total.memory_current_bytes` | The primary footprint: hierarchical `memory.current` of `axern.slice`, including charged anonymous memory, file cache and kernel memory. |
| `node` and `supporting_services` | Separate child cgroup breakdowns. Do not add either to `total`; that would count memory and CPU twice. |
| `memory_stat_bytes` | Raw named `anon`, `file`, `shmem`, `kernel`, `kernel_stack`, `pagetables` and slab counters when available. These overlap: shmem is part of file, slab components are part of slab, and slab is part of kernel. Their sum is not a second memory total. |
| `process_memory` | RSS/PSS snapshots at the beginning and end, for a deduplicated set of processes in each subtree. RSS counts shared pages in multiple processes; PSS apportions them. Neither is added to cgroup memory. Process start ticks detect PID reuse; disappearing/unreadable processes invalidate a snapshot rather than silently becoming zero. These snapshots do not establish a 60-second RSS/PSS peak. |
| `process_count` / `thread_count` | Unique recursive `cgroup.procs` IDs and hierarchical `pids.current`, respectively. Parent/child counts must not be summed. |
| `cpu_delta_usec` | Last minus first `cpu.stat` usage/user/system counters over the actual monotonic sample interval. `cpu_core_equivalents` divides usage by wall time; 1 means one continuously busy core. One-core percent is 100 times this value; host percent additionally divides by reported logical CPU count. |

`idle_samples` retains the individual observations; `idle` summarizes memory minimum/median/maximum, maximum process/thread counts and CPU deltas. The observer itself must run outside the Axern cgroups. Kernel reads and the two RSS/PSS scans still impose some host overhead; this is a repeatable diagnostic measurement, not a zero-overhead profiler or a performance acceptance threshold.

## Bounds and Failure Evidence

The harness uses one invocation, a new private output directory and private files. Lifecycle subprocesses have 190-second deadlines; ordinary CLI operations have a 25-second CLI timeout and a 30-second subprocess deadline. Run inventory pagination is bounded to 100 pages and 60 seconds between requests. First-output polling has a 180-second deadline plus its in-flight API request. Cleanup uses the installed helper's bounded terminal and exact-path checks. A lifecycle timeout is a failed observation; systemd may still finish its job, so inspect the host before another invocation instead of assuming services were rolled back.

The console emits only success/report location or a fixed failure reason. Command output, complete process command lines, environment variables, credentials and arbitrary journal content are not copied into the report. On failure after runner creation, `report.json` retains completed stages and cleanup failures with `passed: false`; failure before payload verification/runner creation may only return a console error. `SIGINT`/`SIGTERM` attempt owned cleanup. A killed observer or unavailable API may leave its recorded Run for explicit operator cleanup; it does not broaden cleanup to other instances. Successful measurement leaves the platform running and retains caches and terminal Run history.

Host-safe checks do not start services or workloads:

```bash
python3 -m unittest discover -s deploy/standalone -p 'measure_native_test.py'
```
