from __future__ import annotations

import inspect
import threading
import unittest

from axern.control.tunnel.v1 import tunnel_pb2 as control_tunnel_pb2
from axern.tunnel.v1 import tunnel_pb2
from axern_sdk import AxernClient, ConnectorConfig, TunnelConnector
from axern_sdk.tunnel.config import _GatewayTransport
from axern_sdk.tunnel.frames import _FrameQueue
from axern_sdk.tunnel.streams import _ConnectorState


class _Client:
    def _gateway_transport(self) -> _GatewayTransport:
        return _GatewayTransport(insecure=True)


class TunnelConnectorContractTest(unittest.TestCase):
    def test_public_constructor_inherits_transport_from_client(self) -> None:
        parameters = inspect.signature(TunnelConnector).parameters
        self.assertIn("client", parameters)
        self.assertNotIn("transport", parameters)

        connector = TunnelConnector(
            client=_Client(),  # type: ignore[arg-type]
            session=control_tunnel_pb2.TunnelSession(
                session_id="tun-1",
                client_edge_target="127.0.0.1:25000",
            ),
            client_token="client-token",
            local_target="127.0.0.1:8080",
        )
        self.assertNotIn("client-token", repr(connector))

    def test_frame_queue_is_bounded_and_close_unblocks_producers(self) -> None:
        stop = threading.Event()
        frames = _FrameQueue(stop, capacity=1)
        frames.put(tunnel_pb2.TunnelFrame(ping=tunnel_pb2.Ping(id="first")))
        producer_done = threading.Event()

        def produce() -> None:
            frames.put(tunnel_pb2.TunnelFrame(ping=tunnel_pb2.Ping(id="second")))
            producer_done.set()

        producer = threading.Thread(target=produce)
        producer.start()
        frames.close()
        self.assertTrue(producer_done.wait(1))
        producer.join()

    def test_connector_state_joins_heartbeat_when_stream_ends(self) -> None:
        stop = threading.Event()
        frames = _FrameQueue(stop, capacity=1)
        state = _ConnectorState(
            frames=frames,
            local_target="127.0.0.1:8080",
            stop=stop,
            config=ConnectorConfig(ping_interval_seconds=60),
        )
        state.run(iter(()))
        self.assertIsNotNone(state._heartbeat)
        assert state._heartbeat is not None
        self.assertFalse(state._heartbeat.is_alive())

    def test_real_client_satisfies_public_connector_contract(self) -> None:
        self.assertTrue(callable(getattr(AxernClient, "_gateway_transport", None)))


if __name__ == "__main__":
    unittest.main()
