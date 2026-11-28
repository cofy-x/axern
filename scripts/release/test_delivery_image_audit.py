import argparse
from datetime import datetime, timedelta, timezone
from hashlib import sha256
from io import BytesIO
import json
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest.mock import patch

import delivery_image_audit as audit
from gvisor_delivery_findings import GVisorFindings, RULES, RUNSC, SUBJECTS


DIGEST = "sha256:" + "b" * 64
SOURCE = "a" * 40
MANIFEST_TYPE = "application/vnd.oci.image.manifest.v1+json"


class GVisorProofTests(unittest.TestCase):
    def setUp(self):
        key = ("GO-2026-6859", "govulndb:language:go", "Medium", "stdlib", "go1.27.3")
        self.description = "Test advisory description"
        self.enterContext(patch.dict(RULES, {key: (*RULES[key][:-1], sha256(self.description.encode()).hexdigest())}))

    def test_classifies_only_the_reviewed_advisory_package_and_location(self):
        finding = {"id": "GO-2026-6859", "namespace": "govulndb:language:go", "severity": "Medium",
                   "fix": {"state": "fixed", "versions": ["1.27.4"]}, "dataSource": "https://go.dev/cl/862185",
                   "description": self.description}
        artifact = {"type": "go-module", "name": "stdlib", "version": "go1.27.3",
                    "locations": [{"path": RUNSC, "accessPath": RUNSC, "layerID": DIGEST}]}
        match = {"vulnerability": finding, "artifact": artifact}
        classifier = GVisorFindings(None, "amd64", None, None, {}, {"layers": []}, {"rootfs": {"diff_ids": []}}, {})
        report = {"matches": [], "ignoredMatches": [match]}
        with patch.object(classifier, "prove", return_value={"result": {"has_http_body_limit": False}}) as prove:
            classified = []
            self.assertEqual(audit.blocker_findings(report, classifier, classified), [])
            self.assertEqual(classified[0]["advisory"], finding["id"])
            prove.assert_called_once_with(RUNSC, DIGEST)
            self.assertEqual(report["ignoredMatches"], [match])

            for change in ({"id": "GO-2026-9999"}, {"namespace": "other:language:go"}, {"severity": "High"},
                           {"description": "Revised upstream scope"}):
                changed = {**match, "vulnerability": {**finding, **change}}
                self.assertEqual(len(audit.blocker_findings({"matches": [changed]}, classifier, [])), 1)
            changed = {**match, "artifact": {**artifact, "locations": artifact["locations"] * 2}}
            self.assertEqual(len(audit.blocker_findings({"matches": [changed]}, classifier, [])), 1)

    def test_linked_implementation_or_unavailable_proof_still_blocks(self):
        match = {
            "vulnerability": {"id": "GO-2026-6859", "namespace": "govulndb:language:go", "severity": "Medium",
                              "fix": {"state": "fixed", "versions": ["1.27.4"]}, "dataSource": "https://go.dev/cl/862185",
                              "description": self.description},
            "artifact": {"type": "go-module", "name": "stdlib", "version": "go1.27.3",
                         "locations": [{"path": RUNSC, "accessPath": RUNSC, "layerID": DIGEST}]},
        }
        for result in (None, {"result": {"has_http_body_limit": True}}, {"result": {}}):
            classifier = GVisorFindings(None, "amd64", None, None, {}, {"layers": []}, {"rootfs": {"diff_ids": []}}, {})
            with patch.object(classifier, "prove", return_value=result):
                classified = []
                self.assertEqual(len(audit.blocker_findings({"matches": [match]}, classifier, classified)), 1)
                self.assertEqual(classified, [])

    def test_layer_bytes_must_match_sbom_and_qualified_payload(self):
        expected = b"locked executable"
        substituted = b"other  executable"
        digest = sha256(expected).hexdigest()
        record = {"location": {"path": RUNSC, "layerID": DIGEST},
                  "digests": [{"algorithm": "sha256", "value": digest}]}
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            blobs = root / "blobs" / "sha256"
            blobs.mkdir(parents=True)
            blob = blobs / DIGEST.removeprefix("sha256:")
            with tarfile.open(blob, "w:gz") as archive:
                item = tarfile.TarInfo("./" + RUNSC.lstrip("/"))
                item.size = len(substituted)
                archive.addfile(item, BytesIO(substituted))
            classifier = GVisorFindings(None, "amd64", root, root, {"files": [record]},
                                       {"layers": [{"digest": DIGEST}]}, {"rootfs": {"diff_ids": [DIGEST]}}, {})
            with patch.dict(SUBJECTS, {"amd64": {RUNSC: (digest, len(expected))}}), patch("subprocess.run") as run:
                self.assertIsNone(classifier.prove(RUNSC, DIGEST))
                run.assert_not_called()


def image_lock() -> str:
    return "".join(f"{key}={audit.REPOSITORY}{name}@{DIGEST}\n" for key, name in audit.IMAGES.items())


def platform(arch: str) -> dict:
    return {"platform": {"os": "linux", "architecture": arch},
            "mediaType": MANIFEST_TYPE, "digest": DIGEST, "size": 42}


def finding(severity: str, fix_versions: list[str]) -> dict:
    return {"vulnerability": {"id": "example", "severity": severity, "fix": {"versions": fix_versions}},
            "artifact": {"name": "example-package"}}


class DeliveryImageAuditTests(unittest.TestCase):
    def test_incomplete_or_duplicate_image_lock_fails(self):
        with tempfile.TemporaryDirectory() as directory:
            lock = Path(directory) / "images.lock"
            rows = image_lock().splitlines()
            for malformed in ("\n".join(rows[:-1]), "\n".join([*rows, rows[0]])):
                lock.write_text(malformed)
                with self.assertRaises(ValueError):
                    audit.read_lock(lock)

    def test_wrong_release_attempt_stops_before_calling_auditors(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            lock = root / "images.lock"
            lock.write_text(image_lock())
            binding = root / "binding.json"
            audit.write_json(binding, audit.lock_binding(lock, SOURCE, 12, 2))
            expected = audit.lock_binding(lock, SOURCE, 12, 1)
            args = argparse.Namespace(output=root / "result", binding=binding)
            with patch.object(audit, "scan") as scanner:
                returned = audit.audit(args, expected)
                scanner.assert_not_called()
            result = json.loads((args.output / "summary.json").read_text())
            self.assertEqual(returned, 1)
            self.assertFalse(result["complete"])
            self.assertIn("not from this release run", result["error"])

    def test_missing_or_duplicate_platform_fails(self):
        for descriptors in ([platform("amd64")], [platform("amd64"), platform("amd64"), platform("arm64")]):
            with self.assertRaises(ValueError):
                audit.platform_manifests({"schemaVersion": 2, "mediaType": audit.INDEX, "manifests": descriptors})

    def test_registry_digest_failure_never_uses_partial_content(self):
        with tempfile.TemporaryDirectory() as directory:
            cache = Path(directory)
            registry = audit.Registry(audit.REPOSITORY + "controld", cache)
            with patch.object(registry, "open", return_value=BytesIO(b"unexpected")):
                with self.assertRaisesRegex(ValueError, "digest mismatch"):
                    registry.fetch("blobs", DIGEST, 16, 10)
            self.assertFalse((cache / DIGEST.removeprefix("sha256:")).exists())

    def test_config_platform_and_layer_diff_id_are_verified(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            cache = root / "cache"
            cache.mkdir()
            registry = audit.Registry(audit.REPOSITORY + "controld", cache)
            target = root / "evidence"
            target.mkdir()

            def store(value, media_type):
                data = value if isinstance(value, bytes) else json.dumps(value).encode()
                digest = sha256(data).hexdigest()
                (cache / digest).write_bytes(data)
                return {"digest": "sha256:" + digest, "size": len(data), "mediaType": media_type}

            layer = store(b"diff bytes", "application/vnd.oci.image.layer.v1.tar")
            for arch, diff_id, error in (("arm64", layer["digest"], "platform mismatch"),
                                         ("amd64", DIGEST, "layer diff ID mismatch")):
                config = store({"os": "linux", "architecture": arch, "rootfs": {"type": "layers", "diff_ids": [diff_id]}},
                               "application/vnd.oci.image.config.v1+json")
                manifest = store({"schemaVersion": 2, "mediaType": MANIFEST_TYPE, "config": config, "layers": [layer]},
                                 MANIFEST_TYPE)
                with self.assertRaisesRegex(ValueError, error):
                    audit.prepare_layout(registry.fetch, manifest, "amd64", root / "layout", target)
            self.assertFalse((root / "layout").exists())

    def test_stale_future_or_unselected_database_fails(self):
        now = datetime(2026, 11, 28, tzinfo=timezone.utc)
        path = Path("/tmp/example/db")
        valid = {"valid": True, "path": str(path), "built": now.isoformat()}
        audit.verify_database(valid, path, now)
        for update in ({"built": (now - timedelta(days=4)).isoformat()},
                       {"built": (now + timedelta(hours=1)).isoformat()}, {"path": "/tmp/unselected"}):
            with self.assertRaises(ValueError):
                audit.verify_database({**valid, **update}, path, now)

    def test_ignored_high_and_available_fixes_block(self):
        report = {"matches": [finding("Low", []), finding("Medium", ["2.0"])],
                  "ignoredMatches": [finding("High", []), finding("Unknown", [])]}
        blockers = audit.blocker_findings(report)
        self.assertEqual({blocker["severity"] for blocker in blockers}, {"Medium", "High", "Unknown"})

    def test_scanner_failure_retains_raw_output(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            command = ["/bin/sh", "-c", "echo report; echo scanner-error >&2; exit 1"]
            with self.assertRaisesRegex(ValueError, "did not complete"):
                audit.scan(command, {}, root, "failed-scan")
            self.assertEqual((root / "failed-scan.json").read_text(), "report\n")
            self.assertEqual((root / "failed-scan.stderr").read_text(), "scanner-error\n")


if __name__ == "__main__":
    unittest.main()
