"""Mount Service V1 volumes into an Axern SDK sandbox."""

from __future__ import annotations

import os

from axern_sdk import AxernClient, Sandbox, VolumeMount
from _context import current_context

TEMPLATE_ID = os.environ.get("AXERN_TEMPLATE_ID", "python311")


def main() -> None:
    context = current_context()
    client = AxernClient.from_context(os.environ.get("AXERN_CONFIG", os.path.expanduser("~/.config/axern/config.json")), context.name)
    try:
        with Sandbox(
            client=client,
            template_id=TEMPLATE_ID,
            volumes=[
                VolumeMount("data", "/data"),
                VolumeMount("cache", "/cache", readonly=True, options=("rbind",)),
            ],
        ) as sandbox:
            result = sandbox.exec(
                "python - <<'PY'\n"
                "from pathlib import Path\n"
                "for path in ('/data', '/cache'):\n"
                "    p = Path(path)\n"
                "    print(path, 'exists=', p.exists(), 'is_dir=', p.is_dir())\n"
                "PY",
                text=True,
                check=True,
            )
            print(result.stdout, end="")
    finally:
        client.close()


if __name__ == "__main__":
    main()
