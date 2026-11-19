import tempfile
import unittest
from pathlib import Path

from store import Store


class StoreTest(unittest.TestCase):
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


if __name__ == "__main__":
    unittest.main()
