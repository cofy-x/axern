#!/usr/bin/env python3
import argparse
import socket
import struct
import threading


def response(query: bytes, answer: str) -> bytes:
    if len(query) < 12:
        return b""
    questions = struct.unpack("!H", query[4:6])[0]
    offset = 12
    for _ in range(questions):
        while offset < len(query) and query[offset]:
            offset += query[offset] + 1
        offset += 5
    question = query[12:offset]
    header = query[:2] + b"\x81\x80" + query[4:6] + b"\x00\x01\x00\x00\x00\x00"
    if ":" in answer:
        packed = socket.inet_pton(socket.AF_INET6, answer)
        body = b"\xc0\x0c\x00\x1c\x00\x01" + struct.pack("!I", 5) + struct.pack("!H", len(packed)) + packed
    else:
        packed = socket.inet_aton(answer)
        body = b"\xc0\x0c\x00\x01\x00\x01" + struct.pack("!I", 5) + struct.pack("!H", len(packed)) + packed
    return header + question + body


def serve_udp(address: str, port: int, answer: str) -> None:
    sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    sock.bind((address, port))
    while True:
        query, peer = sock.recvfrom(65535)
        sock.sendto(response(query, answer), peer)


def serve_tcp(address: str, port: int, answer: str) -> None:
    listener = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    listener.bind((address, port))
    listener.listen()
    while True:
        conn, _ = listener.accept()
        with conn:
            size = conn.recv(2)
            if len(size) != 2:
                continue
            length = struct.unpack("!H", size)[0]
            query = b""
            while len(query) < length:
                chunk = conn.recv(length - len(query))
                if not chunk:
                    break
                query += chunk
            wire = response(query, answer)
            conn.sendall(struct.pack("!H", len(wire)) + wire)


parser = argparse.ArgumentParser()
parser.add_argument("--address", default="0.0.0.0")
parser.add_argument("--port", type=int, required=True)
parser.add_argument("--answer", required=True)
args = parser.parse_args()
threading.Thread(target=serve_tcp, args=(args.address, args.port, args.answer), daemon=True).start()
serve_udp(args.address, args.port, args.answer)
