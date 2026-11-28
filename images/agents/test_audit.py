import io
import json
from pathlib import Path
import tarfile
import tempfile
import unittest

from audit import LocalArchive, read_receipts
from build import source_hashes
from install import ROOT


class ReceiptTests(unittest.TestCase):
    def test_receipt_requires_each_build_and_immutable_id(self):
        record = {
            'target': 'base', 'platform': 'linux/amd64', 'source_sha256': source_hashes(ROOT),
            'image_id': 'sha256:' + 'a' * 64, 'apt_refresh_epoch': '123', 'exit_code': 0,
        }
        with tempfile.TemporaryDirectory() as directory:
            receipts = Path(directory) / 'build-receipts.json'
            receipts.write_text(json.dumps([record]))
            self.assertEqual(read_receipts(receipts, ['base'], 'linux/amd64')['base'], record)
            with self.assertRaisesRegex(ValueError, 'missing targets'):
                read_receipts(receipts, ['base', 'toolchain'], 'linux/amd64')
            receipts.write_text(json.dumps([{**record, 'image_id': 'agent:latest'}]))
            with self.assertRaisesRegex(ValueError, 'immutable image ID'):
                read_receipts(receipts, ['base'], 'linux/amd64')

    def test_receipt_requires_current_recipe(self):
        record = {'target': 'base', 'platform': 'linux/amd64', 'source_sha256': {'Dockerfile': 'old'}}
        with tempfile.TemporaryDirectory() as directory:
            receipts = Path(directory) / 'build-receipts.json'
            receipts.write_text(json.dumps([record]))
            with self.assertRaisesRegex(ValueError, 'source or platform'):
                read_receipts(receipts, ['base'], 'linux/amd64')


class LocalArchiveTests(unittest.TestCase):
    def test_export_must_match_the_inspected_platform_manifest(self):
        descriptor = {
            'mediaType': 'application/vnd.oci.image.manifest.v1+json',
            'digest': 'sha256:' + 'a' * 64, 'size': 1,
            'platform': {'os': 'linux', 'architecture': 'amd64'},
        }
        index = json.dumps({
            'schemaVersion': 2, 'mediaType': 'application/vnd.oci.image.index.v1+json',
            'manifests': [descriptor],
        }).encode()
        buffer = io.BytesIO()
        with tarfile.open(fileobj=buffer, mode='w') as archive:
            entry = tarfile.TarInfo('index.json')
            entry.size = len(index)
            archive.addfile(entry, io.BytesIO(index))
        buffer.seek(0)
        with tarfile.open(fileobj=buffer, mode='r:') as archive:
            source = LocalArchive(archive, None)
            self.assertEqual(source.manifest(descriptor, 'linux', 'amd64'), descriptor)
            with self.assertRaisesRegex(ValueError, 'platform does not match'):
                source.manifest(descriptor, 'linux', 'arm64')
            with self.assertRaisesRegex(ValueError, 'manifest does not match'):
                source.manifest({**descriptor, 'digest': 'sha256:' + 'b' * 64}, 'linux', 'amd64')


if __name__ == '__main__':
    unittest.main()
