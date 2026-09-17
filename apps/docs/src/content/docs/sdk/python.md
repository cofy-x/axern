---
title: Python SDK
description: Create and program an Axern sandbox from Python.
---

The Python SDK offers synchronous `Sandbox` and asynchronous `AsyncSandbox` surfaces.

```bash
uv add axern-sdk==<version>
```

The official package is published as [`axern-sdk` on PyPI](https://pypi.org/project/axern-sdk/).

Pin the SDK to the Axern release used by the gateway and runtime, install it only after publication completes, and review the corresponding [release notes](https://github.com/cofy-x/axern/tree/main/docs/releases) before upgrading.

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

Read-only tool images and Secret projections are explicit immutable Run inputs:

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

The SDK sends Secret references only, never plaintext. A later verification Run must declare its own mounts and Secret projections; it does not inherit them. Keep model-provider keys and certificates in the external runner and expose its loopback model gateway through an Allocation-scoped TunnelSession; do not project Provider identity into the sandbox.

Secret files default to mode `0400`; explicit modes must be read-only. Axern rejects pseudo-filesystems, executable/library trees, its runtime-state paths, and critical system identity files as Secret targets.

Use `exec_stream()` for incremental output and `process()` when you need stdin, termination, or explicit wait behavior. Directory transfer is archive-backed and rejects unsafe paths and links.

- [Python SDK source and full guide](https://github.com/cofy-x/axern/tree/main/sdk/python)
- [Maintained examples](https://github.com/cofy-x/axern/tree/main/sdk/python/examples)
