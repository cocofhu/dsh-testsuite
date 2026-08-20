#!/usr/bin/env python3
"""TCP proxy: dsh web refuses --host 0.0.0.0, so it binds loopback and this
forwards the published container port onto it (HTTP and WebSocket)."""
import socket
import sys
import threading

LISTEN = ("0.0.0.0", int(sys.argv[1]) if len(sys.argv) > 1 else 3080)
TARGET = ("127.0.0.1", int(sys.argv[2]) if len(sys.argv) > 2 else 3081)


def pipe(src, dst):
    try:
        while True:
            data = src.recv(65536)
            if not data:
                break
            dst.sendall(data)
    except OSError:
        pass
    finally:
        try:
            src.shutdown(socket.SHUT_RDWR)
        except OSError:
            pass
        try:
            dst.shutdown(socket.SHUT_RDWR)
        except OSError:
            pass


def handle(client):
    try:
        upstream = socket.create_connection(TARGET, timeout=5)
        upstream.settimeout(None)
    except OSError:
        client.close()
        return
    threading.Thread(target=pipe, args=(client, upstream), daemon=True).start()
    pipe(upstream, client)
    client.close()
    upstream.close()


def main():
    sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    sock.bind(LISTEN)
    sock.listen(128)
    while True:
        client, _ = sock.accept()
        threading.Thread(target=handle, args=(client,), daemon=True).start()


if __name__ == "__main__":
    main()
