#!/usr/bin/env python3
import json
import os
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


COMMAND = r'''printf 'parent_exe='; readlink "/proc/$PPID/exe"; printf 'ld_library_path=%s\n' "${LD_LIBRARY_PATH-unset}"; find /testbed -name "*.php" | xargs grep -l "config.*install\|setup.*install\|upgrade\|patch" 2>/dev/null | head -20'''


def frame(event, payload):
    return f"event: {event}\ndata: {json.dumps(payload, separators=(',', ':'))}\n\n".encode()


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"
    calls = 0

    def do_POST(self):
        length = int(self.headers.get("content-length", "0"))
        request = json.loads(self.rfile.read(length) or b"{}")
        Handler.calls += 1
        if Handler.calls == 1:
            content = [
                frame("message_start", {"type": "message_start", "message": {"id": "msg_test", "type": "message", "role": "assistant", "model": request.get("model", "claude-test"), "content": [], "stop_reason": None, "stop_sequence": None, "usage": {"input_tokens": 1, "output_tokens": 1}}}),
                frame("content_block_start", {"type": "content_block_start", "index": 0, "content_block": {"type": "tool_use", "id": "toolu_test", "name": "Bash", "input": {}}}),
                frame("content_block_delta", {"type": "content_block_delta", "index": 0, "delta": {"type": "input_json_delta", "partial_json": json.dumps({"command": COMMAND}, separators=(",", ":"))}}),
                frame("content_block_stop", {"type": "content_block_stop", "index": 0}),
                frame("message_delta", {"type": "message_delta", "delta": {"stop_reason": "tool_use", "stop_sequence": None}, "usage": {"output_tokens": 1}}),
                frame("message_stop", {"type": "message_stop"}),
            ]
        else:
            content = [
                frame("message_start", {"type": "message_start", "message": {"id": "msg_done", "type": "message", "role": "assistant", "model": request.get("model", "claude-test"), "content": [], "stop_reason": None, "stop_sequence": None, "usage": {"input_tokens": 1, "output_tokens": 1}}}),
                frame("content_block_start", {"type": "content_block_start", "index": 0, "content_block": {"type": "text", "text": ""}}),
                frame("content_block_delta", {"type": "content_block_delta", "index": 0, "delta": {"type": "text_delta", "text": "tool call complete"}}),
                frame("content_block_stop", {"type": "content_block_stop", "index": 0}),
                frame("message_delta", {"type": "message_delta", "delta": {"stop_reason": "end_turn", "stop_sequence": None}, "usage": {"output_tokens": 1}}),
                frame("message_stop", {"type": "message_stop"}),
            ]
        body = b"".join(content)
        self.send_response(200)
        self.send_header("content-type", "text/event-stream")
        self.send_header("content-length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, _format, *_args):
        pass


if __name__ == "__main__":
    server = ThreadingHTTPServer(("0.0.0.0", int(os.environ.get("PORT", "18090"))), Handler)
    port_file = os.environ.get("PORT_FILE")
    if port_file:
        with open(port_file, "w", encoding="utf-8") as stream:
            stream.write(str(server.server_port))
    server.serve_forever()
