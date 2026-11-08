"""Host-safe source, identity, cache and static-binary build contracts."""
import contextlib
import difflib
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import shutil
import struct
import sys
import tarfile
import tempfile
import unittest
from unittest import mock

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('registry_build', HERE / 'build.py')
build = importlib.util.module_from_spec(spec)
spec.loader.exec_module(build)


class RegistryBuildTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.recipe = self.root / 'recipe'
        self.recipe.mkdir()
        for name in ['go.mod', 'go.sum', 'artifacts.lock.json', 'entrypoint.patch']:
            shutil.copyfile(HERE / name, self.recipe / name)
        self.patch = mock.patch.object(build, 'HERE', self.recipe)
        self.patch.start()
        self.addCleanup(self.patch.stop)

    def lock_change(self, mutate):
        path = self.recipe / 'artifacts.lock.json'
        value = json.loads(path.read_text())
        mutate(value)
        path.write_text(json.dumps(value))

    def test_check_is_offline_and_creates_no_output(self):
        before = sorted(self.recipe.iterdir())
        with mock.patch.object(sys, 'argv', ['build.py', '--check']), mock.patch.object(build, 'command', side_effect=AssertionError('Go forbidden')), mock.patch.object(build, 'artifact', side_effect=AssertionError('network forbidden')), contextlib.redirect_stdout(io.StringIO()) as output:
            build.main()
        self.assertTrue(json.loads(output.getvalue())['checked'])
        self.assertEqual(before, sorted(self.recipe.iterdir()))

    def test_dependency_overlay_tampering_is_rejected(self):
        with (self.recipe / 'go.sum').open('a') as target:
            target.write('\n')
        with self.assertRaises(ValueError):
            build.load_lock()

    def test_source_identity_and_ca_authorities_are_pinned(self):
        changes = [lambda x: x['distribution'].update(module='example.test/untrusted'), lambda x: x['distribution'].update(url='http://example.test/source'), lambda x: x['distribution'].update(commit='main'), lambda x: x['ca_bundle'].update(url='https://curl.se/ca/cacert.pem'), lambda x: x.update(go_version='1.27.3')]
        original = (self.recipe / 'artifacts.lock.json').read_bytes()
        for mutate in changes:
            (self.recipe / 'artifacts.lock.json').write_bytes(original)
            self.lock_change(mutate)
            with self.assertRaises(ValueError):
                build.load_lock()

    def test_corrupted_or_symlink_cache_never_downloads_or_overwrites(self):
        cache = self.root / 'cache'
        cache.mkdir()
        content = b'verified'
        record = {'url': 'https://example.test/immutable', 'sha256': hashlib.sha256(content).hexdigest(), 'bytes': len(content)}
        target = cache / record['sha256']
        target.write_bytes(b'corrupt!')
        with mock.patch.object(build.urllib.request, 'build_opener', side_effect=AssertionError('fallback forbidden')), self.assertRaises(ValueError):
            build.artifact(record, cache)
        self.assertEqual(target.read_bytes(), b'corrupt!')
        target.unlink()
        outside = self.root / 'outside'
        outside.write_bytes(content)
        target.symlink_to(outside)
        with self.assertRaises(ValueError):
            build.artifact(record, cache)
        self.assertEqual(outside.read_bytes(), content)

    def source_archive(self, entries):
        path = self.root / 'source.tar.gz'
        with tarfile.open(path, 'w:gz') as archive:
            for name, value in entries:
                entry = tarfile.TarInfo(name)
                if value is None:
                    entry.type = tarfile.SYMTYPE
                    entry.linkname = '../../outside'
                    archive.addfile(entry)
                else:
                    entry.size = len(value)
                    archive.addfile(entry, io.BytesIO(value))
        return path

    def test_source_tests_and_foreign_files_preserved_stale_vendor_excluded(self):
        archive = self.source_archive([('upstream/main.go', b'main'), ('upstream/main_test.go', b'test'), ('upstream/platform_windows.go', b'foreign'), ('upstream/vendor/old/dependency.go', b'obsolete')])
        source = self.root / 'source'
        build.extract_source(archive, source, 'upstream')
        self.assertEqual({x.name for x in source.iterdir()}, {'main.go', 'main_test.go', 'platform_windows.go'})

    def test_profile_exclusions_cannot_expand_to_generic_handlers(self):
        self.lock_change(lambda value: value['profile']['excluded_source_paths'].append('registry/handlers'))
        with self.assertRaises(ValueError):
            build.load_lock()

    def test_only_cloud_paths_excluded_inmemory_and_foreign_tests_remain(self):
        archive = self.source_archive([('upstream/registry/storage/driver/azure/driver.go', b'cloud'), ('upstream/registry/storage/driver/s3-aws/driver_test.go', b'cloud'), ('upstream/registry/storage/driver/inmemory/driver_test.go', b'test'), ('upstream/registry/storage/driver/factory/factory_windows.go', b'foreign')])
        source = self.root / 'profile-source'
        build.extract_source(archive, source, 'upstream')
        self.assertFalse((source / 'registry/storage/driver/azure').exists())
        self.assertFalse((source / 'registry/storage/driver/s3-aws').exists())
        self.assertTrue((source / 'registry/storage/driver/inmemory/driver_test.go').exists())
        self.assertTrue((source / 'registry/storage/driver/factory/factory_windows.go').exists())

    def test_entrypoint_patch_cannot_change_program_logic(self):
        original = 'package main\nimport (\n' + ''.join('\t_ "' + name + '"\n' for name in build.REMOVED_ENTRYPOINT_IMPORTS) + '\t_ "github.com/distribution/distribution/v3/registry/storage/driver/filesystem"\n)\nfunc main() {}\n'
        expected = original
        for name in build.REMOVED_ENTRYPOINT_IMPORTS:
            expected = expected.replace('\t_ "' + name + '"\n', '')
        patch = ''.join(difflib.unified_diff(original.splitlines(True), expected.splitlines(True), fromfile='a/cmd/registry/main.go', tofile='b/cmd/registry/main.go', n=0))
        lock = build.load_lock()
        lock['profile']['upstream_entrypoint_sha256'] = hashlib.sha256(original.encode()).hexdigest()
        source = self.root / 'entrypoint-source'
        (source / 'cmd/registry').mkdir(parents=True)
        path = source / 'cmd/registry/main.go'
        path.write_text(original)
        (self.recipe / 'entrypoint.patch').write_text(patch)
        build.apply_source_profile(source, lock)
        self.assertEqual(path.read_text(), expected)
        path.write_text(original)
        (self.recipe / 'entrypoint.patch').write_text(patch + '+unrelated logic\n')
        with self.assertRaises(ValueError):
            build.apply_source_profile(source, lock)

    def test_archive_traversal_links_and_duplicate_files_are_rejected(self):
        for entries in [[('upstream/../../escape', b'bad')], [('upstream/link', None)], [('upstream/main.go', b'one'), ('upstream/main.go', b'two')]]:
            with self.subTest(entries=entries):
                archive = self.source_archive(entries)
                source = self.root / ('source-' + str(len(list(self.root.glob('source-*')))))
                with self.assertRaises((ValueError, OSError)):
                    build.extract_source(archive, source, 'upstream')
                self.assertFalse((self.root / 'escape').exists())

    def elf(self, machine=62, program_type=1):
        ident = b'\x7fELF\x02\x01' + b'\0' * 10
        header = struct.pack('<16sHHIQQQIHHHHHH', ident, 2, machine, 1, 0, 64, 0, 0, 64, 56, 1, 0, 0, 0)
        path = self.root / 'binary'
        path.write_bytes(header + struct.pack('<IIQQQQQQ', program_type, 0, 0, 0, 0, 0, 0, 0))
        return path

    def test_static_elf_identity_rejects_wrong_architecture_or_interpreter(self):
        build.validate_elf(self.elf(), 'amd64')
        with self.assertRaises(ValueError):
            build.validate_elf(self.elf(machine=183), 'amd64')
        with self.assertRaises(ValueError):
            build.validate_elf(self.elf(program_type=3), 'amd64')

    def test_unqualified_toolchain_cannot_create_outputs(self):
        output = self.root / 'output'
        with mock.patch.object(sys, 'argv', ['build.py', '--output', str(output)]), mock.patch.object(build, 'command', return_value='go1.27.3\n'), self.assertRaises(ValueError):
            build.main()
        self.assertFalse(output.exists())

    def test_go_environment_does_not_forward_model_credentials(self):
        with mock.patch.dict(build.os.environ, {'DEEPSEEK_API_KEY': 'fixture-key'}):
            environment = build.go_environment()
        self.assertNotIn('DEEPSEEK_API_KEY', environment)
        self.assertEqual(environment['GOTOOLCHAIN'], 'local')
        self.assertEqual(environment['GOFLAGS'], '-mod=readonly')


if __name__ == '__main__':
    unittest.main()
