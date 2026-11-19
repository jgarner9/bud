import hashlib
import json
import os
import sqlite3
import tempfile
import unittest
from pathlib import Path

from history import ensure_history, export_approved, export_recovery, record_reference, restore_recovery
from store import Store


class HistoryTest(unittest.TestCase):
    def test_reference_visibility_and_backup_restore(self):
        with tempfile.TemporaryDirectory() as temp:
            store = Store(str(Path(temp) / "memory.db"))
            ensure_history(store.db)
            private = store.stage("private", "1", "fact", "P0", "secret")
            public = store.stage("public", "2", "observation", "P2", "hello")
            staged = store.stage("pending", "3", "summary", "P2", "waiting")
            store.approve(private)
            store.approve(public)
            ref = record_reference(store.db, public, "session", "turn1")
            self.assertEqual(ref, record_reference(store.db, public, "session", "turn1"))
            with self.assertRaises(ValueError):
                record_reference(store.db, private, "session", "turn1")
            with self.assertRaises(KeyError):
                record_reference(store.db, staged, "session", "turn2")
            record_reference(store.db, private, "private-session", "turn3")

            visible = json.loads(export_approved(store.db, ["P2"]))
            self.assertEqual([public], [r["id"] for r in visible["records"]])
            self.assertEqual([ref], [r["id"] for r in visible["references"]])
            self.assertNotIn("secret", json.dumps(visible))
            for invalid in ("P2", [], ["P3"]):
                with self.assertRaises(ValueError):
                    export_approved(store.db, invalid)

            snapshot = export_recovery(store.db)
            restored = Store(str(Path(temp) / "restored.db"))
            ensure_history(restored.db)
            restore_recovery(restored.db, snapshot)
            self.assertEqual(json.loads(snapshot), json.loads(export_recovery(restored.db)))
            self.assertEqual("hello", restored.search("hello", ["P2"])[0][4])
            self.assertEqual([], restored.search("waiting", ["P2"]))
            with self.assertRaises(ValueError):
                restore_recovery(restored.db, snapshot)
            restored.close()
            store.close()

    def test_bad_snapshot_rolls_back_every_record(self):
        with tempfile.TemporaryDirectory() as temp:
            store = Store(str(Path(temp) / "memory.db"))
            ensure_history(store.db)
            body = "valid"
            record = dict(id=1, source="source", dedupe_key="key", kind="fact",
                          privacy="P0", body=body, body_hash=hashlib.sha256(body.encode()).hexdigest(),
                          created_at="2026-01-01", state="approved")
            bad = dict(record, id=2, dedupe_key="key2", body_hash="wrong")
            with self.assertRaises(ValueError):
                restore_recovery(store.db, json.dumps(dict(format=2, records=[record, bad], references=[], vectors=[])))
            self.assertEqual([], store.db.execute("SELECT * FROM records").fetchall())
            store.close()

@unittest.skipUnless(os.environ.get("BUD_TEST_SQLITE_VEC"), "set BUD_TEST_SQLITE_VEC to a test extension")
class VectorRecoveryTest(unittest.TestCase):
    def test_vector_round_trip_and_atomic_restore(self):
        from vectors import VectorIndex
        with tempfile.TemporaryDirectory() as temp:
            def open_store(name):
                store = Store(str(Path(temp) / name))
                store.db.enable_load_extension(True)
                store.db.load_extension(os.environ["BUD_TEST_SQLITE_VEC"])
                store.db.enable_load_extension(False)
                ensure_history(store.db)
                vectors = VectorIndex(store.db)
                return store, vectors

            source, index = open_store("source.db")
            record = source.stage("session", "1", "fact", "P0", "private")
            source.approve(record)
            index.put(record, "test-model", [0.25, 0.5])
            snapshot = export_recovery(source.db)
            target, target_index = open_store("target.db")
            restore_recovery(target.db, snapshot)
            self.assertEqual(record, target_index.search("test-model", [0.25, 0.5], ["P0"])[0][0])
            self.assertEqual([], target_index.search("test-model", [0.25, 0.5], ["P1"]))
            self.assertEqual(json.loads(snapshot), json.loads(export_recovery(target.db)))

            bad, _ = open_store("bad.db")
            malformed = json.loads(snapshot)
            malformed["vectors"][0]["entries"][0]["body_hash"] = "wrong"
            with self.assertRaises(ValueError):
                restore_recovery(bad.db, json.dumps(malformed))
            self.assertEqual(0, bad.db.execute("SELECT count(*) FROM records").fetchone()[0])
            self.assertEqual(0, bad.db.execute("SELECT count(*) FROM embedding_models").fetchone()[0])
            for store in (source, target, bad):
                store.close()
