---
title: Python SDK
description: Create and program an Axern sandbox from Python.
---

The Python SDK offers synchronous `Sandbox` and asynchronous `AsyncSandbox` surfaces.

```bash
uv add axern-sdk==<version>
```

The official package is published as [`axern-sdk` on PyPI](https://pypi.org/project/axern-sdk/).

For v0.8.0, pin `axern-sdk==0.8.0` only after publication completes and upgrade the platform together; review the [breaking release notes](https://github.com/cofy-x/axern/blob/main/docs/releases/v0.8.0.md).

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

Use `exec_stream()` for incremental output and `process()` when you need stdin, termination, or explicit wait behavior. Directory transfer is archive-backed and rejects unsafe paths and links.

- [Python SDK source and full guide](https://github.com/cofy-x/axern/tree/main/sdk/python)
- [Maintained examples](https://github.com/cofy-x/axern/tree/main/sdk/python/examples)
