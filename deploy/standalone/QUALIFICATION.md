# Native Archive Qualification

The packaged `qualify_native.py` runner performs bounded, destructive memory/storage and network-policy acceptance on a disposable native installation. It is opt-in deployment qualification, not a readiness probe, a periodic capability provider, or permission to run OOM workloads on a shared host. A successful report qualifies the recorded archive, gVisor release, kernel and filesystem only; it does not establish arm64 coverage, production reserve sizing, sustained concurrency, upgrades or backups.

## Prepare A Fresh Candidate

Build the complete archive from a clean committed source tree using the [payload build](README.md#build). Preserve its trusted SHA-256 receipt and source revision. Create a fresh Ubuntu 24.04 VM with a new system overlay, independent writable data disk and unique cloud-init identity; an installed VM with replacement binaries is a different regression scenario. Use at least 4 vCPUs and 6 GiB RAM for the installation's example 4 GiB memory budget. The data filesystem must satisfy the [installation prerequisites](INSTALL.md#host-prerequisites).

Before installing Axern, prepare the isolated DNS/TCP fixture described in [Network Qualification](NETWORK-QUALIFICATION.md). Configure its primary address, normally `198.19.0.2`, as the native installation's resolver. Keep the VM's ordinary host resolver available for fetching the explicitly selected OCI image. The fixture provides both allowed and denied targets; an unreachable external website is never evidence that policy worked.

Stage, install and start the final archive using its own helpers. Do not replace individual binaries, regenerate configuration from another checkout or reuse a database from an older candidate. The target needs the documented host packages and the packaged Python runner and CLI; it does not need a source checkout, compiler, Docker or an installed SDK.

## Run The Acceptance Matrix

Use an immutable, trusted OCI image that contains `python3`. The runner creates one Environment and distinct Runs through the public authenticated CLI. It does not change the current CLI context. By default it uses the native administrator identity; the same paired `AXERN_TLS_CERT` and `AXERN_TLS_KEY` override supported by the installer selects a replacement administrator.

```bash
sudo python3 /opt/axern/releases/ARCHIVE_SHA256/libexec/axern/qualify_native.py \
  --expected-source SOURCE_COMMIT \
  --expected-archive-sha256 ARCHIVE_SHA256 \
  --image docker.io/library/python@sha256:TRUSTED_IMAGE_DIGEST \
  --fixture-ip 198.19.0.2 \
  --output-dir /root/native-qualification-UNIQUE
```

The output directory must be new. The runner verifies the installation/data identity, requested source/archive identity and every installed payload file against the staged manifest before creating workloads. It records the actual runsc version and waits for the current daemon's authenticated capability barrier before each case. `--cases memory`, `--cases storage` and `--cases network` select a bounded subset for diagnosis; a subset result is not a full-matrix result.

| Case | Required evidence |
| --- | --- |
| Memory within 128 MiB | A guest touches and retains 16 MiB, reports completion, and causes no new Allocation OOM kill. |
| Memory overflow | A guest waits at a private HTTP barrier until the host has captured its exact Allocation parent and workload cgroup identities and controls, then attempts at most 512 MiB of touched anonymous memory. That Allocation parent's hierarchical `memory.events` must show new `oom` and `oom_kill` events, and the public Run must report `memory-limit-exceeded`. A different failure or a reused old counter fails qualification. |
| Storage within 64 MiB | A guest creates a unique file directly under `/`, verifies the writable root overlay and 64 MiB capacity before writing, then writes and fsyncs 8 MiB successfully. |
| Storage overflow | The same root-overlay path rule applies. A guest attempts at most 96 MiB and catches numeric `ENOSPC` or `EDQUOT`; successful writes must remain within the 64 MiB requested limit. Guest mount/device/statvfs evidence and the exact Sentry's file-backed `--overlay2` limit must agree. Arbitrary nonzero exit status, permission failure or OOM does not count as quota enforcement. |
| Network positive/deny controls | The controlled fixture proves allowed reachability before and after the policy cases, DNS deny returns REFUSED over UDP and TCP, and strict CIDR/port policy blocks reachable excluded targets. Exact Allocation policy and kernel-rule evidence is captured. |

Workload code sleeps after its result so the host can inspect the active Allocation before cleanup. The memory barrier listens temporarily on the native bridge address and uses a random per-case URL; it exists only for the bounded qualification. The host samples memory counters without writing cgroup controls. Lifecycle operations remain public Environment/Run calls; runsc state, cgroup and policy inspection provide read-only host evidence.

Memory inspection requires the exact `sandbox/<pool>/workload` leaf and its direct Allocation parent under the fixed native node unit. Both must have the requested `memory.max` and `memory.swap.max=0`; `memory.oom.group=1` belongs to the Allocation parent, which bounds the entire sandbox subtree. The OCI leaf may retain `memory.oom.group=0`. The runner records both inodes and control sets and samples the verified parent's hierarchical events so OOM in either the parent or its workload leaf remains attributable to that Allocation. It never substitutes a wider ancestor's controls or counters.

Storage qualification follows the existing [ephemeral-storage accounting scope](../../docs/architecture/resource-model.md#ephemeral-storage-accounting-scope): the file-backed root overlay, including metadata and copy-up. gVisor can mount an empty `/tmp` as a separate tmpfs; tmpfs is outside `ephemeral_storage_bytes` and its memory is covered by the memory boundary. The runner preserves `/tmp` mount information for diagnosis, but never treats filling it as root-overlay quota evidence. It rejects redirected probe paths, a different mount/device, a missing or mismatched overlay size, and mismatched filesystem capacity even when a write returns `ENOSPC`.

## Results And Cleanup

`report.json` records the candidate identities, case Run/Allocation IDs, controls, results and cleanup errors. Individual JSON files preserve intermediate evidence. A failure still writes the report and returns a nonzero status; cleanup failure also prevents success. Memory sampling that cannot establish the exact kernel evidence fails rather than substituting an exit-code guess.

Cleanup cancels only Runs created by this invocation and waits for their terminal state, owned cgroup and exact runsc state/temporary-state/lock/control-socket removal. It deletes the invocation's Environment only after those checks succeed. Public Run history remains. The runner never repairs private state, deletes an unknown runtime, rewrites network interfaces or clears an enforcement failure to obtain a pass.

Preserve failed reports. After a product or fixture fix, create a new report directory and record the changed source/archive identity. Do not describe a manually repaired or partially replaced installation as a successful fresh archive installation. After qualification, the host owner stops the fixture and disposable VM using their recorded ownership; the runner does not manage VM disks or unrelated services.
