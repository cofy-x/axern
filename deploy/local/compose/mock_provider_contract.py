#!/usr/bin/env python3
"""Black-box contract gate for the local managed-rollout mock provider."""

import argparse
import json
import socket
import ssl
import urllib.error
import urllib.request


def request(base_url, ca_file, wire, scenario, *, stream=False, timeout=3):
    path = "/v1/messages" if wire == "anthropic" else "/v1/responses"
    headers = {"content-type": "application/json"}
    if wire == "anthropic":
        headers["x-api-key"] = f"mock-{scenario}"
        headers["anthropic-version"] = "2023-06-01"
        payload = {"model": "contract", "max_tokens": 1, "messages": [{"role": "user", "content": "ping"}]}
    else:
        headers["authorization"] = f"Bearer mock-{scenario}"
        payload = {"model": "contract", "max_output_tokens": 1, "input": "ping"}
    if stream:
        payload["stream"] = True
    context = ssl.create_default_context(cafile=ca_file)
    req = urllib.request.Request(base_url + path, json.dumps(payload).encode(), headers, method="POST")
    with urllib.request.urlopen(req, context=context, timeout=timeout) as response:
        return response.status, response.headers.get_content_type(), response.read()


def expect_http_error(base_url, ca_file, scenario, expected):
    try:
        request(base_url, ca_file, "openai", scenario)
    except urllib.error.HTTPError as error:
        if error.code != expected:
            raise AssertionError(f"scenario {scenario}: got {error.code}, want {expected}") from error
        return
    raise AssertionError(f"scenario {scenario}: expected HTTP {expected}")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--base-url", default="https://localhost:24443")
    parser.add_argument("--ca", required=True)
    args = parser.parse_args()

    for scenario, code in (("401", 401), ("403", 403), ("404", 404), ("429", 429), ("500", 500), ("503", 503)):
        expect_http_error(args.base_url, args.ca, scenario, code)

    for wire in ("openai", "anthropic"):
        status, content_type, body = request(args.base_url, args.ca, wire, "success")
        value = json.loads(body)
        if status != 200 or content_type != "application/json" or "usage" not in value:
            raise AssertionError(f"{wire} success contract failed")
        status, content_type, body = request(args.base_url, args.ca, wire, "success", stream=True)
        if status != 200 or content_type != "text/event-stream" or b"data:" not in body:
            raise AssertionError(f"{wire} stream contract failed")

    _, _, body = request(args.base_url, args.ca, "openai", "missing-usage")
    if "usage" in json.loads(body):
        raise AssertionError("missing-usage scenario returned usage")

    _, _, body = request(args.base_url, args.ca, "openai", "malformed")
    try:
        json.loads(body)
    except json.JSONDecodeError:
        pass
    else:
        raise AssertionError("malformed scenario returned valid JSON")

    for scenario, expected in (("timeout", (TimeoutError, socket.timeout)), ("disconnect", Exception)):
        try:
            request(args.base_url, args.ca, "openai", scenario, timeout=0.25)
        except expected:
            pass
        else:
            raise AssertionError(f"{scenario} scenario did not fail deterministically")

    print("mock_provider_contract_ok=true")


if __name__ == "__main__":
    main()
