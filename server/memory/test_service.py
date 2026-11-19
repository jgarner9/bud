import http.client
import json
import os
import socket
import tempfile
import threading
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
                    AGENT: {"actions": ["stage", "search", "vector_put", "vector_search", "reference"], "tiers": ["P0"]},
                    REVIEWER: {"actions": ["approve", "search"], "tiers": ["P0"]},
                    CLOUD: {"actions": ["search", "export", "reference"], "tiers": ["P1", "P2"]},
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


if __name__ == "__main__":
    unittest.main()
