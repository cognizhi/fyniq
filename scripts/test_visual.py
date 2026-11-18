"""The visual snapshots' baselines (scripts/visual.py; QA-UI-07): a run's hashes are taken in, sorted, a changed one
replaces its baseline, and a malformed key or hash is refused. Run with `make script-tests`."""
import json
import pathlib
import sys
import tempfile
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))
import visual  # noqa: E402

A, B = "a" * 64, "b" * 64


class Baselines(unittest.TestCase):
    def test_the_shipped_baselines_are_well_formed(self):
        self.assertEqual(visual.problems(json.loads(visual.BASELINES.read_text())), [])

    def test_a_runs_hashes_are_taken_in_sorted(self):
        with tempfile.TemporaryDirectory() as d:
            run = pathlib.Path(d)
            (run / "hashes-home-light.json").write_text(json.dumps({"home/phone/light": B, "home/desktop/light": A}))
            (run / "hashes-inbox-dark.json").write_text(json.dumps({"inbox/tablet/dark": A}))
            merged = visual.approve(run, {"home/desktop/light": B, "settings/desktop/dark": A})
        self.assertEqual(list(merged), ["home/desktop/light", "home/phone/light", "inbox/tablet/dark", "settings/desktop/dark"])
        self.assertEqual(merged["home/desktop/light"], A)  # the changed picture replaces its baseline

    def test_a_change_is_a_picture_added_altered_or_removed(self):
        self.assertEqual(visual.changed({}, {}), [])  # the file added empty changes no picture
        self.assertEqual(visual.changed({"home/phone/light": A}, {"home/phone/light": A}), [])
        self.assertEqual(visual.changed({"home/phone/light": A, "inbox/phone/dark": A}, {"home/phone/light": B, "home/tablet/light": A}),
                         ["home/phone/light", "home/tablet/light", "inbox/phone/dark"])

    def test_a_malformed_key_or_hash_is_refused(self):
        self.assertEqual(len(visual.problems({"home/watch/light": A, "home/phone/sepia": A, "home/phone/dark": "xyz"})), 3)
        self.assertEqual(visual.problems([]), ["the baselines are not a JSON object of picture → SHA-256"])


if __name__ == "__main__":
    unittest.main()
