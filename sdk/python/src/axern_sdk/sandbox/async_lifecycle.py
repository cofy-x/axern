"""Async service lifecycle helpers for SDK sandboxes."""

from __future__ import annotations

import asyncio

import grpc

from axern.control.common.v1 import common_pb2
from axern.control.service.v1 import service_replica_pb2, service_types_pb2
from axern_sdk.async_client import AsyncAxernClient
from axern_sdk.errors import SandboxTimeoutError


async def wait_ready_replica(
    client: AsyncAxernClient,
    *,
    service_id: str,
    timeout_seconds: float,
) -> service_replica_pb2.ServiceReplica:
    loop = asyncio.get_running_loop()
    deadline = loop.time() + timeout_seconds
    last_replicas: list[service_replica_pb2.ServiceReplica] = []
    watch = client.watch_service(service_id, timeout=timeout_seconds)
    try:
        try:
            async for service in watch:
                remaining = max(0.1, min(30.0, deadline - loop.time()))
                last_replicas = await client.list_service_replicas(
                    service_id,
                    current_only=True,
                    timeout=remaining,
                )
                candidates = [
                    replica
                    for replica in last_replicas
                    if replica.ready
                    and not replica.ended
                    and not replica.outdated
                    and replica.status == common_pb2.ALLOCATION_STATUS_RUNNING
                ]
                if candidates:
                    return sorted(candidates, key=lambda replica: replica.id)[0]
                if service.status in {
                    service_types_pb2.SERVICE_STATUS_FAILED,
                    service_types_pb2.SERVICE_STATUS_DELETING,
                    service_types_pb2.SERVICE_STATUS_DELETED,
                }:
                    raise RuntimeError(
                        f"service {service_id} became {service_types_pb2.ServiceStatus.Name(service.status)} "
                        f"before a sandbox replica was ready: {service.message}"
                    )
        except TimeoutError:
            pass
        except grpc.aio.AioRpcError as exc:
            if exc.code() != grpc.StatusCode.DEADLINE_EXCEEDED or loop.time() < deadline:
                raise
    finally:
        await watch.aclose()
    details = ", ".join(f"{replica.id}:{replica.status}:{replica.message}" for replica in last_replicas)
    raise SandboxTimeoutError(
        f"service {service_id} did not produce a ready sandbox replica within {timeout_seconds}s: {details}"
    )


async def wait_service_deleted(
    client: AsyncAxernClient,
    *,
    service_id: str,
    timeout_seconds: float,
) -> None:
    deadline = asyncio.get_running_loop().time() + max(timeout_seconds, 120.0)
    while True:
        try:
            service = await client.get_service(service_id, timeout=30.0)
        except grpc.aio.AioRpcError as exc:
            if exc.code() == grpc.StatusCode.NOT_FOUND:
                return
            if asyncio.get_running_loop().time() >= deadline:
                raise
        else:
            if service.status == service_types_pb2.SERVICE_STATUS_DELETED:
                return
            if asyncio.get_running_loop().time() >= deadline:
                raise SandboxTimeoutError(f"service {service_id} did not finish deletion within {timeout_seconds}s")
        await asyncio.sleep(2.0)
