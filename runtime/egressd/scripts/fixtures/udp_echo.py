#!/usr/bin/env python3
import argparse
import socket

parser = argparse.ArgumentParser()
parser.add_argument("--port", type=int, required=True)
args = parser.parse_args()
sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
sock.bind(("0.0.0.0", args.port))
while True:
    wire, peer = sock.recvfrom(65535)
    sock.sendto(wire, peer)
