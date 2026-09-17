"""Frame iterator plumbing for Axern tunnel relay streams."""

from __future__ import annotations

import queue
import threading
from typing import Iterator

from axern.tunnel.v1 import tunnel_pb2


class _FrameQueue:
    def __init__(self, stop: threading.Event, *, capacity: int) -> None:
        if capacity <= 0:
            raise ValueError("tunnel frame queue capacity must be positive")
        self._queue: queue.Queue[tunnel_pb2.TunnelFrame | None] = queue.Queue(
            maxsize=capacity
        )
        self._stop = stop
        self._closed = False
        self._lock = threading.Lock()

    def __iter__(self) -> Iterator[tunnel_pb2.TunnelFrame]:
        while not self._stop.is_set() and not self._closed:
            try:
                item = self._queue.get(timeout=0.1)
            except queue.Empty:
                continue
            if item is None:
                return
            yield item

    def put(self, frame: tunnel_pb2.TunnelFrame) -> None:
        while not self._stop.is_set():
            with self._lock:
                if self._closed:
                    return
            try:
                self._queue.put(frame, timeout=0.1)
                return
            except queue.Full:
                continue

    def close(self) -> None:
        with self._lock:
            if self._closed:
                return
            self._closed = True
        while True:
            try:
                self._queue.put_nowait(None)
                return
            except queue.Full:
                try:
                    self._queue.get_nowait()
                except queue.Empty:
                    continue
