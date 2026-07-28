from __future__ import annotations

import unittest

from axern.control.admin.v1 import service_pb2
from axern_sdk.async_client import AsyncAxernClient
from axern_sdk.client import AxernClient


class SyncServiceAdminStub:
    def __init__(self) -> None:
        self.requests: list[tuple[service_pb2.PurgeServiceRequest, float | None]] = []

    def PurgeService(self, request: service_pb2.PurgeServiceRequest, timeout: float | None = None):
        self.requests.append((request, timeout))
        return service_pb2.PurgeServiceResponse(service_id=request.service_id)


class AsyncServiceAdminStub:
    def __init__(self) -> None:
        self.requests: list[tuple[service_pb2.PurgeServiceRequest, float | None]] = []

    async def PurgeService(self, request: service_pb2.PurgeServiceRequest, timeout: float | None = None):
        self.requests.append((request, timeout))
        return service_pb2.PurgeServiceResponse(service_id=request.service_id)


class FakeAsyncClient(AsyncAxernClient):
    def __init__(self, service_admin: AsyncServiceAdminStub) -> None:
        self._fake_service_admin = service_admin

    @property
    def service_admin(self):
        return self._fake_service_admin


class ServiceAdminTest(unittest.TestCase):
    def test_purge_service_uses_admin_api_with_reason(self) -> None:
        service_admin = SyncServiceAdminStub()
        client = object.__new__(AxernClient)
        client.service_admin = service_admin

        service_id = client.admin_purge_service(
            "svc-a",
            operator_reason="benchmark cleanup",
            timeout=12.0,
        )

        self.assertEqual(service_id, "svc-a")
        self.assertEqual(len(service_admin.requests), 1)
        request, timeout = service_admin.requests[0]
        self.assertEqual(request.service_id, "svc-a")
        self.assertEqual(request.operator_reason, "benchmark cleanup")
        self.assertEqual(timeout, 12.0)

    def test_purge_service_requires_reason(self) -> None:
        client = object.__new__(AxernClient)
        with self.assertRaisesRegex(ValueError, "operator_reason is required"):
            client.admin_purge_service("svc-a", operator_reason="  ")


class AsyncServiceAdminTest(unittest.IsolatedAsyncioTestCase):
    async def test_purge_service_uses_admin_api_with_reason(self) -> None:
        service_admin = AsyncServiceAdminStub()
        client = FakeAsyncClient(service_admin)

        service_id = await client.admin_purge_service(
            "svc-a",
            operator_reason="benchmark cleanup",
            timeout=12.0,
        )

        self.assertEqual(service_id, "svc-a")
        self.assertEqual(len(service_admin.requests), 1)
        request, timeout = service_admin.requests[0]
        self.assertEqual(request.service_id, "svc-a")
        self.assertEqual(request.operator_reason, "benchmark cleanup")
        self.assertEqual(timeout, 12.0)

    async def test_purge_service_requires_reason(self) -> None:
        client = FakeAsyncClient(AsyncServiceAdminStub())
        with self.assertRaisesRegex(ValueError, "operator_reason is required"):
            await client.admin_purge_service("svc-a", operator_reason="  ")
