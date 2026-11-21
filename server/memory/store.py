"""Staged working-memory storage. Callers own authentication and authorization."""

import hashlib
import sqlite3

KINDS = ("observation", "fact", "summary")
PRIVACY_TIERS = ("P0", "P1", "P2")


class Store:
    def __init__(self, path):
        self.db = sqlite3.connect(path)
        self.db.execute("PRAGMA foreign_keys = ON")
        version = self.db.execute("PRAGMA user_version").fetchone()[0]
        if version not in (0, 1, 2):
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

        if version < 2:
            with self.db:
                self.db.execute("CREATE VIRTUAL TABLE records_fts USING fts5(body, content='records', content_rowid='id')")
                self.db.execute("INSERT INTO records_fts(records_fts) VALUES ('rebuild')")
                self.db.execute("""CREATE TRIGGER records_fts_insert AFTER INSERT ON records BEGIN
                    INSERT INTO records_fts(rowid, body) VALUES (new.id, new.body);
                END""")
                self.db.execute("""CREATE TRIGGER records_fts_delete AFTER DELETE ON records BEGIN
                    INSERT INTO records_fts(records_fts, rowid, body) VALUES ('delete', old.id, old.body);
                END""")
                self.db.execute("""CREATE TRIGGER records_fts_update AFTER UPDATE OF body ON records BEGIN
                    INSERT INTO records_fts(records_fts, rowid, body) VALUES ('delete', old.id, old.body);
                    INSERT INTO records_fts(rowid, body) VALUES (new.id, new.body);
                END""")
                self.db.execute("PRAGMA user_version = 2")

    def search(self, query, privacy, limit=20):
        """Search approved records. Treat the supplied query as a literal phrase."""
        if not isinstance(query, str) or not query.strip():
            raise ValueError("query must be a non-empty string")
        if not privacy or any(p not in ("P0", "P1", "P2") for p in privacy):
            raise ValueError("privacy must specify allowed tiers")
        if not isinstance(limit, int) or isinstance(limit, bool) or not 1 <= limit <= 100:
            raise ValueError("limit must be between 1 and 100")
        phrase = '"' + query.replace('"', '""') + '"'
        marks = ",".join("?" for _ in privacy)
        return self.db.execute(
            f"""SELECT r.id, r.source, r.kind, r.privacy, r.body, r.created_at
                FROM records_fts f JOIN records r ON r.id = f.rowid
                WHERE records_fts MATCH ? AND r.state = 'approved' AND r.privacy IN ({marks})
                ORDER BY bm25(records_fts), r.id LIMIT ?""",
            (phrase, *privacy, limit),
        ).fetchall()

    def stage(self, source, dedupe_key, kind, privacy, body):
        if not all(isinstance(v, str) and v.strip() for v in (source, dedupe_key, kind, privacy, body)):
            raise ValueError("all fields must be non-empty strings")
        if kind not in KINDS:
            raise ValueError("kind must be observation, fact, or summary")
        if privacy not in PRIVACY_TIERS:
            raise ValueError("privacy must be P0, P1, or P2")
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
