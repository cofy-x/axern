---
title: Python SDK
description: 用 Python 创建和编程 Axern Sandbox。
---

Python SDK 提供同步 `Sandbox` 和异步 `AsyncSandbox` 两套接口。

```bash
uv add axern-sdk==<version>
```

官方包发布在 [PyPI 的 `axern-sdk`](https://pypi.org/project/axern-sdk/)。

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

需要增量输出时用 `exec_stream()`；需要 stdin、终止或显式等待行为时用 `process()`。目录传输基于归档，并拒绝不安全的路径和链接。

- [Python SDK 源码与完整指南](https://github.com/cofy-x/axern/tree/main/sdk/python)
- [维护中的示例](https://github.com/cofy-x/axern/tree/main/sdk/python/examples)
