#!/usr/bin/env python3
"""Exercise the actual inventory predicate without starting a privileged node."""
import json
import os
import pathlib
import re
import subprocess
import tempfile

root = pathlib.Path(__file__).resolve().parents[2]
source = (root / "runtime/axnoded/scripts/qualification/network-policy-scenario-in-container.sh").read_text()
match = re.search(r"^inventory_ready\(\) \{\n(.*?)^\}", source, re.M | re.S)
assert match, "missing inventory readiness predicate"
predicate = re.search(r"jq -e --arg network_capability.*?'\n(.*?)\n  '", match.group(1), re.S)
assert predicate, "missing jq inventory predicate"
prefix = "PLATFORM_CAPABILITY_"
base = ["DNS_POLICY_ENFORCEMENT", "STRICT_EGRESS_ENFORCEMENT"]
backends = sorted(set(re.findall(r"network_capability=PLATFORM_CAPABILITY_(\w+)", source)))
assert backends == ["NETWORK_BPFNET", "NETWORK_BRIDGE"]
probes = ["RUNSC_MEMORY_ENFORCEMENT_SELF_TEST", "RUNSC_EPHEMERAL_ENFORCEMENT_SELF_TEST"]

def check(required, backend, expected, probe_state="AVAILABLE"):
    observations = [{"key": {"platform": prefix + key}, "state": "CAPABILITY_STATE_AVAILABLE"} for key in required]
    # Match node inventory: the unselected backend is explicitly disabled, not
    # another mandatory capability or an inferred fallback.
    observations += [{"key": {"platform": prefix + key}, "state": "CAPABILITY_STATE_UNAVAILABLE", "reason_code": "CAPABILITY_REASON_CODE_DISABLED"} for key in backends if key != backend and key not in required]
    observations += [{"key": {"platform": prefix + key}, "state": "CAPABILITY_STATE_" + probe_state} for key in probes]
    result = subprocess.run(["jq", "-e", "--arg", "network_capability", prefix + backend, predicate.group(1)],
                            input=json.dumps({"node": {"capability_snapshot": {"observations": observations}}}),
                            text=True, capture_output=True)
    assert result.returncode in (0, 1), result.stderr
    assert (result.returncode == 0) == expected, (required, backend, probe_state, result.stdout)

for backend in backends:
    required = sorted(set(base + [backend]))
    check(required, backend, True)
    check(required + required, backend, True)
    check(required, backend, True, "UNAVAILABLE")
    check(required, backend, False, "UNKNOWN")
    for missing in required:
        check([key for key in required if key != missing], backend, False)
    check(base + [key for key in backends if key != backend], backend, False)
check([], "NETWORK_BRIDGE", False)

matrix_path = root / "runtime/axnoded/scripts/qualification/network-policy-linux-smoke.sh"
matrix = matrix_path.read_text()
rejection = re.search(r"^verify_rejected_cell\(\) \{\n.*?^\}", matrix, re.M | re.S)
assert rejection, "missing unsupported backend rejection assertion"
diagnostic = "normalize network config: ebpf network backend supports IPv4 only; select the iptables backend for an IPv6 sandbox range"
with tempfile.TemporaryDirectory(prefix="axern-matrix-rejection-") as directory:
    log = pathlib.Path(directory) / "rejection.log"
    report = pathlib.Path(directory) / "result.json"
    for result, message, reported, expected in (
        (1, diagnostic, False, True),
        (0, diagnostic, False, False),
        (1, "unrelated node startup failure", False, False),
        (1, diagnostic, True, False),
    ):
        log.write_text(message)
        if reported:
            report.write_text("{}")
        checked = subprocess.run(["bash", "-c", rejection.group(0) + '\nverify_rejected_cell "$@"', "matrix-test", str(result), str(log), str(report)], capture_output=True, text=True)
        assert (checked.returncode == 0) == expected, checked.stderr
        if reported:
            report.unlink()

# Run the outer wrapper with a private, host-safe Docker fixture. Assert the
# full exit/cleanup contract even when a scenario never creates its report.
with tempfile.TemporaryDirectory(prefix="axern-matrix-cleanup-") as directory:
    temp = pathlib.Path(directory)
    (temp / "uname").write_text("#!/bin/sh\nprintf 'Linux\\n'\n")
    (temp / "docker").write_text('''#!/usr/bin/env python3
import json
import os
import pathlib
import sys

args = sys.argv[1:]
if args[0] in ("build", "info"):
    sys.exit(0)
if args[:2] == ["image", "inspect"]:
    print("sha256:" + "a" * 64)
    sys.exit(0)
assert args[0] == "run", args
mount = args[args.index("--mount") + 1]
output = pathlib.Path(mount.removeprefix("type=bind,src=").removesuffix(",dst=/qualification-output"))

def report(filename, backend, family, mode):
    (output / filename).write_text(json.dumps({
        "runtime": "runsc", "networkBackend": backend, "ipFamily": family,
        "policyMode": mode, "metrics": {"operations": 1, "failures": 0},
    }))

if "/bin/bash" in args:
    if os.environ["TEST_FAIL_SCENARIO"] == "1":
        (output / "sequential-unrestricted.json.axnoded.log").write_text("private node marker")
        print("private fixture marker", file=sys.stderr)
        sys.exit(47)
    report("sequential-unrestricted.json", "bridge", "ipv4", "unrestricted")
    report("sequential-dns-deny.json", "bridge", "ipv4", "dns_deny")
else:
    backend, family, mode = (args[args.index(flag) + 1] for flag in ("--network-backend", "--ip-family", "--policy-mode"))
    if (backend, family) == ("ebpf", "ipv6"):
        sys.exit(os.environ["TEST_UNSUPPORTED_BACKEND_DIAGNOSTIC"])
    report(pathlib.Path(args[args.index("--output") + 1]).name, backend, family, mode)
''')
    for executable in (temp / "docker", temp / "uname"):
        executable.chmod(0o700)
    evidence_root = temp / "evidence"
    evidence_root.mkdir()
    matrix_env = {
        **os.environ, "PATH": f"{temp}:{os.environ['PATH']}", "TMPDIR": str(evidence_root),
        "IMAGE_TAG": "axnoded-test:matrix-evidence", "VERIFY_DOCKER_PLATFORM": "linux/amd64",
        "VERIFY_DOCKER_VARIANT": "full", "VERIFY_DOCKER_PULL": "false", "VERIFY_DOCKER_BUILDKIT": "1",
        "APT_MIRROR_SOURCE": "archive", "RUNSC_SOURCE": "remote", "AXERN_DOCKER_CACHE_BACKEND": "none",
        "AXERN_NODE_RUNTIME_BASE_CACHE_BACKEND": "none", "NETWORK_POLICY_LINUX_MATRIX_SCOPE": "representative",
        "TEST_UNSUPPORTED_BACKEND_DIAGNOSTIC": diagnostic,
    }
    success = subprocess.run(["bash", matrix_path], env={**matrix_env, "TEST_FAIL_SCENARIO": "0"}, capture_output=True, text=True)
    assert success.returncode == 0, (success.stdout, success.stderr)
    assert "network_policy_linux_smoke_ok=true" in success.stdout
    assert not list(evidence_root.iterdir()), "passed matrix leaked its temporary directory"

    failed = subprocess.run(["bash", matrix_path], env={**matrix_env, "TEST_FAIL_SCENARIO": "1"}, capture_output=True, text=True)
    assert failed.returncode == 47, (failed.returncode, failed.stderr)
    evidence, = evidence_root.iterdir()
    assert evidence.stat().st_mode & 0o777 == 0o700
    assert str(evidence) in failed.stderr
    assert "private fixture marker" not in failed.stdout + failed.stderr
    assert "private node marker" not in failed.stdout + failed.stderr
    assert (evidence / "sequential.stderr.log").read_text() == "private fixture marker\n"
    assert (evidence / "sequential-unrestricted.json.axnoded.log").read_text() == "private node marker"

# Check the real producer/consumer CLI contract, including removed arguments.
qualification = (root / "runtime/egressd/scripts/qualification-matrix.sh").read_text()
driver_call = re.search(r'"\$\{DRIVER\}" \\\n(.*?)\n        if ', qualification, re.S)
assert driver_call, "missing performance driver invocation"
accepted = set(re.findall(r"^    (--[a-z-]+)\)", source, re.M))
passed = set(re.findall(r"(--[a-z-]+)", driver_call.group(1)))
assert passed <= accepted, ("unsupported driver arguments", passed - accepted)
print("network_policy_readiness_contract_ok=true")
