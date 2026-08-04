#!/usr/bin/env python3

from __future__ import annotations

import hashlib
import json
import pathlib
import subprocess
import sys
import tempfile
import unittest
import urllib.error
from unittest import mock

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))

import sdk_publication_readiness as readiness


class PublicationReadinessTest(unittest.TestCase):
    def test_registry_404_is_retryable(self) -> None:
        error = urllib.error.HTTPError(
            "https://registry.example/version",
            404,
            "Not Found",
            hdrs=None,
            fp=None,
        )
        with mock.patch("urllib.request.urlopen", side_effect=error):
            with self.assertRaises(readiness.NotReady):
                readiness.fetch_json("https://registry.example/version", "registry")

    def test_pypi_and_npm_match_candidate_artifacts(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            dist = pathlib.Path(temporary)
            (dist / "python").mkdir()
            (dist / "typescript").mkdir()
            wheel = dist / "python" / "axern_sdk-1.2.3-py3-none-any.whl"
            source = dist / "python" / "axern_sdk-1.2.3.tar.gz"
            npm = dist / "typescript" / "cofy-x-axern-sdk-1.2.3.tgz"
            wheel.write_bytes(b"wheel")
            source.write_bytes(b"source")
            npm.write_bytes(b"npm")

            pypi = {
                "info": {"version": "1.2.3"},
                "urls": [
                    {"filename": path.name, "digests": {"sha256": hashlib.sha256(path.read_bytes()).hexdigest()}}
                    for path in (wheel, source)
                ],
            }
            npm_document = {
                "version": "1.2.3",
                "dist": {"shasum": hashlib.sha1(npm.read_bytes()).hexdigest()},
            }
            readiness.check_pypi("1.2.3", dist, lambda _url, _label: pypi)
            readiness.check_npm("1.2.3", dist, lambda _url, _label: npm_document)

    def test_pypi_digest_mismatch_is_fatal(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            dist = pathlib.Path(temporary)
            (dist / "python").mkdir()
            artifact = dist / "python" / "axern_sdk-1.2.3.tar.gz"
            artifact.write_bytes(b"candidate")
            document = {
                "info": {"version": "1.2.3"},
                "urls": [{"filename": artifact.name, "digests": {"sha256": "0" * 64}}],
            }
            with self.assertRaises(readiness.IntegrityError):
                readiness.check_pypi("1.2.3", dist, lambda _url, _label: document)

    def test_partial_pypi_file_set_is_retryable(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            dist = pathlib.Path(temporary)
            (dist / "python").mkdir()
            wheel = dist / "python" / "axern_sdk-1.2.3-py3-none-any.whl"
            source = dist / "python" / "axern_sdk-1.2.3.tar.gz"
            wheel.write_bytes(b"wheel")
            source.write_bytes(b"source")
            document = {
                "info": {"version": "1.2.3"},
                "urls": [
                    {
                        "filename": wheel.name,
                        "digests": {"sha256": hashlib.sha256(wheel.read_bytes()).hexdigest()},
                    }
                ],
            }
            with self.assertRaises(readiness.NotReady):
                readiness.check_pypi("1.2.3", dist, lambda _url, _label: document)

    def test_go_requires_public_archive_checksum_and_release_origin(self) -> None:
        document = {
            "Path": "github.com/cofy-x/axern/sdk/go",
            "Version": "v1.2.3",
            "Zip": "/tmp/module.zip",
            "Sum": "h1:example",
            "Origin": {
                "Hash": "abc123",
                "Ref": "refs/tags/sdk/go/v1.2.3",
            },
        }
        observed_environment: dict[str, str] = {}

        def run(_command: list[str], **kwargs: object) -> subprocess.CompletedProcess[str]:
            observed_environment.update(kwargs["env"])  # type: ignore[arg-type]
            return subprocess.CompletedProcess([], 0, json.dumps(document), "")

        readiness.check_go("1.2.3", "abc123", run)
        self.assertEqual(observed_environment["GOPROXY"], "https://proxy.golang.org")
        self.assertEqual(observed_environment["GOSUMDB"], "sum.golang.org")
        self.assertNotIn("direct", observed_environment["GOPROXY"])

    def test_go_unknown_revision_is_retryable(self) -> None:
        def run(_command: list[str], **_kwargs: object) -> subprocess.CompletedProcess[str]:
            return subprocess.CompletedProcess([], 1, "", "unknown revision sdk/go/v1.2.3")

        with self.assertRaises(readiness.NotReady):
            readiness.check_go("1.2.3", "abc123", run)

    def test_go_origin_mismatch_is_fatal(self) -> None:
        document = {
            "Path": "github.com/cofy-x/axern/sdk/go",
            "Version": "v1.2.3",
            "Zip": "/tmp/module.zip",
            "Sum": "h1:example",
            "Origin": {
                "Hash": "different",
                "Ref": "refs/tags/sdk/go/v1.2.3",
            },
        }

        def run(_command: list[str], **_kwargs: object) -> subprocess.CompletedProcess[str]:
            return subprocess.CompletedProcess([], 0, json.dumps(document), "")

        with self.assertRaisesRegex(readiness.IntegrityError, "release commit"):
            readiness.check_go("1.2.3", "abc123", run)

    def test_wait_retries_only_pending_checks(self) -> None:
        clock = [0.0]
        attempts = {"ready": 0, "delayed": 0}

        def ready() -> None:
            attempts["ready"] += 1

        def delayed() -> None:
            attempts["delayed"] += 1
            if attempts["delayed"] < 3:
                raise readiness.NotReady("registry returned HTTP 404")

        def sleep(seconds: float) -> None:
            clock[0] += seconds

        readiness.wait_for_checks(
            {"ready": ready, "delayed": delayed},
            timeout_seconds=10,
            poll_seconds=1,
            monotonic=lambda: clock[0],
            sleep=sleep,
        )
        self.assertEqual(attempts, {"ready": 1, "delayed": 3})

    def test_wait_reports_timeout(self) -> None:
        clock = [0.0]

        def pending() -> None:
            raise readiness.NotReady("still propagating")

        def sleep(seconds: float) -> None:
            clock[0] += seconds

        with self.assertRaisesRegex(readiness.NotReady, "still propagating"):
            readiness.wait_for_checks(
                {"go": pending},
                timeout_seconds=2,
                poll_seconds=1,
                monotonic=lambda: clock[0],
                sleep=sleep,
            )


if __name__ == "__main__":
    unittest.main()
