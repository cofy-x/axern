"""Command normalization for sandbox exec APIs."""

from __future__ import annotations

from collections.abc import Sequence

from axern_sdk.node.models import ExecCommand


def exec_argv(command: ExecCommand, *, shell: bool | None = None) -> list[str]:
    """Return the argv sent to the node exec RPC.

    String commands are intentionally shell commands because this is the common
    programmable SDK use case. Sequence commands stay as direct argv execution.
    """

    if isinstance(command, str):
        if shell is False:
            raise ValueError("string commands require shell=True or shell=None")
        if not command:
            raise ValueError("command is required")
        return ["/bin/sh", "-lc", command]
    if shell:
        raise ValueError("shell=True requires a string command")
    argv = _argv_list(command)
    if not argv:
        raise ValueError("argv is required")
    return argv


def _argv_list(command: Sequence[str]) -> list[str]:
    argv = list(command)
    if not all(isinstance(arg, str) for arg in argv):
        raise TypeError("argv must contain only strings")
    return argv
