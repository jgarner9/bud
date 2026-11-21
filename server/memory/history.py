"""Reference history and portable JSON snapshots for a working-memory store.

The caller is responsible for authorizing backup and restore operations.
Reader exports only include approved records in the explicitly allowed tiers.
"""
import hashlib
import json
import re
import sqlite3

FORMAT = 2
TIERS = ("P0", "P1", "P2")
RECORD_FIELDS = ("id", "source", "dedupe_key", "kind", "privacy", "body",
                 "body_hash", "created_at", "state")
REFERENCE_FIELDS = ("id", "record_id", "source", "reference_key", "referenced_at")


def ensure_history(db):
    with db:
        db.execute("""CREATE TABLE IF NOT EXISTS record_references (
            id INTEGER PRIMARY KEY,
            record_id INTEGER NOT NULL REFERENCES records(id) ON DELETE CASCADE,
            source TEXT NOT NULL,
            reference_key TEXT NOT NULL,
            referenced_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
            UNIQUE(source, reference_key)
        )""")


def record_reference(db, record_id, source, reference_key):
    """Record an idempotent reference to an approved memory."""
    if not all(isinstance(value, str) and value.strip() for value in (source, reference_key)):
        raise ValueError("source and reference key must be non-empty strings")
    with db:
        record = db.execute("SELECT state FROM records WHERE id = ?", (record_id,)).fetchone()
        if not record or record[0] != "approved":
            raise KeyError(record_id)
        existing = db.execute(
            "SELECT id, record_id FROM record_references WHERE source = ? AND reference_key = ?",
            (source, reference_key),
        ).fetchone()
        if existing:
            if existing[1] != record_id:
                raise ValueError("reference key already names a different record")
            return existing[0]
        return db.execute(
            "INSERT INTO record_references (record_id, source, reference_key) VALUES (?, ?, ?)",
            (record_id, source, reference_key),
        ).lastrowid


def _rows(db, query, fields, args=()):
    return [dict(zip(fields, row)) for row in db.execute(query, args)]


def export_approved(db, privacy):
    """Return approved records and references allowed by the caller's tier filter."""
    if not isinstance(privacy, (list, tuple, set)) or not privacy or any(tier not in TIERS for tier in privacy):
        raise ValueError("privacy must specify allowed tiers")
    tiers = sorted(set(privacy))
    marks = ",".join("?" for _ in tiers)
    records = _rows(db, f"""SELECT {', '.join('r.' + field for field in RECORD_FIELDS)}
        FROM records r WHERE r.state = 'approved' AND r.privacy IN ({marks}) ORDER BY r.id""",
        RECORD_FIELDS, tiers)
    refs = _rows(db, f"""SELECT {', '.join('h.' + field for field in REFERENCE_FIELDS)}
        FROM record_references h JOIN records r ON r.id = h.record_id
        WHERE r.state = 'approved' AND r.privacy IN ({marks}) ORDER BY h.id""",
        REFERENCE_FIELDS, tiers)
    return json.dumps({"format": FORMAT, "records": records, "references": refs}, ensure_ascii=False)


def _export_vectors(db):
    tables = {row[0] for row in db.execute(
        "SELECT name FROM sqlite_master WHERE type='table' AND name IN ('embedding_models', 'embedding_provenance')"
    )}
    if not tables:
        return []
    if len(tables) != 2:
        raise ValueError("incomplete vector schema")
    vectors = []
    for model, dimensions, table in db.execute(
        "SELECT model, dimensions, table_name FROM embedding_models ORDER BY model"
    ).fetchall():
        expected = "vec_" + hashlib.sha256(model.encode("utf-8")).hexdigest()
        if table != expected or not re.fullmatch(r"vec_[0-9a-f]{64}", table):
            raise ValueError("invalid vector index name")
        rows = db.execute(
            f'SELECT v.rowid, v.embedding, p.body_hash FROM "{table}" v '
            'JOIN embedding_provenance p ON p.record_id = v.rowid AND p.model = ? ORDER BY v.rowid',
            (model,),
        ).fetchall()
        total = db.execute(f'SELECT count(*) FROM "{table}"').fetchone()[0]
        provenance = db.execute("SELECT count(*) FROM embedding_provenance WHERE model = ?", (model,)).fetchone()[0]
        if len(rows) != total or len(rows) != provenance:
            raise ValueError("inconsistent vector index")
        vectors.append({
            "model": model, "dimensions": dimensions,
            "entries": [{"record_id": row[0], "embedding": bytes(row[1]).hex(), "body_hash": row[2]} for row in rows],
        })
    return vectors


def export_recovery(db):
    """Export all records, including staged records, and reference history."""
    # Read the records and references from one consistent snapshot.
    with db:
        db.execute("SAVEPOINT memory_snapshot")
        try:
            records = _rows(db, "SELECT " + ", ".join(RECORD_FIELDS) + " FROM records ORDER BY id", RECORD_FIELDS)
            refs = _rows(db, "SELECT " + ", ".join(REFERENCE_FIELDS) + " FROM record_references ORDER BY id", REFERENCE_FIELDS)
            vectors = _export_vectors(db)
            return json.dumps({"format": FORMAT, "records": records, "references": refs, "vectors": vectors}, ensure_ascii=False)
        finally:
            db.execute("RELEASE memory_snapshot")


def restore_recovery(db, snapshot):
    """Restore a trusted snapshot into an empty, initialized store atomically."""
    try:
        data = json.loads(snapshot)
    except (TypeError, json.JSONDecodeError) as exc:
        raise ValueError("invalid memory snapshot") from exc
    if not isinstance(data, dict) or set(data) != {"format", "records", "references", "vectors"} or data["format"] != FORMAT:
        raise ValueError("unsupported memory snapshot")
    if not isinstance(data["records"], list) or not isinstance(data["references"], list) or not isinstance(data["vectors"], list):
        raise ValueError("invalid memory snapshot")
    with db:
        if db.execute("SELECT EXISTS(SELECT 1 FROM records)").fetchone()[0] or db.execute(
            "SELECT EXISTS(SELECT 1 FROM record_references)"
        ).fetchone()[0]:
            raise ValueError("restore requires an empty store")
        try:
            for record in data["records"]:
                if not isinstance(record, dict) or set(record) != set(RECORD_FIELDS):
                    raise ValueError("invalid record")
                body = record["body"]
                if not isinstance(body, str) or hashlib.sha256(body.encode("utf-8")).hexdigest() != record["body_hash"]:
                    raise ValueError("record hash mismatch")
                db.execute(
                    "INSERT INTO records (" + ", ".join(RECORD_FIELDS) + ") VALUES (" +
                    ", ".join("?" for _ in RECORD_FIELDS) + ")",
                    tuple(record[field] for field in RECORD_FIELDS),
                )
            for reference in data["references"]:
                if not isinstance(reference, dict) or set(reference) != set(REFERENCE_FIELDS):
                    raise ValueError("invalid reference")
                db.execute(
                    "INSERT INTO record_references (" + ", ".join(REFERENCE_FIELDS) + ") VALUES (" +
                    ", ".join("?" for _ in REFERENCE_FIELDS) + ")",
                    tuple(reference[field] for field in REFERENCE_FIELDS),
                )
            if data["vectors"]:
                for model in data["vectors"]:
                    if not isinstance(model, dict) or set(model) != {"model", "dimensions", "entries"}:
                        raise ValueError("invalid vector model")
                    name, dims, entries = model["model"], model["dimensions"], model["entries"]
                    if not isinstance(name, str) or not name.strip() or len(name) > 256:
                        raise ValueError("invalid vector model name")
                    if type(dims) is not int or not 1 <= dims <= 4096 or not isinstance(entries, list):
                        raise ValueError("invalid vector dimensions")
                    table = "vec_" + hashlib.sha256(name.encode("utf-8")).hexdigest()
                    db.execute(f'CREATE VIRTUAL TABLE "{table}" USING vec0(embedding float[{dims}])')
                    db.execute("INSERT INTO embedding_models VALUES (?, ?, ?)", (name, dims, table))
                    for entry in entries:
                        if not isinstance(entry, dict) or set(entry) != {"record_id", "embedding", "body_hash"}:
                            raise ValueError("invalid vector entry")
                        packed = bytes.fromhex(entry["embedding"])
                        if len(packed) != dims * 4:
                            raise ValueError("invalid embedding length")
                        row = db.execute("SELECT body_hash FROM records WHERE id = ?", (entry["record_id"],)).fetchone()
                        if row is None or row[0] != entry["body_hash"]:
                            raise ValueError("vector provenance does not match record")
                        db.execute(f'INSERT INTO "{table}" (rowid, embedding) VALUES (?, ?)', (entry["record_id"], packed))
                        db.execute("INSERT INTO embedding_provenance VALUES (?, ?, ?)", (entry["record_id"], name, row[0]))
        except (sqlite3.IntegrityError, sqlite3.ProgrammingError, TypeError) as exc:
            raise ValueError("invalid memory snapshot data") from exc
