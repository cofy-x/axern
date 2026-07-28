"""Program a sandbox with exec, process, file, directory, metadata, and tunnel APIs."""

from __future__ import annotations

import os
import tempfile
from pathlib import Path

from axern_sdk import AxernClient, Sandbox, SandboxConnectionError, SandboxRpcError

CONTROL_TARGET = os.environ.get("AXERN_ENDPOINT", "127.0.0.1:25000")
TEMPLATE_ID = os.environ.get("AXERN_TEMPLATE_ID", "python311")


def main() -> None:
    client = AxernClient(CONTROL_TARGET)
    upstream = os.environ.get("AXERN_EXAMPLE_UPSTREAM", "")
    try:
        with Sandbox(
            client=client,
            template_id=TEMPLATE_ID,
            upstream=upstream,
        ) as sandbox:
            result = sandbox.exec("python -c \"print('hello from axern')\"", check=True, text=True)
            print(result.stdout, end="")

            sandbox.write_text("/tmp/message.txt", "payload\n")
            print(sandbox.read_text("/tmp/message.txt"), end="")
            sandbox.copy("/tmp/message.txt", "/tmp/message-copy.txt")
            sandbox.chmod("/tmp/message-copy.txt", 0o600)
            sandbox.touch("/tmp/message-copy.txt")

            with tempfile.TemporaryDirectory(prefix="axern-example-") as temp_dir:
                workspace = Path(temp_dir)
                source = workspace / "upload"
                download = workspace / "download"
                source.mkdir()
                source.joinpath("data.txt").write_text("directory payload\n")
                sandbox.upload_dir(source, "/tmp/example-upload", overwrite=True)
                sandbox.download_dir("/tmp/example-upload", download, overwrite=True)
                print(download.joinpath("data.txt").read_text(), end="")

            for event in sandbox.exec_stream(["python", "-c", "print('streamed')"]):
                if event.stream == "stdout":
                    print(event.text(), end="")

            with sandbox.process(["python", "-u", "-c", "import sys; print(sys.stdin.read().upper())"]) as process:
                process.write("attached process\n")
                process.close_stdin()
                for event in process.events():
                    if event.stream == "stdout":
                        print(event.text(), end="")

            metadata = sandbox.metadata
            print(f"sandbox allocation={metadata.allocation_id} node={metadata.node_id}")

            if sandbox.bound_addr:
                print(f"tunnel bound inside sandbox at {sandbox.bound_addr}")
    except SandboxConnectionError as exc:
        print(f"connection issue retryable={exc.retryable} code={exc.code}: {exc}")
        raise
    except SandboxRpcError as exc:
        print(f"sandbox RPC failed operation={exc.operation} code={exc.code} retryable={exc.retryable}: {exc}")
        raise
    finally:
        client.close()


if __name__ == "__main__":
    main()
