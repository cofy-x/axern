"""Async sandbox programming example."""

from __future__ import annotations

import asyncio
import os
import tempfile
from pathlib import Path

from axern_sdk import AsyncAxernClient, AsyncSandbox

CONTROL_TARGET = os.environ.get("AXERN_ENDPOINT", "127.0.0.1:25000")
TEMPLATE_ID = os.environ.get("AXERN_TEMPLATE_ID", "python311")


async def main() -> None:
    async with AsyncAxernClient(CONTROL_TARGET) as client:
        async with AsyncSandbox(client=client, template_id=TEMPLATE_ID) as sandbox:
            result = await sandbox.exec("python -c \"print('hello async')\"", check=True, text=True)
            print(result.stdout, end="")

            await sandbox.write_text("/tmp/message.txt", "payload\n")
            print(await sandbox.read_text("/tmp/message.txt"), end="")
            await sandbox.copy("/tmp/message.txt", "/tmp/message-copy.txt")
            await sandbox.chmod("/tmp/message-copy.txt", 0o600)
            await sandbox.touch("/tmp/message-copy.txt")

            with tempfile.TemporaryDirectory(prefix="axern-async-example-") as temp_dir:
                workspace = Path(temp_dir)
                source = workspace / "upload"
                download = workspace / "download"
                source.mkdir()
                await asyncio.to_thread(source.joinpath("data.txt").write_text, "directory payload\n")
                await sandbox.upload_dir(source, "/tmp/async-example-upload", overwrite=True)
                await sandbox.download_dir("/tmp/async-example-upload", download, overwrite=True)
                print(await asyncio.to_thread(download.joinpath("data.txt").read_text), end="")

            async for event in sandbox.exec_stream(["python", "-c", "print('async streamed')"]):
                if event.stream == "stdout":
                    print(event.text(), end="")

            async with await sandbox.process(["python", "-u", "-c", "import sys; print(sys.stdin.read().upper())"]) as process:
                await process.write("async attached process\n")
                await process.close_stdin()
                async for event in process.events():
                    if event.stream == "stdout":
                        print(event.text(), end="")

            metadata = sandbox.metadata
            print(f"sandbox allocation={metadata.allocation_id} node={metadata.node_id}")


if __name__ == "__main__":
    asyncio.run(main())
