---
title: Computer Use
description: Drive sandbox displays, mouse, and keyboard through Allocation-scoped SDK operations.
---

Computer Use lets an agent observe and drive a sandbox's graphical session: query capability and session status, capture screenshots, and inject mouse and keyboard input through the node data plane.

All operations are sandbox methods — they act on the allocation the Sandbox owns and require the workload image to provide a display session.

## Check capability and status

```python
import os

from axern_sdk import AxernClient, Sandbox

client = AxernClient.from_context(
    os.path.expanduser("~/.config/axern/config.json")
)

with Sandbox(client, image="docker.io/library/python:3.12-slim") as sandbox:
    print(sandbox.capability_status())
    print(sandbox.computer_use_status())

client.close()
```

`capability_status()` reports what the node supports; `computer_use_status()` reports the live display session.

## Screenshots and input

Python:

```python
shot = sandbox.computer_use_screenshot(show_cursor=True)
sandbox.computer_use_mouse(action="click", x=640, y=360)
sandbox.computer_use_keyboard(text="hello", delay_ms=20)
sandbox.computer_use_keyboard(key="Escape")
```

Go:

```go
shot, err := sbx.ComputerUseScreenshot(ctx, axern.ComputerUseScreenshotOptions{ShowCursor: true})
err = sbx.ComputerUseMouse(ctx, axern.ComputerUseMouseOptions{Action: "click", X: 640, Y: 360})
err = sbx.ComputerUseKeyboard(ctx, axern.ComputerUseKeyboardOptions{Text: "hello", DelayMS: 20})
```

TypeScript:

```ts
const shot = await sandbox.computerUseScreenshot({ showCursor: true });
await sandbox.computerUseMouse({ action: "click", x: 640, y: 360 });
await sandbox.computerUseKeyboard({ text: "hello", delayMs: 20 });
```

Screenshots accept a `region`, `format`, `quality`, and `scale`; `computer_use_display()` / `computerUseDisplay()` describes the current display geometry. Mouse actions cover move, click, drag (`to_x`/`to_y`), and scroll (`direction`/`amount`), and the button defaults to the primary button; keyboard input accepts text, a single `key` (such as `Escape`), or a `keys` chord.

## Browser automation belongs to the workload

Axern does not own a separate browser lifecycle. A caller that needs browser automation installs and starts Playwright, Chromium, or another browser through process execution, then drives it with workload code or Computer Use. This keeps the browser version, profile, credentials, and cleanup inside the immutable Environment and its Allocation rather than creating another durable platform object.

```python
result = sandbox.exec("python", "-c", "from playwright.sync_api import sync_playwright; print('caller-owned browser')")
```

A runnable example lives in the repository at [`sdk/python/examples/computer_use.py`](https://github.com/cofy-x/axern/blob/main/sdk/python/examples/computer_use.py).
