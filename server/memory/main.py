"""Run the local working-memory service from explicit environment configuration."""

import hashlib
import json
import os
import signal
from pathlib import Path

from service import MemoryServer
from vectors import VectorIndex
from history import ensure_history


def serve(server):
    # Raising in the serving thread unwinds through main's finally block;
    # socketserver.shutdown() would deadlock when called from that thread.
    def terminate(signum, frame):
        raise SystemExit(0)

    previous = signal.signal(signal.SIGTERM, terminate)
    try:
        server.serve_forever()
    finally:
        signal.signal(signal.SIGTERM, previous)


def main():
    socket_path = os.environ["BUD_MEMORY_SOCKET"]
    database = os.environ["BUD_MEMORY_DATABASE"]
    extension = Path(os.environ["BUD_MEMORY_VEC_EXTENSION"])
    expected = os.environ["BUD_MEMORY_VEC_SHA256"].lower()
    if len(expected) != 64 or any(c not in "0123456789abcdef" for c in expected):
        raise ValueError("BUD_MEMORY_VEC_SHA256 must be a SHA-256 digest")
    if hashlib.sha256(extension.read_bytes()).hexdigest() != expected:
        raise ValueError("sqlite-vec digest mismatch")
    clients = json.loads(os.environ["BUD_MEMORY_CLIENTS"])
    server = MemoryServer(socket_path, database, clients)
    try:
        server.store.db.enable_load_extension(True)
        try:
            server.store.db.load_extension(str(extension.resolve()))
        finally:
            server.store.db.enable_load_extension(False)
        VectorIndex(server.store.db)
        ensure_history(server.store.db)
        serve(server)
    finally:
        server.server_close()


if __name__ == "__main__":
    main()
