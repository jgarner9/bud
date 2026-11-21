import hashlib
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
from contextlib import contextmanager
from pathlib import Path

from service import MemoryServer

AGENT = "a" * 32
REVIEWER = "r" * 32
CLOUD = "c" * 32
BACKUP = "b" * 32


class UnixHTTP(http.client.HTTPConnection):
    def __init__(self, path):
        super().__init__("localhost", timeout=2)
        self.path = os.fspath(path)

    def connect(self):
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.settimeout(self.timeout)
        self.sock.connect(self.path)

    def post(self, endpoint, token, payload):
        body = json.dumps(payload).encode()
        headers = (
            f"POST {endpoint} HTTP/1.1\r\nHost: localhost\r\n"
            f"Authorization: Bearer {token}\r\nContent-Type: application/json\r\n"
            f"Content-Length: {len(body)}\r\n\r\n"
        ).encode()
        try:
            # Send the small fixture in one write. The handler can reject it
            # based on the headers and close without reading its body.
            self.connect()
            self.sock.sendall(headers + body)
            with http.client.HTTPResponse(self.sock) as response:
                response.begin()
                return response.status, json.loads(response.read())
        finally:
            self.close()


class ServiceTest(unittest.TestCase):
    def test_policy_and_staged_visibility(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = str(Path(tmp) / "memory.sock")
            ready = threading.Event()
            holder = []

            def serve():
                server = MemoryServer(path, str(Path(tmp) / "memory.db"), {
                    AGENT: {"actions": ["stage", "search", "vector_put", "vector_search", "vector_pending", "retrieve", "reference"], "tiers": ["P0"]},
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
                return UnixHTTP(path).post(endpoint, token, payload)

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
                pending = request(AGENT, "/vector_pending", {"model": "synthetic"})
                body_hash = hashlib.sha256(b"private orchard").hexdigest()
                self.assertEqual((200, [{"id": data["id"], "privacy": "P0", "body": "private orchard", "body_hash": body_hash}]), (pending[0], pending[1]["records"]))
                stale = {"id": data["id"], "model": "synthetic", "values": [0.1, 0.2], "body_hash": "0" * 64}
                self.assertEqual(409, request(AGENT, "/vector_put", stale)[0])
                self.assertEqual(400, request(AGENT, "/vector_put", {**stale, "body_hash": 1})[0])
                self.assertEqual(403, request(CLOUD, "/vector_pending", {"model": "synthetic"})[0])
                self.assertEqual(400, request(AGENT, "/vector_pending", {"model": "synthetic", "privacy": ["P2"]})[0])
                self.assertEqual([], request(AGENT, "/vector_pending", {"model": "synthetic", "after": data["id"]})[1]["records"])
                self.assertEqual(200, request(AGENT, "/vector_put", {**stale, "body_hash": body_hash})[0])
                self.assertEqual([], request(AGENT, "/vector_pending", {"model": "synthetic"})[1]["records"])
                status, hits = request(AGENT, "/vector_search", {"model": "synthetic", "values": [0.1, 0.2]})
                self.assertEqual(200, status)
                self.assertEqual(data["id"], hits["records"][0]["id"])
                req = {"query": "orchard", "model": "synthetic", "values": [0.1, 0.2]}
                self.assertEqual(data["id"], request(AGENT, "/retrieve", req)[1]["records"][0]["id"])
                self.assertEqual([], request(CLOUD, "/retrieve", req)[1]["records"])
                self.assertEqual(400, request(CLOUD, "/retrieve", {**req, "privacy": ["P0"]})[0])
                self.assertEqual(data["id"], request(AGENT, "/retrieve", {"query": "orchard"})[1]["records"][0]["id"])
                self.assertEqual(400, request(AGENT, "/retrieve", {"query": "orchard", "model": "synthetic"})[0])

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


@unittest.skipUnless(os.environ.get("VEC_EXTENSION"), "sqlite-vec extension not configured")
class LauncherRecoveryTest(unittest.TestCase):
    def request(self, path, token, action, payload):
        return UnixHTTP(path).post("/" + action, token, payload)

    @contextmanager
    def launch(self, directory, clients):
        extension = Path(os.environ["VEC_EXTENSION"]).resolve()
        path = directory / "memory.sock"
        process = subprocess.Popen(
            [sys.executable, str(Path(__file__).with_name("main.py"))],
            env={
                "PYTHONDONTWRITEBYTECODE": "1",
                "BUD_MEMORY_SOCKET": str(path),
                "BUD_MEMORY_DATABASE": str(directory / "memory.db"),
                "BUD_MEMORY_VEC_EXTENSION": str(extension),
                "BUD_MEMORY_VEC_SHA256": hashlib.sha256(extension.read_bytes()).hexdigest(),
                "BUD_MEMORY_CLIENTS": json.dumps(clients),
            },
            umask=0o077,
        )
        try:
            deadline = time.monotonic() + 5
            while process.poll() is None and time.monotonic() < deadline:
                try:
                    status, _ = self.request(path, AGENT, "search", {"query": "orchard"})
                except OSError:
                    time.sleep(0.01)
                    continue
                self.assertEqual(200, status)
                break
            else:
                self.fail("memory launcher did not become ready")

            self.assertEqual(0o600, path.stat().st_mode & 0o777)
            self.assertEqual(0o600, (directory / "memory.db").stat().st_mode & 0o777)
            yield path
            process.terminate()
            self.assertEqual(0, process.wait(timeout=5))
            self.assertFalse(path.exists())
        finally:
            if process.poll() is None:
                process.kill()
                process.wait(timeout=5)

    def test_recovery_through_two_launchers_preserves_privacy_after_restart(self):
        with tempfile.TemporaryDirectory() as tmp:
            source = Path(tmp) / "source"
            target = Path(tmp) / "target"
            source.mkdir(mode=0o700)
            target.mkdir(mode=0o700)
            clients = {
                AGENT: {"actions": ["stage", "search", "vector_put", "vector_search", "reference"], "tiers": ["P0"]},
                CLOUD: {"actions": ["search", "vector_search", "export"], "tiers": ["P2"]},
                BACKUP: {"actions": ["backup", "restore"], "tiers": ["P0", "P1", "P2"]},
            }
            source_clients = {**clients, REVIEWER: {"actions": ["approve"], "tiers": ["P0"]}}
            search = {"query": "orchard"}
            vector = {"model": "synthetic", "values": [0.25, 0.5]}
            record = {"source": "synthetic-session", "kind": "fact", "privacy": "P0", "body": "orchard"}

            with self.launch(source, source_clients) as source_path:
                status, staged = self.request(source_path, AGENT, "stage", {**record, "dedupe_key": "approved"})
                self.assertEqual(200, status)
                record_id = {"id": staged["id"]}
                self.assertEqual(200, self.request(source_path, REVIEWER, "approve", record_id)[0])
                self.assertEqual(200, self.request(source_path, AGENT, "vector_put", {**record_id, **vector})[0])
                reference = {**record_id, "source": "synthetic-session", "reference_key": "turn-1"}
                self.assertEqual(200, self.request(source_path, AGENT, "reference", reference)[0])
                self.assertEqual(200, self.request(source_path, AGENT, "stage", {**record, "dedupe_key": "pending"})[0])
                status, snapshot = self.request(source_path, BACKUP, "backup", {})
                self.assertEqual(200, status)

                with self.launch(target, clients) as target_path:
                    self.assertEqual(401, self.request(target_path, REVIEWER, "approve", record_id)[0])
                    self.assertEqual(403, self.request(target_path, AGENT, "restore", {"snapshot": snapshot})[0])
                    self.assertEqual(200, self.request(target_path, BACKUP, "restore", {"snapshot": snapshot})[0])

            with self.launch(target, clients) as target_path:
                self.assertEqual(401, self.request(target_path, REVIEWER, "approve", record_id)[0])
                self.assertEqual(403, self.request(target_path, AGENT, "approve", record_id)[0])
                self.assertEqual(403, self.request(target_path, AGENT, "restore", {"snapshot": snapshot})[0])
                self.assertEqual((200, snapshot), self.request(target_path, BACKUP, "backup", {}))
                for action, payload in (("search", search), ("vector_search", vector)):
                    with self.subTest(action=action):
                        status, result = self.request(target_path, AGENT, action, payload)
                        self.assertEqual(200, status)
                        self.assertEqual([staged["id"]], [hit["id"] for hit in result["records"]])
                        self.assertEqual("synthetic-session", result["records"][0]["source"])
                        self.assertEqual((200, {"records": []}), self.request(target_path, CLOUD, action, payload))


if __name__ == "__main__":
    unittest.main()
