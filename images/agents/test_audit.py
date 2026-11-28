from contextlib import redirect_stdout
from hashlib import sha256
import io
import json
from pathlib import Path
import sys
import tarfile
import tempfile
import unittest
from unittest.mock import patch

import audit
from audit import LocalArchive, read_receipts
from build import source_hashes
from delivery_image_audit import TOOL_VERSIONS
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


class InventoryReuseTests(unittest.TestCase):
    def setUp(self):
        self.root = Path(self.enterContext(tempfile.TemporaryDirectory()))
        self.prior = self.root / 'prior'
        self.images = {target: self.image_fixture(target) for target in ('base', 'toolchain')}
        self.build_receipts = self.root / 'build-receipts.json'
        hashes = source_hashes(ROOT)
        self.receipts = [
            {'target': target, 'platform': 'linux/amd64', 'source_sha256': hashes,
             'image_id': image['image_id'], 'apt_refresh_epoch': '123', 'exit_code': 0}
            for target, image in self.images.items()
        ]
        audit.write_json(self.build_receipts, self.receipts)
        self.database = self.root / 'vulnerability.db'
        self.database.write_bytes(b'first database')
        self.failing_target = None
        self.scanner = self.enterContext(patch.object(audit, 'scan', side_effect=self.scan))
        self.enterContext(patch.object(audit, 'scanner_inputs', side_effect=self.scanner_inputs))
        self.inspect = self.enterContext(patch.object(audit.subprocess, 'check_output', side_effect=self.inspect_image))
        self.export = self.enterContext(patch.object(audit.subprocess, 'run', side_effect=self.export_image))
        code, self.original, self.original_digest = self.run_audit(self.prior)
        self.assertEqual(code, 0, self.original)
        self.assertTrue(self.original['complete'])
        self.assertEqual(self.export.call_count, len(self.images))
        self.assertCountEqual(self.calls(), ['syft', 'grype', 'syft', 'grype'])
        self.export.reset_mock()
        self.inspect.reset_mock()
        self.scanner.reset_mock()

    @staticmethod
    def image_fixture(target):
        blobs = {}

        def describe(value, media_type):
            raw = value if isinstance(value, bytes) else json.dumps(value).encode()
            digest = sha256(raw).hexdigest()
            blobs[f'blobs/sha256/{digest}'] = raw
            return {'mediaType': media_type, 'digest': 'sha256:' + digest, 'size': len(raw)}

        layer = describe(target.encode(), 'application/vnd.oci.image.layer.v1.tar')
        platform = {'os': 'linux', 'architecture': 'amd64'}
        config = describe({**platform, 'rootfs': {'type': 'layers', 'diff_ids': [layer['digest']]}},
                          'application/vnd.oci.image.config.v1+json')
        manifest = describe({'schemaVersion': 2, 'mediaType': 'application/vnd.oci.image.manifest.v1+json',
                             'config': config, 'layers': [layer]},
                            'application/vnd.oci.image.manifest.v1+json')
        manifest['platform'] = platform
        index = describe({'schemaVersion': 2, 'mediaType': 'application/vnd.oci.image.index.v1+json',
                          'manifests': [manifest]}, 'application/vnd.oci.image.index.v1+json')
        blobs['index.json'] = blobs.pop('blobs/sha256/' + index['digest'].removeprefix('sha256:'))
        return {'image_id': index['digest'], 'manifest': manifest, 'config': config['digest'],
                'layer': layer['digest'], 'blobs': blobs}

    def image_for_id(self, image_id):
        return next(image for image in self.images.values() if image['image_id'] == image_id)

    def inspect_image(self, command, **_):
        self.assertEqual(command[1:3], ['image', 'inspect'])
        image = self.image_for_id(command[-1])
        selected_platform = '--platform' in command
        identity = image['manifest']['digest'] if selected_platform else image['image_id']
        return json.dumps([{'Id': identity, 'Os': 'linux', 'Architecture': 'amd64',
                            'Descriptor': image['manifest'], 'RootFS': {'Layers': [image['layer']]}}]).encode()

    def export_image(self, command, **_):
        self.assertEqual(command[1:3], ['image', 'save'])
        image = self.image_for_id(command[-1])
        with tarfile.open(command[command.index('-o') + 1], 'w') as archive:
            for filename, raw in image['blobs'].items():
                member = tarfile.TarInfo(filename)
                member.size = len(raw)
                archive.addfile(member, io.BytesIO(raw))

    def scanner_inputs(self, output, _tools, _database, summary):
        status = {'valid': True, 'built': self.database.read_text(), 'path': str(self.database)}
        summary['database'] = {'digest': audit.file_digest(self.database), 'built': status['built']}
        summary['tools'] = {name: {'version': version} for name, version in TOOL_VERSIONS.items()}
        return self.database, {'syft': 'syft', 'grype': 'grype'}, {}, output / 'scanner-config.yaml', status

    def scan(self, command, _env, output, name):
        image = self.images[output.name]
        metadata = {'manifestDigest': image['manifest']['digest'], 'imageID': image['config'],
                    'layers': [{'digest': image['layer']}]}
        descriptor = {'name': name, 'version': TOOL_VERSIONS[name]}
        if name == 'syft':
            report = {'descriptor': descriptor, 'source': {'type': 'image', 'metadata': metadata},
                      'artifacts': [{'name': 'fixture-package'}]}
        else:
            self.assertEqual(command[1], f'sbom:{output / "syft.json"}')
            if output.name == self.failing_target:
                (output / 'grype.json').write_bytes(b'partial scanner output')
                raise ValueError('grype did not complete')
            descriptor['configuration'] = {'search': {'scope': 'squashed'}}
            descriptor['db'] = {'status': {'valid': True, 'built': self.database.read_text(),
                                           'path': str(self.database)}}
            matches = [{'vulnerability': {'severity': 'High'}}] if self.database.read_bytes() == b'new database' else []
            report = {'descriptor': descriptor, 'source': {'type': 'image', 'target': metadata},
                      'matches': matches, 'ignoredMatches': []}
        audit.write_json(output / f'{name}.json', report)
        return report

    def calls(self):
        return [call.args[3] for call in self.scanner.call_args_list]

    def run_audit(self, output, *, reuse_digest=None):
        argv = ['audit.py', '--targets', 'base,toolchain', '--receipts', str(self.build_receipts),
                '--tools', str(self.root), '--database', str(self.root), '--output', str(output)]
        if reuse_digest is not None:
            argv += ['--reuse-inventory', str(self.prior), '--inventory-digest', reuse_digest]
        stdout = io.StringIO()
        with patch.object(sys, 'argv', argv), redirect_stdout(stdout):
            code = audit.main()
        emitted = json.loads(stdout.getvalue())
        path = output / 'summary.json'
        self.assertEqual(emitted['summary_digest'], audit.file_digest(path))
        self.assertEqual(Path(emitted['report']), path)
        return code, json.loads(path.read_text()), emitted['summary_digest']

    def snapshot(self):
        return {path.relative_to(self.prior): path.read_bytes()
                for path in self.prior.rglob('*') if path.is_file()}

    def test_reuse_scans_the_copied_inventory_with_the_new_database(self):
        prior = self.snapshot()
        output = self.root / 'rescan'
        self.database.write_bytes(b'new database')
        code, report, _ = self.run_audit(output, reuse_digest=self.original_digest)
        self.assertEqual(code, 0, report)
        self.assertTrue(report['complete'])
        self.assertNotEqual(report['database']['digest'], self.original['database']['digest'])
        for row in report['images']:
            sbom = output / row['target'] / 'syft.json'
            self.assertEqual(sbom.read_bytes(), prior[Path(row['target']) / 'syft.json'])
            self.assertEqual(row['sbom_digest'], audit.file_digest(sbom))
            self.assertEqual(row['matches'], 1)
            self.assertEqual(row['severity_counts']['High'], 1)
        self.assertEqual(self.calls(), ['grype', 'grype'])
        self.export.assert_not_called()
        self.inspect.assert_not_called()
        self.assertEqual(self.snapshot(), prior)

    def test_rejects_wrong_digest_and_invalid_prior_scope(self):
        summary = self.prior / 'summary.json'
        base, toolchain = self.original['images']
        variants = {
            'wrong-digest': self.original,
            'incomplete-audit': {**self.original, 'complete': False},
            'incomplete-target': {**self.original, 'images': [{**base, 'complete': False}, toolchain]},
            'missing-target': {**self.original, 'images': [base]},
            'different-platform': {**self.original, 'images': [{**base, 'platform': 'linux/arm64'}, toolchain]},
            'different-build': {**self.original, 'build_receipts_digest': 'sha256:' + '9' * 64},
        }
        for name, changed in variants.items():
            with self.subTest(name=name):
                audit.write_json(summary, changed)
                digest = 'sha256:' + '0' * 64 if name == 'wrong-digest' else audit.file_digest(summary)
                prior = self.snapshot()
                code, report, _ = self.run_audit(self.root / name, reuse_digest=digest)
                self.assertEqual(code, 1)
                self.assertFalse(report['complete'])
                self.assertEqual(self.snapshot(), prior)
        self.scanner.assert_not_called()
        self.export.assert_not_called()

    def test_rejects_modified_absent_or_wrong_subject_sbom(self):
        summary = self.prior / 'summary.json'
        base = self.prior / 'base' / 'syft.json'
        original = base.read_bytes()
        other_sbom = (self.prior / 'toolchain' / 'syft.json').read_bytes()
        for name, raw in (('changed-bytes', original.replace(b'fixture-package', b'fixture-PackagE')),
                          ('no-legacy-digest', original), ('different-subject', other_sbom)):
            with self.subTest(name=name):
                base.write_bytes(raw)
                records = [row.copy() for row in self.original['images']]
                base_record = next(row for row in records if row['target'] == 'base')
                if name == 'no-legacy-digest':
                    base_record.pop('sbom_digest')
                if name == 'different-subject':
                    base_record['sbom_digest'] = audit.file_digest(base)
                audit.write_json(summary, {**self.original, 'images': records})
                prior = self.snapshot()
                code, report, _ = self.run_audit(self.root / name, reuse_digest=audit.file_digest(summary))
                self.assertEqual(code, 1)
                self.assertFalse(report['complete'])
                self.assertNotIn('grype', [call.args[3] for call in self.scanner.call_args_list
                                          if call.args[2].name == 'base'])
                self.assertEqual(self.snapshot(), prior)
                self.scanner.reset_mock()
        self.export.assert_not_called()

    def test_failing_rescan_target_remains_incomplete(self):
        self.failing_target = 'toolchain'
        prior = self.snapshot()
        output = self.root / 'failed-rescan'
        code, report, _ = self.run_audit(output, reuse_digest=self.original_digest)
        self.assertEqual(code, 1)
        self.assertFalse(report['complete'])
        self.assertEqual({row['target']: row['complete'] for row in report['images']},
                         {'base': True, 'toolchain': False})
        self.assertEqual((output / 'toolchain' / 'grype.json').read_bytes(), b'partial scanner output')
        self.assertEqual(self.snapshot(), prior)
        self.assertEqual(self.calls(), ['grype', 'grype'])
        self.export.assert_not_called()


if __name__ == '__main__':
    unittest.main()
