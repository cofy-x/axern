---
title: Computer Use and Browser
description: Drive sandbox displays, mouse, and keyboard from the SDKs, and automate a managed browser from Python.
---

Computer Use lets an agent observe and drive a sandbox's graphical session:
query capability and session status, capture screenshots, and inject mouse and
keyboard input through the node data plane. The Python SDK additionally ships
a first-class managed Browser API for web automation.

All operations are sandbox methods — they act on the allocation the Sandbox
owns and require the workload image to provide a display session.

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

`capability_status()` reports what the node supports;
`computer_use_status()` reports the live display session.

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

Screenshots accept a `region`, `format`, `quality`, and `scale`;
`computer_use_display()` / `computerUseDisplay()` describes the current
display geometry. Mouse actions cover move, click, drag (`to_x`/`to_y`), and
scroll (`direction`/`amount`), and the button defaults to the primary button;
keyboard input accepts text, a single `key` (such as `Escape`), or a `keys`
chord.

## Managed browser (Python)

The Python SDK drives a managed browser inside the sandbox without manual
display scripting:

```python
sandbox.browser_open("https://example.com")
sandbox.browser_navigate("https://example.com/docs")
sandbox.browser_click(320, 200)
sandbox.browser_type("axern", delay_ms=30)
print(sandbox.browser_status())
sandbox.browser_close()
```

`browser_resize(width, height)` changes the viewport and
`browser_wait(timeout_ms=...)` waits for navigation to settle. Async variants
of every browser and computer-use method exist on `AsyncSandbox`.

A runnable example lives in the repository at
[`sdk/python/examples/computer_use.py`](https://github.com/cofy-x/axern/blob/main/sdk/python/examples/computer_use.py).
