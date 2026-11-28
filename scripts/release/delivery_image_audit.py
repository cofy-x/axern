#!/usr/bin/env python3
"""Audit immutable delivery images before release publication."""

from __future__ import annotations

import argparse
from datetime import datetime, timedelta, timezone
import gzip
import hashlib
import json
import os
from pathlib import Path
import re
import ssl
import subprocess
import tempfile
import urllib.error
import urllib.parse
import urllib.request


REPOSITORY = "ghcr.io/cofy-x/axern/"
IMAGES = {
    "CONTROLD_IMAGE": "controld",
    "GATEWAYD_IMAGE": "gatewayd",
    "NODE_ALL_IN_ONE_IMAGE": "node-all-in-one",
    "POSTGRES_IMAGE": "postgres",
    "REGISTRY_IMAGE": "registry",
    "TUNNELD_IMAGE": "tunneld",
}
ARCHITECTURES = {"amd64", "arm64"}
INDEX = "application/vnd.oci.image.index.v1+json"
INDEX_TYPES = {INDEX, "application/vnd.docker.distribution.manifest.list.v2+json"}
MANIFEST_TYPES = {
    "application/vnd.oci.image.manifest.v1+json",
    "application/vnd.docker.distribution.manifest.v2+json",
}
LAYER_TYPES = {
    "application/vnd.oci.image.layer.v1.tar": False,
    "application/vnd.oci.image.layer.v1.tar+gzip": True,
    "application/vnd.docker.image.rootfs.diff.tar.gzip": True,
}
TOOL_VERSIONS = {"syft": "1.57.2", "grype": "0.122.1"}
MAX_METADATA = 10 * 1024**2
MAX_LAYER = 2 * 1024**3


def require(condition: bool, message: str) -> None:
    if not condition:
        raise ValueError(message)


def file_digest(path: Path) -> str:
    with path.open("rb") as stream:
        return "sha256:" + hashlib.file_digest(stream, "sha256").hexdigest()


def valid_digest(value: str) -> bool:
    return isinstance(value, str) and re.fullmatch(r"sha256:[0-9a-f]{64}", value) is not None


def write_json(path: Path, data: dict) -> None:
    path.write_text(json.dumps(data, indent=2) + "\n")


def load_json(data: bytes) -> dict:
    def unique_keys(pairs):
        mapping = dict(pairs)
        require(len(pairs) == len(mapping), "Duplicate key in audit metadata")
        return mapping

    decoded = json.loads(data, object_pairs_hook=unique_keys)
    require(isinstance(decoded, dict), "Expected an audit metadata object")
    return decoded


def read_lock(path: Path) -> dict[str, str]:
    with path.open() as lock:
        raw = lock.read(65_537)
    require(len(raw) <= 65_536, "Oversized release image lock")
    references = {}
    for line in raw.splitlines():
        key, separator, ref = line.partition("=")
        repository, separator_ref, digest = ref.partition("@")
        require(separator and key in IMAGES and key not in references, "Invalid or duplicate image lock key")
        require(separator_ref and repository == REPOSITORY + IMAGES[key] and valid_digest(digest),
                "Image lock requires the expected repository and an immutable digest")
        references[key] = ref
    require(references.keys() == IMAGES.keys(), "Incomplete delivery image lock")
    return references


def lock_binding(lock: Path, source: str, run_id: int, run_attempt: int) -> dict:
    require(re.fullmatch(r"[0-9a-f]{40}", source) is not None and run_id > 0 and run_attempt > 0,
            "Invalid release run identity")
    read_lock(lock)
    return {"source": source, "run_id": run_id, "run_attempt": run_attempt, "lock_digest": file_digest(lock)}


def descriptor_identity(descriptor: dict, limit: int) -> tuple[str, int]:
    digest, size = descriptor.get("digest"), descriptor.get("size")
    require(valid_digest(digest) and type(size) is int and 0 < size <= limit, "Invalid OCI descriptor")
    return digest, size


def platform_manifests(index: dict) -> dict[str, dict]:
    require(index.get("schemaVersion") == 2 and index.get("mediaType") in INDEX_TYPES, "Expected OCI index")
    platforms = {}
    for descriptor in index["manifests"]:
        platform = descriptor.get("platform", {})
        architecture = platform.get("architecture")
        if (platform.get("os") == architecture == "unknown"
                and descriptor.get("annotations", {}).get("vnd.docker.reference.type") == "attestation-manifest"):
            continue
        require(platform.get("os") == "linux" and architecture in ARCHITECTURES and architecture not in platforms,
                "Unexpected or duplicate delivery platform")
        require(descriptor["mediaType"] in MANIFEST_TYPES and platform.get("variant", "") in ("", "v8"),
                "Unsupported platform manifest")
        descriptor_identity(descriptor, MAX_METADATA)
        platforms[architecture] = descriptor
    require(platforms.keys() == ARCHITECTURES, "Both amd64 and arm64 must be present")
    return platforms


class HTTPSRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, response, code, message, headers, url):
        require(urllib.parse.urlsplit(url).scheme == "https", "Insecure registry redirect")
        redirected = super().redirect_request(request, response, code, message, headers, url)
        if redirected and urllib.parse.urlsplit(url).netloc != urllib.parse.urlsplit(request.full_url).netloc:
            redirected.remove_header("Authorization")
        return redirected


class Registry:
    """Fetch and verify registry content into a private copy-once cache."""

    def __init__(self, repository: str, cache: Path, blob_seed: Path | None = None):
        self.repository = repository
        self.cache = cache
        self.blob_seed = blob_seed
        self.opener = urllib.request.build_opener(HTTPSRedirect())
        self.token = None

    def open(self, kind: str, digest: str):
        headers = {"Accept": ", ".join(sorted(INDEX_TYPES | MANIFEST_TYPES))}
        if self.token:
            headers["Authorization"] = "Bearer " + self.token
        url = f"https://ghcr.io/v2/{self.repository.removeprefix('ghcr.io/')}/{kind}/{digest}"
        try:
            return self.opener.open(urllib.request.Request(url, headers=headers), timeout=120)
        except urllib.error.HTTPError as error:
            if error.code != 401 or self.token:
                raise
            fields = dict(re.findall(r'(realm|service|scope)="([^"]*)"', error.headers.get("WWW-Authenticate", "")))
            scope = f"repository:{self.repository.removeprefix('ghcr.io/')}:pull"
            require(fields == {"realm": "https://ghcr.io/token", "service": "ghcr.io", "scope": scope},
                    "Unexpected GHCR authentication authority")
            query = urllib.parse.urlencode({"service": "ghcr.io", "scope": scope})
            with self.opener.open(f"https://ghcr.io/token?{query}", timeout=120) as response:
                token = load_json(response.read(MAX_METADATA + 1))
            self.token = token.get("token") or token.get("access_token")
            require(isinstance(self.token, str) and bool(self.token), "GHCR pull token unavailable")
            return self.open(kind, digest)

    def open_with_retry(self, kind: str, digest: str):
        for attempt in range(3):
            try:
                return self.open(kind, digest)
            except urllib.error.URLError as error:
                # GHCR blob redirects sometimes fail during the TLS handshake.
                # Certificate, HTTP, and content validation failures remain fatal.
                if (attempt == 2 or not isinstance(error.reason, ssl.SSLError)
                        or getattr(error.reason, "reason", None) != "UNEXPECTED_MESSAGE"):
                    raise

    def fetch(self, kind: str, digest: str, limit: int, size: int | None = None) -> Path:
        require(valid_digest(digest), "Invalid OCI digest")
        target = self.cache / digest.removeprefix("sha256:")
        if not target.exists():
            temporary = target.with_suffix(".partial")
            seed = self.blob_seed / target.name if self.blob_seed and kind == "blobs" else None
            if seed and (seed.exists() or seed.is_symlink()):
                require(seed.is_file() and not seed.is_symlink(), "Invalid cached OCI blob")
                source = seed.open("rb")
            else:
                source = self.open_with_retry(kind, digest)
            actual = 0
            with source, temporary.open("xb") as output:
                while chunk := source.read(min(1024**2, limit + 1 - actual)):
                    actual += len(chunk)
                    require(actual <= limit, "OCI content exceeds its size limit")
                    output.write(chunk)
            require(file_digest(temporary) == digest, "Registry content digest mismatch")
            temporary.rename(target)
        actual = target.stat().st_size
        require(actual <= limit and (size is None or actual == size), "OCI content size mismatch")
        return target


def verify_layer(path: Path, compressed: bool, expected_diff: str) -> None:
    amount = 0
    digest = hashlib.sha256()
    with (gzip.open(path, "rb") if compressed else path.open("rb")) as stream:
        while chunk := stream.read(1024**2):
            amount += len(chunk)
            require(amount <= 4 * 1024**3, "Expanded OCI layer exceeds size limit")
            digest.update(chunk)
    require("sha256:" + digest.hexdigest() == expected_diff, "OCI layer diff ID mismatch")


def prepare_layout(fetch, descriptor: dict, arch: str, layout: Path, target: Path) -> tuple[dict, dict]:
    manifest_digest, size = descriptor_identity(descriptor, MAX_METADATA)
    manifest_path = fetch("manifests", manifest_digest, MAX_METADATA, size)
    manifest_raw = manifest_path.read_bytes()
    (target / "manifest.json").write_bytes(manifest_raw)
    manifest = load_json(manifest_raw)
    require(manifest.get("schemaVersion") == 2 and manifest.get("mediaType") == descriptor.get("mediaType"),
            "Invalid delivery manifest")
    config_digest, size = descriptor_identity(manifest["config"], MAX_METADATA)
    config_path = fetch("blobs", config_digest, MAX_METADATA, size)
    config_raw = config_path.read_bytes()
    (target / "config.json").write_bytes(config_raw)
    config = load_json(config_raw)
    require(config.get("os") == "linux" and config.get("architecture") == arch, "Config platform mismatch")
    layers, diff_ids = manifest["layers"], config["rootfs"]["diff_ids"]
    require(config["rootfs"].get("type") == "layers" and isinstance(layers, list) and isinstance(diff_ids, list)
            and bool(layers) and len(layers) == len(diff_ids), "Incomplete OCI layer or diff ID inventory")
    paths = [manifest_path, config_path]
    for layer, diff_id in zip(layers, diff_ids, strict=True):
        digest, size = descriptor_identity(layer, MAX_LAYER)
        require(layer.get("mediaType") in LAYER_TYPES and valid_digest(diff_id), "Unsupported OCI layer")
        path = fetch("blobs", digest, size, size)
        verify_layer(path, LAYER_TYPES[layer["mediaType"]], diff_id)
        paths.append(path)

    blobs = layout / "blobs" / "sha256"
    blobs.mkdir(parents=True)
    for path in set(paths):
        os.link(path, blobs / path.name)
    write_json(layout / "oci-layout", {"imageLayoutVersion": "1.0.0"})
    write_json(layout / "index.json", {"schemaVersion": 2, "mediaType": INDEX, "manifests": [descriptor]})
    return manifest, config


def verify_database(status: dict, database: Path, now: datetime) -> None:
    built = datetime.fromisoformat(status["built"].replace("Z", "+00:00"))
    require(status.get("valid") is True and Path(status["path"]).resolve() == database,
            "Grype did not use the selected vulnerability database")
    require(timedelta(0) <= now - built <= timedelta(hours=72), "Grype database is stale or future dated")


def blocker_findings(report: dict) -> list[dict]:
    matches, ignored = report["matches"], report.get("ignoredMatches", [])
    require(isinstance(matches, list) and isinstance(ignored, list), "Incomplete Grype findings")
    blockers = []
    for match in matches + ignored:
        finding = match["vulnerability"]
        severity = finding["severity"]
        require(severity in {"Critical", "High", "Medium", "Low", "Negligible", "Unknown"},
                "Unknown Grype severity value")
        if severity in {"Critical", "High", "Unknown"} or finding.get("fix", {}).get("versions"):
            blockers.append({"advisory": finding["id"], "severity": severity, "package": match["artifact"]["name"]})
    return blockers


def verify_sbom(sbom: dict, manifest_digest: str, manifest: dict, config: dict) -> None:
    require(sbom["descriptor"]["name"] == "syft" and sbom["descriptor"]["version"] == TOOL_VERSIONS["syft"],
            "Unexpected or incomplete Syft output")
    source = sbom["source"]
    require(source["type"] == "image" and bool(sbom["artifacts"]), "Syft image inventory is empty")
    metadata = source["metadata"]
    require(metadata["manifestDigest"] == manifest_digest and metadata["imageID"] == manifest["config"]["digest"],
            "SBOM subject does not match the selected image")
    layers = [layer["digest"] for layer in metadata["layers"]]
    require(layers in ([layer["digest"] for layer in manifest["layers"]], config["rootfs"]["diff_ids"]),
            "SBOM layer inventory does not match the verified image")


def verify_grype(report: dict, sbom: dict, status: dict) -> None:
    descriptor = report["descriptor"]
    require(descriptor["name"] == "grype" and descriptor["version"] == TOOL_VERSIONS["grype"],
            "Unexpected or incomplete Grype output")
    require(report["source"]["type"] == "image", "Grype did not scan the selected image")
    metadata = sbom["source"]["metadata"]
    target = report["source"]["target"]
    require(all(target.get(key) == metadata[key] for key in ("manifestDigest", "imageID")),
            "Grype subject does not match the verified SBOM")
    recorded = descriptor["db"]["status"]
    require(recorded["valid"] is True and recorded["built"] == status["built"] and recorded["path"] == status["path"],
            "Grype database changed during the audit")
    settings = descriptor["configuration"]
    require(not (settings.get("only-fixed") or settings.get("only-notfixed") or settings.get("ignore-wontfix"))
            and settings["search"]["scope"] == "squashed", "Grype applied unsupported filtering")


def scan(command: list[str], env: dict, root: Path, name: str, timeout: int = 600) -> dict:
    path = root / f"{name}.json"
    with path.open("xb") as output, (root / f"{name}.stderr").open("xb") as errors:
        result = subprocess.run(command, env=env, cwd=root, stdout=output, stderr=errors, timeout=timeout)
    require(result.returncode == 0, f"{name} did not complete. Raw output was retained")
    return load_json(path.read_bytes())


def scanner_inputs(output: Path, tool_dir: Path, database_dir: Path, result: dict):
    db = database_dir.resolve() / "6" / "vulnerability.db"
    config = output / "scanner-config.yaml"
    config.write_text("{}\n")
    inherited = ("PATH", "SSL_CERT_FILE", "SSL_CERT_DIR")
    env = {name: os.environ[name] for name in inherited if name in os.environ}
    env.update(HOME=str(output), GRYPE_DB_CACHE_DIR=str(db.parents[1]), GRYPE_DB_AUTO_UPDATE="false",
               GRYPE_CHECK_FOR_APP_UPDATE="false", SYFT_CHECK_FOR_APP_UPDATE="false")
    tools = {name: str(tool_dir.resolve() / name) for name in TOOL_VERSIONS}
    for name, binary in tools.items():
        version = scan([binary, "version", "-o", "json"], env, output, f"{name}-version")
        require(version["version"] == TOOL_VERSIONS[name], f"Unexpected {name} version")
        result.setdefault("tools", {})[name] = {"version": version["version"], "digest": file_digest(Path(binary))}
    status = scan([tools["grype"], "db", "status", "-o", "json"], env, output, "db-status")
    verify_database(status, db, datetime.now(timezone.utc))
    result["database"] = {"built": status["built"], "digest": file_digest(db)}
    return db, tools, env, config, status


def audit(args, expected: dict) -> int:
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=False)
    result = {"binding": expected, "complete": False, "passed": False, "images": []}
    try:
        observed = load_json(args.binding.read_bytes())
        producer_attempt = observed.get("run_attempt")
        require(type(producer_attempt) is int and 0 < producer_attempt <= expected["run_attempt"]
                and observed == {**expected, "run_attempt": producer_attempt},
                "Image lock is not from this release run")
        result["lock_producer_attempt"] = producer_attempt
        db, tools, env, config, status = scanner_inputs(output, args.tools, args.database, result)

        with tempfile.TemporaryDirectory(prefix="axern-delivery-oci-") as directory:
            work = Path(directory)
            cache = work / "cache"
            cache.mkdir()
            for key, ref in read_lock(args.lock).items():
                repository, index_digest = ref.split("@", maxsplit=1)
                image = IMAGES[key]
                registry = Registry(repository, cache, args.blob_seed)
                index = registry.fetch("manifests", index_digest, MAX_METADATA).read_bytes()
                (output / f"{image}-index.json").write_bytes(index)
                for arch, descriptor in platform_manifests(load_json(index)).items():
                    target = output / f"{image}-{arch}"
                    target.mkdir()
                    layout = work / target.name
                    manifest, image_config = prepare_layout(registry.fetch, descriptor, arch, layout, target)
                    manifest_digest, config_digest = descriptor["digest"], manifest["config"]["digest"]
                    sbom = scan([tools["syft"], "scan", f"oci-dir:{layout}", "--config", str(config), "-o", "syft-json"],
                                env, target, "syft")
                    verify_sbom(sbom, manifest_digest, manifest, image_config)
                    findings = scan([tools["grype"], f"sbom:{target / 'syft.json'}", "--config", str(config), "-o", "json"],
                                    env, target, "grype")
                    verify_grype(findings, sbom, status)
                    result["images"].append({
                        "name": image, "architecture": arch, "index": ref,
                        "manifest": manifest_digest, "config": config_digest,
                        "diff_ids": image_config["rootfs"]["diff_ids"],
                        "blockers": blocker_findings(findings),
                    })
        require(file_digest(db) == result["database"]["digest"], "Grype database changed during the audit")
        result["complete"] = True
        result["passed"] = not any(image["blockers"] for image in result["images"])
    except (KeyError, OSError, TypeError, ValueError, subprocess.TimeoutExpired) as error:
        result["error"] = str(error)
    finally:
        write_json(output / "summary.json", result)
    print(json.dumps({"passed": result["passed"], "images": len(result["images"]), "report": str(output / "summary.json")}))
    return 0 if result["passed"] else 1


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("bind", "audit"))
    parser.add_argument("--lock", type=Path, required=True)
    parser.add_argument("--binding", type=Path, required=True)
    parser.add_argument("--source", required=True)
    parser.add_argument("--run-id", type=int, required=True)
    parser.add_argument("--run-attempt", type=int, required=True)
    parser.add_argument("--tools", type=Path)
    parser.add_argument("--database", type=Path)
    parser.add_argument("--output", type=Path)
    parser.add_argument("--blob-seed", type=Path, help="Reuse blobs from a prior audit after SHA-256 validation")
    args = parser.parse_args()
    expected = lock_binding(args.lock, args.source, args.run_id, args.run_attempt)
    if args.command == "bind":
        write_json(args.binding, expected)
        return 0
    if not all((args.tools, args.database, args.output)):
        parser.error("audit requires --tools, --database and --output")
    return audit(args, expected)


if __name__ == "__main__":
    raise SystemExit(main())
