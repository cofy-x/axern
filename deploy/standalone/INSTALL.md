# Native Linux Installation

The native installer provisions one experimental Axern installation on a dedicated Ubuntu 24.04 host using systemd and gVisor. It consumes a verified native runtime archive and needs no source checkout, language compiler, Docker or Kubernetes on the target host. It supports fresh installation, resuming the same installation configuration, start/stop, status and explicit service-certificate renewal. It does not implement upgrades, data migration, backup/restore or persistent Workspace semantics.

## Host Prerequisites

Use a native amd64 or arm64 host matching the archive, systemd, cgroup v2, and enabled IPv4 forwarding. Before installation, provision an empty, dedicated ext4 or XFS filesystem with a stable UUID at `/var/lib/axern`, and configure its persistent mount through the host's normal storage management. The installer accepts ext4's `lost+found` but no existing installation data. This mount contains PostgreSQL, node/image state and the runtime's `runsc`/`projections` partitions; the storage reserve applies to their shared capacity. An ordinary directory on the system filesystem does not satisfy the runtime storage contract.

The installer requires an unused sandbox address range, unused service ports and exclusive ownership of `sandbox0`. It never formats disks or changes forwarding configuration. Kernel/runtime capability checks must pass before the node becomes ready; there is no development enforcement bypass.

Install the host packages using the operating system package manager:

```bash
sudo apt-get install postgresql-16 python3 curl ca-certificates busybox-static erofs-utils fuse3 iproute2 iptables nftables kmod util-linux e2fsprogs xfsprogs
```

Existing PostgreSQL clusters are not adopted or modified. Axern initializes its own cluster on loopback port `25432` with generated credentials. Internal service ports are `24000`, `24001`, `24002`, `24100`, `24010`, `23001` and `9876`; the authenticated gateway uses `25000`, and its health listener uses `25080`. These service listeners bind to loopback. Egress enforcement separately owns node-local listeners that receive sandbox traffic.

## Stage And Install

Obtain the archive and standalone `native_runtime.py` tool from the same trusted source revision. Supply the archive SHA-256 from trusted build/release metadata. The staging command snapshots and verifies the download, materializes only the closed payload inventory, and atomically publishes a new directory. It rejects existing destinations and symlinked paths.

```bash
sudo install -d -m 0755 /opt/axern/releases
sudo python3 native_runtime.py stage /absolute/path/to/runtime.tar.gz \
  --sha256 TRUSTED_ARCHIVE_SHA256 \
  --destination /opt/axern/releases/TRUSTED_ARCHIVE_SHA256
```

Create a JSON configuration with all fields below. These values illustrate the configuration format; choose the memory reserve using host qualification, and replace the network range and resolver with reachable, non-conflicting values for the host. The reserve must cover the 512 MiB runtime-conformance ceiling plus measured daemon headroom. The memory budget caps the complete Axern systemd slice, including PostgreSQL and helpers.

```json
{
  "node_id": "node-personal-01",
  "memory_budget_bytes": 4294967296,
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

Installation owns `/etc/axern`, `/var/lib/axern`, its runtime paths below `/opt/axern`, the `axern-native-*` service units, `axern.slice` and dedicated `axern-*` service accounts. Existing unrelated configuration, data, accounts and units cause installation to fail. An interrupted installation can resume with the same payload and configuration; conflicting files fail explicitly rather than being overwritten. A different payload or configuration is rejected because this candidate has no supported upgrade contract.

The installer generates private PostgreSQL, encryption and enrollment secrets; initializes deployment PKI using the existing workload authority implementation; and prepares the full locked gVisor runtime and capability fixtures. The control service receives the signer, while gateway and relay receive only their own workload identities. Public services run under separate unprivileged accounts. Node execution, image mounting, egress enforcement and the node tunnel connector retain their required host privileges. Secret values are not placed in systemd command lines.

## Operate

Use the helper in the installed release directory:

```bash
sudo python3 /opt/axern/releases/TRUSTED_ARCHIVE_SHA256/libexec/axern/native_install.py start
sudo python3 /opt/axern/releases/TRUSTED_ARCHIVE_SHA256/libexec/axern/native_install.py status
sudo journalctl -u axern-native-axnoded.service -n 100 --no-pager
sudo python3 /opt/axern/releases/TRUSTED_ARCHIVE_SHA256/libexec/axern/native_install.py stop
```

Start enables `axern-native.target` for subsequent boots. Database creation, migrations and initial administrator/node admission complete before the control plane starts. Dependency probes use real APIs, not socket-file existence. A failed daemon restarts independently; the runtime continues to own allocation and enforcement recovery. Every service checks the installation and data filesystem/directory identity before startup. Missing or replaced data fails explicitly instead of creating an empty replacement installation.

Stop retains configuration, database, node identity and runtime storage. It does not disable boot startup or promise that workload processes survive a stop or reboot. Recovery uses the existing Run and Allocation contract. To prevent startup after reboot, explicitly disable `axern-native.target` through systemd.

The generated `/etc/axern/client.env` points the CLI at the authenticated gateway using the initial administrator credential. It is root-readable. In an administrator shell, source that file and use the packaged CLI to inspect nodes and run a workload. This does not modify an existing user context or change `axern local` behavior. Treat the private PKI and encryption material as installation authority; never share it with sandbox workloads.

```bash
source /etc/axern/client.env
/opt/axern/releases/TRUSTED_ARCHIVE_SHA256/bin/axern admin node list
/opt/axern/releases/TRUSTED_ARCHIVE_SHA256/bin/axern run IMAGE_REFERENCE -- /bin/sh -c 'printf "native sandbox ready\n"'
```

Renew service certificates explicitly using `native_install.py renew-services`. Renewal preserves the CA and administrator identity and restarts the control, gateway and relay services; active client connections may reconnect. Lost authority must be restored from a consistent backup, not regenerated underneath existing state. Initial provisioning and migration markers are installation state, not a backup or an upgrade mechanism.
