"""Prove narrow non-shipping matches against the qualified gVisor payload."""

import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import tarfile


RUNSC = "/usr/local/bin/runsc"
SENTRY = "/usr/local/bin/gvisor-bin/gvisor_sentry"
# Release 20261102.0, selected amd64 and arm64 payloads. A new release needs fresh evidence.
SUBJECTS = {
    "amd64": {
        RUNSC: ("c5a5a7ba1209b605d08874fdd4429035f95afcb2d482bc95ed3fb13d31a25098", 19407310),
        SENTRY: ("6dfd3af65e664663f332d0e4b9fb82452496e34ebc6cb568ae49f5b1ac9d8166", 54951832),
    },
    "arm64": {
        RUNSC: ("cbca2f059fdbfd3563d7d7c10b4d77d534aaaa1f524ddb47d76b36285acc6499", 18176288),
        SENTRY: ("c394ae17b4a1ccf9d07085f17de9c11bc3de3958410e620f5f8138f6887e424e", 51520121),
    },
}
RULES = {
    ("GO-2026-6859", "govulndb:language:go", "Medium", "stdlib", "go1.27.3"):
        ({RUNSC, SENTRY}, "has_http_body_limit", "https://go.dev/cl/862185", "https://go.dev/cl/862185",
         "70d7ef1a12730558378678930160068c47a1ac5e8462c13800531a21390917b0"),
    ("GHSA-xhgw-qwwf-pg32", "github:language:go", "Low", "github.com/cilium/ebpf", "v0.16.0"):
        ({RUNSC}, "has_cilium_btf_parser", "https://github.com/cilium/ebpf/commit/533dfc82fd228bfadf42ea7180c39de7d9af47fa",
         "https://github.com/advisories/GHSA-xhgw-qwwf-pg32", "9961560f1c2e8f453ad9df9c34171cc170ae2eaf5559cfdc03c72cbb37a514b5"),
}


class GVisorFindings:
    def __init__(self, verifier, arch, layout, output, sbom, manifest, config, env):
        self.verifier = verifier
        self.arch = arch
        self.layout = layout
        self.output = output
        self.sbom = sbom
        self.layers = list(zip(manifest["layers"], config["rootfs"]["diff_ids"], strict=True))
        self.env = env
        self.results = {}

    def classify(self, match):
        finding, artifact = match["vulnerability"], match["artifact"]
        if artifact.get("type") != "go-module" or finding.get("fix", {}).get("state") != "fixed":
            return None
        identity = tuple(finding.get(key) for key in ("id", "namespace", "severity")) + (
            artifact.get("name"), artifact.get("version"),
        )
        rule = RULES.get(identity)
        locations = artifact.get("locations", [])
        if rule is None or len(locations) != 1:
            return None
        paths, check, fix_url, advisory_url, description_digest = rule
        description = finding.get("description")
        if (finding.get("dataSource") != advisory_url or not isinstance(description, str)
                or hashlib.sha256(description.encode()).hexdigest() != description_digest):
            return None
        location = locations[0]
        filename, layer_id = location.get("path"), location.get("layerID")
        if filename not in paths or location.get("accessPath") != filename:
            return None
        subject = (filename, layer_id)
        if subject not in self.results:
            self.results[subject] = self.prove(filename, layer_id)
        evidence = self.results[subject]
        if evidence is None or evidence["result"].get(check) is not False:
            return None
        return {**evidence, "proof": check, "advisory_fix": fix_url, "advisory_description_sha256": description_digest}

    def prove(self, filename, layer_id):
        digest, size = SUBJECTS[self.arch][filename]
        records = [record for record in self.sbom.get("files", []) if record.get("location") == {
            "path": filename, "layerID": layer_id,
        }]
        layers = [layer["digest"] for layer, diff_id in self.layers if layer_id in (layer["digest"], diff_id)]
        if len(records) != 1 or len(layers) != 1 or {"algorithm": "sha256", "value": digest} not in records[0].get("digests", []):
            return None

        subject = self.layout / Path(filename).name
        report = self.output / (subject.name + "-binary-proof.json")
        errors = report.with_suffix(".stderr")
        try:
            blob = self.layout / "blobs" / "sha256" / layers[0].removeprefix("sha256:")
            with tarfile.open(blob, "r:*") as archive:
                files = [member for member in archive if member.name.removeprefix("./") == filename.lstrip("/")]
                if len(files) != 1 or not files[0].isfile() or files[0].size != size:
                    return None
                with archive.extractfile(files[0]) as stream, subject.open("xb") as destination:
                    shutil.copyfileobj(stream, destination, length=1024**2)
            with subject.open("rb") as stream:
                if hashlib.file_digest(stream, "sha256").hexdigest() != digest:
                    return None
            with report.open("xb") as output, errors.open("xb") as stderr:
                subprocess.run([self.verifier, self.arch, str(subject)], env=self.env, stdout=output,
                               stderr=stderr, check=True, timeout=30)
            proof = json.loads(report.read_bytes())
            if proof["go_version"] != "go1.27.3" or not 0 < proof["functions"] <= proof["names"]:
                return None
        except (KeyError, OSError, TypeError, ValueError, subprocess.SubprocessError, tarfile.TarError):
            return None
        finally:
            subject.unlink(missing_ok=True)
        return {"path": filename, "layer": layer_id, "sha256": digest, "result": proof}
