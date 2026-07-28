"""Load and inspect an Axern Function resource spec."""

from __future__ import annotations

import os

from axern_sdk import AxernClient, Function
from _context import current_context

FUNCTION_SPEC = os.environ.get("AXERN_FUNCTION_SPEC", "examples/function-hello/function.yaml")


def main() -> None:
    context = current_context()
    client = AxernClient.from_context(os.environ.get("AXERN_CONFIG", os.path.expanduser("~/.config/axern/config.json")), context.name)
    try:
        function = Function.from_file(client, FUNCTION_SPEC)
        spec = function.spec
        print(f"name={spec.name}")
        print(f"runtime={spec.runtime}")
        print(f"handler={spec.handler}")
        print(f"source={spec.source.root}")
        print(f"timeout_seconds={spec.timeout_seconds}")
        print(f"volumes={len(spec.volumes)}")
    finally:
        client.close()


if __name__ == "__main__":
    main()
