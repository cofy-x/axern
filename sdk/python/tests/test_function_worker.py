from __future__ import annotations

import hashlib
import io
import json
import os
import tarfile
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from axern_sdk.function.worker import FunctionWorker, WorkerConfig


class FunctionWorkerTest(unittest.TestCase):
    def test_invokes_packaged_function(self) -> None:
        payload, digest = _bundle(
            {
                "src/handler.py": """
def init(context):
    return {"ready": True}

def hello(event, context):
    return {
        "message": context.env["GREETING"] + " " + event["name"],
        "request_id": context.request_id,
        "state": context.state,
    }
""",
            }
        )
        with tempfile.NamedTemporaryFile(suffix=".tar") as bundle:
            bundle.write(payload)
            bundle.flush()
            config = WorkerConfig(
                bundle_uri=Path(bundle.name).as_uri(),
                bundle_url="",
                bundle_token="",
                bundle_digest=digest,
                handler_ref="handler.hello",
                initializer_ref="handler.init",
                function_id="fn-1",
                function_name="hello",
                namespace="default",
                revision_id="rev-1",
            )
            with mock.patch.dict(os.environ, {"GREETING": "hello"}):
                worker = FunctionWorker.load(config)

                status, content_type, body = worker.invoke(
                    b'{"name":"Axern"}',
                    {"content-type": "application/json", "x-axern-function-request-id": "req-1"},
                )

        self.assertEqual(status, 200)
        self.assertEqual(content_type, "application/json")
        data = json.loads(body)
        self.assertEqual(data["message"], "hello Axern")
        self.assertEqual(data["request_id"], "req-1")
        self.assertEqual(data["state"], {"ready": True})

    def test_rejects_oversized_bundle(self) -> None:
        with tempfile.NamedTemporaryFile(suffix=".tar") as bundle:
            bundle.write(b"too-large")
            bundle.flush()
            config = WorkerConfig(
                bundle_uri=Path(bundle.name).as_uri(),
                bundle_url="",
                bundle_token="",
                bundle_digest="",
                handler_ref="handler.hello",
                initializer_ref="",
                function_id="fn-1",
                function_name="hello",
                namespace="default",
                revision_id="rev-1",
                max_bundle_bytes=4,
            )

            with self.assertRaisesRegex(RuntimeError, "function bundle exceeds 4 bytes"):
                FunctionWorker.load(config)


def _bundle(files: dict[str, str]) -> tuple[bytes, str]:
    buffer = io.BytesIO()
    with tarfile.open(fileobj=buffer, mode="w") as archive:
        for name, content in files.items():
            data = content.encode("utf-8")
            info = tarfile.TarInfo(name)
            info.size = len(data)
            info.mtime = 0
            archive.addfile(info, io.BytesIO(data))
    payload = buffer.getvalue()
    return payload, "sha256:" + hashlib.sha256(payload).hexdigest()


if __name__ == "__main__":
    unittest.main()
