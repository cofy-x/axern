"""Computer-use protobuf request builders shared by sync and async clients."""

from __future__ import annotations

from axern.node.sandbox.v1 import node_pb2
from axern_sdk.node.models import (
    ComputerUseDependencyStatus,
    ComputerUseDisplay,
    ComputerUseRegion,
    ComputerUseScreenshot,
    ComputerUseStatus,
)


def computer_use_status(response: node_pb2.ComputerUseStatusResponse) -> ComputerUseStatus:
    return ComputerUseStatus(
        available=bool(response.available),
        display=response.display,
        backend=response.backend,
        reason=response.reason,
        dependencies=tuple(
            ComputerUseDependencyStatus(
                name=item.name,
                available=bool(item.available),
                reason=item.reason,
            )
            for item in response.dependencies
        ),
    )


def computer_use_display(response: node_pb2.ComputerUseDisplayResponse) -> ComputerUseDisplay:
    return ComputerUseDisplay(
        display=response.display,
        backend=response.backend,
        width=response.width,
        height=response.height,
    )


def computer_use_screenshot(response: node_pb2.ComputerUseScreenshotResponse) -> ComputerUseScreenshot:
    return ComputerUseScreenshot(data=bytes(response.data), content_type=response.content_type)


def computer_use_screenshot_request(
    allocation_id: str,
    *,
    show_cursor: bool,
    region: ComputerUseRegion | None,
    format: str,
    quality: int,
    scale: float,
) -> node_pb2.ComputerUseScreenshotRequest:
    request = node_pb2.ComputerUseScreenshotRequest(
        allocation_id=allocation_id,
        show_cursor=show_cursor,
        format=format,
        quality=quality,
        scale=scale,
    )
    if region is not None:
        request.region.CopyFrom(computer_use_region(region))
    return request


def computer_use_region(region: ComputerUseRegion) -> node_pb2.ComputerUseRegion:
    return node_pb2.ComputerUseRegion(
        x=region.x,
        y=region.y,
        width=region.width,
        height=region.height,
    )
