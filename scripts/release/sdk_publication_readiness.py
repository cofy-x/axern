#!/usr/bin/env python3
"""Wait until the immutable SDK release is usable through public registries."""

from __future__ import annotations

import hashlib
import json
import os
import pathlib
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request
from collections.abc import Callable, Mapping


class NotReady(RuntimeError):
    """The registry has not exposed the expected immutable release yet."""


class IntegrityError(RuntimeError):
    """The public release differs from the accepted candidate."""


def fetch_json(url: str, label: str) -> object:
    request = urllib.request.Request(
        url,
        headers={"Accept": "application/json", "User-Agent": "axern-release-readiness"},
    )
    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            return json.load(response)
    except urllib.error.HTTPError as error:
        if error.code in {404, 408, 425, 429, 500, 502, 503, 504}:
            raise NotReady(f"{label} returned HTTP {error.code}") from error
        raise IntegrityError(f"{label} returned unexpected HTTP {error.code}") from error
    except (TimeoutError, urllib.error.URLError, UnicodeDecodeError, json.JSONDecodeError) as error:
        raise NotReady(f"{label} is not readable: {error}") from error


def file_digests(directory: pathlib.Path, algorithm: str) -> dict[str, str]:
    if not directory.is_dir():
        raise IntegrityError(f"candidate artifact directory is missing: {directory}")
    digest = getattr(hashlib, algorithm)
    files = {
        path.name: digest(path.read_bytes()).hexdigest()
        for path in directory.iterdir()
        if path.is_file()
    }
    if not files:
        raise IntegrityError(f"candidate artifact directory is empty: {directory}")
    return files


def check_pypi(
    version: str,
    dist: pathlib.Path,
    fetch: Callable[[str, str], object] = fetch_json,
) -> None:
    url = f"https://pypi.org/pypi/axern-sdk/{version}/json"
    document = fetch(url, "PyPI axern-sdk")
    if not isinstance(document, dict):
        raise NotReady("PyPI axern-sdk metadata is incomplete")
    info = document.get("info")
    if not isinstance(info, dict) or info.get("version") is None:
        raise NotReady("PyPI axern-sdk metadata is incomplete")
    if info.get("version") != version:
        raise IntegrityError("PyPI axern-sdk metadata has an unexpected version")
    try:
        remote = {
            item["filename"]: item["digests"]["sha256"]
            for item in document["urls"]
        }
    except (KeyError, TypeError) as error:
        raise NotReady("PyPI axern-sdk metadata is incomplete") from error
    local = file_digests(dist / "python", "sha256")
    if remote.items() <= local.items() and remote.keys() != local.keys():
        raise NotReady("PyPI axern-sdk has not exposed every candidate file")
    if remote != local:
        raise IntegrityError(
            f"PyPI axern-sdk files differ from the candidate: remote={remote!r} local={local!r}"
        )


def check_npm(
    version: str,
    dist: pathlib.Path,
    fetch: Callable[[str, str], object] = fetch_json,
) -> None:
    url = f"https://registry.npmjs.org/@cofy-x%2Faxern-sdk/{version}"
    document = fetch(url, "npm @cofy-x/axern-sdk")
    if not isinstance(document, dict) or document.get("version") is None:
        raise NotReady("npm @cofy-x/axern-sdk metadata is incomplete")
    if document.get("version") != version:
        raise IntegrityError("npm @cofy-x/axern-sdk metadata has an unexpected version")
    tarball = dist / "typescript" / f"cofy-x-axern-sdk-{version}.tgz"
    if not tarball.is_file():
        raise IntegrityError(f"candidate npm artifact is missing: {tarball}")
    local_sha1 = hashlib.sha1(tarball.read_bytes()).hexdigest()
    try:
        remote_sha1 = document["dist"]["shasum"]
    except (KeyError, TypeError) as error:
        raise NotReady("npm @cofy-x/axern-sdk metadata is incomplete") from error
    if remote_sha1 != local_sha1:
        raise IntegrityError("npm @cofy-x/axern-sdk differs from the candidate")


def check_go(
    version: str,
    commit: str,
    run: Callable[..., subprocess.CompletedProcess[str]] = subprocess.run,
) -> None:
    module = "github.com/cofy-x/axern/sdk/go"
    with tempfile.TemporaryDirectory(prefix="axern-go-readiness-") as temporary:
        environment = os.environ.copy()
        environment.update(
            {
                "GOCACHE": f"{temporary}/build-cache",
                "GOMODCACHE": f"{temporary}/module-cache",
                "GOPROXY": "https://proxy.golang.org",
                "GOSUMDB": "sum.golang.org",
                "GOWORK": "off",
            }
        )
        result = run(
            ["go", "mod", "download", "-json", f"{module}@v{version}"],
            capture_output=True,
            check=False,
            env=environment,
            text=True,
        )
    if result.returncode != 0:
        detail = (result.stderr or result.stdout).strip()
        if "checksum mismatch" in detail.lower() or "security error" in detail.lower():
            raise IntegrityError(f"Go module checksum verification failed: {detail}")
        raise NotReady(f"Go module is not publicly resolvable: {detail}")
    try:
        document = json.loads(result.stdout)
    except json.JSONDecodeError as error:
        raise NotReady("Go module download returned incomplete metadata") from error
    if document.get("Path") != module or document.get("Version") != f"v{version}":
        raise IntegrityError("Go proxy resolved an unexpected module version")
    if not document.get("Zip") or not document.get("Sum"):
        raise NotReady("Go proxy has not exposed the complete module archive")
    origin = document.get("Origin")
    if not isinstance(origin, dict) or origin.get("Hash") != commit:
        raise IntegrityError(
            f"Go proxy origin does not match release commit {commit}: {origin!r}"
        )
    if origin.get("Ref") != f"refs/tags/sdk/go/v{version}":
        raise IntegrityError(f"Go proxy resolved an unexpected release ref: {origin!r}")


def wait_for_checks(
    checks: Mapping[str, Callable[[], None]],
    timeout_seconds: float,
    poll_seconds: float,
    monotonic: Callable[[], float] = time.monotonic,
    sleep: Callable[[float], None] = time.sleep,
) -> None:
    if timeout_seconds <= 0 or poll_seconds <= 0:
        raise ValueError("readiness timeout and poll interval must be positive")
    deadline = monotonic() + timeout_seconds
    pending = dict(checks)
    reasons: dict[str, str] = {}
    next_heartbeat = monotonic()
    while pending:
        for name, check in tuple(pending.items()):
            try:
                check()
            except NotReady as error:
                reasons[name] = str(error)
            else:
                print(f"sdk_publication_ready={name}", flush=True)
                pending.pop(name)
                reasons.pop(name, None)
        if not pending:
            return
        now = monotonic()
        if now >= deadline:
            summary = "; ".join(f"{name}: {reasons.get(name, 'not ready')}" for name in pending)
            raise NotReady(f"SDK publication readiness timed out: {summary}")
        if now >= next_heartbeat:
            summary = "; ".join(f"{name}: {reasons.get(name, 'not ready')}" for name in pending)
            print(f"sdk_publication_waiting={summary}", flush=True)
            next_heartbeat = now + 60
        sleep(min(poll_seconds, deadline - now))


def repository_root() -> pathlib.Path:
    return pathlib.Path(__file__).resolve().parents[2]


def main() -> int:
    root = repository_root()
    version = (root / "VERSION").read_text().strip()
    dist = pathlib.Path(os.environ.get("AXERN_SDK_DIST", root / "dist/sdk"))
    commit = os.environ.get("GITHUB_SHA")
    if not commit:
        commit = subprocess.check_output(
            ["git", "-C", str(root), "rev-parse", "HEAD"], text=True
        ).strip()
    try:
        timeout_seconds = float(
            os.environ.get("AXERN_PUBLICATION_READINESS_TIMEOUT_SECONDS", "1800")
        )
        poll_seconds = float(os.environ.get("AXERN_PUBLICATION_READINESS_POLL_SECONDS", "15"))
        checks = {
            "pypi": lambda: check_pypi(version, dist),
            "npm": lambda: check_npm(version, dist),
            "go": lambda: check_go(version, commit),
        }
        wait_for_checks(checks, timeout_seconds, poll_seconds)
    except (IntegrityError, NotReady, ValueError) as error:
        print(f"sdk publication readiness failed: {error}", file=sys.stderr)
        return 1
    print(f"sdk_publication_readiness_ok={version}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
