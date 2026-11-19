"""Unix-socket memory service with explicit credentials and per-client policy."""

import hmac
import json
import os
import socketserver
import stat
from http.server import BaseHTTPRequestHandler

from store import KINDS, PRIVACY_TIERS, Store
from history import ensure_history, record_reference, export_approved, export_recovery, restore_recovery
from vectors import VectorIndex
from retrieval import retrieve

MAX_REQUEST = 65536
MAX_RESTORE = 32 * 1024 * 1024


class MemoryServer(socketserver.UnixStreamServer):
    """Serve sequentially so each request uses the store's owning thread."""

    def __init__(self, socket_path, database, clients):
        # Tokens map to trusted policy. Never derive permissions from the request.
        if not clients or any(
            not isinstance(token, str) or len(token) < 32
            or not isinstance(policy, dict)
            or not isinstance(policy.get("tiers"), list)
            or not policy["tiers"]
            or any(tier not in ("P0", "P1", "P2") for tier in policy["tiers"])
            or not isinstance(policy.get("actions"), list)
            or any(action not in ("stage", "search", "approve", "vector_put", "vector_search", "retrieve", "reference", "export", "backup", "restore") for action in policy["actions"])
            or (any(action in policy["actions"] for action in ("approve", "backup", "restore")) and "stage" in policy["actions"])
            or (any(action in policy["actions"] for action in ("backup", "restore")) and set(policy["tiers"]) != {"P0", "P1", "P2"})
            for token, policy in clients.items()
        ):
            raise ValueError("invalid client policy")
        self.clients = clients
        self.store = Store(database)
        ensure_history(self.store.db)
        self.socket_path = None
        self.socket_identity = None
        # Fail if the path already exists. Never remove another service's socket.
        old_mask = os.umask(0o177)
        try:
            super().__init__(socket_path, MemoryHandler)
            self.socket_path = os.fspath(socket_path)
            socket_info = os.lstat(self.socket_path)
            self.socket_identity = (socket_info.st_dev, socket_info.st_ino)
        except BaseException:
            self.store.close()
            raise
        finally:
            os.umask(old_mask)

    def get_request(self):
        conn, address = super().get_request()
        conn.settimeout(10)
        return conn, address

    def server_close(self):
        try:
            super().server_close()
        finally:
            self.store.close()
            if self.socket_path is not None and self.socket_identity is not None:
                try:
                    socket_info = os.lstat(self.socket_path)
                except FileNotFoundError:
                    pass
                else:
                    if (
                        stat.S_ISSOCK(socket_info.st_mode)
                        and (socket_info.st_dev, socket_info.st_ino) == self.socket_identity
                    ):
                        os.unlink(self.socket_path)


class MemoryHandler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, format, *args):
        # Request bodies and credentials must not enter HTTP access logs.
        return

    def reply(self, status, data):
        body = json.dumps(data, ensure_ascii=True).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Connection", "close")
        self.close_connection = True
        self.end_headers()
        self.wfile.write(body)

    def do_POST(self):
        header = self.headers.get("Authorization", "")
        token = header.removeprefix("Bearer ") if header.startswith("Bearer ") else ""
        policy = next(
            (policy for key, policy in self.server.clients.items()
             if hmac.compare_digest(key.encode(), token.encode())),
            None,
        )
        if policy is None:
            self.reply(401, {"error": "unauthorized"})
            return
        if self.path not in ("/stage", "/search", "/approve", "/vector_put", "/vector_search", "/retrieve", "/reference", "/export", "/backup", "/restore"):
            self.reply(404, {"error": "unknown endpoint"})
            return
        action = self.path[1:]
        if action not in policy["actions"]:
            self.reply(403, {"error": "forbidden"})
            return
        try:
            size = int(self.headers.get("Content-Length", ""))
            if size < 0 or size > (MAX_RESTORE if action == "restore" else MAX_REQUEST) or self.headers.get("Transfer-Encoding"):
                raise ValueError()
            payload = json.loads(self.rfile.read(size))
            if not isinstance(payload, dict):
                raise ValueError()
        except (ValueError, UnicodeError):
            self.reply(400, {"error": "invalid request"})
            return

        try:
            if action == "stage":
                if set(payload) != {"source", "dedupe_key", "kind", "privacy", "body"}:
                    raise ValueError()
                if payload["kind"] not in KINDS or payload["privacy"] not in PRIVACY_TIERS:
                    raise ValueError()
                if payload["privacy"] not in policy["tiers"]:
                    self.reply(403, {"error": "forbidden"})
                    return
                record_id = self.server.store.stage(**payload)
                result = {"id": record_id}
            elif action == "search":
                if set(payload) - {"query", "limit"} or "query" not in payload:
                    raise ValueError()
                rows = self.server.store.search(payload["query"], policy["tiers"], payload.get("limit", 20))
                result = {"records": [
                    dict(zip(("id", "source", "kind", "privacy", "body", "created_at"), row))
                    for row in rows
                ]}
            elif action in ("vector_put", "reference"):
                expected = {"id", "model", "values"} if action == "vector_put" else {"id", "source", "reference_key"}
                if set(payload) != expected or type(payload["id"]) is not int:
                    raise ValueError()
                row = self.server.store.db.execute(
                    "SELECT privacy, state FROM records WHERE id = ?", (payload["id"],)
                ).fetchone()
                if row is None or row[0] not in policy["tiers"] or (action == "reference" and row[1] != "approved"):
                    self.reply(404, {"error": "record unavailable"})
                    return
                if action == "vector_put":
                    VectorIndex(self.server.store.db).put(payload["id"], payload["model"], payload["values"])
                    result = {"stored": True}
                else:
                    result = {"id": record_reference(self.server.store.db, payload["id"], payload["source"], payload["reference_key"])}
            elif action == "retrieve":
                allowed = {"query", "model", "values", "source", "since", "until", "limit"}
                if set(payload) - allowed or not {"query", "model", "values"} <= set(payload):
                    raise ValueError()
                result = {"records": retrieve(
                    self.server.store.db, payload["query"], payload["model"], payload["values"],
                    policy["tiers"], payload.get("source"), payload.get("since"),
                    payload.get("until"), payload.get("limit", 20),
                )}
            elif action == "vector_search":
                if set(payload) - {"model", "values", "limit"} or not {"model", "values"} <= set(payload):
                    raise ValueError()
                rows = VectorIndex(self.server.store.db).search(
                    payload["model"], payload["values"], policy["tiers"], payload.get("limit", 20)
                )
                result = {"records": [dict(zip(("id", "source", "kind", "privacy", "body", "created_at", "distance"), row)) for row in rows]}
            elif action == "export":
                if payload:
                    raise ValueError()
                result = json.loads(export_approved(self.server.store.db, policy["tiers"]))
            elif action == "backup":
                if payload:
                    raise ValueError()
                result = json.loads(export_recovery(self.server.store.db))
            elif action == "restore":
                if set(payload) != {"snapshot"} or not isinstance(payload["snapshot"], dict):
                    raise ValueError()
                restore_recovery(self.server.store.db, json.dumps(payload["snapshot"]))
                result = {"restored": True}
            else:
                # Approval privileges are assigned to the dedicated reviewer
                # identity and still constrained to that identity's tiers.
                if set(payload) != {"id"} or type(payload["id"]) is not int:
                    raise ValueError()
                row = self.server.store.db.execute(
                    "SELECT privacy FROM records WHERE id = ?", (payload["id"],)
                ).fetchone()
                if row is None or row[0] not in policy["tiers"]:
                    self.reply(404, {"error": "record unavailable"})
                    return
                self.server.store.approve(payload["id"])
                result = {"approved": True}
        except (ValueError, TypeError):
            self.reply(400, {"error": "invalid request"})
            return
        except Exception:
            self.reply(500, {"error": "memory operation failed"})
            return
        self.reply(200, result)
