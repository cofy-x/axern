"""Recover results over a loopback gRPC fixture exposing only read operations."""
from concurrent.futures import ThreadPoolExecutor
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
from tempfile import TemporaryDirectory
import unittest
from unittest.mock import Mock

import grpc

from axern.control.run.v1 import run_pb2, run_pb2_grpc
from axern.node.sandbox.v1 import node_pb2, node_pb2_grpc
from axern_sdk import AxernClient


SDK = Path(__file__).resolve().parents[1]
DATA = b"verified worker result"


class OutputServer(run_pb2_grpc.RunControlServicer, node_pb2_grpc.NodeSandboxServicer):
    mode = "ok"

    def GetRun(self, request, context):
        if request.run_id != "run-a":
            context.abort(grpc.StatusCode.NOT_FOUND, "unknown run")
        return run_pb2.GetRunResponse(run=run_pb2.Run(id="run-a", allocation_id="alloc-a"))

    def GetSealedOutputManifest(self, request, context):
        if request.allocation_id != "alloc-a":
            context.abort(grpc.StatusCode.NOT_FOUND, "unknown allocation")
        output = node_pb2.SealedOutput(output_id="out-a", path="/job/result",
                                       size_bytes=len(DATA), sha256=hashlib.sha256(DATA).hexdigest(),
                                       status=node_pb2.SEALED_OUTPUT_STATUS_AVAILABLE)
        return node_pb2.GetSealedOutputManifestResponse(outputs=[output])

    def DownloadSealedOutput(self, request, context):
        if (request.allocation_id, request.output_id, request.offset) != ("alloc-a", "out-a", 0):
            context.abort(grpc.StatusCode.NOT_FOUND, "unknown output")
        yield node_pb2.DownloadSealedOutputResponse(data=DATA[:2], next_offset=2)
        if self.mode == "interrupt":
            context.abort(grpc.StatusCode.UNAVAILABLE, "caller disconnected")
        tail = DATA[2:] if self.mode == "ok" else b"x" * (len(DATA) - 2)
        yield node_pb2.DownloadSealedOutputResponse(data=tail, next_offset=len(DATA), eof=True)


class OutputRecoveryTest(unittest.TestCase):
    def setUp(self):
        pool = ThreadPoolExecutor(max_workers=2)
        self.addCleanup(pool.shutdown)
        self.fixture = OutputServer()
        server = grpc.server(pool)
        run_pb2_grpc.add_RunControlServicer_to_server(self.fixture, server)
        node_pb2_grpc.add_NodeSandboxServicer_to_server(self.fixture, server)
        self.endpoint = f"127.0.0.1:{server.add_insecure_port('127.0.0.1:0')}"
        server.start()
        self.addCleanup(lambda: server.stop(0).wait(5))
        directory = TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        self.target = Path(directory.name) / "result"

    def recover(self):
        env = {k: v for k, v in os.environ.items() if not k.startswith("AXERN_")}
        env.update(AXERN_ENDPOINT=self.endpoint, AXERN_PROXY_MODE="direct", PYTHONPATH=str(SDK / "src"))
        return subprocess.run([sys.executable, str(SDK / "examples/recover_sealed_output.py"),
                               "run-a", "out-a", str(self.target)], env=env,
                              capture_output=True, timeout=12, text=True)

    def test_fresh_clients_recover_without_restarting_work(self):
        for state in ("delivered", "already_delivered"):
            result = self.recover()
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(json.loads(result.stdout)["state"], state)
            self.assertEqual(self.target.read_bytes(), DATA)
        self.target.write_bytes(b"previous work")
        self.assertNotEqual(self.recover().returncode, 0)
        self.assertEqual(self.target.read_bytes(), b"previous work")

    def test_interruption_and_digest_mismatch_never_publish_partial_result(self):
        for mode in ("interrupt", "corrupt"):
            self.fixture.mode = mode
            with self.subTest(mode=mode):
                self.assertNotEqual(self.recover().returncode, 0)
                self.assertEqual(list(self.target.parent.iterdir()), [])
        self.fixture.mode = "ok"
        self.assertEqual(self.recover().returncode, 0)

    def test_full_download_requires_acknowledged_sink_writes(self):
        client = AxernClient(self.endpoint, proxy_mode="direct")
        self.addCleanup(client.close)
        for count in (0, None, len(DATA) + 1):
            sink = Mock()
            sink.write.return_value = count
            with self.subTest(count=count), self.assertRaises(OSError):
                client.download_sealed_output("run-a", "out-a", sink)
