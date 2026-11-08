"""Host-safe checks for verified inputs and confined build/extraction boundaries."""

import base64
import hashlib
import io
import json
from pathlib import Path
import sys
import tarfile
import tempfile
import unittest
from unittest.mock import patch

import install


def record(data):
    return {'version': '1.0.0', 'url': 'https://example.invalid/tool.tgz', 'size': len(data), 'sha256': hashlib.sha256(data).hexdigest(), 'integrity': 'sha512-' + base64.b64encode(hashlib.sha512(data).digest()).decode()}


class SourceTests(unittest.TestCase):
    def test_corrupt_cached_source_fails_without_network_fallback(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / 'tool.tgz').write_bytes(b'wrong')
            with patch.object(install, 'urlopen') as network:
                with self.assertRaisesRegex(ValueError, 'checksum mismatch'):
                    install.fetch_source(record(b'right'), root / 'output', root)
                network.assert_not_called()
            self.assertFalse((root / 'output').exists())

    def test_sha512_integrity_is_checked_after_valid_sha256(self):
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / 'source'
            path.write_bytes(b'tool')
            expected = record(b'tool')
            expected['integrity'] = 'sha512-invalid'
            with self.assertRaisesRegex(ValueError, 'npm integrity mismatch'):
                install.verify_source(path, expected)

    def test_missing_cache_fetches_exact_locked_public_source(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            with patch.object(install, 'urlopen', return_value=io.BytesIO(b'public artifact')) as network:
                install.fetch_source(record(b'public artifact'), root / 'output', root / 'missing-cache')
                network.assert_called_once_with('https://example.invalid/tool.tgz', timeout=120)
            self.assertEqual((root / 'output').read_bytes(), b'public artifact')

    def test_download_size_overflow_is_rejected(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            with patch.object(install, 'urlopen', return_value=io.BytesIO(b'too long')):
                with self.assertRaisesRegex(ValueError, 'exceeds its locked size'):
                    install.fetch_source(record(b'x'), root / 'output', root / 'missing')

    def test_base_rejects_corrupt_pip_wheel_before_running_the_installer(self):
        lock = {'base': {'python_version': '.'.join(map(str, sys.version_info[:3]))},
                'artifacts': {'pip': record(b'right')}}
        with patch.object(install, 'urlopen', return_value=io.BytesIO(b'wrong')), patch.object(install.subprocess, 'run') as process:
            with self.assertRaisesRegex(ValueError, 'checksum mismatch'):
                install.install('base', lock)
            process.assert_not_called()

    def test_cache_symlinks_are_rejected(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / 'outside').write_bytes(b'tool')
            (root / 'tool.tgz').symlink_to(root / 'outside')
            with self.assertRaisesRegex(ValueError, 'regular file'):
                install.prepare_cache(root, root / 'copy', {'artifacts': {'tool': record(b'tool')}})

    def test_cache_staging_excludes_unlisted_files(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            cache = root / 'cache'
            cache.mkdir()
            (cache / 'tool.tgz').write_bytes(b'tool')
            (cache / 'auth.json').write_text('private placeholder')
            install.prepare_cache(cache, root / 'staged', {'artifacts': {'tool': record(b'tool')}})
            self.assertEqual([p.name for p in (root / 'staged').iterdir()], ['tool.tgz'])

    def test_context_staging_excludes_credentials_and_unlisted_source(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            source = root / 'source'
            source.mkdir()
            for name in install.CONTEXT_FILES:
                (source / name).write_text('public build input')
            for name in ('.env', 'auth.json', 'session.json', 'README.md'):
                (source / name).write_text('private placeholder')
            install.prepare_context(source, root / 'context')
            self.assertEqual({p.name for p in (root / 'context').iterdir()}, set(install.CONTEXT_FILES))

    def test_context_rejects_an_allowlisted_symlink(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            for name in install.CONTEXT_FILES:
                (root / name).write_text('public')
            (root / 'outside').write_text('private')
            (root / 'install.py').unlink()
            (root / 'install.py').symlink_to(root / 'outside')
            with self.assertRaisesRegex(ValueError, 'regular source files'):
                install.prepare_context(root, root / 'context')


class LockTests(unittest.TestCase):
    def rejected_lock(self, edit):
        lock = install.load_lock()
        edit(lock)
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / 'lock.json'
            path.write_text(json.dumps(lock))
            with self.assertRaises(ValueError):
                install.load_lock(path)

    def test_nonhex_base_digest_is_rejected(self):
        self.rejected_lock(lambda lock: lock['base'].update(image='python@sha256:' + 'x' * 64))

    def test_source_credentials_are_rejected(self):
        self.rejected_lock(lambda lock: lock['artifacts']['go'].update(url='https://user:password@go.dev/archive.tgz'))

    def test_source_query_and_fragment_are_rejected(self):
        for tail in ('?credential=placeholder', '#fragment'):
            self.rejected_lock(lambda lock: lock['artifacts']['go'].update(url=lock['artifacts']['go']['url'] + tail))

    def test_unrecognized_distribution_host_is_rejected(self):
        self.rejected_lock(lambda lock: lock['artifacts']['go'].update(url='https://unexpected.invalid/archive.tgz'))


class NodeRuntimeTests(unittest.TestCase):
    def fixture(self, root):
        (root / 'bin').mkdir()
        (root / 'bin/node').write_bytes(b'runtime')
        (root / 'include/node').mkdir(parents=True)
        (root / 'include/node/node.h').write_bytes(b'headers')
        (root / 'LICENSE').write_bytes(b'upstream license')
        for name in ('npm', 'corepack'):
            directory = root / 'lib/node_modules' / name
            directory.mkdir(parents=True)
            (directory / 'bundled-dependency.js').write_bytes(b'old dependency')
        for name in ('npm', 'npx', 'corepack', 'pnpm', 'pnpx', 'yarn', 'yarnpkg'):
            (root / 'bin' / name).symlink_to('../lib/node_modules/corepack/bin/' + name)

    def test_managers_and_their_vendors_are_absent_while_runtime_inputs_survive(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            self.fixture(root)
            install.strip_node_package_managers(root)
            self.assertEqual({p.name for p in (root / 'bin').iterdir()}, {'node'})
            self.assertFalse((root / 'lib/node_modules').exists())
            self.assertEqual((root / 'bin/node').read_bytes(), b'runtime')
            self.assertEqual((root / 'include/node/node.h').read_bytes(), b'headers')
            self.assertEqual((root / 'LICENSE').read_bytes(), b'upstream license')

    def test_unrecognized_bundle_or_executable_requires_recipe_review(self):
        for added in ('bin/new-manager', 'lib/node_modules/new-package'):
            with self.subTest(added=added), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                self.fixture(root)
                (root / added).write_bytes(b'unrecognized')
                with self.assertRaises(ValueError):
                    install.strip_node_package_managers(root)


class ExtractionTests(unittest.TestCase):
    def archive(self, root, name, link=None):
        path = root / 'archive.tgz'
        with tarfile.open(path, 'w:gz') as output:
            member = tarfile.TarInfo(name)
            if link is not None:
                member.type = tarfile.SYMTYPE
                member.linkname = link
                output.addfile(member)
            else:
                member.size = 4
                output.addfile(member, io.BytesIO(b'tool'))
        return path

    def test_parent_member_cannot_escape_extraction_root(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            with self.assertRaisesRegex(ValueError, 'non-local member path'):
                install.extract_source(self.archive(root, '../escaped'), root / 'destination')
            self.assertFalse((root / 'escaped').exists())

    def test_absolute_members_are_rejected_rather_than_rewritten(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            with self.assertRaisesRegex(ValueError, 'non-local member path'):
                install.extract_source(self.archive(root, '/absolute-file'), root / 'destination')

    def test_relative_link_cannot_escape_extraction_root(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            with self.assertRaises(tarfile.LinkOutsideDestinationError):
                install.extract_source(self.archive(root, 'bin/tool', '../../outside'), root / 'destination')

    def test_internal_links_needed_by_official_node_archive_work(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            install.extract_source(self.archive(root, 'node/bin/npm', '../lib/npm.js'), root / 'destination')
            self.assertEqual((root / 'destination/node/bin/npm').readlink(), Path('../lib/npm.js'))


if __name__ == '__main__':
    unittest.main()
