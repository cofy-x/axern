from __future__ import annotations

import unittest
from unittest import mock

import grpc

from axern.control.service.v1 import service_event_pb2, service_pb2, service_replica_pb2, service_types_pb2
from axern_sdk.async_client import AsyncAxernClient
from axern_sdk.client import AxernClient


def watch_response(version: int) -> service_pb2.WatchServiceResponse:
    return service_pb2.WatchServiceResponse(
        service=service_types_pb2.Service(
            id="svc-a",
            version=version,
            status=service_types_pb2.SERVICE_STATUS_RECONCILING,
        )
    )


class UnavailableRpcError(grpc.RpcError):
    def code(self) -> grpc.StatusCode:
        return grpc.StatusCode.UNAVAILABLE


class DeadlineExceededRpcError(grpc.RpcError):
    def code(self) -> grpc.StatusCode:
        return grpc.StatusCode.DEADLINE_EXCEEDED


class UnavailableAioRpcError(grpc.aio.AioRpcError):
    def __init__(self) -> None:
        super().__init__(grpc.StatusCode.UNAVAILABLE, (), (), "scripted failure")


class SyncWatchCall:
    def __init__(self, responses: list[service_pb2.WatchServiceResponse], unavailable: bool) -> None:
        self.responses = iter(responses)
        self.unavailable = unavailable
        self.cancelled = False

    def __iter__(self):
        return self

    def __next__(self) -> service_pb2.WatchServiceResponse:
        try:
            return next(self.responses)
        except StopIteration:
            if self.unavailable:
                self.unavailable = False
                raise UnavailableRpcError() from None
            raise

    def cancel(self) -> None:
        self.cancelled = True


class SyncServiceStub:
    def __init__(self) -> None:
        self.calls = [
            SyncWatchCall([watch_response(2)], unavailable=True),
            SyncWatchCall([watch_response(2), watch_response(3)], unavailable=False),
        ]
        self.after_versions: list[int] = []

    def WatchService(self, request, timeout=None):
        del timeout
        self.after_versions.append(request.after_version)
        return self.calls[len(self.after_versions) - 1]


class AsyncWatchCall:
    def __init__(self, responses: list[service_pb2.WatchServiceResponse], unavailable: bool) -> None:
        self.responses = iter(responses)
        self.unavailable = unavailable
        self.cancelled = False

    def __aiter__(self):
        return self

    async def __anext__(self) -> service_pb2.WatchServiceResponse:
        try:
            return next(self.responses)
        except StopIteration:
            if self.unavailable:
                self.unavailable = False
                raise UnavailableAioRpcError() from None
            raise StopAsyncIteration from None

    def cancel(self) -> None:
        self.cancelled = True


class AsyncServiceStub:
    def __init__(self) -> None:
        self.calls = [
            AsyncWatchCall([watch_response(2)], unavailable=True),
            AsyncWatchCall([watch_response(2), watch_response(3)], unavailable=False),
        ]
        self.after_versions: list[int] = []

    def WatchService(self, request, timeout=None):
        del timeout
        self.after_versions.append(request.after_version)
        return self.calls[len(self.after_versions) - 1]


class FakeAsyncClient(AsyncAxernClient):
    def __init__(self, services: AsyncServiceStub) -> None:
        self._fake_services = services

    @property
    def services(self):
        return self._fake_services


class ServiceWatchTest(unittest.TestCase):
    def test_sync_watch_resumes_and_suppresses_duplicate_versions(self) -> None:
        services = SyncServiceStub()
        client = object.__new__(AxernClient)
        client.services = services

        with mock.patch("axern_sdk.client.time.sleep"):
            watch = client.watch_service("svc-a", after_version=1)
            self.assertEqual(next(watch).version, 2)
            self.assertEqual(next(watch).version, 3)
            watch.close()

        self.assertEqual(services.after_versions, [1, 2])
        self.assertTrue(all(call.cancelled for call in services.calls))


class AsyncServiceWatchTest(unittest.IsolatedAsyncioTestCase):
    async def test_async_watch_resumes_and_suppresses_duplicate_versions(self) -> None:
        services = AsyncServiceStub()
        client = FakeAsyncClient(services)

        with mock.patch("axern_sdk.async_client.asyncio.sleep", new=mock.AsyncMock()):
            watch = client.watch_service("svc-a", after_version=1)
            self.assertEqual((await anext(watch)).version, 2)
            self.assertEqual((await anext(watch)).version, 3)
            await watch.aclose()

        self.assertEqual(services.after_versions, [1, 2])
        self.assertTrue(all(call.cancelled for call in services.calls))


class SyncReplicaListStub:
    def __init__(self) -> None:
        self.calls = 0

    def ListServiceReplicas(self, request, timeout=None):
        del request, timeout
        self.calls += 1
        if self.calls == 1:
            raise UnavailableRpcError()
        return service_replica_pb2.ListServiceReplicasResponse(
            replicas=[service_replica_pb2.ServiceReplica(id="alloc-a")]
        )


class AsyncReplicaListStub:
    def __init__(self) -> None:
        self.calls = 0

    async def ListServiceReplicas(self, request, timeout=None):
        del request, timeout
        self.calls += 1
        if self.calls == 1:
            raise UnavailableAioRpcError()
        return service_replica_pb2.ListServiceReplicasResponse(
            replicas=[service_replica_pb2.ServiceReplica(id="alloc-a")]
        )


class ServiceReplicaListRetryTest(unittest.TestCase):
    def test_sync_list_retries_unavailable(self) -> None:
        services = SyncReplicaListStub()
        client = object.__new__(AxernClient)
        client.services = services

        with mock.patch("axern_sdk.client.time.sleep"):
            replicas = client.list_service_replicas("svc-a")

        self.assertEqual([replica.id for replica in replicas], ["alloc-a"])
        self.assertEqual(services.calls, 2)

    def test_sync_list_retries_call_deadline_before_overall_deadline(self) -> None:
        services = SyncReplicaListStub()
        client = object.__new__(AxernClient)
        client.services = services
        services.calls = 0
        original = services.ListServiceReplicas

        def deadline_then_success(request, timeout=None):
            if services.calls == 0:
                services.calls += 1
                raise DeadlineExceededRpcError()
            return original(request, timeout)

        services.ListServiceReplicas = deadline_then_success
        with mock.patch("axern_sdk.client.time.sleep"):
            replicas = client.list_service_replicas("svc-a")

        self.assertEqual([replica.id for replica in replicas], ["alloc-a"])


class AsyncServiceReplicaListRetryTest(unittest.IsolatedAsyncioTestCase):
    async def test_async_list_retries_unavailable(self) -> None:
        client = FakeAsyncClient(AsyncReplicaListStub())

        with mock.patch("axern_sdk.async_client.asyncio.sleep", new=mock.AsyncMock()):
            replicas = await client.list_service_replicas("svc-a")

        self.assertEqual([replica.id for replica in replicas], ["alloc-a"])
        self.assertEqual(client.services.calls, 2)


class SyncServiceReadStub:
    def __init__(self) -> None:
        self.cursors: list[str] = []
        self.status_filters: list[list[int]] = []

    def GetService(self, request, timeout=None):
        del timeout
        return service_pb2.GetServiceResponse(service=service_types_pb2.Service(id=request.service_id))

    def ListServices(self, request, timeout=None):
        del timeout
        cursor = request.filter.cursor
        self.cursors.append(cursor)
        self.status_filters.append(list(request.filter.statuses))
        if not cursor:
            return service_pb2.ListServicesResponse(
                services=[service_types_pb2.Service(id="svc-a")],
                next_cursor="next",
            )
        return service_pb2.ListServicesResponse(services=[service_types_pb2.Service(id="svc-b")])

    def ListServiceEvents(self, request, timeout=None):
        del timeout
        return service_event_pb2.ListServiceEventsResponse(
            events=[service_event_pb2.ServiceEvent(service_id=request.service_id)]
        )


class ServiceReadTest(unittest.TestCase):
    def setUp(self) -> None:
        self.stub = SyncServiceReadStub()
        self.client = object.__new__(AxernClient)
        self.client.services = self.stub

    def test_get_service_returns_service(self) -> None:
        self.assertEqual(self.client.get_service("svc-a").id, "svc-a")

    def test_list_services_follows_cursor(self) -> None:
        statuses = (status for status in [service_types_pb2.SERVICE_STATUS_READY])
        services = self.client.list_services(namespace="default", statuses=statuses, labels={"run": "a"})

        self.assertEqual([service.id for service in services], ["svc-a", "svc-b"])
        self.assertEqual(self.stub.cursors, ["", "next"])
        self.assertEqual(
            self.stub.status_filters,
            [[service_types_pb2.SERVICE_STATUS_READY], [service_types_pb2.SERVICE_STATUS_READY]],
        )

    def test_list_service_events_returns_events(self) -> None:
        events = self.client.list_service_events("svc-a")

        self.assertEqual([event.service_id for event in events], ["svc-a"])
