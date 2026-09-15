#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
python3 - "${root}" <<'PY'
import pathlib
import subprocess
import sys

chart = pathlib.Path(sys.argv[1]) / "deploy/helm/axern"
args = ["helm", "template", "identity-test", str(chart),
        "--set-string", "node.memorySystemReserveBytes=1",
        "--set-string", "node.enrollment.existingSecret=initial-tokens",
        "--set-json", 'node.enrollment.nodes=[{"nodeName":"worker-a","nodeID":"identity-a"},{"nodeName":"worker-b","nodeID":"identity-b"}]']
rendered = subprocess.check_output(args, text=True)
nodes = [doc for doc in rendered.split("\n---") if "\nkind: DaemonSet\n" in doc]
assert len(nodes) == 2, "each admitted host needs its own token projection"
for name, identity, other_identity in (("worker-a", "identity-a", "identity-b"), ("worker-b", "identity-b", "identity-a")):
    documents = [doc for doc in nodes if f'- key: "{identity}"' in doc]
    assert len(documents) == 1, f"missing isolated enrollment token for {name}"
    doc = documents[0]
    assert other_identity not in doc, "another Node token leaked"
    assert 'key: metadata.name' in doc and f'- "{name}"' in doc, "token is not pinned to its host"
    assert "signer.pem" not in doc and "controld.pem" not in doc, "control authority leaked to Node"
    assert f'export AXNODED_CONTROL_PLANE_NODE_ID="{identity}"' in doc
    token_volume = doc.split("secretName: initial-tokens", 1)[1].split("- name:", 1)[0]
    assert token_volume.count("- key:") == 1 and "defaultMode: 384" in token_volume and "optional: true" in token_volume
for component in ("gatewayd", "tunneld"):
    docs = [doc for doc in rendered.split("\n---") if f"# Source: axern/templates/{component}.yaml" in doc]
    assert all("signer.pem" not in doc for doc in docs), "signer leaked outside controld"
duplicate = subprocess.run(args[:-2] + ["--set-json", 'node.enrollment.nodes=[{"nodeName":"worker-a","nodeID":"identity-a"},{"nodeName":"worker-a","nodeID":"identity-b"}]'],
                           capture_output=True, text=True)
assert duplicate.returncode != 0, "duplicate Node deployment accepted"
for invalid in (
    '[{"nodeName":"worker-a","nodeID":"same"},{"nodeName":"worker-b","nodeID":"same"}]',
    '[{"nodeName":"worker-a","nodeID":"../invalid"}]',
    '[{"nodeName":"worker-a"}]',
):
    rejected = subprocess.run(args[:-2] + ["--set-json", "node.enrollment.nodes=" + invalid], capture_output=True, text=True)
    assert rejected.returncode != 0, "invalid identity binding accepted"
print("helm_identity_contract_ok=true")
PY
