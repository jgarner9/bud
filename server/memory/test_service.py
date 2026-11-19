import http.client
import json
import os
import socket
import subprocess
import sys
import tempfile
import threading
import time
import unittest
from pathlib import Path

from service import MemoryServer

AGENT = "a" * 32
REVIEWER = "r" * 32
CLOUD = "c" * 32
BACKUP = "b" * 32


class UnixHTTP(http.client.HTTPConnection):
    def __init__(self, path):
        super().__init__("localhost")
        self.path = path

    def connect(self):
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.connect(self.path)


class ServiceTest(unittest.TestCase):
    def test_policy_and_staged_visibility(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = str(Path(tmp) / "memory.sock")
            ready = threading.Event()
            holder = []

            def serve():
                server = MemoryServer(path, str(Path(tmp) / "memory.db"), {
                    AGENT: {"actions": ["stage", "search", "vector_put", "vector_search", "retrieve", "reference"], "tiers": ["P0"]},
                    REVIEWER: {"actions": ["approve", "search"], "tiers": ["P0"]},
                    CLOUD: {"actions": ["search", "retrieve", "export", "reference"], "tiers": ["P1", "P2"]},
                    BACKUP: {"actions": ["backup", "restore"], "tiers": ["P0", "P1", "P2"]},
                })
                if os.environ.get("VEC_EXTENSION"):
                    server.store.db.enable_load_extension(True)
                    server.store.db.load_extension(os.environ["VEC_EXTENSION"])
                    server.store.db.enable_load_extension(False)
                holder.append(server)
                ready.set()
                try:
                    server.serve_forever()
                finally:
                    server.server_close()

            thread = threading.Thread(target=serve)
            thread.start()
            self.assertTrue(ready.wait(3))
            server = holder[0]
            self.addCleanup(thread.join, 3)
            self.addCleanup(server.shutdown)
            self.assertEqual(0o600, os.stat(path).st_mode & 0o777)

            def request(token, endpoint, payload):
                conn = UnixHTTP(path)
                conn.request("POST", endpoint, json.dumps(payload), {
                    "Authorization": "Bearer " + token,
                    "Content-Type": "application/json",
                })
                response = conn.getresponse()
                result = response.status, json.loads(response.read())
                conn.close()
                return result

            record = {"source": "synthetic", "dedupe_key": "1", "kind": "fact",
                      "privacy": "P0", "body": "private orchard"}
            self.assertEqual(401, request("invalid", "/stage", record)[0])
            status, data = request(AGENT, "/stage", record)
            self.assertEqual(200, status)
            for field, value in (("kind", "invalid"), ("privacy", "private")):
                with self.subTest(field=field):
                    invalid_record = {**record, field: value, "dedupe_key": field}
                    self.assertEqual(400, request(AGENT, "/stage", invalid_record)[0])
            self.assertEqual(403, request(AGENT, "/approve", {"id": data["id"]})[0])
            self.assertEqual([], request(AGENT, "/search", {"query": "orchard"})[1]["records"])
            self.assertEqual(200, request(REVIEWER, "/approve", {"id": data["id"]})[0])
            self.assertEqual(1, len(request(AGENT, "/search", {"query": "orchard"})[1]["records"]))
            self.assertEqual([], request(CLOUD, "/search", {"query": "orchard"})[1]["records"])
            self.assertEqual(400, request(CLOUD, "/search", {"query": "orchard", "privacy": ["P0"]})[0])
            self.assertEqual(403, request(CLOUD, "/stage", record)[0])
            self.assertEqual(200, request(AGENT, "/reference", {"id": data["id"], "source": "synthetic", "reference_key": "ref"})[0])
            if os.environ.get("VEC_EXTENSION"):
                self.assertEqual(200, request(AGENT, "/vector_put", {"id": data["id"], "model": "synthetic", "values": [0.1, 0.2]})[0])
                status, hits = request(AGENT, "/vector_search", {"model": "synthetic", "values": [0.1, 0.2]})
                self.assertEqual(200, status)
                self.assertEqual(data["id"], hits["records"][0]["id"])
                req = {"query": "orchard", "model": "synthetic", "values": [0.1, 0.2]}
                self.assertEqual(data["id"], request(AGENT, "/retrieve", req)[1]["records"][0]["id"])
                self.assertEqual([], request(CLOUD, "/retrieve", req)[1]["records"])
                self.assertEqual(400, request(CLOUD, "/retrieve", {**req, "privacy": ["P0"]})[0])

            self.assertEqual(404, request(CLOUD, "/reference", {"id": data["id"], "source": "test", "reference_key": "1"})[0])
            self.assertEqual([], request(CLOUD, "/export", {})[1]["records"])
            self.assertEqual(403, request(CLOUD, "/backup", {})[0])
            status, snapshot = request(BACKUP, "/backup", {})
            self.assertEqual(200, status)
            self.assertEqual("private orchard", snapshot["records"][0]["body"])
            self.assertEqual(400, request(BACKUP, "/restore", {"snapshot": snapshot})[0])

    def test_invalid_policy_and_existing_socket_fail_closed(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = str(Path(tmp) / "memory.sock")
            db = str(Path(tmp) / "memory.db")
            with self.assertRaises(ValueError):
                MemoryServer(path, db, {})
            with self.assertRaises(ValueError):
                MemoryServer(path, db, {AGENT: {"actions": ["stage", "approve"], "tiers": ["P0"]}})
            with self.assertRaises(ValueError):
                MemoryServer(path, db, {AGENT: {"actions": ["backup"], "tiers": ["P0"]}})
            with self.assertRaises(ValueError):
                MemoryServer(path, db, {AGENT: {"actions": ["restore", "stage"], "tiers": ["P0", "P1", "P2"]}})
            Path(path).write_text("keep")
            with self.assertRaises(OSError):
                MemoryServer(path, db, {AGENT: {"actions": ["search"], "tiers": ["P0"]}})
            self.assertEqual("keep", Path(path).read_text())

    def test_server_close_removes_owned_socket_and_allows_restart(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = str(Path(tmp) / "memory.sock")
            db = str(Path(tmp) / "memory.db")
            clients = {AGENT: {"actions": ["search"], "tiers": ["P0"]}}
            server = MemoryServer(path, db, clients)
            self.assertTrue(Path(path).exists())
            server.server_close()
            self.assertFalse(Path(path).exists())
            restarted = MemoryServer(path, db, clients)
            restarted.server_close()

    def test_sigterm_cleans_socket_and_allows_process_restart(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = str(Path(tmp) / "memory.sock")
            db = str(Path(tmp) / "memory.db")
            script = """
import sys
from main import serve
from service import MemoryServer
server = MemoryServer(sys.argv[1], sys.argv[2], {'a' * 32: {'actions': ['search'], 'tiers': ['P0']}})
try:
    serve(server)
finally:
    server.server_close()
"""
            for _ in range(2):
                process = subprocess.Popen([sys.executable, "-c", script, path, db],
                                           cwd=Path(__file__).resolve().parent)
                try:
                    deadline = time.monotonic() + 5
                    while not Path(path).exists() and process.poll() is None and time.monotonic() < deadline:
                        time.sleep(0.01)
                    self.assertTrue(Path(path).exists(), "service failed to start")
                    # A successful request proves the signal handler is installed.
                    conn = UnixHTTP(path)
                    conn.request("POST", "/search", '{"query":"synthetic"}', {"Authorization": "Bearer " + AGENT})
                    self.assertEqual(200, conn.getresponse().status)
                    conn.close()
                    process.terminate()
                    self.assertEqual(0, process.wait(timeout=5))
                    self.assertFalse(Path(path).exists())
                finally:
                    if process.poll() is None:
                        process.kill()
                        process.wait()

    def test_close_preserves_replacement_socket(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = str(Path(tmp) / "memory.sock")
            server = MemoryServer(path, str(Path(tmp) / "memory.db"),
                                  {AGENT: {"actions": ["search"], "tiers": ["P0"]}})
            os.unlink(path)
            replacement = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
            try:
                replacement.bind(path)
                identity = os.lstat(path).st_ino
                server.server_close()
                self.assertEqual(identity, os.lstat(path).st_ino)
            finally:
                replacement.close()


if __name__ == "__main__":
    unittest.main()
