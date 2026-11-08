#!/usr/bin/env python3
"""Build and inspect native Linux payloads; never install or start services."""

from __future__ import annotations

import argparse
import ctypes
import gzip
import hashlib
import io
import importlib.util
import json
import os
import pathlib
import platform
import re
import shutil
import stat
import struct
import subprocess
import sys
import tarfile
import tempfile

ROOT = pathlib.Path(__file__).resolve().parents[2]
# Isolated Python (-I) excludes the script directory. Resolve only the fixed
# packaged sibling or this checkout's source file, never CWD/site imports.
_selector = pathlib.Path(__file__).resolve().with_name('gvisor_payload.py')
if not _selector.exists():
    _selector = ROOT / 'runtime/axnoded/gvisor_payload.py'
if _selector.is_symlink() or not _selector.is_file():
    raise ImportError('the matching gvisor_payload.py staging tool is required')
_selector_spec = importlib.util.spec_from_file_location('gvisor_payload', _selector)
gvisor_payload = importlib.util.module_from_spec(_selector_spec)
# Compile the verified source directly: do not load or write unmanifested .pyc.
exec(compile(_selector.read_bytes(), str(_selector), 'exec'), gvisor_payload.__dict__)

SCHEMA = "axern.native-runtime.v3"
MANIFEST = "manifest.json"
MAX_FILE_SIZE = 1024 * 1024 * 1024
MAX_TOTAL_SIZE = 4 * MAX_FILE_SIZE
MACHINES = {"amd64": 62, "arm64": 183}

# Source entrypoints stay owned by their modules. This is the native payload inventory.
GO_BINARIES = {
    "bin/axern": "./apps/cli",
    "bin/controld": "./control/controld/cmd/controld",
    "bin/controld-migrate": "./control/controld/cmd/migrate",
    "bin/controld-access-bootstrap": "./control/controld/cmd/access-bootstrap",
    "bin/controld-workload-pki": "./control/controld/cmd/workload-pki",
    "bin/gatewayd": "./gateway/gatewayd",
    "bin/axnoded": "./runtime/axnoded/cmd/axnoded",
    "bin/axctl": "./runtime/axnoded/axctl",
    "bin/imagemgr": "./runtime/imagemgr/cmd/imagemgr",
    "bin/egressd": "./runtime/egressd/cmd/egressd",
    "bin/egressdctl": "./runtime/egressd/cmd/egressdctl",
    "bin/tunneld": "./runtime/tunneld/cmd/tunneld",
    "bin/node-tunneld": "./runtime/tunneld/cmd/node-tunneld",
    "bin/bpfnetctl": "./network/bpfnet/cmd/bpfnetctl",
    "libexec/axnoded/axern-sandboxd": "./runtime/axnoded/cmd/axern-sandboxd",
    "libexec/axnoded/egress-probe": "./runtime/axnoded/cmd/egress-probe",
    "libexec/axnoded/dns-probe": "./runtime/axnoded/cmd/dns-probe",
    "libexec/axnoded/tunnel-agent": "./runtime/tunneld/cmd/tunnel-agent",
    "share/axern/runtime-selftest/memory-hog": "./runtime/axnoded/cmd/memory-hog",
}
SOURCE_FILES = {
    "libexec/axern/native_install.py": "deploy/standalone/native_install.py",
    "libexec/axern/native_runtime.py": "scripts/release/native_runtime.py",
    "libexec/axern/qualify_native.py": "deploy/standalone/qualify_native.py",
    "libexec/axern/qualify_network.py": "deploy/standalone/qualify_network.py",
    "LICENSE": "LICENSE",
    "NOTICE": "NOTICE",
    "share/axern/gvisor.lock": "runtime/axnoded/gvisor.lock",
    "share/axern/gvisor-selected.lock.json": "runtime/axnoded/gvisor-selected.lock.json",
    "libexec/axern/gvisor_payload.py": "runtime/axnoded/gvisor_payload.py",
    "share/axern/erofs-root/hello.txt": "deploy/images/fixtures/erofs-root/hello.txt",
    "share/axern/erofs-root/etc/runtime-fixture": "deploy/images/fixtures/erofs-root/etc/runtime-fixture",
}
GVISOR_PATHS = {"runtime/gvisor/" + name for name in gvisor_payload.MEMBERS}
EXECUTABLES = set(GO_BINARIES) | GVISOR_PATHS
PAYLOAD_PATHS = EXECUTABLES | set(SOURCE_FILES) | {
    "share/axern/registry_auths.json", "requirements.json",
}
REQUIREMENTS = {
    "profile": "ubuntu-24.04-candidate",
    "qualification": "experimental native installation; platform acceptance is revision-specific",
    "external_packages": [
        "postgresql-16", "ca-certificates", "libc6", "libgcc-s1",
        "busybox-static", "erofs-utils", "iproute2", "iptables",
        "nftables", "kmod", "util-linux", "e2fsprogs", "xfsprogs", "python3", "curl",
    ],
    "host_capabilities": ["systemd", "cgroup-v2-delegation", "runsc-systrap", "mount-and-network-enforcement"],
    "installer_work": [
        "qualify resource enforcement and recovery on each supported host profile",
        "qualify dependencies, licenses and provenance before publication",
        "implement supported upgrades and coordinated backup/restore before durable serving promotion",
    ],
}


class BundleError(ValueError):
    pass


def json_bytes(value: object) -> bytes:
    return (json.dumps(value, sort_keys=True, indent=2) + "\n").encode()


def unique_object(pairs: list[tuple[str, object]]) -> dict:
    result = {}
    for key, value in pairs:
        if key in result:
            raise BundleError(f"duplicate JSON key: {key}")
        result[key] = value
    return result


def read_json(data: bytes) -> dict:
    result = json.loads(data, object_pairs_hook=unique_object)
    if not isinstance(result, dict):
        raise BundleError("expected a JSON object")
    return result


def digest(stream, algorithm: str = "sha256") -> str:
    value = hashlib.new(algorithm)
    while chunk := stream.read(1024 * 1024):
        value.update(chunk)
    return value.hexdigest()


def file_digest(path: pathlib.Path, algorithm: str = "sha256") -> str:
    with path.open("rb") as stream:
        return digest(stream, algorithm)


def check_elf(header: bytes, arch: str, name: str) -> None:
    if (len(header) < 20 or header[:4] != b"\x7fELF" or header[4:7] != b"\x02\x01\x01"
            or struct.unpack_from("<H", header, 16)[0] not in (2, 3)
            or struct.unpack_from("<H", header, 18)[0] != MACHINES[arch]):
        raise BundleError(f"expected a Linux {arch} ELF executable: {name}")


def parse_lock(data: str, arch: str) -> dict:
    values = {}
    for line in data.splitlines():
        if not line or line.startswith("#"):
            continue
        match = re.fullmatch(r"(AXERN_GVISOR_[A-Z0-9_]+)=([A-Za-z0-9.-]+)", line)
        if not match or match[1] in values:
            raise BundleError("invalid or duplicate gVisor lock assignment")
        values[match[1]] = match[2]
    try:
        release = values["AXERN_GVISOR_RELEASE"]
        commit = values["AXERN_GVISOR_COMMIT"]
        checksum = values[f"AXERN_GVISOR_SHA512_{arch.upper()}"]
    except KeyError as error:
        raise BundleError("incomplete gVisor lock") from error
    if not re.fullmatch(r"[0-9a-f]{40}", commit) or not re.fullmatch(r"[0-9a-f]{128}", checksum):
        raise BundleError("invalid gVisor identity/checksum")
    if not re.fullmatch(r"[0-9]{8}\.[0-9]+", release):
        raise BundleError("invalid gVisor release")
    return {"release": release, "commit": commit, "archive_sha512": checksum}


def source_identity(root: pathlib.Path) -> tuple[str, int]:
    def git(*args: str) -> str:
        return subprocess.check_output(["git", *args], cwd=root, text=True).strip()
    if git("status", "--porcelain", "--untracked-files=normal"):
        raise BundleError("native payload builds require a clean committed source tree")
    revision = git("rev-parse", "HEAD")
    if not re.fullmatch(r"[0-9a-f]{40}", revision):
        raise BundleError("unsupported source revision")
    return revision, int(git("show", "-s", "--format=%ct", "HEAD"))


def inventory(stage: pathlib.Path, arch: str) -> list[dict]:
    paths = set()
    for path in stage.rglob("*"):
        if path.is_symlink() or (not path.is_file() and not path.is_dir()):
            raise BundleError(f"payload links/special files are forbidden: {path.name}")
        if path.is_file():
            paths.add(path.relative_to(stage).as_posix())
    if paths != PAYLOAD_PATHS:
        raise BundleError(f"payload inventory mismatch: missing={sorted(PAYLOAD_PATHS - paths)}, extra={sorted(paths - PAYLOAD_PATHS)}")
    entries = []
    for name in sorted(paths):
        path = stage / name
        size = path.stat().st_size
        if size > MAX_FILE_SIZE:
            raise BundleError(f"payload file too large: {name}")
        if name in EXECUTABLES:
            with path.open("rb") as stream:
                check_elf(stream.read(64), arch, name)
        entries.append({"path": name, "size": size, "sha256": file_digest(path),
                        "mode": 0o755 if name in EXECUTABLES else 0o644})
    return entries


def create_bundle(stage: pathlib.Path, output: pathlib.Path, metadata: dict, timestamp: int) -> str:
    if output.exists() or output.is_symlink():
        raise BundleError(f"refusing to overwrite output: {output}")
    arch = metadata["architecture"]
    entries = inventory(stage, arch)
    manifest = {**metadata, "schema": SCHEMA, "os": "linux", "files": entries}
    output.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix=".native-bundle-", dir=output.parent) as temporary:
        candidate = pathlib.Path(temporary) / "candidate.tar.gz"
        with candidate.open("wb") as raw:
            with gzip.GzipFile(filename="", mode="wb", fileobj=raw, mtime=0) as compressed:
                with tarfile.open(fileobj=compressed, mode="w", format=tarfile.USTAR_FORMAT) as archive:
                    data = json_bytes(manifest)
                    member = tarfile.TarInfo(MANIFEST)
                    member.size, member.mode, member.mtime = len(data), 0o644, timestamp
                    archive.addfile(member, io.BytesIO(data))
                    for entry in entries:
                        member = tarfile.TarInfo(entry["path"])
                        member.size, member.mode, member.mtime = entry["size"], entry["mode"], timestamp
                        with (stage / entry["path"]).open("rb") as stream:
                            archive.addfile(member, stream)
        checksum = file_digest(candidate)
        verify_bundle(candidate, checksum)
        # Publish only a verified complete file; link fails atomically on a collision.
        os.chmod(candidate, 0o644)
        os.link(candidate, output)
    return checksum


def validate_manifest(manifest: dict) -> dict[str, dict]:
    if set(manifest) != {"schema", "os", "architecture", "version", "source_revision", "build_host", "gvisor", "files"}:
        raise BundleError("invalid manifest fields")
    if (manifest["schema"] != SCHEMA or manifest["os"] != "linux"
            or not isinstance(manifest["architecture"], str) or manifest["architecture"] not in MACHINES):
        raise BundleError("unsupported native runtime manifest")
    if not isinstance(manifest["version"], str) or not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9.-]+)?", manifest["version"]):
        raise BundleError("invalid package version")
    if not isinstance(manifest["source_revision"], str) or not re.fullmatch(r"[0-9a-f]{40}", manifest["source_revision"]):
        raise BundleError("invalid source revision")
    if not isinstance(manifest["build_host"], dict) or manifest["build_host"].get("profile") != REQUIREMENTS["profile"]:
        raise BundleError("unsupported build profile")
    if not isinstance(manifest["files"], list) or len(manifest["files"]) != len(PAYLOAD_PATHS):
        raise BundleError("invalid payload inventory")
    entries = {}
    for entry in manifest["files"]:
        if not isinstance(entry, dict) or set(entry) != {"path", "size", "sha256", "mode"}:
            raise BundleError("invalid inventory entry")
        name = entry["path"]
        if not isinstance(name, str) or name not in PAYLOAD_PATHS or name in entries:
            raise BundleError("unexpected or duplicate payload path")
        if type(entry["size"]) is not int or not 0 <= entry["size"] <= MAX_FILE_SIZE:
            raise BundleError("invalid payload size")
        if entry["mode"] != (0o755 if name in EXECUTABLES else 0o644):
            raise BundleError("invalid payload mode")
        if not isinstance(entry["sha256"], str) or not re.fullmatch(r"[0-9a-f]{64}", entry["sha256"]):
            raise BundleError("invalid payload checksum")
        entries[name] = entry
    return entries


def verify_bundle(path: pathlib.Path, expected_sha256: str) -> dict:
    """Inspect without extracting or executing anything. The caller supplies trust."""
    if not re.fullmatch(r"[0-9a-f]{64}", expected_sha256):
        raise BundleError("a trusted lowercase SHA-256 checksum is required")
    with path.open("rb") as raw:
        if digest(raw) != expected_sha256:
            raise BundleError("archive SHA-256 mismatch")
        raw.seek(0)
        with tarfile.open(fileobj=raw, mode="r|gz") as archive:
            member = archive.next()
            if (member is None or member.name != MANIFEST or not member.isfile() or member.size > 65536
                    or member.mode != 0o644 or member.uid != 0 or member.gid != 0 or member.pax_headers):
                raise BundleError("archive must start with a bounded regular manifest.json")
            stream = archive.extractfile(member)
            assert stream is not None
            manifest = read_json(stream.read())
            entries = validate_manifest(manifest)
            seen = set()
            total = 0
            lock = None
            selected_data = None
            # next() avoids iterating the already-read first member a second time.
            while (member := archive.next()) is not None:
                name = member.name
                if name not in entries or name in seen or not member.isfile() or member.pax_headers:
                    raise BundleError(f"unexpected, duplicate or non-regular archive member: {name}")
                entry = entries[name]
                total += member.size
                if member.size != entry["size"] or total > MAX_TOTAL_SIZE or member.mode != entry["mode"]:
                    raise BundleError(f"size/mode mismatch: {name}")
                if member.uid != 0 or member.gid != 0:
                    raise BundleError(f"noncanonical archive ownership: {name}")
                stream = archive.extractfile(member)
                assert stream is not None
                sha256 = hashlib.sha256()
                small = bytearray()
                first = True
                while chunk := stream.read(1024 * 1024):
                    if first and name in EXECUTABLES:
                        check_elf(chunk[:64], manifest["architecture"], name)
                    first = False
                    sha256.update(chunk)
                    if name in ("share/axern/gvisor.lock", "share/axern/gvisor-selected.lock.json", "requirements.json", "share/axern/registry_auths.json"):
                        if len(small) + len(chunk) > 65536:
                            raise BundleError(f"oversized metadata: {name}")
                        small.extend(chunk)
                if first and name in EXECUTABLES:
                    raise BundleError(f"empty executable: {name}")
                if sha256.hexdigest() != entry["sha256"]:
                    raise BundleError(f"file SHA-256 mismatch: {name}")
                if name == "share/axern/gvisor-selected.lock.json":
                    selected_data = bytes(small)
                elif name == "share/axern/gvisor.lock":
                    lock = parse_lock(small.decode(), manifest["architecture"])
                elif name == "requirements.json" and read_json(small) != REQUIREMENTS:
                    raise BundleError("runtime requirements mismatch")
                elif name == "share/axern/registry_auths.json" and read_json(small) != {}:
                    raise BundleError("registry credential template must be empty")
                seen.add(name)
            if seen != set(entries):
                raise BundleError("archive is missing required payload members")
            if lock != manifest["gvisor"] or lock is None or selected_data is None:
                raise BundleError("gVisor input identity does not match bundled lock and manifest")
            try:
                selected = gvisor_payload.parse_selected(selected_data, manifest["architecture"], lock)
            except (ValueError, KeyError, TypeError) as error:
                raise BundleError("invalid selected gVisor lock") from error
            for name, record in selected.items():
                entry = entries["runtime/gvisor/" + name]
                if {key: entry[key] for key in ("size", "mode", "sha256")} != record:
                    raise BundleError("selected gVisor payload does not match the bundled member lock")
            return manifest


def build(root: pathlib.Path, output_dir: pathlib.Path, gvisor_archive: pathlib.Path) -> tuple[pathlib.Path, str]:
    arch = {"x86_64": "amd64", "aarch64": "arm64"}.get(platform.machine())
    if platform.system() != "Linux" or arch is None:
        raise BundleError("build requires a native Linux amd64 or arm64 host")
    os_release = platform.freedesktop_os_release()
    if os_release.get("ID") != "ubuntu" or os_release.get("VERSION_ID") != "24.04":
        raise BundleError("initial native build profile requires Ubuntu 24.04")
    revision, timestamp = source_identity(root)
    version = (root / "VERSION").read_text().strip()
    if not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9.-]+)?", version):
        raise BundleError("invalid package version")
    lock = parse_lock((root / SOURCE_FILES["share/axern/gvisor.lock"]).read_text(), arch)
    if not gvisor_archive.is_file() or gvisor_archive.is_symlink() or file_digest(gvisor_archive, "sha512") != lock["archive_sha512"]:
        raise BundleError("gVisor archive is missing or does not match the repository lock")
    output = output_dir / f"axern-runtime_{version}_linux_{arch}.tar.gz"
    if output.exists() or output.is_symlink():
        raise BundleError(f"refusing to overwrite output: {output}")
    metadata = {
        "architecture": arch, "version": version, "source_revision": revision, "gvisor": lock,
        "build_host": {"profile": REQUIREMENTS["profile"], "libc": list(platform.libc_ver())},
    }
    env = os.environ.copy()
    env.update({"GOOS": "linux", "GOARCH": arch, "CGO_ENABLED": "0", "GOFLAGS": ""})
    with tempfile.TemporaryDirectory(prefix="axern-native-build-") as temporary:
        stage = pathlib.Path(temporary) / "payload"
        for name in PAYLOAD_PATHS - GVISOR_PATHS:
            (stage / name).parent.mkdir(parents=True, exist_ok=True)
        for name, package in GO_BINARIES.items():
            print(f"building {name}", file=sys.stderr, flush=True)
            subprocess.run([
                os.environ.get("GO", "go"), "build", "-trimpath", "-buildvcs=false",
                "-ldflags", f"-s -w -X github.com/cofy-x/axern/sdk/go.version={version}",
                "-o", str(stage / name), package,
            ], cwd=root, env=env, check=True)
        for name, source in SOURCE_FILES.items():
            shutil.copyfile(root / source, stage / name)
        try:
            selected = gvisor_payload.parse_selected((stage / "share/axern/gvisor-selected.lock.json").read_bytes(), arch, lock)
            # Only the authenticated selected profile enters the delivery inventory.
            (stage / "runtime").mkdir()
            gvisor_payload.extract_archive(gvisor_archive, stage / "runtime/gvisor", selected, arch, lock)
        except (ValueError, tarfile.TarError) as error:
            raise BundleError("invalid selected gVisor build input") from error
        (stage / "share/axern/registry_auths.json").write_bytes(json_bytes({}))
        (stage / "requirements.json").write_bytes(json_bytes(REQUIREMENTS))
        if source_identity(root) != (revision, timestamp):
            raise BundleError("source changed during the build")
        checksum = create_bundle(stage, output, metadata, timestamp)
    return output, checksum


def stage_bundle(path: pathlib.Path, expected_sha256: str, destination: pathlib.Path) -> dict:
    """Materialize a verified private snapshot into a new Linux directory only."""
    if platform.system() != "Linux":
        raise BundleError("staging requires Linux")
    destination = destination.absolute()
    for parent in (destination, *destination.parents):
        if parent.is_symlink():
            raise BundleError("staging paths must not contain symlinks")
        if parent.exists():
            info = parent.stat()
            if info.st_uid not in (0, os.geteuid()) or (info.st_mode & 0o022 and not info.st_mode & 0o1000):
                raise BundleError("staging parents must not be writable or owned by another user")
    if destination.exists():
        raise BundleError(f"refusing to replace destination: {destination}")
    if not destination.parent.is_dir():
        raise BundleError("destination parent must already exist")
    with tempfile.TemporaryDirectory(prefix=".axern-stage-", dir=destination.parent) as temporary:
        private = pathlib.Path(temporary)
        snapshot = private / "archive.tar.gz"
        descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        with os.fdopen(descriptor, "rb") as source, snapshot.open("xb") as target:
            info = os.fstat(source.fileno())
            if not stat.S_ISREG(info.st_mode) or info.st_size > MAX_TOTAL_SIZE:
                raise BundleError("staging requires a bounded regular archive file")
            remaining = MAX_TOTAL_SIZE
            while chunk := source.read(min(1024 * 1024, remaining + 1)):
                if len(chunk) > remaining:
                    raise BundleError("archive grew beyond the staging size limit")
                target.write(chunk)
                remaining -= len(chunk)
        manifest = verify_bundle(snapshot, expected_sha256)
        payload = private / "payload"
        payload.mkdir(mode=0o755)
        with tarfile.open(snapshot, "r|gz") as archive:
            for member in archive:
                # All entries were validated in the private snapshot. Do not use extractall.
                target = payload / member.name
                target.parent.mkdir(parents=True, exist_ok=True)
                with archive.extractfile(member) as source, target.open("xb") as output:
                    shutil.copyfileobj(source, output)
                    output.flush()
                    os.fsync(output.fileno())
                target.chmod(member.mode)
        (payload / "stage-receipt.json").write_bytes(json_bytes({"archive_sha256": expected_sha256}))
        (payload / "stage-receipt.json").chmod(0o644)
        for directory in (payload, *[entry for entry in payload.rglob('*') if entry.is_dir()]):
            directory.chmod(0o755)
        # Linux renameat2 makes publication atomic without replacing even an empty directory.
        libc = ctypes.CDLL(None, use_errno=True)
        rename = libc.renameat2
        rename.argtypes = [ctypes.c_int, ctypes.c_char_p, ctypes.c_int, ctypes.c_char_p, ctypes.c_uint]
        rename.restype = ctypes.c_int
        if rename(-100, os.fsencode(payload), -100, os.fsencode(destination), 1) != 0:
            error = ctypes.get_errno()
            raise OSError(error, os.strerror(error), str(destination))
    return manifest


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    builder = commands.add_parser("build", help="build a candidate native payload from clean committed source")
    builder.add_argument("--gvisor-archive", required=True, type=pathlib.Path)
    builder.add_argument("--output-dir", type=pathlib.Path, default=ROOT / "dist/native-runtime")
    verifier = commands.add_parser("verify", help="verify without extracting or executing the payload")
    verifier.add_argument("archive", type=pathlib.Path)
    verifier.add_argument("--sha256", required=True, help="expected digest from a trusted build receipt/release")
    stager = commands.add_parser("stage", help="verify and materialize into a new directory; does not install services")
    stager.add_argument("archive", type=pathlib.Path)
    stager.add_argument("--sha256", required=True)
    stager.add_argument("--destination", type=pathlib.Path, required=True)
    args = parser.parse_args()
    try:
        if args.command == "build":
            output, checksum = build(ROOT, args.output_dir.resolve(), args.gvisor_archive.absolute())
            print(json.dumps({"archive": str(output), "sha256": checksum}))
        elif args.command == "stage":
            manifest = stage_bundle(args.archive, args.sha256, args.destination)
            print(json.dumps({"status": "staged", "destination": str(args.destination), "source_revision": manifest["source_revision"]}))
        else:
            manifest = verify_bundle(args.archive, args.sha256)
            print(json.dumps({"status": "verified", "version": manifest["version"],
                              "architecture": manifest["architecture"], "source_revision": manifest["source_revision"]}))
    except (BundleError, OSError, subprocess.CalledProcessError, tarfile.TarError, UnicodeError, json.JSONDecodeError) as error:
        parser.exit(1, f"native-runtime: {error}\n")


if __name__ == "__main__":
    main()
