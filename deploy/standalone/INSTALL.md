# Native Linux Installation

The native installer provisions one experimental Axern installation on a dedicated Ubuntu 24.04 host using systemd and gVisor. It consumes a verified native runtime archive and needs no source checkout, language compiler, Docker or Kubernetes on the target host. It supports fresh installation, resuming the same installation configuration, start/stop, status and explicit service-certificate renewal. The experimental [cold archive transition](#qualify-an-experimental-archive-transition) includes the published amd64 upgrade listed below. The [replacement Node procedure](#qualify-a-replacement-node) covers a cold block-image backup made with the same archive. Experimental Workspaces require the explicit XFS quota settings below.

## Published Native Previews

Use the CLI and services from the same archive. Download the archive, checksums, evidence and third-party notices for your architecture from the release page. Its verification commands check the archive, checksum file, receipt and both software inventories against the specific GitHub signing workflow and commit. Check the source revision in the signed receipt before staging. The [0.13.0 baseline](https://github.com/cofy-x/axern/releases/tag/native-preview-0.13.0-1) and [0.14.0 preview](https://github.com/cofy-x/axern/releases/tag/native-preview-0.14.0-1) are native prereleases. The stable CLI, SDK, Helm and service-image release has its own version and artifacts.

| Ubuntu 24.04 amd64, dedicated XFS with project quotas | Source commit | Archive SHA-256 |
| --- | --- | --- |
| [0.13.0 baseline](https://github.com/cofy-x/axern/releases/tag/native-preview-0.13.0-1) | `4f8208c5e7b58e04feed5806f81336fbe425b55f` | `dbff48cc4366af36093e5a525fd280c411b507b13491f97178a6ed69e8748ac1` |
| [0.14.0 successor](https://github.com/cofy-x/axern/releases/tag/native-preview-0.14.0-1) | `90bc377b44f2c7609fb59a7951b2aaa80d49d6d9` | `180b2eef09ec003c3c3b24dd7a65e5ea55cb006b3bb987b47fb569d8d56f702c` |

These two downloads form the qualified amd64 upgrade pair. Follow the [cold transition](#qualify-an-experimental-archive-transition) with a verified backup of the stopped system and data disks. The original installation must stay fenced. The [upgrade record](https://github.com/cofy-x/axern/issues/200) tracks recovered files, the Secret projection, quota and single-writer checks. The [host qualification record](https://github.com/cofy-x/axern/issues/197) distinguishes native execution, retained-workspace coverage and execution-only filesystems.

Both previews publish amd64 archives. The [ARM64 hardware trial](https://github.com/cofy-x/axern/actions/runs/46107363320) verified two matching ARM64 builds and completed host preparation. The runner exited before the eight runtime cases produced results. ARM64 execution and stateful upgrades on ARM64 remain unqualified.

Use the staging scripts from the source commit in the verified receipt. Run the following in a new directory containing the extracted `evidence/receipt.json` from the release verification procedure.

```bash
set -euo pipefail
source_revision=$(jq -er '.source_revision | select(test("^[0-9a-f]{40}$"))' evidence/receipt.json)
mkdir -m 0700 staging
for path in scripts/release/native_runtime.py runtime/axnoded/gvisor_payload.py; do
  curl --fail --location --proto '=https' --proto-redir '=https' \
    "https://raw.githubusercontent.com/cofy-x/axern/$source_revision/$path" -o "staging/${path##*/}" || exit 1
done
```

## Host Prerequisites

Use a native amd64 or arm64 host matching the archive, systemd 254 or newer, cgroup v2, and enabled IPv4 forwarding. The supported Ubuntu 24.04 profile provides systemd 255; older systemd versions lack the delegated subgroup contract used for node recovery. Before installation, provision an empty, dedicated ext4 or XFS filesystem with a stable UUID at `/var/lib/axern`, and configure its persistent mount through the host's normal storage management. The installer accepts ext4's `lost+found` but no existing installation data. This mount contains PostgreSQL, node/image state and the runtime's `runsc`/`projections` partitions; the storage reserve applies to their shared capacity. An ordinary directory on the system filesystem does not satisfy the runtime storage contract.

The installer requires an unused sandbox address range, unused service ports and exclusive ownership of `sandbox0`. It never formats disks or changes forwarding configuration. Kernel/runtime capability checks must pass before the node becomes ready; there is no development enforcement bypass.

Apply the distribution's security updates before installation, then install the host packages. Disable download retries during package-manager bootstrap so an older client cannot use an unsafe redirected retry path:

```bash
sudo apt-get -o Acquire::Retries=0 update
sudo apt-get -o Acquire::Retries=0 upgrade --yes
sudo apt-get -o Acquire::Retries=0 install --no-install-recommends postgresql-16 python3 curl ca-certificates busybox-static erofs-utils iproute2 iptables nftables kmod util-linux e2fsprogs xfsprogs
```

Existing PostgreSQL clusters are not adopted or modified. Axern initializes its own cluster on loopback port `25432` with generated credentials. Internal service ports are `24000`, `24001`, `24002`, `24100`, `24010` and `23001`; the authenticated gateway uses `25000`, and its health listener uses `25080`. These service listeners bind to loopback. Egress enforcement separately owns node-local listeners that receive sandbox traffic. Imagemgr manages OCI image extraction and read-only mounts.

## Stage And Install

Obtain the archive and the same-revision staging tools, `native_runtime.py` and `gvisor_payload.py`, from trusted source. Keep both tools together in a private directory; the stager imports its sibling verifier even under isolated Python. The archive also includes both tools under `libexec/axern`. Supply the archive SHA-256 from trusted build/release metadata. The staging command snapshots and verifies the download, materializes only the closed payload inventory, and atomically publishes a new directory. It rejects existing destinations and symlinked paths.

```bash
sudo install -d -m 0755 /opt/axern/releases
sudo python3 -I /absolute/path/to/staging-tools/native_runtime.py stage /absolute/path/to/runtime.tar.gz \
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

To enable experimental Workspaces, use XFS with project accounting and enforcement enabled (`prjquota` in the persistent mount configuration), and add both optional settings to this JSON:

```json
{
  "workspace_capacity_bytes": 2147483648,
  "workspace_inode_capacity": 200000
}
```

These fields extend the complete configuration above; they are not a standalone configuration. The byte budget is positive and 512-byte aligned, and the inode budget is positive. Enabling Workspaces requires at least 1 GiB of operator storage reserve. The installer subtracts the full Workspace budget from ephemeral-rootfs capacity in addition to that reserve. Quotas cap allocation; they do not reserve physical blocks against image caches, PostgreSQL or other host writers.

The installer creates the private Workspace root only during installation. Start and recovery reject absent or replaced retained storage. The node verifies effective XFS limits before advertising support; mount options alone cannot establish enforcement. Keep the dedicated mount available across host startup. Leave both settings absent for ephemeral-only serving on ext4 or XFS.

With Workspace storage enabled, the optional positive integer `workspace_project_budget` sets the lifetime limit on assigned projects. The node defaults to 4,096. The [storage owner guide](../../runtime/axnoded/docs/rootfs-storage.md) explains how to inspect usage and size an increase.

```bash
sudo python3 /opt/axern/releases/TRUSTED_ARCHIVE_SHA256/libexec/axern/native_install.py \
  install --config /absolute/path/to/native.json
```

Installation owns `/etc/axern`, `/var/lib/axern`, its runtime paths below `/opt/axern`, the `axern-native-*` service units, `axern.slice`, `axern-services.slice` and dedicated `axern-*` service accounts. Existing unrelated configuration, data, accounts and units cause installation to fail. An interrupted installation can resume with the same payload and configuration; conflicting files fail explicitly rather than being overwritten. After completion, missing PostgreSQL identity or managed data directories are rejected before initialization or repair. A missing completion marker cannot turn a bootstrapped database into a first installation. A different payload or configuration is rejected because this candidate has no supported upgrade contract.

The image cache has an explicit extraction format under `/var/lib/axern/imagemgr/oci/metadata.db`. An incompatible or unversioned nonempty cache is rejected before recovery, rather than recursively changing permissions or silently reusing it. Preserve that cache and `/var/lib/axern/imagemgr/resource_leases.db` for operator resolution using the matching runtime; see [image-cache ownership](../../runtime/imagemgr/docs/architecture.md#guest-metadata-and-cache-format). Cache replacement is not an ordinary start/stop operation, an installation upgrade or permission to clear the data filesystem.

The native profile fixes each Allocation at 256 guest tasks and 1024 host tasks. Guest tasks count processes and threads together, including sandboxd, root, changed UIDs and subsequent Exec calls. Host tasks include the runtime's Sentry/gofer processes and threads; they are a separate budget. The guest limit must be installed and read back before sandboxd starts any user workload. The guest cgroup is read-only and cannot create descendants. Recovery checks the limits recorded for the Allocation and refuses to serve live allocations whose enforcement cannot be verified.

Systemd gives axnoded `max_instances * 1024 + 512` host tasks, reserves another 1024 for supporting services, and caps the enclosing Axern slice at their sum. With the example configuration these ceilings are 8704, 1024 and 9728 respectively. They are ceilings rather than capacity reservations: memory and host-wide limits may constrain actual concurrency. Qualify these budgets with representative workloads before relying on the experimental profile for sustained serving.

The installer generates private PostgreSQL, encryption and enrollment secrets; initializes deployment PKI using the existing workload authority implementation; and prepares the selected, locked direct-runsc runtime and capability fixtures. The runtime contains `runsc` plus its four required helpers, verified against the architecture-specific selected-member lock with their relative layout preserved. The upstream input archive, containerd shim, checkpoint-gofer and standalone metric-server are not installed. See the [payload contract](README.md#payload-contract) for the exact inventory and integrity boundary. The control service receives the signer, while gateway and relay receive only their own workload identities. Public services run under separate unprivileged accounts. Node execution, image mounting, egress enforcement and the node tunnel connector retain their required host privileges. Secret values are not placed in systemd command lines.

History retention runs as a background task in `controld`, sharing its database pool and supervised shutdown. Its [retention policy](../../control/controld/README.md#run) belongs to the control service; no separate retention service, account or database credential is required.

## Operate

Use the CLI in the installed release directory. It verifies installation ownership and payload identity, then delegates lifecycle operations to the installed helper:

```bash
sudo /opt/axern/releases/TRUSTED_ARCHIVE_SHA256/bin/axern server start
sudo /opt/axern/releases/TRUSTED_ARCHIVE_SHA256/bin/axern server status
sudo /opt/axern/releases/TRUSTED_ARCHIVE_SHA256/bin/axern server logs axnoded --tail 100
sudo /opt/axern/releases/TRUSTED_ARCHIVE_SHA256/bin/axern server stop
```

Start enables `axern-native.target` for subsequent boots. Database creation, migrations and initial administrator/node admission complete before the control plane starts. Dependency probes use real APIs, not socket-file existence. Start checks every required service individually before and after waiting for a fresh node summary and network, memory and storage enforcement evidence through the authenticated gateway. The packaged CLI applies the canonical capability freshness and dependency checks, including evidence from the current boot. All required observations and their dependencies must also be newer than the current node daemon's start time; a previous daemon's still-fresh snapshot cannot satisfy this barrier. Start rechecks the systemd invocation before reporting readiness. After a host reboot or node restart, invoke start again to wait for this admission barrier; an active systemd target alone is not proof that node observations have reached the controller. A failed daemon restarts independently; the runtime continues to own allocation and enforcement recovery. Every service checks the installation and data filesystem/directory identity before startup. Missing or replaced data fails explicitly instead of creating an empty replacement installation.

To restart only the node daemon while preserving eligible live instances, use `sudo systemctl restart axern-native-axnoded.service`. When the daemon exits, including a crash or failed startup, its systemd hook ends outstanding node commands and pauses running gVisor containers. User execution remains suspended until the replacement node validates durable Allocation identity, execution authority and enforcement, then resumes the existing instance. The hook uses only the installation's fixed runtime root and fails closed by terminating the node unit if it cannot verify the complete inventory or suspension. An interrupted pause/resume transition also ends affected node workloads before startup; saved runsc status alone is not accepted as proof that execution stopped. A restart that outlasts execution authority does not revive an expired Allocation.

The installer enables `native_recovery = true` together with required cgroup enforcement. This protocol requires the generated native systemd hooks and is disabled by default in other deployments, including existing Compose and explicit development configurations. The node service uses `DelegateSubgroup=internal` to separate its daemon and command processes from the sandbox cgroups, and `ExitType=main` so a failed daemon restarts even while runtime processes remain. The startup readiness helper checks the main process before each API probe and exits promptly if the daemon dies, allowing the stop hook to run without waiting for the full readiness timeout. Its `KillMode=process` is paired with the mandatory pause hook; copying that setting without the hook would leave workloads executing without node supervision. A single-service stop leaves eligible runtimes paused. Use the installed helper's `stop` command to stop the complete installation and terminate all remaining runtime processes; it checks the node cgroup is empty before reporting success.

Full stop waits for the target and every owned service's stop job before the final node-cgroup termination check. It retains configuration, database, node identity and runtime storage. Stop does not clear prior unit failures; inspect service status and the journal to diagnose them. It does not disable boot startup or promise that workload processes survive a full stop or reboot. Recovery uses the existing Run and Allocation contract. To prevent startup after reboot, explicitly disable `axern-native.target` through systemd.

The generated `/etc/axern/client.env` points the CLI at the authenticated gateway using the initial administrator credential. It is root-readable. In an administrator shell, source that file and use the packaged CLI to inspect nodes and run a workload. This does not modify an existing user context or change `axern dev` behavior. Treat the private PKI and encryption material as installation authority; never share it with sandbox workloads.

Native bootstrap admits the Node and creates the administrator, but does not create an application Namespace. Create `default` once before the first Environment, Run or Workspace operation; if it already exists, verify it with `namespace get default`. The examples explicitly select it with `--namespace default`. Remote clients must use a context and credential for this same gateway and an authorized existing Namespace.

The start readiness check uses that initial credential by default. After rotating or revoking it, supply both `AXERN_TLS_CERT` and `AXERN_TLS_KEY` for a valid replacement administrator; a partial override is rejected before services start. The installation still selects its own local gateway, CA and server identity. Authentication or authorization failure is reported immediately instead of being retried as missing capability evidence. For example:

```bash
sudo env AXERN_TLS_CERT=/absolute/path/to/current-admin.crt \
  AXERN_TLS_KEY=/absolute/path/to/current-admin.key \
  python3 /opt/axern/releases/TRUSTED_ARCHIVE_SHA256/libexec/axern/native_install.py start
```

```bash
source /etc/axern/client.env
/opt/axern/releases/TRUSTED_ARCHIVE_SHA256/bin/axern admin node list
/opt/axern/releases/TRUSTED_ARCHIVE_SHA256/bin/axern namespace create default
/opt/axern/releases/TRUSTED_ARCHIVE_SHA256/bin/axern run \
  --namespace default --request-memory 256Mi --limit-memory 256Mi \
  IMAGE_REFERENCE -- /bin/sh -c 'printf "native sandbox ready\n"'
```

Renew service certificates explicitly using the installed CLI’s `server renew-services` command. Renewal preserves the CA and administrator identity and restarts the control, gateway and relay services; active client connections may reconnect. An expired initial administrator does not prevent service renewal, and renewal does not restore revoked administrator authorization. Use a valid replacement administrator for readiness checks and administrative API access. Lost authority must be restored from a consistent backup, not regenerated underneath existing state. Initial provisioning and migration markers are installation state, not a backup or an upgrade mechanism.

## Protect Cold Backup Keys And Credentials

Keep the complete stopped system and data images together. The system image holds the PostgreSQL credential, Axern Secret encryption key and private PKI. The data image holds the corresponding PostgreSQL cluster, encrypted Secret records, retained Workspace bytes and node-local recovery facts. Preserve those pairs from the same stopped installation. Keep the backup and its writable restore copies encrypted at rest with access limited to the operators responsible for recovery. Verify both image hashes before use. Store the backup media's unlock credentials separately from the images and leave generated keys, credentials and raw dumps out of build artifacts and shared receipts.

Keep the original immutable OCI digests and sufficient registry access to retrieve them. When verifying recovery, launch new work from the recovered Environment ID. If a digest is unavailable and the image is absent from the cache, retain the failure and restore access to the exact digest before starting that Environment again.

The native recovery procedure preserves the original Secret encryption key. In-place master-key or CA rotation remains unqualified. After recovering the coherent pair, use `server renew-services` to renew service leaf certificates. The administrative replacement-Node operation issues a distinct Node identity and revokes the previous identity before admission. Client certificate issuance and revocation remain administrator operations. Keep the restored gateway isolated until the administrator verifies that its client access policy includes revocations and other authority changes made after the backup.

## Qualify An Experimental Archive Transition

The incoming helper accepts the exact source revision and archive version pairs below. Use the matching CLI and SDK for each archive. The helper checks the two distinct archive digests and source revisions, the archive profile and host architecture, the matching gVisor payload, the Node configuration, and the original data filesystem.

| Source archive label | Source revision | Incoming archive labels | Owned Node configuration |
| --- | --- | --- | --- |
| `0.13.0` | `7de820e813ba2766645af100548753692d951051` | `0.13.0` | Historical native layout. The transition adds the default Workspace project budget. |
| `0.13.0` | `4f8208c5e7b58e04feed5806f81336fbe425b55f` | `0.13.0`, `0.14.0` | Current managed layout with an existing Workspace project-budget field. |

Before changing activation state, the helper verifies stopped systemd services, PostgreSQL and runsc. It opens PostgreSQL on a private Unix socket to verify the source migration history and confirm that the original Node has no unfinished Runs, unreleased Allocations or unresolved Workspace writers. Drain outstanding work on the source before attempting the transition. The [published preview pair](#published-native-previews) identifies the exact source and successor for the qualified amd64 transition. Independently qualify other builds and retain their archive digests before selecting them.

Follow the [cold-restore qualification](QUALIFICATION.md#qualify-a-same-installation-cold-restore) to fence new work, drain writers, disable automatic startup and verify a backup of the system and data disks. Store both complete disk images privately. After verifying their hashes, the operator supplies a private receipt with this shape:

```json
{
  "schema": "axern.native-cold-backup.v1",
  "archive_sha256": "SOURCE_ARCHIVE_SHA256",
  "data_identity": {
    "filesystem_uuid": "SOURCE_DATA_FILESYSTEM_UUID",
    "directory_inode": 128
  },
  "images_sha256": {
    "system": "VERIFIED_SYSTEM_DISK_SHA256",
    "data": "VERIFIED_DATA_DISK_SHA256"
  }
}
```

Read the exact data identity from the private source installation record at `/etc/axern/installation.json`. The helper validates the receipt's binding to that installation. The operator is responsible for storing and verifying both images. Stage the incoming complete archive under its own trusted SHA-256, then start its helper while the old installation remains stopped and disabled.

```bash
sudo python3 -I /opt/axern/releases/TARGET_ARCHIVE_SHA256/libexec/axern/native_install.py \
  upgrade --from-archive SOURCE_ARCHIVE_SHA256 \
  --backup-receipt /absolute/private/path/backup-receipt.json
sudo /opt/axern/releases/TARGET_ARCHIVE_SHA256/bin/axern server start
```

The transition applies the candidate's database migrations over a private PostgreSQL Unix socket while the native services remain fenced. It then replaces the owned service definitions as a unit and preserves Workspace bytes, existing configuration and keys. An interrupted transition blocks service startup. Resume with the same target archive, source digest and receipt after PostgreSQL is stopped. After the transition, rollback requires restoring the whole verified cold backup with the newer installation fenced. Recovery and normal execution still require a new Allocation after a full stop.

For a development installation or an archive outside the qualified transition, retain the original installation and export the required files through its authorized SDK or declared output delivery. Verify the exported bytes, then create fresh Namespaces, Environments and Workspaces on a separately installed compatible native archive, and supply Secrets through its normal API. The operator chooses which retained files to import. Run and Allocation identities and execution history remain with the original installation.

## Qualify A Replacement Node

Use a verified cold backup made on the same source-built archive as the replacement. Restore the complete system and data images to an isolated host with the original XFS filesystem identity and project quotas intact. Keep the original host powered off and externally fenced before restoring. The replacement uses a new explicit Node ID and new enrollment credentials. Historical archives first use the separately qualified archive transition while services remain disabled.

With the restored services stopped and automatic startup disabled, invoke the restored archive's root maintenance helper using the matching [private backup receipt](#qualify-an-experimental-archive-transition). The operator confirms the external fence. The helper also checks the cold database and runtime, requires fully drained Runs and Workspaces, and requires every retained directory to match its recorded identity and quota.

```bash
sudo python3 -I /opt/axern/releases/ARCHIVE_SHA256/libexec/axern/native_install.py \
  replace-node --from-node ORIGINAL_NODE --to-node REPLACEMENT_NODE \
  --backup-receipt /absolute/private/path/backup-receipt.json --source-fenced
sudo /opt/axern/releases/ARCHIVE_SHA256/bin/axern server start
```

The control transaction revokes the previous Node and records the replacement and retained Workspace mappings. The prior local execution facts and Node key are parked privately. Workspace IDs and generation floors stay intact, while each Workspace gets a storage identity bound to the replacement Node. Start the new Node and confirm its new credential, a new Run and Allocation, retained content and quota, and rejection of the old Node and Allocation authorities.

If the operation is interrupted, stop PostgreSQL or reboot the isolated host with startup still disabled, then retry the same Nodes and backup receipt. Restart and rollback continue to require that only one installation can serve. The qualification covers a verified block-image restore and a fully drained installation. A different XFS identity, ambiguous retained data or an unresolved writer fails closed.

Qualify a complete archive on a disposable fresh installation with the opt-in [memory, storage and network acceptance matrix](QUALIFICATION.md). Serving readiness alone does not run or replace that matrix.

For startup and footprint observations, use the separate [measurement runbook](MEASUREMENT.md). It records the canonical helper's authenticated readiness boundary, systemd timestamps, first public workload output and a 60-second idle interval with memory, process and CPU measurements. Record the installed candidate and cache conditions with the results.
