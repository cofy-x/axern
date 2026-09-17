from __future__ import annotations

import sys

import axern_sdk
from axern.control.environment.v1 import environment_pb2
from axern_sdk import AxernClient, DeclaredOutput, DeclaredOutputFormat, NetworkPolicy, Sandbox


def main() -> None:
    expected = sys.argv[1]
    if axern_sdk.__version__ != expected:
        raise SystemExit(f"unexpected Python SDK version {axern_sdk.__version__!r}")
    if axern_sdk.platform_name() != "axern":
        raise SystemExit("unexpected Python SDK platform name")
    if not all(callable(value) for value in (AxernClient, Sandbox, environment_pb2.Environment)):
        raise SystemExit("Python SDK artifact is missing public or generated modules")
    for method in ("capability_status", "computer_use_status", "computer_use_screenshot"):
        if not callable(getattr(Sandbox, method, None)):
            raise SystemExit(f"Python SDK artifact is missing Sandbox.{method}")
    for method in ("get_sealed_output_manifest", "download_sealed_output", "wait_run"):
        if not callable(getattr(AxernClient, method, None)):
            raise SystemExit(f"Python SDK artifact is missing AxernClient.{method}")
    declared = DeclaredOutput("/tmp/result.json", DeclaredOutputFormat.FILE, "application/json")
    if declared.path != "/tmp/result.json":
        raise SystemExit("Python SDK artifact cannot construct a declared output")
    policy = NetworkPolicy.deny_dns("GitHub.COM.", "*.github.com")
    if list(policy._to_proto().dns_deny.denied_domains) != ["github.com", "*.github.com"]:
        raise SystemExit("Python SDK artifact cannot construct a DNS deny policy")


if __name__ == "__main__":
    main()
