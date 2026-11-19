"""Staged working-memory storage. Callers own authentication and authorization."""

import hashlib
import sqlite3


class Store:
    def __init__(self, path):
        self.db = sqlite3.connect(path)
        self.db.execute("PRAGMA foreign_keys = ON")
        version = self.db.execute("PRAGMA user_version").fetchone()[0]
        if version not in (0, 1):
            raise ValueError("unsupported memory schema version")
        if version == 0:
            with self.db:
                self.db.execute("""CREATE TABLE records (
                    id INTEGER PRIMARY KEY,
                    source TEXT NOT NULL,
                    dedupe_key TEXT NOT NULL,
                    kind TEXT NOT NULL CHECK(kind IN ('observation', 'fact', 'summary')),
                    privacy TEXT NOT NULL CHECK(privacy IN ('P0', 'P1', 'P2')),
                    body TEXT NOT NULL,
                    body_hash TEXT NOT NULL,
                    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
                    state TEXT NOT NULL DEFAULT 'staged' CHECK(state IN ('staged', 'approved')),
                    UNIQUE(source, dedupe_key)
                )""")
                self.db.execute("PRAGMA user_version = 1")

    def stage(self, source, dedupe_key, kind, privacy, body):
        if not all(isinstance(v, str) and v.strip() for v in (source, dedupe_key, kind, privacy, body)):
            raise ValueError("all fields must be non-empty strings")
        digest = hashlib.sha256(body.encode("utf-8")).hexdigest()
        with self.db:
            existing = self.db.execute(
                "SELECT id, kind, privacy, body_hash FROM records WHERE source = ? AND dedupe_key = ?",
                (source, dedupe_key),
            ).fetchone()
            if existing:
                if existing[1:] != (kind, privacy, digest):
                    raise ValueError("deduplication key already names a different record")
                return existing[0]
            cursor = self.db.execute(
                "INSERT INTO records (source, dedupe_key, kind, privacy, body, body_hash) VALUES (?, ?, ?, ?, ?, ?)",
                (source, dedupe_key, kind, privacy, body, digest),
            )
            return cursor.lastrowid

    def approve(self, record_id):
        with self.db:
            cursor = self.db.execute("UPDATE records SET state = 'approved' WHERE id = ?", (record_id,))
            if cursor.rowcount != 1:
                raise KeyError(record_id)

    def approved(self, privacy):
        if not privacy or any(p not in ("P0", "P1", "P2") for p in privacy):
            raise ValueError("privacy must specify allowed tiers")
        marks = ",".join("?" for _ in privacy)
        return self.db.execute(
            f"SELECT id, source, kind, privacy, body, created_at FROM records WHERE state = 'approved' AND privacy IN ({marks}) ORDER BY id",
            tuple(privacy),
        ).fetchall()

    def close(self):
        self.db.close()
