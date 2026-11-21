"""Bounded hybrid retrieval over approved working-memory records."""

from datetime import datetime
import re

from store import PRIVACY_TIERS
from vectors import VectorIndex


def retrieve(db, query, model, values, tiers, source=None, since=None, until=None, limit=20):
    """Approved records in tiers matching query, ranked by text and, when
    model and values are given, by vector too. Without them it ranks by
    text alone, with the same filters, for when the embedder is down."""
    if not isinstance(query, str) or not query.strip() or len(query) > 4096:
        raise ValueError("query must be a non-empty string up to 4096 characters")
    if not isinstance(tiers, (list, tuple)) or not tiers or any(t not in PRIVACY_TIERS for t in tiers):
        raise ValueError("allowed privacy tiers are required")
    if type(limit) is not int or not 1 <= limit <= 100:
        raise ValueError("limit must be between 1 and 100")
    if source is not None and (not isinstance(source, str) or not source.strip() or len(source) > 256):
        raise ValueError("source must be a non-empty string up to 256 characters")
    for value in (since, until):
        if value is not None:
            if not isinstance(value, str):
                raise ValueError("dates must be YYYY-MM-DD")
            try:
                datetime.strptime(value, "%Y-%m-%d")
            except ValueError as exc:
                raise ValueError("dates must be YYYY-MM-DD") from exc
            if not re.fullmatch(r"\d{4}-\d{2}-\d{2}", value):
                raise ValueError("dates must be YYYY-MM-DD")
    if since and until and since > until:
        raise ValueError("since must not be after until")

    if (model is None) != (values is None):
        raise ValueError("give model and values together, or neither")
    if model is not None:
        packed = VectorIndex._packed(values)
        if not isinstance(model, str) or not model.strip():
            raise ValueError("model is required")
    filters = ["r.state = 'approved'", "r.privacy IN (" + ",".join("?" for _ in tiers) + ")"]
    args = list(tiers)
    if source is not None:
        filters.append("r.source = ?")
        args.append(source)
    if since:
        filters.append("r.created_at >= ?")
        args.append(since + " 00:00:00")
    if until:
        filters.append("r.created_at < date(?, '+1 day')")
        args.append(until)
    where = " AND ".join(filters)
    phrase = '"' + query.replace('"', '""') + '"'
    fields = "r.id, r.source, r.kind, r.privacy, r.body, r.created_at"
    # Bound the candidate work independently of the requested output size.
    candidate_limit = 1000
    text_rows = db.execute(
        f"SELECT {fields} FROM records_fts f JOIN records r ON r.id=f.rowid "
        f"WHERE records_fts MATCH ? AND {where} ORDER BY bm25(records_fts), r.id LIMIT ?",
        (phrase, *args, candidate_limit),
    ).fetchall()
    entry = None
    if model is not None:
        entry = db.execute("SELECT dimensions, table_name FROM embedding_models WHERE model = ?", (model,)).fetchone()
    vector_rows = []
    if entry:
        dimensions, table = entry
        if dimensions != len(values) or not re.fullmatch(r"vec_[0-9a-f]{64}", table):
            raise ValueError("invalid vector model or dimensions")
        vector_rows = db.execute(
            f'SELECT {fields}, vec_distance_L2(v.embedding, ?) AS distance '
            f'FROM records r JOIN embedding_provenance p ON p.record_id = r.id '
            f'JOIN "{table}" v ON v.rowid = r.id '
            f'WHERE p.model = ? AND p.body_hash = r.body_hash AND {where} '
            'ORDER BY distance, r.id LIMIT ?',
            (packed, model, *args, candidate_limit),
        ).fetchall()

    # Reciprocal-rank fusion combines unlike FTS and distance scores.
    results = {}
    for method, rows in (("text", text_rows), ("vector", vector_rows)):
        for rank, row in enumerate(rows, 1):
            record = results.setdefault(row[0], {
                "id": row[0], "source": row[1], "kind": row[2], "privacy": row[3],
                "body": row[4], "created_at": row[5], "matches": [], "score": 0.0,
            })
            record["matches"].append(method)
            record["score"] += 1.0 / (60 + rank)
    return sorted(results.values(), key=lambda item: (-item["score"], item["id"]))[:limit]
