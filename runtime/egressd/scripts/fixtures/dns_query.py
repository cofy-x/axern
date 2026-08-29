#!/usr/bin/env python3
import argparse
import random
import socket
import struct


def query_wire(name: str, qtype: int) -> tuple[int, bytes]:
    identifier = random.randint(1, 65535)
    labels = b"".join(bytes([len(label)]) + label.encode("ascii") for label in name.rstrip(".").split(".")) + b"\x00"
    return identifier, struct.pack("!HHHHHH", identifier, 0x0100, 1, 0, 0, 0) + labels + struct.pack("!HH", qtype, 1)


parser = argparse.ArgumentParser()
parser.add_argument("name")
parser.add_argument("--server", required=True)
parser.add_argument("--port", type=int, default=53)
parser.add_argument("--tcp", action="store_true")
parser.add_argument("--aaaa", action="store_true")
parser.add_argument("--expect-rcode", type=int, required=True)
parser.add_argument("--timeout", type=float, default=2)
args = parser.parse_args()
identifier, wire = query_wire(args.name, 28 if args.aaaa else 1)
if args.tcp:
    sock = socket.create_connection((args.server, args.port), args.timeout)
    sock.sendall(struct.pack("!H", len(wire)) + wire)
    size = sock.recv(2)
    if len(size) != 2:
        raise SystemExit("short TCP DNS response")
    length = struct.unpack("!H", size)[0]
    response = b""
    while len(response) < length:
        response += sock.recv(length - len(response))
else:
    family, socktype, proto, _, peer = socket.getaddrinfo(args.server, args.port, 0, socket.SOCK_DGRAM)[0]
    sock = socket.socket(family, socktype, proto)
    sock.settimeout(args.timeout)
    sock.sendto(wire, peer)
    response, _ = sock.recvfrom(65535)
if len(response) < 12 or struct.unpack("!H", response[:2])[0] != identifier:
    raise SystemExit("invalid DNS response")
rcode = response[3] & 0x0F
print(f"dns_query name={args.name} tcp={args.tcp} rcode={rcode}")
if rcode != args.expect_rcode:
    raise SystemExit(f"rcode {rcode}, expected {args.expect_rcode}")
