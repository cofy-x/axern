#!/usr/bin/env python3
"""Exercise the actual inventory predicate without starting a privileged node."""
import json
import pathlib
import re
import subprocess

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
print("network_policy_readiness_contract_ok=true")
