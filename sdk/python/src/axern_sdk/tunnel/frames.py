"""Frame iterator plumbing for Axern tunnel relay streams."""

from __future__ import annotations

import queue
import threading
from typing import Iterator

from axern.tunnel.v1 import tunnel_pb2


class _FrameQueue:
    def __init__(self, stop: threading.Event) -> None:
        self._queue: queue.Queue[tunnel_pb2.TunnelFrame | None] = queue.Queue()
        self._stop = stop
        self._closed = False

    def __iter__(self) -> Iterator[tunnel_pb2.TunnelFrame]:
        while not self._stop.is_set():
            item = self._queue.get()
            if item is None:
                return
            yield item

    def put(self, frame: tunnel_pb2.TunnelFrame) -> None:
        if not self._closed:
            self._queue.put(frame)

    def close(self) -> None:
        self._closed = True
        self._queue.put(None)
