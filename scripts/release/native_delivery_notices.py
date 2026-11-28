#!/usr/bin/env python3
"""Collect source notices for the Go dependencies in an exact native archive."""

import argparse
import base64
from collections import defaultdict
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import subprocess
import tarfile
import tempfile
from urllib.parse import unquote
import zipfile

from native_runtime import (
    BundleError, EXECUTABLES, GVISOR_PATHS, ROOT, digest, json_bytes,
    parse_lock, publish_new_directory, read_json, verify_bundle,
)


NOTICE_NAME = re.compile(r"(?:LICEN[CS]E|COPYRIGHT|COPYING|NOTICE|AUTHORS|CONTRIBUTORS|PATENTS)(?:[-.].*)?", re.IGNORECASE)
MAX_NOTICE_SIZE = 1024 * 1024
GO_MODULE = re.compile(r"[A-Za-z0-9._~-]+(?:/[A-Za-z0-9._~-]+)*")
GO_VERSION = re.compile(r"v[0-9][A-Za-z0-9.+-]*")
GO_RELEASE = re.compile(r"go1\.[0-9]+\.[0-9]+")
GVISOR_MODULES = {"runsc", "runsc/cmd/sentry/gvisor_sentry"}


def git(repo: Path, *args: str) -> bytes:
    return subprocess.check_output(
        ["git", "--no-replace-objects", "-C", str(repo), *args], timeout=120,
    )


def tracked_files(repo: Path, commit: str, recursive: bool = False) -> list[str]:
    args = ["-r"] if recursive else []
    output = git(repo, "ls-tree", *args, "--name-only", "-z", commit)
    return output.decode().rstrip("\0").split("\0")


def go_sums(data: bytes) -> dict[tuple[str, str], str]:
    entries = {}
    for line in data.decode().splitlines():
        if not line:
            continue
        module, version, checksum = line.split()
        if not version.endswith("/go.mod") and entries.setdefault((module, version), checksum) != checksum:
            raise BundleError(f"conflicting Go locks for {module}@{version}")
    return entries


def gvisor_dependencies(source: Path, commit: str) -> dict:
    checksums = go_sums(git(source, "show", f"{commit}:go.sum"))
    bazel = git(source, "show", f"{commit}:MODULE.bazel").decode()
    for stanza in re.findall(r"^go_deps\.module\(\n(.*?)^\)", bazel, re.MULTILINE | re.DOTALL):
        fields = dict(re.findall(r'^\s+(path|sum|version) = "([^"]+)",$', stanza, re.MULTILINE))
        if set(fields) != {"path", "sum", "version"}:
            raise BundleError("could not read an explicit gVisor Bazel Go module lock")
        module, version, checksum = fields["path"], fields["version"], fields["sum"]
        if checksums.setdefault((module, version), checksum) != checksum:
            raise BundleError(f"conflicting gVisor Go locks for {module}@{version}")
    return checksums


def axern_dependencies(source: Path, revision: str) -> tuple[set[str], dict]:
    local, checksums = set(), {}
    for path in tracked_files(source, revision, recursive=True):
        filename = PurePosixPath(path).name
        if filename in ("go.sum", "go.work.sum"):
            for key, value in go_sums(git(source, "show", f"{revision}:{path}")).items():
                if checksums.setdefault(key, value) != value:
                    raise BundleError(f"conflicting Axern Go locks for {key[0]}@{key[1]}")
        elif filename == "go.mod":
            match = re.search(rb"^module\s+(\S+)", git(source, "show", f"{revision}:{path}"), re.MULTILINE)
            if match:
                local.add(match[1].decode())
    return local, checksums


def load_inventory(directory: Path, archive_sha256: str, manifest: dict) -> tuple[list, dict]:
    receipt = read_json((directory / "receipt.json").read_bytes())
    expected = {"schema": "axern.native-delivery-inventory.v1", "archive_sha256": archive_sha256}
    expected.update({key: manifest[key] for key in ("source_revision", "architecture", "version")})
    if any(receipt.get(key) != value for key, value in expected.items()):
        raise BundleError("inventory receipt belongs to a different source or archive")
    hashes = receipt["inventory_sha256"]
    if set(hashes) != {"runtime.spdx.json", "runtime.cdx.json"}:
        raise BundleError("inventory receipt must identify both SBOMs")
    for name, expected_sha in hashes.items():
        contents = (directory / name).read_bytes()
        if hashlib.sha256(contents).hexdigest() != expected_sha:
            raise BundleError(f"inventory sidecar SHA-256 mismatch: {name}")
        if name == "runtime.spdx.json":
            packages = read_json(contents)["packages"]
    if not isinstance(packages, list) or len(packages) != receipt["spdx_package_count"] or not packages:
        raise BundleError("inventory does not contain the recorded package count")
    return packages, hashes


def read_packages(packages: list, manifest: dict, local: set[str]) -> tuple[dict, dict]:
    modules, standard_library = defaultdict(set), defaultdict(set)
    root_name = f"axern-runtime-linux-{manifest['architecture']}"
    source_prefix = "acquired package info from go module information: /"
    for package in packages:
        name, version = package["name"], package.get("versionInfo")
        if name == root_name and version == manifest["version"] and not package.get("sourceInfo"):
            continue
        source = package.get("sourceInfo", "")
        path = source.removeprefix(source_prefix)
        if not source.startswith(source_prefix) or path not in EXECUTABLES:
            raise BundleError(f"no notice source rule for package {name}@{version} ({source})")
        if name == "stdlib" and GO_RELEASE.fullmatch(version or ""):
            standard_library[version].add(path)
            continue
        if version == "UNKNOWN" and name in (GVISOR_MODULES if path in GVISOR_PATHS else local):
            continue
        purls = {unquote(entry["referenceLocator"]) for entry in package.get("externalRefs", [])
                 if entry.get("referenceType") == "purl"}
        if (not GO_MODULE.fullmatch(name) or not GO_VERSION.fullmatch(version or "")
                or any(part in (".", "..") for part in name.split("/"))
                or f"pkg:golang/{name}@{version}" not in purls):
            raise BundleError(f"no exact Go module source for {name}@{version} ({path})")
        modules[name, version].add(path)
    if not standard_library or not modules:
        raise BundleError("inventory must include versioned Go modules and standard libraries")
    return modules, standard_library


def store_notices(output: Path, unit: str, notices: dict[str, bytes]) -> list[dict]:
    if not notices:
        raise BundleError(f"source contains no notice texts: {unit}")
    records = []
    for name, data in sorted(notices.items()):
        relative = PurePosixPath(name)
        if relative.is_absolute() or ".." in relative.parts or len(data) > MAX_NOTICE_SIZE:
            raise BundleError(f"invalid notice file: {unit}/{name}")
        path = output / unit / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        with path.open("xb") as stream:
            stream.write(data)
        records.append({
            "path": path.relative_to(output).as_posix(),
            "sha256": hashlib.sha256(data).hexdigest(),
            "size": len(data),
        })
    return records


def escape_cache_path(path: str) -> str:
    # Go's module cache encodes each uppercase character as ! plus its lowercase form.
    return re.sub(r"[A-Z]", lambda match: "!" + match[0].lower(), path)


def module_notices(cache_path: Path, module: str, version: str, expected_h1: str) -> tuple[str, dict]:
    prefix = f"{module}@{version}/"
    with cache_path.open("rb") as source:
        archive_sha256 = digest(source)
        source.seek(0)
        notices, contents = {}, hashlib.sha256()
        with zipfile.ZipFile(source) as archive:
            names = archive.namelist()
            if len(set(names)) != len(names):
                raise BundleError(f"duplicate Go module ZIP paths: {module}@{version}")
            for name in sorted(names):
                if not name.startswith(prefix) or "\n" in name:
                    raise BundleError(f"invalid Go module ZIP member: {module}@{version}")
                with archive.open(name) as stream:
                    contents.update(f"{digest(stream)}  {name}\n".encode())
                if not name.endswith("/") and NOTICE_NAME.fullmatch(PurePosixPath(name).name):
                    if archive.getinfo(name).file_size > MAX_NOTICE_SIZE:
                        raise BundleError(f"oversized module notice: {module}@{version} {name}")
                    notices[name.removeprefix(prefix)] = archive.read(name)
        actual_h1 = "h1:" + base64.b64encode(contents.digest()).decode()
        if actual_h1 != expected_h1:
            raise BundleError(f"Go module ZIP disagrees with locked h1: {module}@{version}")
    return archive_sha256, notices


def dependency_record(output: Path, caches: list[Path], key: tuple[str, str], paths: set[str], locks: dict) -> dict:
    module, version = key
    identity = f"{module}@{version}"
    owners = {"gvisor" if path in GVISOR_PATHS else "axern" for path in paths}
    hashes = {locks[owner].get(key) for owner in owners}
    if None in hashes or len(hashes) != 1:
        raise BundleError(f"missing or inconsistent source locks from {', '.join(sorted(owners))}: {identity}")
    checksum = hashes.pop()
    relative = Path("cache/download") / escape_cache_path(module) / "@v" / (escape_cache_path(version) + ".zip")
    archive = next((base / relative for base in caches if (base / relative).is_file()), None)
    if archive is None:
        raise BundleError(f"Go module ZIP missing from cache: go mod download {identity}")
    archive_sha256, notices = module_notices(archive, module, version, checksum)
    files = store_notices(output, "modules/" + hashlib.sha256(identity.encode()).hexdigest(), notices)
    return {"id": identity, "kind": "go-module", "go_h1": checksum, "source_zip_sha256": archive_sha256,
            "lock_sources": sorted(owners), "archive_paths": sorted(paths), "files": files}


def toolchain_record(output: Path, go: str, version: str, paths: set[str], caches: list[Path]) -> dict:
    env = {
        **os.environ, "GOTOOLCHAIN": version, "GOWORK": "off", "GOENV": "off",
        "GOMODCACHE": str(caches[0].absolute()), "GOPROXY": "off",
    }
    env.pop("GOROOT", None)
    settings = subprocess.check_output(
        [go, "env", "-json", "GOROOT", "GOVERSION"], env=env, timeout=20,
    )
    toolchain = read_json(settings)
    if toolchain["GOVERSION"] != version:
        raise BundleError(f"expected an installed {version} Go toolchain")
    root = Path(toolchain["GOROOT"])
    candidates = (*root.iterdir(), *(root / "src/vendor").rglob("*"))
    notices = {path.relative_to(root).as_posix(): path.read_bytes() for path in candidates
               if path.is_file() and NOTICE_NAME.fullmatch(path.name)}
    if not {"LICENSE", "PATENTS"} <= notices.keys():
        raise BundleError(f"Go toolchain notices are incomplete: {version}")
    return {"id": version, "kind": "go-stdlib", "archive_paths": sorted(paths),
            "files": store_notices(output, f"go-stdlib/{version}", notices)}


def collect(archive: Path, sha256: str, inventory: Path, output: Path, gvisor: Path, caches: list[Path], go: str) -> dict:
    output = output.absolute()
    if output.exists() or output.is_symlink():
        raise BundleError(f"refusing to replace notice evidence: {output}")
    manifest = verify_bundle(archive, sha256)
    packages, hashes = load_inventory(inventory, sha256, manifest)
    revision, gvisor_commit = manifest["source_revision"], manifest["gvisor"]["commit"]
    lock = git(ROOT, "show", f"{revision}:runtime/axnoded/gvisor.lock").decode()
    if parse_lock(lock, manifest["architecture"]) != manifest["gvisor"]:
        raise BundleError("source revision does not contain the archive gVisor lock")
    tag = re.search(r"^AXERN_GVISOR_TAG_OBJECT=([0-9a-f]{40})$", lock, re.MULTILINE)
    if not tag or git(gvisor, "rev-parse", f"{tag[1]}^{{commit}}").decode().strip() != gvisor_commit:
        raise BundleError("gVisor source tag object does not resolve to the locked commit")
    local, axern_sums = axern_dependencies(ROOT, revision)
    locks = {"axern": axern_sums, "gvisor": gvisor_dependencies(gvisor, gvisor_commit)}
    modules, standard_library = read_packages(packages, manifest, local)
    with tempfile.TemporaryDirectory(prefix=".axern-native-notices-", dir=output.parent) as scratch:
        evidence = Path(scratch) / "evidence"
        evidence.mkdir(mode=0o755)
        records = []
        for owner, repo, commit in (("axern", ROOT, revision), ("gvisor", gvisor, gvisor_commit)):
            notices = {name: git(repo, "show", f"{commit}:{name}") for name in tracked_files(repo, commit)
                       if NOTICE_NAME.fullmatch(name)}
            if "LICENSE" not in notices:
                raise BundleError(f"release source has no LICENSE: {owner}")
            records.append({"id": owner, "kind": "release-source", "commit": commit,
                            "files": store_notices(evidence, f"sources/{owner}", notices)})
        records.extend(dependency_record(evidence, caches, key, paths, locks) for key, paths in sorted(modules.items()))
        records.extend(toolchain_record(evidence, go, version, paths, caches)
                       for version, paths in sorted(standard_library.items()))
        receipt = {
            "schema": "axern.native-delivery-notices.v1", "archive_sha256": sha256,
            "source_revision": revision, "architecture": manifest["architecture"], "version": manifest["version"],
            "gvisor": {**manifest["gvisor"], "tag_object": tag[1]}, "inventory_sha256": hashes,
            "source_notice_units": records,
        }
        (evidence / "receipt.json").write_bytes(json_bytes(receipt))
        publish_new_directory(evidence, output)
    return receipt


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("archive", type=Path)
    parser.add_argument("--sha256", required=True, help="expected trusted archive SHA-256")
    parser.add_argument("--inventory-dir", required=True, type=Path, help="matching verified archive SBOMs")
    parser.add_argument("--gvisor-source", required=True, type=Path, help="local Git repository with locked upstream tag")
    parser.add_argument("--module-cache", required=True, type=Path, action="append", help="Go module cache; repeat if needed")
    parser.add_argument("--output-dir", required=True, type=Path, help="new notice evidence directory")
    parser.add_argument("--go", default="go", help="trusted Go executable for already installed toolchains")
    args = parser.parse_args()
    try:
        receipt = collect(args.archive, args.sha256, args.inventory_dir, args.output_dir,
                          args.gvisor_source, args.module_cache, args.go)
        print(json.dumps({"archive_sha256": receipt["archive_sha256"],
                          "source_notice_unit_count": len(receipt["source_notice_units"])}))
    except (ValueError, OSError, EOFError, KeyError, TypeError, subprocess.SubprocessError,
            tarfile.TarError, zipfile.BadZipFile) as error:
        parser.exit(1, f"native-delivery-notices: {error}\n")


if __name__ == "__main__":
    main()
