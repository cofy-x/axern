---
title: Python SDK
description: Create and program an Axern sandbox from Python.
---

The Python SDK offers synchronous `Sandbox` and asynchronous `AsyncSandbox`
surfaces.

```bash
uv add axern-sdk==<version>
```

The official package is published as
[`axern-sdk` on PyPI](https://pypi.org/project/axern-sdk/).

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

Use `exec_stream()` for incremental output and `process()` when you need stdin,
termination, or explicit wait behavior. Directory transfer is archive-backed
and rejects unsafe paths and links.

- [Python SDK source and full guide](https://github.com/cofy-x/axern/tree/main/sdk/python)
- [Maintained examples](https://github.com/cofy-x/axern/tree/main/sdk/python/examples)
