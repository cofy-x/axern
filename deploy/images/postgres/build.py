#!/usr/bin/env python3
"""Build the pinned static gosu replacement; never mutate dependency versions."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import struct
import subprocess
import tarfile
import tempfile
import urllib.request

HERE = Path(__file__).resolve().parent
SOURCE_FILES = ("main.go", "setup-user.go", "version.go", "LICENSE")


def require(ok, message):
    if not ok:
        raise ValueError(message)


def load_pins():
    pins = json.loads((HERE / "pins.json").read_text())
    require(pins["schema_version"] == 1, "unsupported pins schema")
    require(re.fullmatch(r"go1\.\d+\.\d+", pins["go_version"]), "invalid Go version")
    gosu = pins["gosu"]
    require(re.fullmatch(r"[0-9a-f]{40}", gosu["commit"]), "invalid source commit")
    require(re.fullmatch(r"[0-9a-f]{64}", gosu["sha256"]), "invalid source digest")
    require(gosu["url"] == "https://codeload.github.com/tianon/gosu/tar.gz/" + gosu["commit"], "unexpected source origin")
    require(type(gosu["size"]) is int and 0 < gosu["size"] <= 1024 * 1024, "invalid source size")
    require((HERE / "Dockerfile").read_text().splitlines()[0] == "FROM " + pins["base_image"], "base image differs from pins")
    require((HERE / "gosu.mod").read_text().splitlines()[2] == "go " + pins["go_version"][2:], "module toolchain differs from pins")
    require((HERE / "gosu.sum").is_file(), "missing dependency checksums")
    return pins


def unpack_source(archive, target, pins):
    raw = Path(archive).read_bytes()
    gosu = pins["gosu"]
    require(len(raw) == gosu["size"] and hashlib.sha256(raw).hexdigest() == gosu["sha256"], "source archive integrity mismatch")
    prefix = "gosu-" + gosu["commit"] + "/"
    found = set()
    with tarfile.open(archive, "r:gz") as source:
        for member in source:
            if member.name not in {prefix + name for name in SOURCE_FILES}:
                continue
            name = member.name[len(prefix):]
            require(name not in found and member.isfile() and 0 <= member.size <= 1024 * 1024, "invalid source member")
            found.add(name)
            with source.extractfile(member) as stream:
                (target / name).write_bytes(stream.read())
    require(found == set(SOURCE_FILES), "missing source member")


def verify_elf(path, arch):
    raw = path.read_bytes()
    require(len(raw) >= 64 and raw[:7] == b"\x7fELF\x02\x01\x01", "not a Linux 64-bit ELF")
    require(struct.unpack_from("<H", raw, 18)[0] == {"amd64": 62, "arm64": 183}[arch], "ELF architecture mismatch")
    offset = struct.unpack_from("<Q", raw, 32)[0]
    stride, count = struct.unpack_from("<HH", raw, 54)
    require(stride >= 56 and offset + stride * count <= len(raw), "invalid ELF program headers")
    require(all(struct.unpack_from("<I", raw, offset + i * stride)[0] not in (2, 3) for i in range(count)), "gosu must be static without an interpreter")


def go_environment(arch):
    allowed = ("PATH", "HOME", "GOCACHE", "GOMODCACHE", "GOPATH", "TMPDIR", "SSL_CERT_FILE", "SSL_CERT_DIR",
               "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy")
    env = {name: os.environ[name] for name in allowed if name in os.environ}
    env.update(GOWORK="off", GOTOOLCHAIN="local", GOENV="off", GOFLAGS="-mod=readonly", GOEXPERIMENT="",
               GOPROXY="https://proxy.golang.org", GOSUMDB="sum.golang.org", GOPRIVATE="", GONOSUMDB="",
               CGO_ENABLED="0", GOOS="linux", GOARCH=arch, GOAMD64="v1", GOARM64="v8.0")
    return env


def build(args):
    pins = load_pins()
    go = shutil.which(args.go)
    require(go is not None, "Go executable not found")
    env = go_environment(args.arch or "amd64")
    version = subprocess.check_output([go, "version"], env=env, text=True).split()[2]
    require(version == pins["go_version"], "wrong Go toolchain")
    output = Path(args.output).resolve()
    require(not output.exists(), "output already exists; preserve the previous build")
    with tempfile.TemporaryDirectory(prefix="axern-postgres-gosu-") as temporary:
        temporary = Path(temporary)
        archive = Path(args.source_archive) if args.source_archive else temporary / "source.tar.gz"
        if not args.source_archive:
            with urllib.request.urlopen(pins["gosu"]["url"], timeout=60) as response:
                raw = response.read(pins["gosu"]["size"] + 1)
            archive.write_bytes(raw)
        source = temporary / "source"
        source.mkdir()
        unpack_source(archive, source, pins)
        shutil.copyfile(HERE / "gosu.mod", source / "go.mod")
        shutil.copyfile(HERE / "gosu.sum", source / "go.sum")
        if args.prepare_only:
            output.mkdir(parents=True, mode=0o755)
            shutil.copytree(source, output / "source")
            (output / "source.json").write_text(json.dumps({"source": pins["gosu"], "go_version": version}, indent=2) + "\n")
            return
        binary = temporary / "gosu"
        command = [go, "build", "-mod=readonly", "-trimpath", "-buildvcs=false", "-ldflags=-s -w -X main.Version=" + pins["gosu"]["version"], "-o", str(binary), "."]
        subprocess.run(command, cwd=source, env=env, check=True)
        verify_elf(binary, args.arch)
        metadata = subprocess.check_output([go, "version", "-m", str(binary)], env=env, text=True)
        require(pins["go_version"] in metadata and "github.com/moby/sys/user\tv0.4.1" in metadata and "golang.org/x/sys\tv0.49.0" in metadata, "unexpected embedded module versions")
        context = output / "context"
        context.mkdir(parents=True, mode=0o755)
        shutil.copyfile(binary, context / "gosu")
        (context / "gosu").chmod(0o755)
        for name in ("pins.json", "gosu.mod", "gosu.sum"):
            shutil.copyfile(HERE / name, context / name)
        shutil.copyfile(source / "LICENSE", context / "LICENSE")
        receipt = {"schema_version": 1, "architecture": args.arch, "source": pins["gosu"], "go_version": version, "sha256": hashlib.sha256(binary.read_bytes()).hexdigest(), "size": binary.stat().st_size, "mode": "0755", "go_build_metadata": metadata, "command": command[:-2] + ["<output>", "."]}
        (context / "build.json").write_text(json.dumps(receipt, indent=2) + "\n")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true", help="validate source pins without downloads or compilation")
    parser.add_argument("--prepare-only", action="store_true", help="retain verified source and locked go.mod/go.sum under OUTPUT/source for independent auditing; do not compile")
    parser.add_argument("--arch", choices=("amd64", "arm64"))
    parser.add_argument("--go", default="go")
    parser.add_argument("--output")
    parser.add_argument("--source-archive", help="use an existing archive, still checked against the lock")
    args = parser.parse_args()
    if args.check:
        load_pins()
    else:
        if not args.output or (not args.arch and not args.prepare_only):
            parser.error("--output and either --arch or --prepare-only are required")
        build(args)


if __name__ == "__main__":
    main()
