import argparse
from datetime import datetime, timedelta, timezone
from hashlib import sha256
from io import BytesIO
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import delivery_image_audit as audit


DIGEST = "sha256:" + "b" * 64
SOURCE = "a" * 40
MANIFEST_TYPE = "application/vnd.oci.image.manifest.v1+json"


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
                    audit.prepare_layout(registry, manifest, "amd64", root / "layout", target)
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
