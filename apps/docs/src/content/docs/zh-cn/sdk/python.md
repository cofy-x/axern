---
title: Python SDK
description: 用 Python 创建和编程 Axern Sandbox。
---

Python SDK 提供同步 `Sandbox` 和异步 `AsyncSandbox` 两套接口。

```bash
uv add axern-sdk==<version>
```

官方包发布在 [PyPI 的 `axern-sdk`](https://pypi.org/project/axern-sdk/)。

将 SDK 锁定到 Gateway 和运行时使用的 Axern Release，仅在发布完成后安装，并在升级前阅读对应的[发布说明](https://github.com/cofy-x/axern/tree/main/docs/releases)。

```python
import os

from axern_sdk import AxernClient, Sandbox

client = AxernClient.from_context(
    os.path.expanduser("~/.config/axern/config.json")
)

with Sandbox(
    client=client,
    image="docker.io/library/python:3.12-slim",
) as sandbox:
    result = sandbox.exec(
        "python -c \"print('hello from Python')\"",
        text=True,
        check=True,
    )
    print(result.stdout)

    sandbox.write_text("/tmp/message.txt", "payload\n")
    print(sandbox.read_text("/tmp/message.txt"))

client.close()
```

只读工具镜像与 Secret 投影是显式且不可变的 Run 输入：

```python
from axern_sdk import ImageMount, SecretFile

with Sandbox(
    client=client,
    image="docker.io/library/python:3.12-slim",
    image_mounts=[ImageMount("registry.example/claude-code@sha256:<digest>", "/__claude_code")],
    secret_files=[SecretFile("/run/secrets/config", "secret-workload", "config.json")],
) as sandbox:
    sandbox.exec(["/__claude_code/bin/claude"], check=True)
```

SDK 只发送 Secret 引用，不发送明文。每个 Run 都必须显式声明自己的镜像挂载和 Secret 投影，不能从之前的 Run 自动继承。调用方本地服务的凭据保留在 sandbox 外部，sandbox 可通过 Allocation-scoped TunnelSession 访问该服务。

Secret 文件默认权限为 `0400`，显式 mode 也不得包含写权限。伪文件系统、可执行文件/系统库目录、Axern 运行时状态路径和关键系统身份文件不能作为 Secret 目标。

需要增量输出时用 `exec_stream()`；需要 stdin、终止或显式等待行为时用 `process()`。目录传输基于归档，并拒绝不安全的路径和链接。

- [Python SDK 源码与完整指南](https://github.com/cofy-x/axern/tree/main/sdk/python)
- [维护中的示例](https://github.com/cofy-x/axern/tree/main/sdk/python/examples)
