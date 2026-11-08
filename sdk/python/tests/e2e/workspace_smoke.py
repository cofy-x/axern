"""Bounded public-API Workspace truth on an exclusive native Linux installation."""
from __future__ import annotations

import argparse
import datetime
import errno
import hashlib
import importlib.metadata
import json
import os
from pathlib import Path
import re
import signal
import sys
import time
from typing import Any

import grpc
from axern.control.quota.v1 import quota_pb2, quota_pb2_grpc
from axern.control.run.v1 import run_pb2
from axern.control.workspace.v1 import workspace_pb2
from axern_sdk import AxernClient, NetworkPolicy, Sandbox


CAPACITY = 8 * 1024**2
INODES = 64
UID = "1000:1000"
TRANSPORT_KEYS = {"endpoint", "tls_ca_cert", "tls_cert", "tls_key", "tls_server_name"}


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def timestamp():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


def error_fact(error):
    code = getattr(error, "code", "")
    if callable(code):
        code = code()
    if isinstance(code, grpc.StatusCode):
        code = code.name
    if not isinstance(code, str) or not re.fullmatch(r"[A-Z_0-9]{0,64}", code):
        code = ""
    return {"error_type": type(error).__name__, "code": code}


def timeout_alarm(_signum, _frame):
    raise TimeoutError("bounded Workspace qualification deadline elapsed")


class Probe:
    def __init__(self, args, transport):
        self.args = args
        self.output = Path(args.output_dir).absolute()
        self.output.mkdir(mode=0o700, parents=True, exist_ok=False)
        self.deadline = time.monotonic() + args.timeout
        self.nonce = os.urandom(16).hex()
        self.labels = {"smoke.axern.dev/nonce": self.nonce}
        self.client = AxernClient(transport["endpoint"], proxy_mode="direct",
                                  **{key: value for key, value in transport.items() if key != "endpoint"})
        credentials = grpc.ssl_channel_credentials(
            root_certificates=Path(transport["tls_ca_cert"]).read_bytes(),
            private_key=Path(transport["tls_key"]).read_bytes(),
            certificate_chain=Path(transport["tls_cert"]).read_bytes(),
        )
        options = [("grpc.enable_http_proxy", 0)]
        if transport.get("tls_server_name"):
            options.append(("grpc.ssl_target_name_override", transport["tls_server_name"]))
        self.quota_channel = grpc.secure_channel(transport["endpoint"], credentials, options=options)
        self.quota = quota_pb2_grpc.QuotaControlStub(self.quota_channel)
        self.environment_id = ""
        self.boxes = []
        self.runs = {}
        self.workspaces: list[dict[str, Any]] = []
        self.report = {
            "schema": "axern.workspace-public-smoke.v1", "passed": False,
            "source_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
            "sdk_version": importlib.metadata.version("axern-sdk"),
            "started_at": timestamp(), "nonce": self.nonce,
            "node_id": args.node, "image": args.image,
            "scope": "public lifecycle, retained bytes, writer rejection and guest-visible quota failures",
            "operator_kernel_audit": "separate; public metadata is not host-kernel ownership evidence",
            "backup_restore": "not qualified by this probe", "checks": {},
            "workspaces": self.workspaces, "runs": self.runs, "cleanup_errors": [],
        }
        self.stage("initialization")

    def limit(self, seconds=30):
        remaining = self.deadline - time.monotonic()
        if remaining <= 0:
            raise TimeoutError("Workspace qualification deadline elapsed")
        return min(seconds, remaining)

    def save(self):
        temporary = self.output / ".report.pending"
        descriptor = os.open(temporary, os.O_CREAT | os.O_TRUNC | os.O_WRONLY, 0o600)
        with os.fdopen(descriptor, "w") as stream:
            json.dump(self.report, stream, indent=2)
            stream.write("\n")
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, self.output / "report.json")

    def stage(self, name):
        self.report["stage"] = name
        self.save()

    def check(self, name, value):
        self.report["checks"][name] = value
        self.save()

    def charge(self):
        quota = self.quota.GetNamespaceQuota(
            quota_pb2.GetNamespaceQuotaRequest(namespace="default"), timeout=self.limit()
        ).quota
        return {"bytes": quota.used_persistent_storage_bytes, "inodes": quota.used_persistent_storage_inodes}

    def workspace(self, name):
        self.stage("create-" + name)
        value = self.client.create_workspace(
            node_id=self.args.node, capacity_bytes=CAPACITY, inode_limit=INODES,
            namespace="default", labels={**self.labels, "smoke.axern.dev/case": name}, timeout=self.limit(),
        )
        require(bool(value.id), "Workspace creation returned no data identity")
        receipt = {"case": name, "id": value.id, "capacity_bytes": CAPACITY, "inode_limit": INODES}
        self.workspaces.append(receipt)
        self.save()
        self.client.wait_workspace_ready(value.id, timeout=self.limit(120))
        return value.id

    def box(self, name, workspace_id):
        self.stage("start-" + name)
        box = Sandbox(
            client=self.client, environment_id=self.environment_id,
            workspace_id=workspace_id, namespace="default", network_policy=NetworkPolicy.deny_all(),
            request_cpu="100m", request_memory="512Mi", limit_cpu="1", limit_memory="512Mi",
            request_ephemeral_storage="64Mi", limit_ephemeral_storage="64Mi",
            argv=["python3", "-u", "-c", "import time; time.sleep(900)"],
            labels={**self.labels, "smoke.axern.dev/case": name},
            ready_timeout_seconds=self.limit(180),
        )
        self.boxes.append(box)
        box.start()
        state = box.state
        require(state.workspace_id == workspace_id and state.workspace_generation > 0,
                "Sandbox did not expose its admitted Workspace identity")
        self.runs[name] = {"run_id": state.run_id, "allocation_id": state.allocation_id,
                           "workspace_id": state.workspace_id, "generation": state.workspace_generation}
        self.save()
        return box

    def guest(self, box, source):
        seconds = max(1, int(self.limit(60)))
        result = box.exec(["python3", "-c", source], user=UID, check=True, text=True,
                          timeout_seconds=seconds, rpc_timeout=self.limit(seconds + 10))
        require(len(result.stdout.encode()) <= 16384, "guest receipt exceeded its bound")
        value = json.loads(result.stdout)
        require(isinstance(value, dict), "guest receipt is not an object")
        return value

    def close(self, box, workspace_id):
        box.close()
        return self.client.wait_workspace_ready(workspace_id, timeout=self.limit(120))

    def reject(self, name, callback, code):
        try:
            unexpected = callback()
        except Exception as error:
            require(getattr(error, "code", "") == code, name + " returned an unrelated failure")
            self.check(name, {"passed": True, "code": code})
            return
        if hasattr(unexpected, "allocation_id"):
            self.runs[name] = {"run_id": unexpected.id, "allocation_id": unexpected.allocation_id}
        elif hasattr(unexpected, "capacity_bytes"):
            self.workspaces.append({"case": name, "id": unexpected.id})
        self.save()
        raise RuntimeError(name + " unexpectedly succeeded")

    def delete(self, workspace_id):
        self.stage("explicit-delete-" + workspace_id)
        value = self.client.delete_workspace(workspace_id, timeout=self.limit())
        require(value.id == workspace_id, "Workspace deletion returned another identity")
        deadline = time.monotonic() + self.limit(120)
        while value.status != workspace_pb2.WORKSPACE_STATUS_DELETED:
            require(time.monotonic() < deadline, "Workspace deletion did not finish within its bound")
            time.sleep(0.2)
            value = self.client.get_workspace(workspace_id, timeout=self.limit())
        self.check("deleted-" + workspace_id, {"passed": True, "status": "deleted"})

    def run(self):
        baseline = self.charge()
        self.check("baseline_charge", baseline)
        self.stage("create-immutable-environment")
        environment = self.client.create_environment(image_ref=self.args.image, namespace="default",
                                                     labels=self.labels, timeout=self.limit())
        self.environment_id = environment.id
        self.report["environment_id"] = environment.id
        self.save()
        require(bool(environment.id), "Environment creation returned no identity")
        workspace_id = self.workspace("retention-and-blocks")
        expected_charge = {"bytes": baseline["bytes"] + CAPACITY, "inodes": baseline["inodes"] + INODES}
        require(self.charge() == expected_charge, "Workspace creation did not retain its independent capacity charge")
        box_a = self.box("instance-a", workspace_id)
        self.stage("ordinary-user-write")
        seed = self.guest(box_a, SEED.replace("__NONCE__", repr(self.nonce)))
        expected = ((self.nonce + "\n").encode() * 2048)[:65536]
        expected_hash = hashlib.sha256(expected).hexdigest()
        require(seed.get("uid") == 1000 and seed.get("bytes") == 65536 and
                seed.get("sha256") == expected_hash and seed.get("same_inode") is True,
                "ordinary-user retained bytes or hardlink receipt is incorrect")
        self.check("ordinary_user_write", seed)
        self.reject("active_delete_rejected", lambda: self.client.delete_workspace(workspace_id, timeout=self.limit()), "FAILED_PRECONDITION")
        self.reject("competing_writer_rejected", lambda: self.client.create_run(
            environment_id=self.environment_id, workspace_id=workspace_id, namespace="default",
            argv=["python3", "-c", "raise SystemExit('competing writer must not execute')"],
            limit_memory="512Mi", limit_ephemeral_storage="64Mi", labels=self.labels, timeout=self.limit()), "FAILED_PRECONDITION")
        old_id = box_a.state.allocation_id
        self.stage("close-instance-a")
        detached = self.close(box_a, workspace_id)
        require(detached.attached_allocation_id == "" and self.charge() == expected_charge,
                "closing execution released retained storage capacity")
        self.check("detached_charge", expected_charge)
        box_b = self.box("instance-b", workspace_id)
        require(self.runs["instance-a"]["run_id"] != box_b.state.run_id and old_id != box_b.state.allocation_id,
                "replacement reused an execution identity")
        self.stage("read-retained-bytes")
        restored = self.guest(box_b, VERIFY.replace("__NONCE__", repr(self.nonce)))
        require(restored.get("sha256") == expected_hash and restored.get("bytes") == 65536 and
                restored.get("same_inode") is True and restored.get("nonce") == self.nonce and
                restored.get("ephemeral_marker_absent") is True and restored.get("uid") == 1000,
                "replacement did not reuse exact retained bytes independently of its ephemeral root")
        require(hashlib.sha256(box_b.read_bytes("/workspace/payload.bin")).hexdigest() == expected_hash,
                "public file RPC returned different retained bytes")
        self.check("fresh_execution_retains_files", restored)
        try:
            self.client.allocation(old_id).read_file("/workspace/payload.bin", rpc_timeout=self.limit())
        except Exception as error:
            require(getattr(error, "code", "") in {"FAILED_PRECONDITION", "NOT_FOUND"},
                    "stale Allocation access returned an unrelated failure")
            self.check("stale_allocation_denied", {"passed": True, "code": getattr(error, "code", "")})
        else:
            raise RuntimeError("stale Allocation handle read the replacement data")
        self.stage("block-quota-exhaustion")
        blocks = self.guest(box_b, BLOCKS)
        require(blocks.get("quota_errno") in {errno.ENOSPC, errno.EDQUOT} and
                blocks.get("fsync_count", 0) > 0 and 0 < blocks.get("written_bytes", 0) <= 16 * 1024**2 and
                blocks.get("root_fsync_ok") is True and blocks.get("uid") == 1000,
                "Workspace block limit did not reject writes independently of the ephemeral root")
        self.check("block_quota", blocks)
        self.close(box_b, workspace_id)
        require(self.charge() == expected_charge, "block exhaustion or close released retained charge")
        self.delete(workspace_id)
        require(self.charge() == baseline, "confirmed data deletion did not return capacity charge to baseline")
        self.check("block_workspace_charge_released", baseline)
        inode_workspace = self.workspace("inodes")
        box_c = self.box("inode-instance", inode_workspace)
        self.stage("inode-quota-exhaustion")
        inodes = self.guest(box_c, INODE_FILL)
        require(inodes.get("quota_errno") in {errno.ENOSPC, errno.EDQUOT} and
                0 < inodes.get("created_files", 0) < 128 and inodes.get("root_fsync_ok") is True,
                "Workspace inode limit did not reject bounded empty-file creation")
        self.check("inode_quota", inodes)
        self.close(box_c, inode_workspace)
        require(self.charge() == expected_charge, "inode exhaustion or close released retained charge")
        self.delete(inode_workspace)
        require(self.charge() == baseline, "inode Workspace deletion did not return capacity to baseline")
        self.reject("unsupported_capacity_rejected", lambda: self.client.create_workspace(
            node_id=self.args.node, capacity_bytes=((2**63 - 1) // 512) * 512, inode_limit=64,
            namespace="default", labels=self.labels, timeout=self.limit()), "RESOURCE_EXHAUSTED")
        self.client.delete_environment(self.environment_id, timeout=self.limit())
        self.report["environment_deleted"] = True
        self.report["passed"] = True
        self.stage("complete")

    def cleanup_runs(self):
        """Cancel only exact owned executions; leave failed Workspace evidence intact."""
        try:
            cursor = ""
            for _ in range(10):
                response = self.client.list_runs(namespace="default", labels=self.labels, cursor=cursor, page_size=100, timeout=10)
                for run in response.runs:
                    require(run.labels.get("smoke.axern.dev/nonce") == self.nonce,
                            "Run discovery returned an unowned execution")
                    if not any(value.get("run_id") == run.id for value in self.runs.values()):
                        self.runs["discovered-" + run.id] = {"run_id": run.id, "allocation_id": run.allocation_id}
                    if run.status not in {run_pb2.RUN_STATUS_SUCCEEDED, run_pb2.RUN_STATUS_FAILED, run_pb2.RUN_STATUS_CANCELLED}:
                        self.client.cancel_run(run.id, timeout=10)
                cursor = response.next_cursor
                if not cursor:
                    break
            require(not cursor, "owned Run discovery exceeded its page bound")
        except Exception as error:
            self.report["cleanup_errors"].append(error_fact(error))
            self.report["passed"] = False
        try:
            cursor = ""
            for _ in range(10):
                response = self.client.list_workspaces(namespace="default", cursor=cursor, page_size=100, timeout=10)
                for value in response.workspaces:
                    if value.labels.get("smoke.axern.dev/nonce") != self.nonce:
                        continue
                    require(value.node_id == self.args.node and value.namespace == "default",
                            "Workspace discovery returned an unexpected owned binding")
                    existing = next((item for item in self.workspaces if item["id"] == value.id), None)
                    if existing is None:
                        existing = dict[str, Any](case="discovered-after-interruption", id=value.id)
                        self.workspaces.append(existing)
                    existing["final_observation"] = {
                        "status": workspace_pb2.WorkspaceStatus.Name(value.status),
                        "capacity_bytes": value.capacity_bytes, "inode_limit": value.inode_limit,
                        "generation": value.generation, "attached_allocation_id": value.attached_allocation_id,
                        "used_bytes": value.used_bytes, "used_inodes": value.used_inodes,
                    }
                cursor = response.next_cursor
                if not cursor:
                    break
            require(not cursor, "owned Workspace discovery exceeded its page bound")
        except Exception as error:
            self.report["cleanup_errors"].append(error_fact(error))
            self.report["passed"] = False
        self.report["finished_at"] = timestamp()
        self.save()


SEED = """import hashlib,json,os
nonce=__NONCE__
data=((nonce+'\\n').encode()*2048)[:65536]
with open('/workspace/payload.bin','xb') as f:
 f.write(data);f.flush();os.fsync(f.fileno())
os.link('/workspace/payload.bin','/workspace/payload.link')
with open('/workspace/nonce.txt','x') as f:
 f.write(nonce);f.flush();os.fsync(f.fileno())
with open('/tmp/workspace-smoke-root-marker','x') as f:
 f.write(nonce);f.flush();os.fsync(f.fileno())
a=os.stat('/workspace/payload.bin');b=os.stat('/workspace/payload.link')
print(json.dumps({'uid':os.getuid(),'bytes':len(data),'sha256':hashlib.sha256(data).hexdigest(),'same_inode':a.st_ino==b.st_ino,'links':a.st_nlink}))
"""

VERIFY = """import hashlib,json,os
nonce=__NONCE__
with open('/workspace/payload.bin','rb') as f:data=f.read(65537)
with open('/workspace/nonce.txt') as f:saved=f.read(128)
a=os.stat('/workspace/payload.bin');b=os.stat('/workspace/payload.link')
print(json.dumps({'uid':os.getuid(),'bytes':len(data),'sha256':hashlib.sha256(data).hexdigest(),'same_inode':a.st_ino==b.st_ino,'nonce':saved,'ephemeral_marker_absent':not os.path.exists('/tmp/workspace-smoke-root-marker')}))
"""

ROOT_FSYNC = """with open('/tmp/workspace-smoke-independent-root','wb') as root:
 root.write(b'R'*65536);root.flush();os.fsync(root.fileno())
"""

BLOCKS = """import errno,json,os
quota_errno=0;written=0;fsync_count=0;operation='create'
try:
 with open('/workspace/block-fill','xb',buffering=0) as f:
  for _ in range(64):
   operation='write';count=f.write(b'B'*262144);written+=count
   operation='fsync';os.fsync(f.fileno());fsync_count+=1
  operation='close'
except OSError as error:
 quota_errno=error.errno
""" + ROOT_FSYNC + """print(json.dumps({'uid':os.getuid(),'quota_errno':quota_errno,'failed_operation':operation,'written_bytes':written,'fsync_count':fsync_count,'root_fsync_ok':True}))
"""

INODE_FILL = """import errno,json,os
quota_errno=0;created=0
try:
 for index in range(128):
  with open('/workspace/inode-'+str(index),'xb') as f:os.fsync(f.fileno())
  created+=1
except OSError as error:
 quota_errno=error.errno
""" + ROOT_FSYNC + """print(json.dumps({'uid':os.getuid(),'quota_errno':quota_errno,'created_files':created,'root_fsync_ok':True}))
"""


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--transport-config", required=True, help="private JSON containing only endpoint and mTLS paths")
    parser.add_argument("--node", required=True)
    parser.add_argument("--image", required=True, help="immutable OCI reference ending in @sha256:<digest>")
    parser.add_argument("--output-dir", required=True, help="new private evidence directory")
    parser.add_argument("--timeout", type=int, default=900, help="hard test deadline in seconds")
    args = parser.parse_args()
    try:
        require(re.fullmatch(r"[^\s@]+@sha256:[0-9a-f]{64}", args.image), "an immutable OCI image digest is required")
        require(30 <= args.timeout <= 1800, "timeout must be in [30, 1800]")
        with open(args.transport_config, encoding="utf-8") as stream:
            transport = json.load(stream)
        require(isinstance(transport, dict) and set(transport) <= TRANSPORT_KEYS and
                TRANSPORT_KEYS - {"tls_server_name"} <= set(transport) and
                all(isinstance(value, str) and (value or key == "tls_server_name") for key, value in transport.items()),
                "transport config must contain only a complete endpoint and mTLS paths")
    except Exception as error:
        print(json.dumps({"passed": False, **error_fact(error)}))
        return 1
    os.umask(0o077)
    probe = None
    signal.signal(signal.SIGALRM, timeout_alarm)
    signal.alarm(args.timeout)
    try:
        probe = Probe(args, transport)
        probe.run()
    except Exception as error:
        if probe is None:
            print(json.dumps({"passed": False, **error_fact(error)}))
        else:
            probe.report["error"] = error_fact(error)
            probe.report["passed"] = False
            probe.save()
    finally:
        signal.alarm(120)
        if probe is not None:
            try:
                probe.cleanup_runs()
            except Exception as error:
                probe.report["cleanup_errors"].append(error_fact(error))
                probe.report["passed"] = False
                probe.save()
            finally:
                probe.client.close()
                probe.quota_channel.close()
        signal.alarm(0)
    passed = probe is not None and probe.report["passed"]
    print(json.dumps({"passed": passed, "report": str(probe.output / "report.json") if probe else ""}))
    return 0 if passed else 1


if __name__ == "__main__":
    sys.exit(main())
