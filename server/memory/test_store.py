import sqlite3
import tempfile
import unittest
from contextlib import closing
from pathlib import Path

from store import Store


class StoreTest(unittest.TestCase):
    def test_sqlite_wal_backup_contains_only_committed_memory(self):
        with tempfile.TemporaryDirectory() as tmp:
            source_path = Path(tmp) / "source.db"
            backup_path = Path(tmp) / "backup.db"
            with closing(Store(source_path)) as writer:
                self.assertEqual(("wal",), writer.db.execute("PRAGMA journal_mode=WAL").fetchone())
                record = writer.stage("synthetic", "1", "fact", "P0", "orchard")
                writer.approve(record)
                with closing(Store(source_path)) as reader, closing(sqlite3.connect(backup_path)) as backup:
                    # Read the committed snapshot while the writer holds an
                    # open transaction.
                    writer.db.execute("BEGIN")
                    writer.db.execute("UPDATE records SET body = ? WHERE id = ?", ("fixture's replacement", record))
                    self.assertEqual(record, writer.search("replacement", ["P0"])[0][0])
                    self.assertEqual([], reader.search("replacement", ["P0"]))
                    reader.db.backup(backup)
                    writer.db.rollback()
                    self.assertEqual([], writer.search("replacement", ["P0"]))

            with closing(Store(backup_path)) as restored:
                self.assertEqual(record, restored.search("orchard", ["P0"])[0][0])
                self.assertEqual([], restored.search("replacement", ["P0"]))

    def test_search_migrates_existing_records_and_filters_visibility(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = str(Path(tmp) / "memory.db")
            db = sqlite3.connect(path)
            db.execute("""CREATE TABLE records (
                id INTEGER PRIMARY KEY, source TEXT NOT NULL, dedupe_key TEXT NOT NULL,
                kind TEXT NOT NULL, privacy TEXT NOT NULL, body TEXT NOT NULL,
                body_hash TEXT NOT NULL, created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
                state TEXT NOT NULL DEFAULT 'staged', UNIQUE(source, dedupe_key))""")
            db.execute("INSERT INTO records (source, dedupe_key, kind, privacy, body, body_hash, state) VALUES (?, ?, ?, ?, ?, ?, ?)",
                       ("old", "1", "fact", "P0", "summer project", "hash", "approved"))
            db.execute("PRAGMA user_version = 1")
            db.commit()
            db.close()
            store = Store(path)
            self.assertEqual("summer project", store.search("summer project", ["P0"])[0][4])
            self.assertEqual([], store.search("summer project", ["P1"]))
            staged = store.stage("new", "2", "fact", "P1", "summer project")
            self.assertEqual([], store.search("summer project", ["P1"]))
            store.approve(staged)
            self.assertEqual(staged, store.search("summer project", ["P1"])[0][0])
            self.assertEqual([], store.search('summer" OR project', ["P0"]))
            with self.assertRaises(ValueError):
                store.search("summer", ["P0"], 0)
            store.close()
            store = Store(path)
            self.assertEqual(staged, store.search("summer project", ["P1"])[0][0])
            store.close()

    def test_staging_deduplication_and_visibility(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = str(Path(tmp) / "memory.db")
            store = Store(path)
            record = store.stage("session", "event-1", "fact", "P0", "example")
            self.assertEqual(record, store.stage("session", "event-1", "fact", "P0", "example"))
            with self.assertRaises(ValueError):
                store.stage("session", "event-1", "fact", "P1", "changed")
            self.assertEqual([], store.approved(["P0"]))
            store.approve(record)
            self.assertEqual([], store.approved(["P1"]))
            self.assertEqual("example", store.approved(["P0"])[0][4])
            store.close()
            store = Store(path)
            self.assertEqual("example", store.approved(["P0"])[0][4])
            store.close()

    def test_stage_rejects_invalid_kind_and_privacy(self):
        with tempfile.TemporaryDirectory() as tmp:
            store = Store(str(Path(tmp) / "memory.db"))
            for kind, privacy in (("invalid", "P0"), ("fact", "private")):
                with self.subTest(kind=kind, privacy=privacy):
                    with self.assertRaises(ValueError):
                        store.stage("session", "event-1", kind, privacy, "example")
            store.close()


if __name__ == "__main__":
    unittest.main()
