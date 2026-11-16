"""The launch subset and its exceptions (scripts/roadmap.py; p1-launch-readiness task 1.1, design D1).

Run with `make script-tests` (part of `make check`)."""
import copy
import json
import pathlib
import sys
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))
import roadmap  # noqa: E402


class TheLaunchSubset(unittest.TestCase):
    """The subset follows the plan's launch column, and holds each launching change's requirements and acceptance
    scenarios.
    # req: QA-TRACE-01
    """

    def setUp(self):
        self.saved = copy.deepcopy(roadmap.C)

    def tearDown(self):
        roadmap.C[:] = self.saved

    def ids(self, subset):
        return [c["id"] for c in subset["changes"]]

    def test_a_change_moves_in_and_out_with_its_launch_column(self):
        reqs = roadmap.requirements()
        self.assertIn("p1-reports", self.ids(roadmap.launch_subset(reqs)))
        next(c for c in roadmap.C if c["id"] == "p1-reports")["launch"] = "After go-live (a test's decision)"
        self.assertNotIn("p1-reports", self.ids(roadmap.launch_subset(reqs)))
        next(c for c in roadmap.C if c["id"] == "p1-projects")["launch"] = "Launch (a test's decision)"
        self.assertIn("p1-projects", self.ids(roadmap.launch_subset(reqs)))

    def test_a_part_at_launch_launches_its_change(self):
        gst = next(c for c in roadmap.launch_subset(roadmap.requirements())["changes"] if c["id"] == "p1-gst")
        self.assertIn("FR-TAX-02", gst["requirements"])

    def test_each_change_holds_its_requirements_and_scenarios(self):
        reqs = [dict(cap="quality/test-gates", rid="QA-TRACE-01", head="QA-TRACE-01 Requirement traceability", phase=None,
                     target="p1-launch-readiness", scenarios=["G4-001"]),
                dict(cap="quality/calculation-correctness", rid=None, head="Pinned hourly cost", phase=None,
                     target="p1-launch-readiness", scenarios=[]),
                dict(cap="projects/time-and-costs", rid="FR-PRJ-01", head="FR-PRJ-01 Projects", phase="P1",
                     target="p1-projects", scenarios=["G4-090"])]
        mine = next(c for c in roadmap.launch_subset(reqs)["changes"] if c["id"] == "p1-launch-readiness")
        self.assertEqual(mine["requirements"], ["QA-TRACE-01", "quality/calculation-correctness: Pinned hourly cost"])
        self.assertEqual(mine["scenarios"], ["G4-001"])


class TheExceptions(unittest.TestCase):
    """An exception names something of the subset once, with a known kind, a why, and the decision, evidence or gate it
    rests on; the repository's own exceptions pass.
    # req: QA-TRACE-01
    """

    subset = {"changes": [{"id": "p1-gst", "requirements": ["FR-TAX-02", "C.2 #46"], "scenarios": ["G4-033"]}]}

    def errors(self, *exceptions):
        return roadmap.check_exceptions(self.subset, json.dumps({"exceptions": list(exceptions)}))

    def test_the_repositorys_exceptions_hold(self):
        self.assertEqual(roadmap.check_exceptions(roadmap.launch_subset(roadmap.requirements())), [])

    def test_a_well_formed_exception_of_each_kind(self):
        self.assertEqual(self.errors(
            {"id": "FR-TAX-02", "kind": "after-go-live", "why": "At registration.", "decision": "rev 0.25 (Q2)"},
            {"id": "C.2 #46", "kind": "live-verify", "why": "In the dev cloud.", "evidence": "docs/runbooks/super-admin-recovery.md"},
            {"id": "G4-033", "kind": "covered-elsewhere", "why": "Another gate.", "by": "QA-ENG-05"}), [])

    def test_an_exception_resting_on_nothing_fails(self):
        cases = {
            "not in the subset": {"id": "FR-XYZ-99", "kind": "after-go-live", "why": "x", "decision": "rev 0.25"},
            "a kind not known": {"id": "FR-TAX-02", "kind": "later", "why": "x"},
            "no why": {"id": "FR-TAX-02", "kind": "after-go-live", "why": " ", "decision": "rev 0.25"},
            "no revision": {"id": "FR-TAX-02", "kind": "after-go-live", "why": "x", "decision": "the owner said so"},
            "a revision the history lacks": {"id": "FR-TAX-02", "kind": "after-go-live", "why": "x", "decision": "rev 0.9999"},
            "evidence that is no file": {"id": "FR-TAX-02", "kind": "live-verify", "why": "x", "evidence": "docs/runbooks/none.md"},
            "no gate": {"id": "FR-TAX-02", "kind": "covered-elsewhere", "why": "x"},
        }
        for name, x in cases.items():
            with self.subTest(name):
                self.assertNotEqual(self.errors(x), [], name)
        dup = {"id": "FR-TAX-02", "kind": "after-go-live", "why": "x", "decision": "rev 0.25"}
        self.assertTrue(any("listed twice" in e for e in self.errors(dup, dup)))


if __name__ == "__main__":
    unittest.main()
