#!/usr/bin/env python3
"""Minimal RCON client for the vanilla oracle server (stdlib only).

Usage: oracle_rcon.py <host:port> <password> <command> [<command> ...]
Each command is executed in order; responses print one per line.
"""
import socket
import struct
import sys


def packet(req_id: int, ptype: int, payload: str) -> bytes:
    body = struct.pack("<ii", req_id, ptype) + payload.encode() + b"\x00\x00"
    return struct.pack("<i", len(body)) + body


def read_packet(sock) -> tuple[int, int, str]:
    raw = b""
    while len(raw) < 4:
        raw += sock.recv(4 - len(raw))
    (length,) = struct.unpack("<i", raw)
    body = b""
    while len(body) < length:
        body += sock.recv(length - len(body))
    req_id, ptype = struct.unpack("<ii", body[:8])
    return req_id, ptype, body[8:-2].decode(errors="replace")


def main() -> None:
    host, port = sys.argv[1].rsplit(":", 1)
    password, commands = sys.argv[2], sys.argv[3:]
    with socket.create_connection((host, int(port)), timeout=10) as s:
        s.sendall(packet(1, 3, password))  # login
        rid, _, _ = read_packet(s)
        if rid == -1:
            sys.exit("rcon: authentication failed")
        for i, cmd in enumerate(commands, start=2):
            s.sendall(packet(i, 2, cmd))
            _, _, resp = read_packet(s)
            print(f"[{cmd}] {resp}")


if __name__ == "__main__":
    main()
