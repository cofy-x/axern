from __future__ import annotations

import unittest
import tarfile
from io import BytesIO
from pathlib import Path
from tempfile import TemporaryDirectory

from axern_sdk import AsyncSandbox, BrowserStatus, ComputerUseScreenshot, ComputerUseStatus, Sandbox, SandboxFileInfo, SandboxFileKind
from fakes import _AsyncFakeClient, _FakeClient


class SandboxTest(unittest.TestCase):

    def test_file_helpers_use_node_file_rpc(self) -> None:
        client = _FakeClient()
        read_calls = []
        write_calls = []

        class FakeNodeClient:
            def __init__(self, **kwargs) -> None:
                del kwargs

            def read_file(self, path, **kwargs):
                read_calls.append((path, kwargs))
                return b"hello"

            def write_file(self, path, data, **kwargs):
                write_calls.append((path, data, kwargs))

            def computer_use_status(self, **kwargs):
                return ComputerUseStatus(available=True, display=":99", backend="x11")

            def computer_use_screenshot(self, **kwargs):
                return ComputerUseScreenshot(data=b"png", content_type="image/png")

            def browser_status(self, **kwargs):
                return BrowserStatus(available=True, command="chromium")

            def browser_open(self, url="", **kwargs):
                return BrowserStatus(available=True, command="chromium", running=True, pid=88, url=url)

            def browser_close(self, **kwargs):
                return BrowserStatus(available=True, command="chromium")

        with Sandbox(
            client=client,
            image="docker.io/library/python:3.12-slim",
            _node_client_factory=FakeNodeClient,
        ) as sandbox:
            self.assertEqual(sandbox.read_file("/tmp/out.txt"), "hello")
            self.assertEqual(sandbox.read_bytes("/tmp/out.txt"), b"hello")
            self.assertEqual(sandbox.read_text("/tmp/out.txt"), "hello")
            self.assertEqual(sandbox.computer_use_status().display, ":99")
            self.assertEqual(sandbox.computer_use_screenshot().data, b"png")
            self.assertEqual(sandbox.browser_status().command, "chromium")
            self.assertEqual(sandbox.browser_open("data:text/html,open").url, "data:text/html,open")
            self.assertFalse(sandbox.browser_close().running)
            sandbox.write_file("/tmp/out.txt", "hello")
            sandbox.write_bytes("/tmp/raw.bin", b"\xff", create_parents=False)
            sandbox.write_text("/tmp/latin1.txt", "é", encoding="latin-1")

        self.assertEqual([path for path, _ in read_calls], ["/tmp/out.txt", "/tmp/out.txt", "/tmp/out.txt"])
        self.assertEqual(write_calls[0], ("/tmp/out.txt", b"hello", {"create_parents": True, "rpc_timeout": 30}))
        self.assertEqual(write_calls[1], ("/tmp/raw.bin", b"\xff", {"create_parents": False, "rpc_timeout": 30}))
        self.assertEqual(write_calls[2], ("/tmp/latin1.txt", b"\xe9", {"create_parents": True, "rpc_timeout": 30}))

    def test_file_mutation_helpers_use_node_file_rpc(self) -> None:
        client = _FakeClient()
        calls = []

        class FakeNodeClient:
            def __init__(self, **kwargs) -> None:
                del kwargs

            def copy(self, *args, **kwargs):
                calls.append(("copy", args, kwargs))

            def move(self, *args, **kwargs):
                calls.append(("move", args, kwargs))

            def chmod(self, *args, **kwargs):
                calls.append(("chmod", args, kwargs))

            def touch(self, *args, **kwargs):
                calls.append(("touch", args, kwargs))

        with Sandbox(
            client=client,
            image="docker.io/library/python:3.12-slim",
            runtime_class="runsc",
            _node_client_factory=FakeNodeClient,
        ) as sandbox:
            metadata = sandbox.metadata
            sandbox.copy("/tmp/a", "/tmp/b", recursive=True, overwrite=False)
            sandbox.move("/tmp/b", "/tmp/c", overwrite=False)
            sandbox.chmod("/tmp/c", 0o600, recursive=True)
            sandbox.touch("/tmp/c", create=False, mtime_ns=7)

        self.assertEqual(metadata.runtime_class, "runsc")
        self.assertEqual(metadata.allocation_id, "alloc-1")
        self.assertGreater(metadata.started_at_ns, 0)
        self.assertEqual(calls[0], ("copy", ("/tmp/a", "/tmp/b"), {"recursive": True, "overwrite": False, "rpc_timeout": 30}))
        self.assertEqual(calls[1], ("move", ("/tmp/b", "/tmp/c"), {"overwrite": False, "rpc_timeout": 30}))
        self.assertEqual(calls[2], ("chmod", ("/tmp/c", 0o600), {"recursive": True, "rpc_timeout": 30}))
        self.assertEqual(calls[3], ("touch", ("/tmp/c",), {"create": False, "mtime_ns": 7, "rpc_timeout": 30}))

    def test_upload_and_download_file_use_bytes_helpers(self) -> None:
        client = _FakeClient()
        remote_data = b"downloaded"
        write_calls = []

        class FakeNodeClient:
            def __init__(self, **kwargs) -> None:
                del kwargs

            def read_file(self, path, **kwargs):
                self.read_call = (path, kwargs)
                return remote_data

            def write_file(self, path, data, **kwargs):
                write_calls.append((path, data, kwargs))

        with TemporaryDirectory() as tmp:
            root = Path(tmp)
            source = root / "source.bin"
            target = root / "nested" / "target.bin"
            source.write_bytes(b"payload")

            with Sandbox(
                client=client,
                image="docker.io/library/python:3.12-slim",
                _node_client_factory=FakeNodeClient,
            ) as sandbox:
                sandbox.upload_file(source, "/tmp/source.bin", create_parents=False)
                sandbox.download_file("/tmp/result.bin", target)

            self.assertEqual(target.read_bytes(), remote_data)

        self.assertEqual(write_calls[0], ("/tmp/source.bin", b"payload", {"create_parents": False, "rpc_timeout": 30}))

    def test_download_file_rejects_existing_target_without_overwrite(self) -> None:
        client = _FakeClient()

        class FakeNodeClient:
            def __init__(self, **kwargs) -> None:
                del kwargs

            def read_file(self, path, **kwargs):
                del path, kwargs
                return b"new"

        with TemporaryDirectory() as tmp:
            target = Path(tmp) / "target.bin"
            target.write_bytes(b"old")
            with Sandbox(
                client=client,
                image="docker.io/library/python:3.12-slim",
                _node_client_factory=FakeNodeClient,
            ) as sandbox:
                with self.assertRaises(FileExistsError):
                    sandbox.download_file("/tmp/result.bin", target, overwrite=False)
            self.assertEqual(target.read_bytes(), b"old")

    def test_upload_and_download_dir_use_node_archive_rpc(self) -> None:
        client = _FakeClient()
        upload_calls = []

        class FakeNodeClient:
            def __init__(self, **kwargs) -> None:
                del kwargs

            def upload_archive(self, path, chunk_factory, **kwargs):
                upload_calls.append((path, b"".join(chunk_factory()), kwargs))

            def download_archive(self, path, writer, **kwargs):
                del path, kwargs
                writer(_tar_bytes({"nested/data.txt": b"data", "root.txt": b"root"}))

        with TemporaryDirectory() as tmp:
            root = Path(tmp)
            source = root / "tree"
            (source / "nested").mkdir(parents=True)
            (source / "nested" / "data.txt").write_bytes(b"data")
            (source / "empty").mkdir()
            target = root / "downloaded"

            with Sandbox(
                client=client,
                image="docker.io/library/python:3.12-slim",
                _node_client_factory=FakeNodeClient,
            ) as sandbox:
                sandbox.upload_dir(source, "/tmp/tree", create_parents=False, overwrite=False)
                sandbox.download_dir("/tmp/tree", target)

            self.assertEqual((target / "nested" / "data.txt").read_bytes(), b"data")
            self.assertEqual((target / "root.txt").read_bytes(), b"root")

        self.assertEqual(upload_calls[0][0], "/tmp/tree")
        self.assertEqual(upload_calls[0][2], {"create_parents": False, "overwrite": False, "rpc_timeout": 30})
        self.assertIn("nested/data.txt", _tar_names(upload_calls[0][1]))
        self.assertIn("empty", _tar_names(upload_calls[0][1]))

    def test_upload_dir_rejects_local_symlinks(self) -> None:
        client = _FakeClient()

        class FakeNodeClient:
            def __init__(self, **kwargs) -> None:
                del kwargs

        with TemporaryDirectory() as tmp:
            source = Path(tmp) / "tree"
            source.mkdir()
            (source / "target.txt").write_text("target")
            (source / "link.txt").symlink_to(source / "target.txt")
            with Sandbox(
                client=client,
                image="docker.io/library/python:3.12-slim",
                _node_client_factory=FakeNodeClient,
            ) as sandbox:
                with self.assertRaises(ValueError):
                    sandbox.upload_dir(source, "/tmp/tree")

    def test_download_dir_rejects_unsafe_archive_entries(self) -> None:
        client = _FakeClient()

        class FakeNodeClient:
            def __init__(self, **kwargs) -> None:
                del kwargs

            def download_archive(self, path, writer, **kwargs):
                del path, kwargs
                writer(_tar_bytes({"../escape.txt": b"no"}))

        with TemporaryDirectory() as tmp:
            with Sandbox(
                client=client,
                image="docker.io/library/python:3.12-slim",
                _node_client_factory=FakeNodeClient,
            ) as sandbox:
                with self.assertRaises(ValueError):
                    sandbox.download_dir("/tmp/tree", Path(tmp) / "tree")

    def test_download_dir_rejects_existing_local_symlink_targets(self) -> None:
        client = _FakeClient()

        class FakeNodeClient:
            def __init__(self, **kwargs) -> None:
                del kwargs

            def download_archive(self, path, writer, **kwargs):
                del path, kwargs
                writer(_tar_bytes({"link/data.txt": b"no"}))

        with TemporaryDirectory() as tmp:
            root = Path(tmp)
            target = root / "tree"
            outside = root / "outside"
            target.mkdir()
            outside.mkdir()
            (target / "link").symlink_to(outside, target_is_directory=True)
            with Sandbox(
                client=client,
                image="docker.io/library/python:3.12-slim",
                _node_client_factory=FakeNodeClient,
            ) as sandbox:
                with self.assertRaises(ValueError):
                    sandbox.download_dir("/tmp/tree", target)

    def test_file_helpers_reject_empty_paths(self) -> None:
        client = _FakeClient()

        with Sandbox(client=client, image="docker.io/library/python:3.12-slim") as sandbox:
            for call in (
                lambda: sandbox.read_bytes(""),
                lambda: sandbox.write_bytes("", b"data"),
                lambda: sandbox.exists(""),
                lambda: sandbox.stat(""),
                lambda: sandbox.list_dir(""),
                lambda: sandbox.mkdir(""),
                lambda: sandbox.remove(""),
            ):
                with self.assertRaises(ValueError):
                    call()

    def test_file_management_helpers_use_node_file_rpc(self) -> None:
        client = _FakeClient()
        exists_calls = []
        stat_calls = []
        list_calls = []
        mkdir_calls = []
        remove_calls = []

        class FakeNodeClient:
            def __init__(self, **kwargs) -> None:
                del kwargs

            def exists(self, path, **kwargs):
                exists_calls.append((path, kwargs))
                return True

            def stat_file(self, path, **kwargs):
                stat_calls.append((path, kwargs))
                return SandboxFileInfo(path="/tmp/out.txt", kind=SandboxFileKind.FILE, size=5, mode=420, mtime_ns=7)

            def list_dir(self, path, **kwargs):
                list_calls.append((path, kwargs))
                return [SandboxFileInfo(path="/tmp/out.txt", kind=SandboxFileKind.FILE, size=5, mode=420, mtime_ns=7)]

            def mkdir(self, path, **kwargs):
                mkdir_calls.append((path, kwargs))

            def remove(self, path, **kwargs):
                remove_calls.append((path, kwargs))

        with Sandbox(
            client=client,
            image="docker.io/library/python:3.12-slim",
            _node_client_factory=FakeNodeClient,
        ) as sandbox:
            self.assertTrue(sandbox.exists("/tmp/out.txt"))
            info = sandbox.stat("/tmp/out.txt")
            entries = sandbox.list_dir("/tmp")
            sandbox.mkdir("/tmp/nested")
            sandbox.remove("/tmp/nested", recursive=True, force=True)

        self.assertEqual(info.path, "/tmp/out.txt")
        self.assertEqual(info.kind, SandboxFileKind.FILE)
        self.assertEqual(entries[0].path, "/tmp/out.txt")
        self.assertEqual(exists_calls[0][0], "/tmp/out.txt")
        self.assertEqual(stat_calls[0][0], "/tmp/out.txt")
        self.assertEqual(list_calls[0][0], "/tmp")
        self.assertEqual(mkdir_calls[0], ("/tmp/nested", {"parents": True, "rpc_timeout": 30}))
        self.assertEqual(remove_calls[0], ("/tmp/nested", {"recursive": True, "force": True, "rpc_timeout": 30}))



class AsyncSandboxTest(unittest.IsolatedAsyncioTestCase):

    async def test_async_file_helpers_use_node_file_rpc(self) -> None:
        client = _AsyncFakeClient()
        read_calls = []
        write_calls = []

        class FakeAsyncNodeClient:
            def __init__(self, **kwargs) -> None:
                del kwargs

            async def read_file(self, path, **kwargs):
                read_calls.append((path, kwargs))
                return b"hello"

            async def write_file(self, path, data, **kwargs):
                write_calls.append((path, data, kwargs))

            async def computer_use_status(self, **kwargs):
                return ComputerUseStatus(available=True, display=":99", backend="x11")

            async def computer_use_screenshot(self, **kwargs):
                return ComputerUseScreenshot(data=b"png", content_type="image/png")

            async def browser_status(self, **kwargs):
                return BrowserStatus(available=True, command="chromium")

            async def browser_open(self, url="", **kwargs):
                return BrowserStatus(available=True, command="chromium", running=True, pid=88, url=url)

            async def browser_close(self, **kwargs):
                return BrowserStatus(available=True, command="chromium")

        async with AsyncSandbox(
            client=client,
            image="docker.io/library/python:3.12-slim",
            _node_client_factory=FakeAsyncNodeClient,
        ) as sandbox:
            self.assertEqual(await sandbox.read_file("/tmp/out.txt"), "hello")
            self.assertEqual(await sandbox.read_bytes("/tmp/out.txt"), b"hello")
            self.assertEqual(await sandbox.read_text("/tmp/out.txt"), "hello")
            self.assertEqual((await sandbox.computer_use_status()).display, ":99")
            self.assertEqual((await sandbox.computer_use_screenshot()).data, b"png")
            self.assertEqual((await sandbox.browser_status()).command, "chromium")
            self.assertEqual((await sandbox.browser_open("data:text/html,open")).url, "data:text/html,open")
            self.assertFalse((await sandbox.browser_close()).running)
            await sandbox.write_file("/tmp/out.txt", "hello")
            await sandbox.write_bytes("/tmp/raw.bin", b"\xff", create_parents=False)
            await sandbox.write_text("/tmp/latin1.txt", "é", encoding="latin-1")

        self.assertEqual([path for path, _ in read_calls], ["/tmp/out.txt", "/tmp/out.txt", "/tmp/out.txt"])
        self.assertEqual(write_calls[0], ("/tmp/out.txt", b"hello", {"create_parents": True, "rpc_timeout": 30}))
        self.assertEqual(write_calls[1], ("/tmp/raw.bin", b"\xff", {"create_parents": False, "rpc_timeout": 30}))
        self.assertEqual(write_calls[2], ("/tmp/latin1.txt", b"\xe9", {"create_parents": True, "rpc_timeout": 30}))

    async def test_async_upload_and_download_file_use_bytes_helpers(self) -> None:
        client = _AsyncFakeClient()
        remote_data = b"async-downloaded"
        write_calls = []

        class FakeAsyncNodeClient:
            def __init__(self, **kwargs) -> None:
                del kwargs

            async def read_file(self, path, **kwargs):
                self.read_call = (path, kwargs)
                return remote_data

            async def write_file(self, path, data, **kwargs):
                write_calls.append((path, data, kwargs))

        with TemporaryDirectory() as tmp:
            root = Path(tmp)
            source = root / "source.bin"
            target = root / "nested" / "target.bin"
            source.write_bytes(b"payload")

            async with AsyncSandbox(
                client=client,
                image="docker.io/library/python:3.12-slim",
                _node_client_factory=FakeAsyncNodeClient,
            ) as sandbox:
                await sandbox.upload_file(source, "/tmp/source.bin", create_parents=False)
                await sandbox.download_file("/tmp/result.bin", target)

            self.assertEqual(target.read_bytes(), remote_data)

        self.assertEqual(write_calls[0], ("/tmp/source.bin", b"payload", {"create_parents": False, "rpc_timeout": 30}))

    async def test_async_download_file_rejects_existing_target_without_overwrite(self) -> None:
        client = _AsyncFakeClient()

        class FakeAsyncNodeClient:
            def __init__(self, **kwargs) -> None:
                del kwargs

            async def read_file(self, path, **kwargs):
                del path, kwargs
                return b"new"

        with TemporaryDirectory() as tmp:
            target = Path(tmp) / "target.bin"
            target.write_bytes(b"old")
            async with AsyncSandbox(
                client=client,
                image="docker.io/library/python:3.12-slim",
                _node_client_factory=FakeAsyncNodeClient,
            ) as sandbox:
                with self.assertRaises(FileExistsError):
                    await sandbox.download_file("/tmp/result.bin", target, overwrite=False)
            self.assertEqual(target.read_bytes(), b"old")

    async def test_async_upload_and_download_dir_use_node_archive_rpc(self) -> None:
        client = _AsyncFakeClient()
        upload_calls = []

        class FakeAsyncNodeClient:
            def __init__(self, **kwargs) -> None:
                del kwargs

            async def upload_archive(self, path, chunk_factory, **kwargs):
                upload_calls.append((path, b"".join(chunk_factory()), kwargs))

            async def download_archive(self, path, writer, **kwargs):
                del path, kwargs
                writer(_tar_bytes({"nested/data.txt": b"data"}))

        with TemporaryDirectory() as tmp:
            root = Path(tmp)
            source = root / "tree"
            (source / "nested").mkdir(parents=True)
            (source / "nested" / "data.txt").write_bytes(b"data")
            target = root / "downloaded"

            async with AsyncSandbox(
                client=client,
                image="docker.io/library/python:3.12-slim",
                _node_client_factory=FakeAsyncNodeClient,
            ) as sandbox:
                await sandbox.upload_dir(source, "/tmp/tree")
                await sandbox.download_dir("/tmp/tree", target)

            self.assertEqual((target / "nested" / "data.txt").read_bytes(), b"data")

        self.assertEqual(upload_calls[0][0], "/tmp/tree")
        self.assertEqual(upload_calls[0][2], {"create_parents": True, "overwrite": True, "rpc_timeout": 30})
        self.assertIn("nested/data.txt", _tar_names(upload_calls[0][1]))

    async def test_async_file_helpers_reject_empty_paths(self) -> None:
        client = _AsyncFakeClient()

        async with AsyncSandbox(client=client, image="docker.io/library/python:3.12-slim") as sandbox:
            calls = (
                lambda: sandbox.read_bytes(""),
                lambda: sandbox.write_bytes("", b"data"),
                lambda: sandbox.exists(""),
                lambda: sandbox.stat(""),
                lambda: sandbox.list_dir(""),
                lambda: sandbox.mkdir(""),
                lambda: sandbox.remove(""),
            )
            for call in calls:
                with self.assertRaises(ValueError):
                    await call()

    async def test_async_file_management_helpers_use_node_file_rpc(self) -> None:
        client = _AsyncFakeClient()
        exists_calls = []
        stat_calls = []
        list_calls = []
        mkdir_calls = []
        remove_calls = []

        class FakeAsyncNodeClient:
            def __init__(self, **kwargs) -> None:
                del kwargs

            async def exists(self, path, **kwargs):
                exists_calls.append((path, kwargs))
                return True

            async def stat_file(self, path, **kwargs):
                stat_calls.append((path, kwargs))
                return SandboxFileInfo(path="/tmp/out.txt", kind=SandboxFileKind.FILE, size=5, mode=420, mtime_ns=7)

            async def list_dir(self, path, **kwargs):
                list_calls.append((path, kwargs))
                return [SandboxFileInfo(path="/tmp/out.txt", kind=SandboxFileKind.FILE, size=5, mode=420, mtime_ns=7)]

            async def mkdir(self, path, **kwargs):
                mkdir_calls.append((path, kwargs))

            async def remove(self, path, **kwargs):
                remove_calls.append((path, kwargs))

        async with AsyncSandbox(
            client=client,
            image="docker.io/library/python:3.12-slim",
            _node_client_factory=FakeAsyncNodeClient,
        ) as sandbox:
            self.assertTrue(await sandbox.exists("/tmp/out.txt"))
            info = await sandbox.stat("/tmp/out.txt")
            entries = await sandbox.list_dir("/tmp")
            await sandbox.mkdir("/tmp/nested")
            await sandbox.remove("/tmp/nested", recursive=True, force=True)

        self.assertEqual(info.path, "/tmp/out.txt")
        self.assertEqual(entries[0].path, "/tmp/out.txt")
        self.assertEqual(exists_calls[0][0], "/tmp/out.txt")
        self.assertEqual(stat_calls[0][0], "/tmp/out.txt")
        self.assertEqual(list_calls[0][0], "/tmp")
        self.assertEqual(mkdir_calls[0], ("/tmp/nested", {"parents": True, "rpc_timeout": 30}))
        self.assertEqual(remove_calls[0], ("/tmp/nested", {"recursive": True, "force": True, "rpc_timeout": 30}))


def _tar_bytes(files: dict[str, bytes]) -> bytes:
    buf = BytesIO()
    with tarfile.open(fileobj=buf, mode="w") as archive:
        for name, data in files.items():
            info = tarfile.TarInfo(name)
            info.size = len(data)
            archive.addfile(info, BytesIO(data))
    return buf.getvalue()


def _tar_names(data: bytes) -> list[str]:
    with tarfile.open(fileobj=BytesIO(data), mode="r") as archive:
        return archive.getnames()


if __name__ == "__main__":
    unittest.main()
