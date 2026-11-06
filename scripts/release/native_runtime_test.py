#!/usr/bin/env python3
"""Archive boundary tests use synthetic ELF headers, not qualified runtime binaries."""

from __future__ import annotations

import copy
import io
import json
import pathlib
import struct
import subprocess
import sys
import tarfile
import tempfile
import unittest

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
        self.gvisor_digest = runtime.file_digest(self.stage / "runtime/gvisor.tar.bz2", "sha512")
        lock = ("AXERN_GVISOR_RELEASE=20260914.0\n"
                f"AXERN_GVISOR_COMMIT={'a' * 40}\n"
                f"AXERN_GVISOR_SHA512_AMD64={self.gvisor_digest}\n")
        (self.stage / "share/axern/gvisor.lock").write_text(lock)
        (self.stage / "requirements.json").write_bytes(runtime.json_bytes(runtime.REQUIREMENTS))
        (self.stage / "share/axern/registry_auths.json").write_bytes(b"{}\n")
        self.metadata = {
            "architecture": "amd64", "version": "0.12.1", "source_revision": "b" * 40,
            "build_host": {"profile": runtime.REQUIREMENTS["profile"], "libc": ["glibc", "2.39"]},
            "gvisor": runtime.parse_lock(lock, "amd64"),
        }

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
        self.metadata["architecture"] = "arm64"
        self.assertEqual(runtime.verify_bundle(self.archive, self.package())["architecture"], "arm64")

    def test_extra_file_is_not_packaged(self) -> None:
        (self.stage / "secret.pem").write_text("not a payload input")
        with self.assertRaisesRegex(runtime.BundleError, "inventory mismatch"):
            self.package()

    def test_missing_required_file(self) -> None:
        (self.stage / "bin/controld-retention").unlink()
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
        (self.stage / "runtime/gvisor.tar.bz2").write_bytes(b"different upstream archive")
        with self.assertRaisesRegex(runtime.BundleError, "gVisor archive"):
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


if __name__ == "__main__":
    unittest.main()
