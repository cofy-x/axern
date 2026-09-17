from __future__ import annotations

import sys
import inspect

import axern_sdk
from axern.control.environment.v1 import environment_pb2
from axern_sdk import (
    AsyncAxernClient,
    AsyncSandbox,
    AxernClient,
    DeclaredOutput,
    DeclaredOutputFormat,
    ImageMount,
    NetworkPolicy,
    Sandbox,
    SecretEnvVar,
    SecretFile,
    TunnelConnector,
)


def main() -> None:
    expected = sys.argv[1]
    if axern_sdk.__version__ != expected:
        raise SystemExit(f"unexpected Python SDK version {axern_sdk.__version__!r}")
    if axern_sdk.platform_name() != "axern":
        raise SystemExit("unexpected Python SDK platform name")
    if not all(
        callable(value) for value in (AxernClient, Sandbox, environment_pb2.Environment)
    ):
        raise SystemExit("Python SDK artifact is missing public or generated modules")
    for method in (
        "capability_status",
        "computer_use_status",
        "computer_use_screenshot",
    ):
        if not callable(getattr(Sandbox, method, None)):
            raise SystemExit(f"Python SDK artifact is missing Sandbox.{method}")
    for method in ("get_sealed_output_manifest", "download_sealed_output", "wait_run"):
        if not callable(getattr(AxernClient, method, None)):
            raise SystemExit(f"Python SDK artifact is missing AxernClient.{method}")
    declared = DeclaredOutput(
        "/tmp/result.json", DeclaredOutputFormat.FILE, "application/json"
    )
    if declared.path != "/tmp/result.json":
        raise SystemExit("Python SDK artifact cannot construct a declared output")
    policy = NetworkPolicy.deny_dns("GitHub.COM.", "*.github.com")
    if list(policy._to_proto().dns_deny.denied_domains) != [
        "github.com",
        "*.github.com",
    ]:
        raise SystemExit("Python SDK artifact cannot construct a DNS deny policy")
    projections = (
        ImageMount("registry.example/tool@sha256:aaa", "/__tool"),
        SecretEnvVar("WORKLOAD_TOKEN", "secret-workload", "token"),
        SecretFile("/run/secrets/config", "secret-config", "config.json"),
    )
    if [value.__class__.__name__ for value in projections] != [
        "ImageMount",
        "SecretEnvVar",
        "SecretFile",
    ]:
        raise SystemExit("Python SDK artifact cannot construct Run projections")
    for target in (
        AxernClient.create_run,
        AsyncAxernClient.create_run,
        Sandbox,
        AsyncSandbox,
    ):
        parameters = inspect.signature(target).parameters
        for name in ("image_mounts", "secret_env", "secret_files"):
            if name not in parameters:
                raise SystemExit(
                    f"Python SDK artifact {target!r} is missing parameter {name}"
                )
    connector_parameters = inspect.signature(TunnelConnector).parameters
    if "client" not in connector_parameters or "transport" in connector_parameters:
        raise SystemExit(
            "Python SDK artifact TunnelConnector does not inherit public client transport"
        )
    if not callable(getattr(TunnelConnector, "wait_closed", None)):
        raise SystemExit("Python SDK artifact TunnelConnector cannot await cleanup")


if __name__ == "__main__":
    main()
