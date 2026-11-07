# Native Linux Installation

The native installer provisions one experimental Axern installation on a dedicated Ubuntu 24.04 host using systemd and gVisor. It consumes a verified native runtime archive and needs no source checkout, language compiler, Docker or Kubernetes on the target host. It supports fresh installation, resuming the same installation configuration, start/stop, status and explicit service-certificate renewal. It does not implement upgrades, data migration, backup/restore or persistent Workspace semantics.

## Host Prerequisites

Use a native amd64 or arm64 host matching the archive, systemd 254 or newer, cgroup v2, and enabled IPv4 forwarding. The supported Ubuntu 24.04 profile provides systemd 255; older systemd versions lack the delegated subgroup contract used for node recovery. Before installation, provision an empty, dedicated ext4 or XFS filesystem with a stable UUID at `/var/lib/axern`, and configure its persistent mount through the host's normal storage management. The installer accepts ext4's `lost+found` but no existing installation data. This mount contains PostgreSQL, node/image state and the runtime's `runsc`/`projections` partitions; the storage reserve applies to their shared capacity. An ordinary directory on the system filesystem does not satisfy the runtime storage contract.

The installer requires an unused sandbox address range, unused service ports and exclusive ownership of `sandbox0`. It never formats disks or changes forwarding configuration. Kernel/runtime capability checks must pass before the node becomes ready; there is no development enforcement bypass.

Install the host packages using the operating system package manager:

```bash
sudo apt-get install postgresql-16 python3 curl ca-certificates busybox-static erofs-utils iproute2 iptables nftables kmod util-linux e2fsprogs xfsprogs
```

Existing PostgreSQL clusters are not adopted or modified. Axern initializes its own cluster on loopback port `25432` with generated credentials. Internal service ports are `24000`, `24001`, `24002`, `24100`, `24010` and `23001`; the authenticated gateway uses `25000`, and its health listener uses `25080`. These service listeners bind to loopback. Egress enforcement separately owns node-local listeners that receive sandbox traffic. Imagemgr runs in OCI-only mode without Nydus/imagefsd or a chunk server.

## Stage And Install

Obtain the archive and standalone `native_runtime.py` tool from the same trusted source revision. Supply the archive SHA-256 from trusted build/release metadata. The staging command snapshots and verifies the download, materializes only the closed payload inventory, and atomically publishes a new directory. It rejects existing destinations and symlinked paths.

```bash
sudo install -d -m 0755 /opt/axern/releases
sudo python3 native_runtime.py stage /absolute/path/to/runtime.tar.gz \
  --sha256 TRUSTED_ARCHIVE_SHA256 \
  --destination /opt/axern/releases/TRUSTED_ARCHIVE_SHA256
```

Create a JSON configuration with all fields below. These values illustrate the configuration format; qualify the budgets on the host, and replace the network range and resolver with reachable, non-conflicting values. The total memory budget caps the complete Axern slice with swap disabled. Supporting services share `service_memory_budget_bytes`; axnoded receives the remaining budget as its explicit delegated cgroup limit. Its `memory_reserve_bytes` covers the 512 MiB runtime-conformance ceiling plus node daemon headroom. The example leaves 2 GiB for admitted sandbox memory after both reservations, within a 4 GiB total installation budget.

```json
{
  "node_id": "node-personal-01",
  "memory_budget_bytes": 4294967296,
  "service_memory_budget_bytes": 1073741824,
  "memory_reserve_bytes": 1073741824,
  "storage_reserve_bytes": 1073741824,
  "sandbox_cidr": "172.30.0.1/24",
  "dns_nameservers": ["192.168.1.1"],
  "max_instances": 8
}
```

```bash
sudo python3 /opt/axern/releases/TRUSTED_ARCHIVE_SHA256/libexec/axern/native_install.py \
  install --config /absolute/path/to/native.json
```

Installation owns `/etc/axern`, `/var/lib/axern`, its runtime paths below `/opt/axern`, the `axern-native-*` service units, `axern.slice`, `axern-services.slice` and dedicated `axern-*` service accounts. Existing unrelated configuration, data, accounts and units cause installation to fail. An interrupted installation can resume with the same payload and configuration; conflicting files fail explicitly rather than being overwritten. A different payload or configuration is rejected because this candidate has no supported upgrade contract.

The native profile fixes each Allocation at 256 guest tasks and 1024 host tasks. Guest tasks count processes and threads together, including sandboxd, root, changed UIDs and subsequent Exec calls. Host tasks include the runtime's Sentry/gofer processes and threads; they are a separate budget. The guest limit must be installed and read back before sandboxd starts any user workload. The guest cgroup is read-only and cannot create descendants. Recovery checks the limits recorded for the Allocation and refuses to serve live allocations whose enforcement cannot be verified.

Systemd gives axnoded `max_instances * 1024 + 512` host tasks, reserves another 1024 for supporting services, and caps the enclosing Axern slice at their sum. With the example configuration these ceilings are 8704, 1024 and 9728 respectively. They are ceilings rather than capacity reservations: memory and host-wide limits may constrain actual concurrency. Qualify these budgets with representative workloads before relying on the experimental profile for sustained serving.

The installer generates private PostgreSQL, encryption and enrollment secrets; initializes deployment PKI using the existing workload authority implementation; and prepares the full locked gVisor runtime and capability fixtures. The control service receives the signer, while gateway and relay receive only their own workload identities. Public services run under separate unprivileged accounts. Node execution, image mounting, egress enforcement and the node tunnel connector retain their required host privileges. Secret values are not placed in systemd command lines.

## Operate

Use the helper in the installed release directory:

```bash
sudo python3 /opt/axern/releases/TRUSTED_ARCHIVE_SHA256/libexec/axern/native_install.py start
sudo python3 /opt/axern/releases/TRUSTED_ARCHIVE_SHA256/libexec/axern/native_install.py status
sudo journalctl -u axern-native-axnoded.service -n 100 --no-pager
sudo python3 /opt/axern/releases/TRUSTED_ARCHIVE_SHA256/libexec/axern/native_install.py stop
```

Start enables `axern-native.target` for subsequent boots. Database creation, migrations and initial administrator/node admission complete before the control plane starts. Dependency probes use real APIs, not socket-file existence. Start checks every required service individually before and after waiting for a fresh node summary and network, memory and storage enforcement evidence through the authenticated gateway. The packaged CLI applies the canonical capability freshness and dependency checks, including evidence from the current boot. All required observations and their dependencies must also be newer than the current node daemon's start time; a previous daemon's still-fresh snapshot cannot satisfy this barrier. Start rechecks the systemd invocation before reporting readiness. After a host reboot or node restart, invoke start again to wait for this admission barrier; an active systemd target alone is not proof that node observations have reached the controller. A failed daemon restarts independently; the runtime continues to own allocation and enforcement recovery. Every service checks the installation and data filesystem/directory identity before startup. Missing or replaced data fails explicitly instead of creating an empty replacement installation.

To restart only the node daemon while preserving eligible live instances, use `sudo systemctl restart axern-native-axnoded.service`. When the daemon exits, including a crash or failed startup, its systemd hook ends outstanding node commands and pauses running gVisor containers. User execution remains suspended until the replacement node validates durable Allocation identity, execution authority and enforcement, then resumes the existing instance. The hook uses only the installation's fixed runtime root and fails closed by terminating the node unit if it cannot verify the complete inventory or suspension. An interrupted pause/resume transition also ends affected node workloads before startup; saved runsc status alone is not accepted as proof that execution stopped. A restart that outlasts execution authority does not revive an expired Allocation.

The installer enables `native_recovery = true` together with required cgroup enforcement. This protocol requires the generated native systemd hooks and is disabled by default in other deployments, including existing Compose and explicit development configurations. The node service uses `DelegateSubgroup=internal` to separate its daemon and command processes from the sandbox cgroups, and `ExitType=main` so a failed daemon restarts even while runtime processes remain. The startup readiness helper checks the main process before each API probe and exits promptly if the daemon dies, allowing the stop hook to run without waiting for the full readiness timeout. Its `KillMode=process` is paired with the mandatory pause hook; copying that setting without the hook would leave workloads executing without node supervision. A single-service stop leaves eligible runtimes paused. Use the installed helper's `stop` command to stop the complete installation and terminate all remaining runtime processes; it checks the node cgroup is empty before reporting success.

Full stop retains configuration, database, node identity and runtime storage. It does not disable boot startup or promise that workload processes survive a full stop or reboot. Recovery uses the existing Run and Allocation contract. To prevent startup after reboot, explicitly disable `axern-native.target` through systemd.

The generated `/etc/axern/client.env` points the CLI at the authenticated gateway using the initial administrator credential. It is root-readable. In an administrator shell, source that file and use the packaged CLI to inspect nodes and run a workload. This does not modify an existing user context or change `axern local` behavior. Treat the private PKI and encryption material as installation authority; never share it with sandbox workloads.

The start readiness check uses that initial credential by default. After rotating or revoking it, supply both `AXERN_TLS_CERT` and `AXERN_TLS_KEY` for a valid replacement administrator; a partial override is rejected before services start. The installation still selects its own local gateway, CA and server identity. Authentication or authorization failure is reported immediately instead of being retried as missing capability evidence. For example:

```bash
sudo env AXERN_TLS_CERT=/absolute/path/to/current-admin.crt \
  AXERN_TLS_KEY=/absolute/path/to/current-admin.key \
  python3 /opt/axern/releases/TRUSTED_ARCHIVE_SHA256/libexec/axern/native_install.py start
```

```bash
source /etc/axern/client.env
/opt/axern/releases/TRUSTED_ARCHIVE_SHA256/bin/axern admin node list
/opt/axern/releases/TRUSTED_ARCHIVE_SHA256/bin/axern run \
  --request-memory 256Mi --limit-memory 256Mi \
  IMAGE_REFERENCE -- /bin/sh -c 'printf "native sandbox ready\n"'
```

Renew service certificates explicitly using `native_install.py renew-services`. Renewal preserves the CA and administrator identity and restarts the control, gateway and relay services; active client connections may reconnect. An expired initial administrator does not prevent service renewal, and renewal does not restore revoked administrator authorization. Use a valid replacement administrator for readiness checks and administrative API access. Lost authority must be restored from a consistent backup, not regenerated underneath existing state. Initial provisioning and migration markers are installation state, not a backup or an upgrade mechanism.

Qualify a complete archive on a disposable fresh installation with the opt-in [memory, storage and network acceptance matrix](QUALIFICATION.md). Serving readiness alone does not run or replace that matrix.
