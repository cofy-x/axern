#!/usr/bin/env python3
"""Deterministic local-only provider used by the managed rollout Compose gate."""

import argparse
import json
import socket
import ssl
import time
import uuid
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from threading import Lock
from urllib.parse import urlsplit


REQUESTS = []
REQUESTS_LOCK = Lock()


def scenario_for(headers, payload):
    token = headers.get("x-api-key", "")
    authorization = headers.get("authorization", "")
    if authorization.lower().startswith("bearer "):
        token = authorization[7:]
    model = str(payload.get("model", ""))
    for candidate in (token, model):
        normalized = candidate.lower().replace("mock/", "").replace("mock-", "")
        if normalized in {
            "success", "401", "403", "404", "429", "500", "503", "timeout",
            "disconnect", "malformed", "missing-usage", "rotate-v1", "rotate-v2",
            "scripted-agent",
        }:
            return normalized
    return "401"


def credential_label(headers):
    token = headers.get("x-api-key", "")
    authorization = headers.get("authorization", "")
    if authorization.lower().startswith("bearer "):
        token = authorization[7:]
    if token.endswith("rotate-v1"):
        return "v1"
    if token.endswith("rotate-v2"):
        return "v2"
    return "scenario"


def has_tool_result(payload):
    pending = [payload.get("input", payload.get("messages", []))]
    while pending:
        value = pending.pop()
        if isinstance(value, dict):
            if value.get("type") in {"function_call_output", "tool_result"}:
                return True
            pending.extend(value.values())
        elif isinstance(value, list):
            pending.extend(value)
    return False


def choose_tool(payload):
    tools = payload.get("tools", [])
    for tool in tools:
        name = tool.get("name") or tool.get("function", {}).get("name") or ""
        lowered = name.lower()
        if any(part in lowered for part in ("shell", "bash", "exec", "terminal")):
            command_key = "cmd" if "exec" in lowered else "command"
            return name, {command_key: "printf 'managed rollout mock provider ok\\n' > /workspace/scripted-agent.txt"}
    return "", {}


class Handler(BaseHTTPRequestHandler):
    server_version = "axern-mock-provider/1"

    def log_message(self, _format, *_args):
        return

    def json_response(self, status, value):
        body = json.dumps(value, separators=(",", ":")).encode()
        self.send_response(status)
        self.send_header("content-type", "application/json")
        self.send_header("content-length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        request_path = urlsplit(self.path).path
        if request_path == "/healthz":
            self.json_response(200, {"ok": True})
            return
        if request_path == "/__mock/requests":
            with REQUESTS_LOCK:
                requests = list(REQUESTS)
            self.json_response(200, {"requests": requests})
            return
        self.json_response(404, {"error": {"message": "not found"}})

    def do_POST(self):
        request_path = urlsplit(self.path).path
        if request_path == "/__mock/reset":
            with REQUESTS_LOCK:
                REQUESTS.clear()
            self.json_response(200, {"ok": True})
            return
        try:
            length = int(self.headers.get("content-length", "0"))
            payload = json.loads(self.rfile.read(length) or b"{}")
        except (ValueError, json.JSONDecodeError):
            self.json_response(400, {"error": {"message": "invalid request"}})
            return
        scenario = scenario_for(self.headers, payload)
        wire = "anthropic_messages" if request_path.endswith("/messages") else "responses"
        chosen_tool, _ = choose_tool(payload)
        tool_names = [
            tool.get("name") or tool.get("function", {}).get("name") or ""
            for tool in payload.get("tools", [])
        ]
        with REQUESTS_LOCK:
            REQUESTS.append({
                "wire_api": wire,
                "scenario": scenario,
                "credential_version": credential_label(self.headers),
                "model": payload.get("model", ""),
                "stream": bool(payload.get("stream")),
                "tool_names": tool_names,
                "chosen_tool": chosen_tool,
                "has_tool_result": has_tool_result(payload),
            })
        if scenario == "timeout":
            time.sleep(20)
            return
        if scenario == "disconnect":
            self.send_response(200)
            self.send_header("content-type", "application/json")
            self.send_header("content-length", "100")
            self.end_headers()
            self.wfile.write(b'{"id":"partial"')
            self.wfile.flush()
            self.connection.shutdown(socket.SHUT_RDWR)
            self.connection.close()
            return
        if scenario == "malformed":
            body = b"{not-json"
            self.send_response(200)
            self.send_header("content-type", "application/json")
            self.send_header("content-length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
            return
        status = {"401": 401, "403": 403, "404": 404, "429": 429, "500": 500, "503": 503}.get(scenario)
        if status:
            self.json_response(status, {"error": {"message": "deterministic mock failure", "type": scenario}})
            return
        if not (request_path.endswith("/responses") or request_path.endswith("/messages")):
            self.json_response(404, {"error": {"message": "required protocol endpoint not found"}})
            return
        if payload.get("stream"):
            if wire == "responses":
                self.openai_stream(payload)
            else:
                self.anthropic_stream(payload)
            return
        usage = {} if scenario == "missing-usage" else {"input_tokens": 7, "output_tokens": 1}
        if wire == "responses":
            value = {"id": "resp_" + uuid.uuid4().hex, "object": "response", "status": "completed", "output": []}
        else:
            value = {"id": "msg_" + uuid.uuid4().hex, "type": "message", "role": "assistant", "content": [{"type": "text", "text": "OK"}]}
        if usage:
            value["usage"] = usage
        self.json_response(200, value)

    def sse(self, event, value):
        encoded = json.dumps(value, separators=(",", ":"))
        self.wfile.write(f"event: {event}\ndata: {encoded}\n\n".encode())
        self.wfile.flush()

    def openai_stream(self, payload):
        response_id = "resp_" + uuid.uuid4().hex
        self.send_response(200)
        self.send_header("content-type", "text/event-stream")
        self.send_header("cache-control", "no-cache")
        self.end_headers()
        self.sse("response.created", {"type": "response.created", "response": {"id": response_id, "object": "response", "status": "in_progress", "output": []}})
        tool_name, arguments = choose_tool(payload)
        if tool_name and not has_tool_result(payload):
            call_id = "call_" + uuid.uuid4().hex
            item = {"id": "fc_" + uuid.uuid4().hex, "type": "function_call", "call_id": call_id, "name": tool_name, "arguments": json.dumps(arguments)}
            self.sse("response.output_item.added", {"type": "response.output_item.added", "output_index": 0, "item": item})
            self.sse("response.output_item.done", {"type": "response.output_item.done", "output_index": 0, "item": item})
            output = [item]
        else:
            item = {"id": "msg_" + uuid.uuid4().hex, "type": "message", "role": "assistant", "status": "completed", "content": [{"type": "output_text", "text": "scripted task complete", "annotations": []}]}
            self.sse("response.output_item.added", {"type": "response.output_item.added", "output_index": 0, "item": item})
            self.sse("response.output_item.done", {"type": "response.output_item.done", "output_index": 0, "item": item})
            output = [item]
        completed = {"id": response_id, "object": "response", "status": "completed", "output": output, "usage": {"input_tokens": 11, "output_tokens": 3, "total_tokens": 14}}
        self.sse("response.completed", {"type": "response.completed", "response": completed})
        self.wfile.write(b"data: [DONE]\n\n")

    def anthropic_stream(self, payload):
        message_id = "msg_" + uuid.uuid4().hex
        self.send_response(200)
        self.send_header("content-type", "text/event-stream")
        self.send_header("cache-control", "no-cache")
        self.end_headers()
        self.sse("message_start", {"type": "message_start", "message": {"id": message_id, "type": "message", "role": "assistant", "model": payload.get("model", "mock"), "content": [], "stop_reason": None, "usage": {"input_tokens": 11, "output_tokens": 0}}})
        tool_name, arguments = choose_tool(payload)
        if tool_name and not has_tool_result(payload):
            block = {"type": "tool_use", "id": "toolu_" + uuid.uuid4().hex, "name": tool_name, "input": {}}
            stop_reason = "tool_use"
        else:
            block = {"type": "text", "text": ""}
            stop_reason = "end_turn"
        self.sse("content_block_start", {"type": "content_block_start", "index": 0, "content_block": block})
        if stop_reason == "tool_use":
            delta = {"type": "input_json_delta", "partial_json": json.dumps(arguments, separators=(",", ":"))}
        else:
            delta = {"type": "text_delta", "text": "scripted task complete"}
        self.sse("content_block_delta", {"type": "content_block_delta", "index": 0, "delta": delta})
        self.sse("content_block_stop", {"type": "content_block_stop", "index": 0})
        self.sse("message_delta", {"type": "message_delta", "delta": {"stop_reason": stop_reason, "stop_sequence": None}, "usage": {"output_tokens": 3}})
        self.sse("message_stop", {"type": "message_stop"})


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--listen", default="0.0.0.0:24443")
    parser.add_argument("--cert", required=True)
    parser.add_argument("--key", required=True)
    args = parser.parse_args()
    host, port = args.listen.rsplit(":", 1)
    server = ThreadingHTTPServer((host, int(port)), Handler)
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.load_cert_chain(args.cert, args.key)
    server.socket = context.wrap_socket(server.socket, server_side=True)
    server.serve_forever()


if __name__ == "__main__":
    main()
