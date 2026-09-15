---
title: Computer Use
description: 通过绑定 Allocation 的 SDK 操作驱动 Sandbox 的显示、鼠标和键盘。
---

Computer Use 让 Agent 观察和操作 Sandbox 的图形会话：查询能力和会话状态、截屏，并通过节点数据面注入鼠标和键盘输入。

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

## 浏览器自动化属于工作负载

Axern 不拥有独立的浏览器生命周期。需要浏览器自动化的调用方通过进程执行安装和启动 Playwright、Chromium 或其他浏览器，再使用工作负载代码或 Computer Use 驱动它。浏览器版本、配置、凭据和清理因此属于不可变 Environment 及其 Allocation，而不会形成新的持久平台对象。

```python
result = sandbox.exec("python", "-c", "from playwright.sync_api import sync_playwright; print('caller-owned browser')")
```

可运行示例在仓库的 [`sdk/python/examples/computer_use.py`](https://github.com/cofy-x/axern/blob/main/sdk/python/examples/computer_use.py)。
