#!/usr/bin/env python3
import argparse
import socket

parser = argparse.ArgumentParser()
parser.add_argument("--address", required=True)
parser.add_argument("--port", type=int, required=True)
parser.add_argument("--expect-timeout", action="store_true")
args = parser.parse_args()
sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
sock.settimeout(1)
sock.sendto(b"axern-egress-truth", (args.address, args.port))
try:
    response, _ = sock.recvfrom(65535)
except TimeoutError:
    if args.expect_timeout:
        print("udp_probe timeout=true")
        raise SystemExit(0)
    raise
if args.expect_timeout:
    raise SystemExit("unexpected UDP response")
if response != b"axern-egress-truth":
    raise SystemExit("invalid UDP echo")
print("udp_probe reachable=true")
