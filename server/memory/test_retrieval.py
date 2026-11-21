import os
import tempfile
import unittest

from retrieval import retrieve
from store import Store
from vectors import VectorIndex


@unittest.skipUnless(os.environ.get("VEC_EXTENSION"), "sqlite-vec extension not configured")
class RetrievalTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.store = Store(os.path.join(self.temp.name, "memory.db"))
        self.store.db.enable_load_extension(True)
        self.store.db.load_extension(os.environ["VEC_EXTENSION"])
        self.store.db.enable_load_extension(False)
        self.index = VectorIndex(self.store.db)

    def tearDown(self):
        self.store.close()
        self.temp.cleanup()

    def add(self, source, key, privacy, body, vector, timestamp, approved=True):
        record = self.store.stage(source, key, "fact", privacy, body)
        self.store.db.execute("UPDATE records SET created_at = ? WHERE id = ?", (timestamp, record))
        self.store.db.commit()
        self.index.put(record, "embed", vector)
        if approved:
            self.store.approve(record)
        return record

    def test_hybrid_filters_before_ranking_and_includes_provenance(self):
        self.add("home", "private", "P0", "orchard private", [0, 0], "2026-01-01 12:00:00")
        self.add("home", "staged", "P2", "orchard staged", [0, 0], "2026-01-02 12:00:00", False)
        self.add("other", "source", "P2", "orchard wrong source", [0, 0], "2026-01-02 12:00:00")
        self.add("home", "old", "P2", "orchard old", [0, 0], "2025-12-31 12:00:00")
        target = self.add("home", "target", "P2", "orchard visible", [1, 0], "2026-01-02 12:00:00")
        rows = retrieve(self.store.db, "orchard", "embed", [0, 0], ["P2"], "home", "2026-01-01", "2026-01-02", 1)
        self.assertEqual([r["id"] for r in rows], [target])
        self.assertEqual(rows[0]["matches"], ["text", "vector"])
        self.assertEqual(rows[0]["source"], "home")
        self.assertEqual(rows[0]["privacy"], "P2")
        # without model and values it ranks by text alone, with the same filters
        rows = retrieve(self.store.db, "orchard", None, None, ["P2"], "home", "2026-01-01", "2026-01-02", 5)
        self.assertEqual([(r["id"], r["matches"]) for r in rows], [(target, ["text"])])
        with self.assertRaises(ValueError):
            retrieve(self.store.db, "orchard", "embed", None, ["P2"])

    def test_invalid_filters_fail_closed(self):
        for kwargs in ({"tiers": []}, {"tiers": ["P3"]}, {"tiers": ["P2"], "since": "2026-1-1"},
                       {"tiers": ["P2"], "since": "2026-02-01", "until": "2026-01-01"}):
            with self.assertRaises(ValueError):
                retrieve(self.store.db, "word", "embed", [0, 0], **kwargs)


class FusionWindowTests(unittest.TestCase):
    def test_shared_second_place_can_win_a_single_result(self):
        # A wins text only, B wins vector only, C is second in both.
        def row(record):
            return (record, "source", "fact", "P0", "synthetic", "2026-01-01")

        class Result:
            def __init__(self, rows):
                self.rows = rows
            def fetchall(self):
                return self.rows
            def fetchone(self):
                return self.rows[0]

        class DB:
            def execute(self, sql, args):
                if "embedding_models" in sql:
                    return Result([(2, "vec_" + "a" * 64)])
                self_test.assertEqual(args[-1], 1000)
                rows = [row(1), row(3)] if "records_fts MATCH" in sql else [row(2), row(3)]
                return Result(rows[:args[-1]])

        self_test = self
        results = retrieve(DB(), "word", "embed", [0, 0], ["P0"], limit=1)
        self.assertEqual([r["id"] for r in results], [3])
        self.assertEqual(results[0]["matches"], ["text", "vector"])


if __name__ == "__main__":
    unittest.main()
