#!/usr/bin/env python3
"""Check notice source integrity independently of Syft and the public module proxy."""

import hashlib
from pathlib import Path
import tempfile
import unittest
from unittest import mock
import zipfile

import native_delivery_notices as notices
from native_runtime import BundleError, json_bytes


class NativeDeliveryNoticesTest(unittest.TestCase):
    def setUp(self):
        root = tempfile.TemporaryDirectory()
        self.addCleanup(root.cleanup)
        self.root = Path(root.name)

    def test_modified_module_fails_locked_go_checksum(self):
        # Known Go h1 for a single file called example.com/library@v1.0.0/LICENSE.
        archive = self.root / "module.zip"
        with zipfile.ZipFile(archive, "w") as source:
            source.writestr("example.com/library@v1.0.0/LICENSE", b"license fixture\n")
        locked = "h1:zeDAGQmXrNbx7NAxpxoH6I+NdoCHifPw0DifK+TSbkg="
        _, texts = notices.module_notices(archive, "example.com/library", "v1.0.0", locked)
        self.assertEqual(texts, {"LICENSE": b"license fixture\n"})
        with zipfile.ZipFile(archive, "w") as source:
            source.writestr("example.com/library@v1.0.0/LICENSE", b"changed fixture\n")
        with self.assertRaisesRegex(BundleError, "disagrees with locked h1"):
            notices.module_notices(archive, "example.com/library", "v1.0.0", locked)

    def test_gvisor_only_bazel_lock_is_a_source(self):
        module = b'''go_deps.module(
    path = "gopkg.in/yaml.v2",
    sum = "h1:D8xgwECY7CYvx+Y2n4sBz93Jn9JRvxdiyyo8CTfuKaY=",
    version = "v2.4.0",
)
'''
        with mock.patch.object(notices, "git", side_effect=[b"", module]):
            result = notices.gvisor_dependencies(self.root, "candidate")
        self.assertEqual(result[("gopkg.in/yaml.v2", "v2.4.0")], "h1:D8xgwECY7CYvx+Y2n4sBz93Jn9JRvxdiyyo8CTfuKaY=")
        with mock.patch.object(notices, "git", side_effect=[b"gopkg.in/yaml.v2 v2.4.0 h1:conflict\n", module]):
            with self.assertRaisesRegex(BundleError, "conflicting gVisor Go locks"):
                notices.gvisor_dependencies(self.root, "candidate")

    def test_spdx_and_cyclonedx_must_match_the_archive_receipt(self):
        files = {
            "runtime.spdx.json": b'{"packages":[{"name":"stdlib"}]}',
            "runtime.cdx.json": b'{"components":[{"name":"stdlib"}]}',
        }
        manifest = {"source_revision": "a" * 40, "version": "0.13.0", "architecture": "amd64"}
        archive_sha = "b" * 64
        for filename, data in files.items():
            (self.root / filename).write_bytes(data)
        receipt = {
            **manifest, "archive_sha256": archive_sha, "schema": "axern.native-delivery-inventory.v1",
            "spdx_package_count": 1,
            "inventory_sha256": {name: hashlib.sha256(data).hexdigest() for name, data in files.items()},
        }
        (self.root / "receipt.json").write_bytes(json_bytes(receipt))
        self.assertEqual(notices.load_inventory(self.root, archive_sha, manifest)[0], [{"name": "stdlib"}])
        with self.assertRaisesRegex(BundleError, "different source or archive"):
            notices.load_inventory(self.root, "c" * 64, manifest)
        (self.root / "runtime.cdx.json").write_bytes(b"changed")
        with self.assertRaisesRegex(BundleError, "sidecar SHA-256 mismatch"):
            notices.load_inventory(self.root, archive_sha, manifest)


if __name__ == "__main__":
    unittest.main()
