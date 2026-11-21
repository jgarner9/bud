import os
import tempfile
import unittest
from store import Store
from vectors import StaleRecord, VectorIndex


@unittest.skipUnless(os.environ.get("VEC_EXTENSION"), "sqlite-vec extension not configured")
class VectorTests(unittest.TestCase):
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

    def test_privacy_staging_provenance_and_replacement(self):
        private = self.store.stage("a", "1", "fact", "P0", "private")
        public = self.store.stage("a", "2", "fact", "P2", "public")
        staged = self.store.stage("a", "3", "fact", "P2", "staged")
        for rid, vector in ((private, [0, 0]), (public, [1, 0]), (staged, [0, 0])):
            self.index.put(rid, "model", vector)
        self.store.approve(private)
        self.store.approve(public)
        self.assertEqual([r[0] for r in self.index.search("model", [0, 0], ["P2"])], [public])
        self.index.put(public, "model", [2, 0])
        self.assertAlmostEqual(self.index.search("model", [0, 0], ["P2"])[0][-1], 2)
        self.store.db.execute("UPDATE records SET body = ?, body_hash = ? WHERE id = ?", ("edited", "new", public))
        self.assertEqual(self.index.search("model", [0, 0], ["P2"]), [])
        self.index.put(public, "model", [3, 0])
        self.assertEqual(len(self.index.search("model", [0, 0], ["P2"])), 1)

    def test_dimensions_and_invalid_vectors(self):
        rid = self.store.stage("a", "1", "fact", "P0", "test")
        self.index.put(rid, "model", [1, 2])
        for value in ([1], [float("nan"), 2], [True, 1], [1e200, 1]):
            with self.assertRaises(ValueError):
                self.index.put(rid, "model", value)
        with self.assertRaises(ValueError):
            self.index.search("model", [1], ["P0"])

    def test_pending_lists_what_a_model_still_has_to_embed(self):
        done = self.store.stage("a", "1", "fact", "P0", "embedded")
        edited = self.store.stage("a", "2", "fact", "P0", "edited later")
        fresh = self.store.stage("a", "3", "fact", "P0", "never embedded")
        hidden = self.store.stage("a", "4", "fact", "P2", "another tier")
        staged = self.store.stage("a", "5", "fact", "P0", "not approved")
        for rid in (done, edited, fresh, hidden):
            self.store.approve(rid)
        self.index.put(done, "old", [1, 0])
        self.index.put(edited, "old", [1, 0])
        self.store.db.execute("UPDATE records SET body = ?, body_hash = ? WHERE id = ?", ("changed", "new", edited))
        self.assertEqual([r[0] for r in self.index.pending("old", ["P0"])], [edited, fresh])
        # a new model starts with every approved record pending, then drains
        self.assertEqual([r[0] for r in self.index.pending("new", ["P0"])], [done, edited, fresh])
        first = self.index.pending("new", ["P0"], limit=1)
        self.assertEqual([row[:3] for row in first], [(done, "P0", "embedded")])
        # an edit between reading a record and storing its vector is refused
        self.store.db.execute("UPDATE records SET body = ?, body_hash = ? WHERE id = ?", ("again", "newer", done))
        with self.assertRaises(StaleRecord):
            self.index.put(done, "new", [0, 1, 0], first[0][3])
        self.assertIn(done, [r[0] for r in self.index.pending("new", ["P0"])])
        for rid, _, _, body_hash in self.index.pending("new", ["P0"]):
            self.index.put(rid, "new", [0, 1, 0], body_hash)
        self.assertEqual(self.index.pending("new", ["P0"]), [])
        self.assertNotIn(staged, [r[0] for r in self.index.pending("new", ["P0", "P2"])])
        for bad in ((" ", ["P0"], 20), ("m", [], 20), ("m", ["P3"], 20), ("m", ["P0"], 0), ("m", ["P0"], True)):
            with self.assertRaises(ValueError):
                self.index.pending(*bad)

if __name__ == "__main__":
    unittest.main()
