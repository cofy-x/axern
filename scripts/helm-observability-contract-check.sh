#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
python3 - "${root}" <<'PY'
import pathlib
import re
import subprocess
import sys

chart = pathlib.Path(sys.argv[1]) / "deploy/helm/axern"
base = ["helm", "template", "telemetry-test", str(chart), "--namespace", "telemetry-test",
        "--set-string", "node.memorySystemReserveBytes=1",
        "--set-string", "node.enrollment.existingSecret=initial-tokens",
        "--set-json", 'node.enrollment.nodes=[{"nodeName":"worker-a","nodeID":"identity-a"}]']
components = ("collector", "prometheus", "tempo", "loki", "grafana")
default = subprocess.check_output(base, text=True)
assert "kind: PersistentVolumeClaim" not in default, "default install acquired telemetry PVCs"
external = subprocess.check_output(base + ["--set-string", "observability.otlp.endpoint=https://telemetry.example:4317"], text=True)
for component in ("controld", "gatewayd", "tunneld", "node"):
    documents = [doc for doc in external.split("\n---") if ("\nkind: Deployment\n" in doc or "\nkind: DaemonSet\n" in doc) and f'app.kubernetes.io/component: "{component}"\n' in doc]
    assert documents, f"external exporter omitted {component}"
    for doc in documents:
        assert re.search(r'name: AXERN_OTEL_ENABLED\s+value: "true"', doc), f"{component} did not enable external export"
        assert 'value: "https://telemetry.example:4317"' in doc, f"{component} lost the external receiver"
        assert re.search(r'name: OTEL_EXPORTER_OTLP_INSECURE\s+value: "false"', doc), f"{component} disabled transport security by default"
assert "kind: PersistentVolumeClaim" not in external, "external receiver acquired backend PVCs"
for template in ("otel-collector", "prometheus", "tempo", "loki", "grafana"):
    assert f"# Source: axern/templates/{template}.yaml" not in external, f"external receiver started {template}"
enabled = base + ["--set", "observability.enabled=true", "--set-string",
                  "observability.grafana.admin.existingSecret=operator-admin"]
missing = subprocess.run(enabled, capture_output=True, text=True)
assert missing.returncode != 0 and ".image.repository is required" in missing.stderr, "unqualified backends accepted"

def images(skip=None, repository="telemetry.example/qualified-backend", digest="sha256:" + "a" * 64):
    args = []
    for component in components:
        args += ["--set-string", f"observability.{component}.image.repository={repository}"]
        if component != skip:
            args += ["--set-string", f"observability.{component}.image.digest={digest}"]
    return args

for component in components:
    rejected = subprocess.run(enabled + images(skip=component), capture_output=True, text=True)
    assert rejected.returncode != 0 and f"observability.{component}.image.digest is required" in rejected.stderr, f"{component} accepted a mutable image"
for repository, digest in (("backend/image", "sha256:" + "a" * 64),
                           ("telemetry.example/backend", "sha256:invalid")):
    rejected = subprocess.run(enabled + images(repository=repository, digest=digest), capture_output=True, text=True)
    assert rejected.returncode != 0, "invalid backend artifact identity accepted"

rendered = subprocess.check_output(enabled + images(), text=True)
pvcs = [doc for doc in rendered.split("\n---") if "\nkind: PersistentVolumeClaim\n" in doc]
assert len(pvcs) == 5, "qualified backend install changed the five-PVC contract"
for name in ("otel-collector", "prometheus", "tempo", "loki", "grafana"):
    assert sum(f"\n  name: {name}\n" in doc for doc in pvcs) == 1, f"retained {name} PVC identity changed"
assert rendered.count('image: "telemetry.example/qualified-backend@sha256:' + "a" * 64 + '"') == 5
for component in components:
    retained = subprocess.check_output(enabled + images() + ["--set-string", f"observability.{component}.persistence.existingClaim=operator-retained"], text=True)
    assert "claimName: operator-retained" in retained or 'claimName: "operator-retained"' in retained, f"{component} ignored an operator-owned PVC"
    assert retained.count("\nkind: PersistentVolumeClaim\n") == 4, f"{component} recreated its existing PVC"
print("helm_observability_contract_ok=true")
PY
