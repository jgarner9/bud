"""Vector index for approved working-memory records."""

import hashlib
import math
import re
import struct


class StaleRecord(Exception):
    """The record's body changed after its text was read for embedding."""


class VectorIndex:
    def __init__(self, db):
        self.db = db
        # The caller loads its pinned extension before constructing the index.
        try:
            db.execute("SELECT vec_version()").fetchone()
        except Exception as exc:
            raise RuntimeError("sqlite-vec must be loaded before opening the vector index") from exc
        with db:
            db.execute("""CREATE TABLE IF NOT EXISTS embedding_models (
                model TEXT PRIMARY KEY,
                dimensions INTEGER NOT NULL CHECK(dimensions BETWEEN 1 AND 4096),
                table_name TEXT NOT NULL UNIQUE
            )""")
            db.execute("""CREATE TABLE IF NOT EXISTS embedding_provenance (
                record_id INTEGER NOT NULL REFERENCES records(id) ON DELETE CASCADE,
                model TEXT NOT NULL REFERENCES embedding_models(model),
                body_hash TEXT NOT NULL,
                PRIMARY KEY(record_id, model)
            )""")

    @staticmethod
    def _packed(values):
        if not isinstance(values, (list, tuple)) or not 1 <= len(values) <= 4096:
            raise ValueError("embedding must contain 1 to 4096 numbers")
        if any(isinstance(v, bool) or not isinstance(v, (int, float)) or not math.isfinite(v) for v in values):
            raise ValueError("embedding values must be finite numbers")
        try:
            return struct.pack("<" + "f" * len(values), *values)
        except (OverflowError, struct.error) as exc:
            raise ValueError("embedding values must fit float32") from exc

    def put(self, record_id, model, values, body_hash=None):
        """Store a record's vector by model. A re-embed passes the body_hash
        that pending returned, so a record edited in between is refused
        (StaleRecord) instead of having an old body's vector marked current."""
        if not isinstance(model, str) or not model.strip() or len(model) > 256:
            raise ValueError("model must be a non-empty string of at most 256 characters")
        packed = self._packed(values)
        table = "vec_" + hashlib.sha256(model.encode()).hexdigest()
        with self.db:
            record = self.db.execute("SELECT body_hash FROM records WHERE id = ?", (record_id,)).fetchone()
            if record is None:
                raise KeyError(record_id)
            if body_hash is not None and body_hash != record[0]:
                raise StaleRecord(record_id)
            existing = self.db.execute("SELECT dimensions, table_name FROM embedding_models WHERE model = ?", (model,)).fetchone()
            if existing and existing != (len(values), table):
                raise ValueError("embedding dimensions differ from the model's index")
            if not existing:
                self.db.execute(f'CREATE VIRTUAL TABLE "{table}" USING vec0(embedding float[{len(values)}])')
                self.db.execute("INSERT INTO embedding_models VALUES (?, ?, ?)", (model, len(values), table))
            self.db.execute(f'DELETE FROM "{table}" WHERE rowid = ?', (record_id,))
            self.db.execute(f'INSERT INTO "{table}" (rowid, embedding) VALUES (?, ?)', (record_id, packed))
            self.db.execute("""INSERT INTO embedding_provenance (record_id, model, body_hash)
                VALUES (?, ?, ?) ON CONFLICT(record_id, model)
                DO UPDATE SET body_hash = excluded.body_hash""", (record_id, model, record[0]))

    def pending(self, model, privacy, limit=20, after=0):
        """Approved records in the allowed tiers that have no current vector
        for model: never embedded by it, or edited since. A client re-embeds
        by embedding these and calling put until none are left, which is how
        a new model or new dims is rolled out (Suite D52). Each row carries
        the body_hash to pass back to put. Rows come in id order after the
        id given, so a client can page past records it skipped."""
        if not isinstance(model, str) or not model.strip() or len(model) > 256:
            raise ValueError("model must be a non-empty string of at most 256 characters")
        if not privacy or any(p not in ("P0", "P1", "P2") for p in privacy):
            raise ValueError("privacy must specify allowed tiers")
        if not isinstance(limit, int) or isinstance(limit, bool) or not 1 <= limit <= 100:
            raise ValueError("limit must be between 1 and 100")
        if not isinstance(after, int) or isinstance(after, bool) or after < 0:
            raise ValueError("after must be a record id, or 0 to start from the first")
        marks = ",".join("?" for _ in privacy)
        return self.db.execute(
            f"""SELECT r.id, r.privacy, r.body, r.body_hash
                FROM records r LEFT JOIN embedding_provenance p
                  ON p.record_id = r.id AND p.model = ?
                WHERE r.state = 'approved' AND r.privacy IN ({marks})
                  AND (p.body_hash IS NULL OR p.body_hash != r.body_hash)
                  AND r.id > ?
                ORDER BY r.id LIMIT ?""",
            (model, *privacy, after, limit),
        ).fetchall()

    def search(self, model, values, privacy, limit=20):
        packed = self._packed(values)
        if not privacy or any(p not in ("P0", "P1", "P2") for p in privacy):
            raise ValueError("privacy must specify allowed tiers")
        if not isinstance(limit, int) or isinstance(limit, bool) or not 1 <= limit <= 100:
            raise ValueError("limit must be between 1 and 100")
        entry = self.db.execute("SELECT dimensions, table_name FROM embedding_models WHERE model = ?", (model,)).fetchone()
        if entry is None:
            return []
        dimensions, table = entry
        if dimensions != len(values):
            raise ValueError("embedding dimensions differ from the model's index")
        if not re.fullmatch(r"vec_[0-9a-f]{64}", table):
            raise ValueError("invalid vector index name")
        marks = ",".join("?" for _ in privacy)
        # Apply privacy before ranking so hidden records do not crowd out results.
        return self.db.execute(
            f"""SELECT r.id, r.source, r.kind, r.privacy, r.body, r.created_at,
                       vec_distance_L2(v.embedding, ?) AS distance
                FROM records r JOIN embedding_provenance p ON p.record_id = r.id
                JOIN "{table}" v ON v.rowid = r.id
                WHERE p.model = ? AND p.body_hash = r.body_hash
                  AND r.state = 'approved' AND r.privacy IN ({marks})
                ORDER BY distance, r.id LIMIT ?""",
            (packed, model, *privacy, limit),
        ).fetchall()
