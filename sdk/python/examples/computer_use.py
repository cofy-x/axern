"""Use a desktop-capable sandbox through the computer-use API."""

from __future__ import annotations

import os
from pathlib import Path

from axern_sdk import AxernClient, ComputerUseRegion, Sandbox

CONTROL_TARGET = os.environ.get("AXERN_ENDPOINT", "127.0.0.1:25000")
TEMPLATE_ID = os.environ.get("AXERN_TEMPLATE_ID", "desktop-base")


def main() -> None:
    client = AxernClient(CONTROL_TARGET)
    try:
        with Sandbox(client=client, template_id=TEMPLATE_ID) as sandbox:
            status = sandbox.computer_use_status()
            print(f"computer_use available={status.available} display={status.display} backend={status.backend}")
            for dependency in status.dependencies:
                print(f"dependency {dependency.name}: available={dependency.available} reason={dependency.reason}")
            if not status.available:
                raise RuntimeError(status.reason or "computer_use is unavailable")

            display = sandbox.computer_use_display()
            print(f"display {display.width}x{display.height}")

            screenshot = sandbox.computer_use_screenshot(
                region=ComputerUseRegion(x=0, y=0, width=min(display.width, 320), height=min(display.height, 180)),
            )
            output = Path("computer-use-screenshot.png")
            output.write_bytes(screenshot.data)
            print(f"wrote {output} ({screenshot.content_type}, {len(screenshot.data)} bytes)")

            sandbox.computer_use_mouse(action="move", x=10, y=10)
            sandbox.computer_use_keyboard(key="Escape")
    finally:
        client.close()


if __name__ == "__main__":
    main()
