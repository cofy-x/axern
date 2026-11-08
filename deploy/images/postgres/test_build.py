"""Inert integrity and execution-profile tests for the PostgreSQL helper build."""
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import struct
import tarfile
import tempfile
import unittest
from unittest import mock
from unittest import mock
from types import SimpleNamespace

SPEC = importlib.util.spec_from_file_location("postgres_build", Path(__file__).with_name("build.py"))
build = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(build)


class BuildTests(unittest.TestCase):
    def test_build_environment_excludes_credentials_and_ambient_compiler_overrides(self):
        with mock.patch.dict(build.os.environ, {"DEEPSEEK_API_KEY": "inert-fixture", "GOFLAGS": "-overlay=unqualified", "GOEXPERIMENT": "unqualified"}):
            env = build.go_environment("arm64")
        self.assertNotIn("DEEPSEEK_API_KEY", env)
        self.assertEqual(env["GOFLAGS"], "-mod=readonly")
        self.assertEqual(env["GOEXPERIMENT"], "")
        self.assertEqual((env["GOOS"], env["GOARCH"], env["CGO_ENABLED"]), ("linux", "arm64", "0"))

    def source(self, directory, missing=None, link=None, duplicate=None):
        path = directory / "source.tar.gz"
        pins = {"gosu": {"commit": "a" * 40}}
        with tarfile.open(path, "w:gz") as archive:
            for name in build.SOURCE_FILES:
                if name == missing:
                    continue
                item = tarfile.TarInfo("gosu-" + "a" * 40 + "/" + name)
                if name == link:
                    item.type = tarfile.SYMTYPE
                    item.linkname = "/outside"
                    archive.addfile(item)
                else:
                    item.size = 4
                    archive.addfile(item, io.BytesIO(b"test"))
                    if name == duplicate:
                        archive.addfile(item, io.BytesIO(b"test"))
            item = tarfile.TarInfo("../../outside")
            item.size = 4
            archive.addfile(item, io.BytesIO(b"evil"))
        raw = path.read_bytes()
        pins["gosu"].update(size=len(raw), sha256=hashlib.sha256(raw).hexdigest())
        return path, pins

    def test_locked_contract(self):
        pins = build.load_pins()
        self.assertEqual(pins["go_version"], "go1.26.10")
        dockerfile = (build.HERE / "Dockerfile").read_text()
        self.assertIn("apk upgrade --no-cache", dockerfile)
        self.assertIn("'zlib>=1.4-r0'", dockerfile)
        self.assertFalse(any(line.startswith(("ENTRYPOINT", "CMD", "USER", "VOLUME", "ENV PGDATA")) for line in dockerfile.splitlines()))

    def test_only_fixed_source_files_extracted(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            archive, pins = self.source(root)
            target = root / "target"
            target.mkdir()
            build.unpack_source(archive, target, pins)
            self.assertEqual(set(p.name for p in target.iterdir()), set(build.SOURCE_FILES))
            self.assertFalse((root / "outside").exists())

    def test_archive_integrity_precedes_extraction(self):
        for mutation in ("sha256", "size"):
            with self.subTest(mutation=mutation), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                archive, pins = self.source(root)
                pins["gosu"][mutation] = "0" * 64 if mutation == "sha256" else 1
                target = root / "target"
                target.mkdir()
                with self.assertRaisesRegex(ValueError, "integrity"):
                    build.unpack_source(archive, target, pins)
                self.assertEqual(list(target.iterdir()), [])

    def test_required_members_fail_closed(self):
        for change in ("missing", "link", "duplicate"):
            with self.subTest(change=change), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                archive, pins = self.source(root, **{change: "main.go"})
                target = root / "target"
                target.mkdir()
                with self.assertRaises(ValueError):
                    build.unpack_source(archive, target, pins)

    def test_elf_architecture_and_static_contract(self):
        with tempfile.TemporaryDirectory() as temporary:
            target = Path(temporary) / "elf"
            for arch, machine in (("amd64", 62), ("arm64", 183)):
                raw = bytearray(120)
                raw[:7] = b"\x7fELF\x02\x01\x01"
                struct.pack_into("<H", raw, 18, machine)
                struct.pack_into("<Q", raw, 32, 64)
                struct.pack_into("<HH", raw, 54, 56, 1)
                target.write_bytes(raw)
                build.verify_elf(target, arch)
                with self.assertRaisesRegex(ValueError, "architecture"):
                    build.verify_elf(target, "arm64" if arch == "amd64" else "amd64")
                for dynamic_header in (2, 3):
                    struct.pack_into("<I", raw, 64, dynamic_header)
                    target.write_bytes(raw)
                    with self.assertRaisesRegex(ValueError, "static"):
                        build.verify_elf(target, arch)

    def test_prepare_only_preserves_module_locks_without_compiling(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            archive, pins = self.source(root)
            pins["go_version"] = "go1.26.10"
            output = root / "prepared"
            args = SimpleNamespace(go="go", arch=None, output=str(output), source_archive=str(archive), prepare_only=True)
            with mock.patch.object(build, "load_pins", return_value=pins), mock.patch.object(build.shutil, "which", return_value="/trusted/go"), mock.patch.object(build.subprocess, "check_output", return_value="go version go1.26.10 linux/amd64\n"), mock.patch.object(build.subprocess, "run") as compile_command:
                build.build(args)
                compile_command.assert_not_called()
                self.assertEqual((output / "source/go.mod").read_bytes(), (build.HERE / "gosu.mod").read_bytes())
                self.assertEqual((output / "source/go.sum").read_bytes(), (build.HERE / "gosu.sum").read_bytes())
                self.assertFalse((output / "context").exists())
                with self.assertRaisesRegex(ValueError, "already exists"):
                    build.build(args)

    def test_wrong_toolchain_rejected_before_creating_output(self):
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary) / "output"
            args = SimpleNamespace(go="go", arch="amd64", output=str(output))
            with mock.patch.object(build.shutil, "which", return_value="/trusted/go"), mock.patch.object(build.subprocess, "check_output", return_value="go version go1.24.6 linux/amd64\n"):
                with self.assertRaisesRegex(ValueError, "toolchain"):
                    build.build(args)
            self.assertFalse(output.exists())


if __name__ == "__main__":
    unittest.main()
