#!/usr/bin/env python3
"""Archive boundary tests use synthetic ELF headers, not qualified runtime binaries."""

from __future__ import annotations

import copy
import concurrent.futures
import io
import json
import marshal
import importlib.util
import pathlib
import struct
import shutil
import subprocess
import sys
import tarfile
import tempfile
import unittest
from unittest import mock

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))
import native_runtime as runtime


class NativeRuntimeTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = pathlib.Path(self.temporary.name)
        self.stage = self.root / "stage"
        self.archive = self.root / "runtime.tar.gz"
        header = bytearray(64)
        header[:7] = b"\x7fELF\x02\x01\x01"
        struct.pack_into("<HH", header, 16, 2, 62)
        for name in runtime.PAYLOAD_PATHS:
            path = self.stage / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(header if name in runtime.EXECUTABLES else b"fixture")
        self.gvisor_digest = "c" * 128
        lock = ("AXERN_GVISOR_RELEASE=20261102.0\n"
                f"AXERN_GVISOR_COMMIT={'a' * 40}\n"
                f"AXERN_GVISOR_SHA512_AMD64={self.gvisor_digest}\n")
        (self.stage / "share/axern/gvisor.lock").write_text(lock)
        self.selected_lock = {
            "schema": runtime.gvisor_payload.SCHEMA, "profile": runtime.gvisor_payload.PROFILE,
            "release": "20261102.0", "commit": "a" * 40,
            "architectures": {arch: {"archive_sha512": self.gvisor_digest, "members": {
                name: {"size": 64, "mode": 0o755, "sha256": runtime.file_digest(self.stage / ("runtime/gvisor/" + name))}
                for name in runtime.gvisor_payload.MEMBERS}} for arch in ("amd64", "arm64")},
        }
        self.write_selected_lock()
        (self.stage / "requirements.json").write_bytes(runtime.json_bytes(runtime.REQUIREMENTS))
        (self.stage / "share/axern/registry_auths.json").write_bytes(b"{}\n")
        self.metadata = {
            "architecture": "amd64", "version": "0.12.1", "source_revision": "b" * 40,
            "build_host": {"profile": runtime.REQUIREMENTS["profile"], "libc": ["glibc", "2.39"]},
            "gvisor": runtime.parse_lock(lock, "amd64"),
        }

    def write_selected_lock(self):
        (self.stage / "share/axern/gvisor-selected.lock.json").write_bytes(runtime.json_bytes(self.selected_lock))

    def package(self) -> str:
        return runtime.create_bundle(self.stage, self.archive, self.metadata, 1234567890)

    def rewrite(self, transform) -> pathlib.Path:
        self.package()
        altered = self.root / "altered.tar.gz"
        with tarfile.open(self.archive, "r:gz") as source:
            entries = [(member, source.extractfile(member).read()) for member in source]
        entries = transform(entries)
        with tarfile.open(altered, "w:gz") as destination:
            for member, data in entries:
                destination.addfile(member, io.BytesIO(data) if member.isfile() else None)
        return altered

    def reject_altered(self, transform) -> None:
        altered = self.rewrite(transform)
        with self.assertRaises(runtime.BundleError):
            runtime.verify_bundle(altered, runtime.file_digest(altered))

    def test_packaged_staging_and_install_helpers_load_under_isolated_python(self):
        scripts = self.root / 'release/libexec/axern'
        scripts.mkdir(parents=True)
        for name in ('native_runtime.py', 'native_install.py', 'gvisor_payload.py'):
            source = runtime.ROOT / runtime.SOURCE_FILES['libexec/axern/' + name]
            shutil.copyfile(source, scripts / name)
        for name in ('native_runtime.py', 'native_install.py'):
            with self.subTest(helper=name):
                result = subprocess.run([sys.executable, '-I', str(scripts / name), '--help'],
                    cwd=self.root, capture_output=True, text=True, timeout=10)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertIn('usage:', result.stdout)
        self.assertFalse((scripts / '__pycache__').exists(), 'bootstrap must not mutate the immutable payload')
        # A timestamp-valid stale cache must never substitute for checked source.
        selector = scripts / 'gvisor_payload.py'
        code = compile('raise RuntimeError("inert stale bytecode must never run")', str(selector), 'exec')
        stale = importlib.util.MAGIC_NUMBER + struct.pack('<III', 0, int(selector.stat().st_mtime), selector.stat().st_size) + marshal.dumps(code)
        cache = pathlib.Path(importlib.util.cache_from_source(str(selector)))
        cache.parent.mkdir()
        cache.write_bytes(stale)
        for name in ('native_runtime.py', 'native_install.py'):
            with self.subTest(helper=name, cache='untrusted-bytecode'):
                result = subprocess.run([sys.executable, '-I', str(scripts / name), '--help'],
                    cwd=self.root, capture_output=True, text=True, timeout=10)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(cache.read_bytes(), stale)
        (scripts / 'gvisor_payload.py').unlink()
        result = subprocess.run([sys.executable, '-I', str(scripts / 'native_runtime.py'), '--help'],
            cwd=self.root, capture_output=True, text=True, timeout=10)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('matching gvisor_payload.py staging tool is required', result.stderr)

    def test_roundtrip_and_deterministic_packaging(self) -> None:
        checksum = self.package()
        manifest = runtime.verify_bundle(self.archive, checksum)
        self.assertEqual(manifest["source_revision"], self.metadata["source_revision"])
        self.assertEqual({entry["path"] for entry in manifest["files"]}, runtime.PAYLOAD_PATHS)
        second = self.root / "second.tar.gz"
        self.assertEqual(runtime.create_bundle(self.stage, second, self.metadata, 1234567890), checksum)

    def test_all_executable_architectures_checked(self) -> None:
        for name in runtime.EXECUTABLES:
            with self.subTest(name=name):
                path = self.stage / name
                original = path.read_bytes()
                other_arch = bytearray(original)
                struct.pack_into("<H", other_arch, 18, 183)
                path.write_bytes(other_arch)
                with self.assertRaises(runtime.BundleError):
                    self.package()
                self.assertFalse(self.archive.exists())
                path.write_bytes(original)

    def test_arm64_is_distinct_from_amd64(self) -> None:
        for name in runtime.EXECUTABLES:
            path = self.stage / name
            header = bytearray(path.read_bytes())
            struct.pack_into("<H", header, 18, 183)
            path.write_bytes(header)
        lock = self.stage / "share/axern/gvisor.lock"
        lock.write_text(lock.read_text().replace("SHA512_AMD64", "SHA512_ARM64"))
        for name, record in self.selected_lock["architectures"]["arm64"]["members"].items():
            record["sha256"] = runtime.file_digest(self.stage / ("runtime/gvisor/" + name))
        self.write_selected_lock()
        self.metadata["architecture"] = "arm64"
        self.assertEqual(runtime.verify_bundle(self.archive, self.package())["architecture"], "arm64")

    def test_extra_file_is_not_packaged(self) -> None:
        (self.stage / "secret.pem").write_text("not a payload input")
        with self.assertRaisesRegex(runtime.BundleError, "inventory mismatch"):
            self.package()

    def test_missing_required_file(self) -> None:
        (self.stage / "bin/controld").unlink()
        with self.assertRaises(runtime.BundleError):
            self.package()

    def test_symlink_is_rejected(self) -> None:
        path = self.stage / "LICENSE"
        path.unlink()
        path.symlink_to(self.stage / "NOTICE")
        with self.assertRaises(runtime.BundleError):
            self.package()

    def test_existing_output_is_preserved(self) -> None:
        self.archive.write_bytes(b"keep me")
        with self.assertRaises(runtime.BundleError):
            self.package()
        self.assertEqual(self.archive.read_bytes(), b"keep me")

    def test_gvisor_pin_mismatch_does_not_publish(self) -> None:
        (self.stage / "runtime/gvisor/runsc").write_bytes((self.stage / "runtime/gvisor/runsc").read_bytes() + b"changed")
        with self.assertRaisesRegex(runtime.BundleError, "selected gVisor payload"):
            self.package()
        self.assertFalse(self.archive.exists())

    def test_registry_credentials_are_rejected(self) -> None:
        (self.stage / "share/axern/registry_auths.json").write_text('{"registry.example": {"Auth": "not-allowed"}}')
        with self.assertRaisesRegex(runtime.BundleError, "credential template"):
            self.package()
        self.assertFalse(self.archive.exists())

    def test_outer_checksum_is_required_and_checked(self) -> None:
        self.package()
        for checksum in ("", "bad", "0" * 64):
            with self.subTest(checksum=checksum), self.assertRaises(runtime.BundleError):
                runtime.verify_bundle(self.archive, checksum)

    def test_internal_file_corruption(self) -> None:
        def corrupt(entries):
            member, data = entries[-1]
            entries[-1] = member, b"!" + data[1:]
            return entries
        self.reject_altered(corrupt)

    def test_missing_archive_member(self) -> None:
        self.reject_altered(lambda entries: entries[:-1])

    def test_duplicate_archive_member(self) -> None:
        self.reject_altered(lambda entries: entries + [entries[-1]])

    def test_path_traversal_is_rejected_without_extraction(self) -> None:
        def traversal(entries):
            member = tarfile.TarInfo("../outside")
            member.size = 4
            return entries + [(member, b"nope")]
        self.reject_altered(traversal)
        self.assertFalse((self.root / "outside").exists())

    def test_symlink_and_device_archive_members(self) -> None:
        for kind in (tarfile.SYMTYPE, tarfile.LNKTYPE, tarfile.CHRTYPE):
            with self.subTest(kind=kind):
                if self.archive.exists():
                    self.archive.unlink()
                def replace(entries):
                    member = copy.copy(entries[-1][0])
                    member.type, member.size, member.linkname = kind, 0, "/etc/passwd"
                    entries[-1] = member, b""
                    return entries
                self.reject_altered(replace)

    def test_duplicate_manifest_key(self) -> None:
        def duplicate(entries):
            member, data = entries[0]
            data = b'{"os": "linux",' + data[1:]
            member.size = len(data)
            entries[0] = member, data
            return entries
        self.reject_altered(duplicate)

    def test_mismatched_manifest_architecture(self) -> None:
        def rewrite_arch(entries):
            member, data = entries[0]
            manifest = json.loads(data)
            manifest["architecture"] = "arm64"
            data = runtime.json_bytes(manifest)
            member.size = len(data)
            entries[0] = member, data
            return entries
        self.reject_altered(rewrite_arch)

    def test_dirty_and_untracked_source_rejected(self) -> None:
        source = self.root / "source"
        source.mkdir()
        def git(*args):
            return subprocess.run(["git", *args], cwd=source, check=True, capture_output=True)
        git("init")
        (source / "VERSION").write_text("0.12.1\n")
        git("add", "VERSION")
        git("-c", "user.name=Packaging Test", "-c", "user.email=test@example.invalid",
            "-c", "commit.gpgsign=false", "commit", "-m", "fixture")
        revision, _ = runtime.source_identity(source)
        self.assertEqual(len(revision), 40)
        (source / "VERSION").write_text("0.12.2\n")
        with self.assertRaises(runtime.BundleError):
            runtime.source_identity(source)
        (source / "VERSION").write_text("0.12.1\n")
        (source / "untracked").write_text("pending input")
        with self.assertRaises(runtime.BundleError):
            runtime.source_identity(source)

    @unittest.skipUnless(sys.platform == 'linux', 'native staging uses Linux renameat2')
    def test_stage_preserves_payload_modes_and_refuses_existing_destination(self) -> None:
        checksum = self.package()
        destination = self.root / 'installed'
        runtime.stage_bundle(self.archive, checksum, destination)
        self.assertEqual((destination / 'bin/axern').read_bytes(), (self.stage / 'bin/axern').read_bytes())
        self.assertEqual((destination / 'bin/axern').stat().st_mode & 0o777, 0o755)
        self.assertEqual(json.loads((destination / 'stage-receipt.json').read_text())['archive_sha256'], checksum)
        with self.assertRaises(runtime.BundleError):
            runtime.stage_bundle(self.archive, checksum, destination)

    @unittest.skipUnless(sys.platform == 'linux', 'native staging uses Linux renameat2')
    def test_stage_verifies_before_publishing(self) -> None:
        self.package()
        destination = self.root / 'installed'
        with self.assertRaises(runtime.BundleError):
            runtime.stage_bundle(self.archive, '0' * 64, destination)
        self.assertFalse(destination.exists())
        self.assertFalse(list(self.root.glob('.axern-stage-*')))

    @unittest.skipUnless(sys.platform == 'linux', 'native staging uses Linux renameat2')
    def test_stage_uses_verified_snapshot_when_download_changes(self) -> None:
        checksum = self.package()
        verify = runtime.verify_bundle
        def replace_download(path, expected):
            manifest = verify(path, expected)
            self.archive.write_bytes(b'changed after verification')
            return manifest
        destination = self.root / 'installed'
        with mock.patch.object(runtime, 'verify_bundle', side_effect=replace_download):
            runtime.stage_bundle(self.archive, checksum, destination)
        self.assertEqual((destination / 'LICENSE').read_bytes(), (self.stage / 'LICENSE').read_bytes())

    @unittest.skipUnless(sys.platform == 'linux', 'native staging uses Linux renameat2')
    def test_stage_bounds_a_download_that_grows_after_open(self) -> None:
        checksum = self.package()
        original_size = self.archive.stat().st_size
        fstat = runtime.os.fstat
        def grow_after_stat(descriptor):
            info = fstat(descriptor)
            with self.archive.open('ab') as output:
                output.write(b'changed during staging')
            return info
        with mock.patch.object(runtime, 'MAX_TOTAL_SIZE', original_size), mock.patch.object(runtime.os, 'fstat', side_effect=grow_after_stat):
            with self.assertRaisesRegex(runtime.BundleError, 'grew beyond'):
                runtime.stage_bundle(self.archive, checksum, self.root / 'installed')
        self.assertFalse((self.root / 'installed').exists())
        self.assertFalse(list(self.root.glob('.axern-stage-*')))

    @unittest.skipUnless(sys.platform == 'linux', 'native staging uses Linux renameat2')
    def test_stage_rejects_fifo_without_waiting_for_a_writer(self) -> None:
        runtime.os.mkfifo(self.archive)
        with self.assertRaisesRegex(runtime.BundleError, 'regular archive'):
            runtime.stage_bundle(self.archive, '0' * 64, self.root / 'installed')

    @unittest.skipUnless(sys.platform == 'linux', 'native staging uses Linux renameat2')
    def test_concurrent_staging_has_exactly_one_publisher(self) -> None:
        checksum = self.package()
        destination = self.root / 'installed'
        def attempt():
            try:
                runtime.stage_bundle(self.archive, checksum, destination)
                return True
            except (runtime.BundleError, FileExistsError):
                return False
        with concurrent.futures.ThreadPoolExecutor(max_workers=2) as executor:
            self.assertEqual(sum(executor.map(lambda _: attempt(), range(2))), 1)
        self.assertEqual((destination / 'bin/axern').read_bytes(), (self.stage / 'bin/axern').read_bytes())


class SelectedGVisorTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = pathlib.Path(self.temporary.name)
        self.archive = self.root / 'input.tar.bz2'
        self.destination = self.root / 'selected'
        self.header = bytearray(64)
        self.header[:7] = b'\x7fELF\x02\x01\x01'
        struct.pack_into('<HH', self.header, 16, 2, 62)
        self.payloads = {name: bytes(self.header) + name.encode() for name in runtime.gvisor_payload.UPSTREAM_MEMBERS}
        self.members = {name: {'size': len(self.payloads[name]), 'mode': 0o755,
            'sha256': runtime.hashlib.sha256(self.payloads[name]).hexdigest()} for name in runtime.gvisor_payload.MEMBERS}

    def prepare_archive(self, alter=None):
        entries = []
        for name, data in sorted(self.payloads.items()):
            entry = tarfile.TarInfo(name)
            entry.size, entry.mode = len(data), 0o755
            entries.append((entry, data))
        if alter:
            entries = alter(entries)
        with tarfile.open(self.archive, 'w:bz2') as output:
            for entry, data in entries:
                output.addfile(entry, io.BytesIO(data) if entry.isfile() else None)
        return {'release': '20261102.0', 'commit': 'a' * 40,
                'archive_sha512': runtime.file_digest(self.archive, 'sha512')}

    def test_authenticates_complete_input_but_only_delivers_selected_profile(self):
        upstream = self.prepare_archive()
        runtime.gvisor_payload.extract_archive(self.archive, self.destination, self.members, 'amd64', upstream)
        runtime.gvisor_payload.verify_tree(self.destination, self.members, 'amd64')
        self.assertEqual({path.relative_to(self.destination).as_posix() for path in self.destination.rglob('*') if path.is_file()}, runtime.gvisor_payload.MEMBERS)
        for absent in ('containerd-shim-runsc-v1', 'gvisor-bin/checkpointgofer', 'gvisor-bin/runsc-metric-server', 'gvisor.tar.bz2'):
            self.assertFalse((self.destination / absent).exists())

    def test_rejects_input_checksum_before_publication(self):
        upstream = self.prepare_archive()
        upstream['archive_sha512'] = '0' * 128
        with self.assertRaisesRegex(runtime.gvisor_payload.PayloadError, 'SHA512'):
            runtime.gvisor_payload.extract_archive(self.archive, self.destination, self.members, 'amd64', upstream)
        self.assertFalse(self.destination.exists())

    def test_rejects_wrong_selected_member_even_with_new_authenticated_input(self):
        self.payloads['runsc'] += b'changed after lock derivation'
        upstream = self.prepare_archive()
        with self.assertRaises(runtime.gvisor_payload.PayloadError):
            runtime.gvisor_payload.extract_archive(self.archive, self.destination, self.members, 'amd64', upstream)
        self.assertFalse(self.destination.exists())

    def test_rejects_duplicate_traversal_link_device_and_unknown_members(self):
        for variant in ('duplicate', 'traversal', 'symlink', 'device', 'unknown', 'wrong-arch'):
            with self.subTest(variant=variant):
                def alter(entries):
                    entry = copy.copy(entries[-1][0])
                    data = entries[-1][1]
                    if variant == 'duplicate':
                        return entries + [(entry, data)]
                    if variant == 'traversal':
                        entry.name = '../outside'
                    elif variant == 'unknown':
                        entry.name = 'unused-extra'
                    elif variant in ('symlink', 'device'):
                        entry.type = tarfile.SYMTYPE if variant == 'symlink' else tarfile.CHRTYPE
                        entry.size, entry.linkname = 0, '/etc/invalid'
                    else:
                        data = bytearray(data)
                        struct.pack_into('<H', data, 18, 183)
                    return entries[:-1] + [(entry, data)]
                upstream = self.prepare_archive(alter)
                with self.assertRaises(runtime.gvisor_payload.PayloadError):
                    runtime.gvisor_payload.extract_archive(self.archive, self.destination, self.members, 'amd64', upstream)
                self.assertFalse(self.destination.exists())
                self.assertFalse((self.root / 'outside').exists())

    def test_repository_lock_is_consistent_for_both_shipping_architectures(self):
        source = runtime.ROOT / 'runtime/axnoded'
        for arch in runtime.gvisor_payload.MACHINES:
            upstream = runtime.gvisor_payload.upstream_lock(source / 'gvisor.lock', arch)
            selected = runtime.gvisor_payload.parse_selected((source / 'gvisor-selected.lock.json').read_bytes(), arch, upstream)
            self.assertEqual(set(selected), runtime.gvisor_payload.MEMBERS)

    def test_every_delivery_recipe_uses_selected_verification_without_cache_copy_layers(self):
        for relative in ('deploy/images/lib/node-runtime-base.Dockerfile',
                         'runtime/axnoded/docker/benchmark/Dockerfile', 'docker/devbox/Dockerfile'):
            with self.subTest(recipe=relative):
                text = (runtime.ROOT / relative).read_text()
                self.assertNotIn('COPY runtime/axnoded/.cache/gvisor/', text)
                for retired in ('containerd-shim-runsc-v1', 'checkpointgofer', 'runsc-metric-server'):
                    self.assertNotIn(retired, text)
                self.assertIn('gvisor-selected.lock.json', text)
                self.assertIn('gvisor_payload.py extract', text)
                if relative != 'docker/devbox/Dockerfile':
                    self.assertIn('source=runtime/axnoded/.cache/gvisor,target=/opt/gvisor-cache,readonly', text)
                    self.assertIn('gvisor_payload.py copy', text)

    def test_cache_copy_revalidates_bytes_modes_and_rejects_retired_files(self):
        upstream = self.prepare_archive()
        runtime.gvisor_payload.extract_archive(self.archive, self.destination, self.members, 'amd64', upstream)
        for variant in ('modified', 'mode', 'root-mode', 'directory-mode', 'retired'):
            with self.subTest(variant=variant):
                target = self.destination / 'runsc'
                if variant == 'modified':
                    target.write_bytes(self.payloads['runsc'] + b'changed')
                elif variant == 'mode':
                    target.chmod(0o644)
                elif variant == 'root-mode':
                    self.destination.chmod(0o777)
                elif variant == 'directory-mode':
                    (self.destination / 'gvisor-bin').chmod(0o777)
                else:
                    (self.destination / 'containerd-shim-runsc-v1').write_bytes(b'not shipped')
                with self.assertRaises(runtime.gvisor_payload.PayloadError):
                    runtime.gvisor_payload.copy_tree(self.destination, self.root / ('copy-' + variant), self.members, 'amd64')
                target.write_bytes(self.payloads['runsc'])
                target.chmod(0o755)
                self.destination.chmod(0o755)
                (self.destination / 'gvisor-bin').chmod(0o755)


if __name__ == "__main__":
    unittest.main()
