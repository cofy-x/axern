---
title: Computer Use 与浏览器
description: 用 SDK 驱动 Sandbox 的显示、鼠标和键盘，并从 Python 自动化托管浏览器。
---

Computer Use 让 Agent 观察和操作 Sandbox 的图形会话：查询能力和会话状态、截屏，并通过节点数据面注入鼠标和键盘输入。Python SDK 还额外提供一流的托管浏览器 API，用于 Web 自动化。

所有操作都是 Sandbox 方法——作用于 Sandbox 持有的 Allocation，并要求工作负载镜像提供显示会话。

## 检查能力和状态

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

`capability_status()` 报告节点支持的能力；`computer_use_status()` 报告活跃的显示会话。

## 截屏和输入

Python：

```python
shot = sandbox.computer_use_screenshot(show_cursor=True)
sandbox.computer_use_mouse(action="click", x=640, y=360)
sandbox.computer_use_keyboard(text="hello", delay_ms=20)
sandbox.computer_use_keyboard(key="Escape")
```

Go：

```go
shot, err := sbx.ComputerUseScreenshot(ctx, axern.ComputerUseScreenshotOptions{ShowCursor: true})
err = sbx.ComputerUseMouse(ctx, axern.ComputerUseMouseOptions{Action: "click", X: 640, Y: 360})
err = sbx.ComputerUseKeyboard(ctx, axern.ComputerUseKeyboardOptions{Text: "hello", DelayMS: 20})
```

TypeScript：

```ts
const shot = await sandbox.computerUseScreenshot({ showCursor: true });
await sandbox.computerUseMouse({ action: "click", x: 640, y: 360 });
await sandbox.computerUseKeyboard({ text: "hello", delayMs: 20 });
```

截屏接受 `region`、`format`、`quality` 和 `scale`；`computer_use_display()` / `computerUseDisplay()` 描述当前显示几何。鼠标动作覆盖移动、点击、拖拽（`to_x`/`to_y`）和滚动（`direction`/`amount`），按键默认主键；键盘输入接受文本、单个 `key`（如 `Escape`）或 `keys` 组合键。

## 托管浏览器（Python）

Python SDK 可以驱动 Sandbox 内的托管浏览器，无需手动编写显示脚本：

```python
sandbox.browser_open("https://example.com")
sandbox.browser_navigate("https://example.com/docs")
sandbox.browser_click(320, 200)
sandbox.browser_type("axern", delay_ms=30)
print(sandbox.browser_status())
sandbox.browser_close()
```

`browser_resize(width, height)` 改变视口，`browser_wait(timeout_ms=...)` 等待导航稳定。每个浏览器和 Computer Use 方法在 `AsyncSandbox` 上都有异步变体。

可运行示例在仓库的 [`sdk/python/examples/computer_use.py`](https://github.com/cofy-x/axern/blob/main/sdk/python/examples/computer_use.py)。
