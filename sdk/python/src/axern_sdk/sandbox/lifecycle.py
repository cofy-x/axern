"""Run lifecycle helpers for SDK sandboxes."""

from __future__ import annotations

from contextlib import closing
import time

import grpc

from axern.control.run.v1 import run_pb2
from axern_sdk.client import AxernClient
from axern_sdk.errors import SandboxTimeoutError


def wait_running_run(
    client: AxernClient,
    *,
    run_id: str,
    timeout_seconds: float,
) -> run_pb2.Run:
    deadline = time.monotonic() + timeout_seconds
    last_run: run_pb2.Run | None = None
    try:
        with closing(client.watch_run(run_id, timeout=timeout_seconds)) as watch:
            for run in watch:
                last_run = run
                if run.status == run_pb2.RUN_STATUS_RUNNING and run.allocation_id:
                    return run
                if run.status in {run_pb2.RUN_STATUS_SUCCEEDED, run_pb2.RUN_STATUS_FAILED, run_pb2.RUN_STATUS_CANCELLED}:
                    raise RuntimeError(
                        f"run {run_id} became {run_pb2.RunStatus.Name(run.status)} "
                        f"before its sandbox allocation was running: {run.message}"
                    )
    except TimeoutError:
        pass
    except grpc.RpcError as exc:
        if exc.code() != grpc.StatusCode.DEADLINE_EXCEEDED or time.monotonic() < deadline:
            raise
    details = "no state observed" if last_run is None else f"{run_pb2.RunStatus.Name(last_run.status)}: {last_run.message}"
    raise SandboxTimeoutError(
        f"run {run_id} did not reach a running sandbox allocation within {timeout_seconds}s: {details}"
    )
