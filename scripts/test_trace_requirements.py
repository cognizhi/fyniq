"""The trace gate (scripts/trace_requirements.py; PRD-001 QA-TRACE-01; p1-launch-readiness task 1.2, design D2), on planted
PRD tables and test files; and the release's launch gate (task 7.2), release.yml's step run on a planted repository.
Run with `make script-tests` (part of `make check`)."""
import json
import os
import pathlib
import re
import shutil
import subprocess
import sys
import tempfile
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))
import prdspec  # noqa: E402
import trace_requirements as tr  # noqa: E402

PRD = """# A PRD

## 8. Features

### 8.1 Ledger

| ID | Requirement | Pri | Phase |
|---|---|---|---|
| FR-GL-01 | A tested row | M | P1 |
| FR-GL-02 | An untested row | M | P1 |
| FR-GL-03 | A should row | S | P1 |
| FR-GL-04 | A could row | C | P1 |
| FR-GL-05 | Files now, APIs later | M before GST registration; else S | P1 (files) / P2 (APIs) |
| FR-GL-06 | A later row | M | P2 |
| FR-GL-07 | As another | M | As FR-GL-02 |
| INV-01 | An invariant | M | P1 |
| CTL-FIN-01 | A control | M | P1 |
| CTL-FIN-02 | A control waiting | M | P1 |
| FR-PAY-01 | Pay runs | M | P1 |
| FR-PAY-09 | The CPF file | M | P1 |

### 8.2 Tasks

| Task type | What it proposes | Tier | Phase |
|---|---|---|---|
| sec.create_task | A task | 1 | P1 |
"""

PRD2 = """# PRD-002

**Delivery map (NM-PAY-07)**

| PRD-001 row | Runs in Nemura | Fyniq's part | Phase in Nemura |
|---|---|---|---|
| FR-PAY-01 pay runs | Nemura | Posts | P1 |
| FR-PAY-09 CPF submission file | Nemura | — | P2 |
"""


def sources(prd=PRD):
    d = pathlib.Path(tempfile.mkdtemp())
    (d / "prd.md").write_text(prd, encoding="utf-8")
    (d / "src.json").write_text(json.dumps({"doc": "PRD-001", "prd": str(d / "prd.md"), "task_sections": ["8.2"]}), encoding="utf-8")
    return [prdspec.Source(d / "src.json")]


TESTS = {
    "internal/ledger/ledger_test.go": "// req: FR-GL-01\nfunc TestX(t *testing.T) {}",
    "apps/web/src/a.test.tsx": "// req: sec.create_task\nit('x')",
    "oracle/tests/test_a.py": "# req: FR-PAY-01",
    "db/checks/a_test.sql": "-- req: FR-GL-05",
    "internal/ledger/ledger.go": "// req: FR-GL-02 (not a test file)",
}


class TheTraceGate(unittest.TestCase):
    """# req: QA-TRACE-01"""

    def setUp(self):
        self.src = sources()
        self.rows = tr.rows(self.src, PRD2)
        files = [f for f in TESTS if tr.TEST_FILE.search(f)]
        self.anchors, self.titled = tr.covered(tr.tags(files, TESTS.get), self.src)
        self.ctl = {"CTL-FIN-01": ["running"], "CTL-FIN-02": ["planned"]}

    def gate(self, phase):
        g = tr.phase_gate(phase, self.rows, self.anchors, self.titled, self.ctl, self.src)
        return [r["anchor"] for r in g["blocking"]], [r["anchor"] for r in g["reported"]]

    def test_tags_in_go_typescript_python_and_sql_and_in_test_files_only(self):
        self.assertEqual(self.anchors, {"FR-GL-01", "sec.create_task", "FR-PAY-01", "FR-GL-05"})

    def test_the_browser_suites_count(self):
        for f in ("apps/web/e2e/devices.spec.ts", "apps/web/journeys/sell.spec.ts", "apps/web/visual/visual.spec.ts"):
            self.assertTrue(tr.TEST_FILE.search(f), f)
        self.assertFalse(tr.TEST_FILE.search("apps/web/visual/visual.config.ts"))

    def test_an_untested_m_row_blocks_and_s_and_c_rows_are_reported(self):
        blocking, reported = self.gate("P1")
        self.assertIn("FR-GL-02", blocking)
        self.assertNotIn("FR-GL-01", blocking)
        self.assertEqual(sorted(reported), ["FR-GL-03", "FR-GL-04"])

    def test_several_phases_and_a_conditional_priority(self):
        self.assertEqual(self.rows["FR-GL-05"]["phases"], ["P1", "P2"])
        self.assertEqual(self.rows["FR-GL-05"]["pri"], "M")
        self.assertEqual(self.rows["FR-GL-07"]["phases"], ["P1"])  # as FR-GL-02
        self.assertIn("FR-GL-06", self.gate("P2")[0])
        self.assertNotIn("FR-GL-06", self.gate("P1")[0])

    def test_invariants_and_controls_are_covered_elsewhere(self):
        blocking, _ = self.gate("P1")
        self.assertNotIn("INV-01", blocking)  # QA-ENG-05's
        self.assertNotIn("CTL-FIN-01", blocking)  # its check runs in the control registry
        self.assertIn("CTL-FIN-02", blocking)  # its check is only planned

    def test_both_prds_nemuras_payroll_rows_at_their_phase_in_nemura(self):
        self.assertEqual(self.rows["FR-PAY-09"]["phases"], ["P2"])
        self.assertEqual(self.rows["FR-PAY-09"]["doc"], "PRD-002 (NM-PAY-07)")
        self.assertNotIn("FR-PAY-09", self.gate("P1")[0])
        self.assertEqual(self.rows["sec.create_task"]["pri"], "M")  # a table with no priority column

    def test_an_unparseable_row_fails_and_names_it(self):
        for bad in ("| FR-GL-09 | A row | X | P1 |", "| FR-GL-09 | A row | M | soon |", "| FR-GL-09 | A row | M | As FR-GL-99 |"):
            with self.subTest(bad):
                with self.assertRaisesRegex(tr.TraceError, "FR-GL-09"):
                    tr.rows(sources(PRD.replace("| FR-GL-06 | A later row | M | P2 |", bad)), PRD2)

    def test_a_recorded_phase_move_must_match_g4_001(self):
        # req: G4-001
        moved = PRD.replace("| FR-GL-06 | A later row | M | P2 |", "| FR-GL-06 | (rev 0.7: P1, with the files) A row | M | P2 |")
        with self.assertRaisesRegex(tr.TraceError, "FR-GL-06.*G4-001"):
            tr.rows(sources(moved), PRD2)
        tr.rows(sources(PRD.replace("| FR-GL-06 | A later row | M | P2 |", "| FR-GL-06 | (rev 0.7: P1) A row | M | P1 / P2 |")), PRD2)

    def test_a_tag_on_a_capability_and_title(self):
        anchors, titled = tr.covered({"governance/company-secretary FR-CS-05 Documents drafted": set(),
                                      "quality/calculation-correctness Golden data set, sample company": set()}, self.src)
        self.assertIn("FR-CS-05", anchors)
        self.assertTrue(tr.is_covered("quality/calculation-correctness: Golden data set, sample company", anchors, titled, {}, self.src))
        self.assertFalse(tr.is_covered("quality/calculation-correctness: Golden data set, regressions", anchors, titled, {}, self.src))

    def test_the_launch_gate_counts_exceptions_and_reports_prose(self):
        subset = {"changes": [{"id": "p1-x", "requirements": ["FR-GL-01", "FR-GL-02", "FR-GL-03", "C.2 #9",
                                                               "ledger/posting-rules: A posting rule"], "scenarios": ["G4-001"]}]}
        exceptions = {"exceptions": [{"id": "C.2 #9", "kind": "after-go-live"}]}
        g = tr.launch_gate(subset, exceptions, self.rows, self.anchors, self.titled, self.ctl, self.src)
        self.assertEqual(g["blocking"]["p1-x"], ["FR-GL-02", "G4-001"])
        self.assertEqual(g["reported"]["p1-x"], ["FR-GL-03", "ledger/posting-rules: A posting rule"])
        self.assertEqual(g["excepted"]["p1-x"], [{"id": "C.2 #9", "kind": "after-go-live"}])
        self.assertEqual(g["covered"], 1)

    def test_the_repositorys_prds_parse(self):
        self.assertGreater(len(tr.rows()), 700)


RELEASE = prdspec.ROOT / ".github/workflows/release.yml"
MAKEFILE = prdspec.ROOT / "Makefile"


def release_jobs(text):
    """release.yml's jobs: each one's `run:` commands and the jobs it needs (a reading of the YAML's own layout)."""
    jobs, cur = {}, None
    for line in text.split("\n")[text.split("\n").index("jobs:") + 1:]:
        m = re.match(r"^  ([a-z0-9-]+):$", line)
        if m:
            cur = jobs.setdefault(m.group(1), {"runs": [], "needs": []})
        elif cur is not None and (m := re.match(r"^      - run: (.+)$", line)):
            cur["runs"].append(m.group(1).strip())
        elif cur is not None and (m := re.match(r"^    needs: \[?([^\]]+?)\]?$", line)):
            cur["needs"] = [n.strip() for n in m.group(1).split(",")]
    return jobs


def planted_repository(tagged):
    """A repository holding the trace gate's Makefile target and scripts, the planted PRDs, a launch subset of FR-GL-01
    and FR-GL-02, and a test tagging FR-GL-01 (and FR-GL-02 when tagged)."""
    d = pathlib.Path(tempfile.mkdtemp())
    rule = re.search(r"^trace-gate:.*\n(?:\t.*\n)+", MAKEFILE.read_text(encoding="utf-8"), re.M).group(0)
    files = {
        "Makefile": "SHELL := /usr/bin/env bash\n.SHELLFLAGS := -euo pipefail -c\n\n" + rule,
        "openspec/sources/prd-001.json": json.dumps({"doc": "PRD-001", "prd": "docs/prd/prd.md", "task_sections": ["8.2"]}),
        "docs/prd/prd.md": PRD,
        "docs/prd/prd-002-nemura-people.md": PRD2,
        "docs/prd/plans/launch-subset.json": json.dumps({"changes": [{"id": "p1-launch", "requirements": ["FR-GL-01", "FR-GL-02"], "scenarios": []}]}),
        "docs/prd/plans/launch-exceptions.json": json.dumps({"exceptions": []}),
        "internal/ledger/ledger_test.go": "// req: FR-GL-01\n" + ("// req: FR-GL-02\n" if tagged else ""),
    }
    for rel, text in files.items():
        (d / rel).parent.mkdir(parents=True, exist_ok=True)
        (d / rel).write_text(text, encoding="utf-8")
    (d / "scripts").mkdir()
    for script in ("trace_requirements.py", "prdspec.py"):
        shutil.copy(prdspec.ROOT / "scripts" / script, d / "scripts" / script)
    subprocess.run(["git", "init", "-q"], cwd=d, check=True)
    subprocess.run(["git", "add", "-A"], cwd=d, check=True)
    return d


class TheReleasesLaunchGate(unittest.TestCase):
    """The release runs the launch gate before it builds an image (p1-launch-readiness task 7.2), and its dry run: the
    gate's step from release.yml, through the Makefile, on a planted repository.
    # req: QA-TRACE-01"""

    def test_the_release_runs_the_launch_gate_before_its_images(self):
        jobs = release_jobs(RELEASE.read_text(encoding="utf-8"))
        self.assertEqual([j for j, v in jobs.items() if "make trace-gate PHASE=launch" in v["runs"]], ["launch-gate"])
        self.assertIn("launch-gate", jobs["images"]["needs"])

    def test_the_dry_run_fails_on_a_planted_untested_row_and_passes_once_a_test_tags_it(self):
        step = release_jobs(RELEASE.read_text(encoding="utf-8"))["launch-gate"]["runs"]
        self.assertEqual(step, ["make trace-gate PHASE=launch"])
        env = {k: v for k, v in os.environ.items() if not k.startswith(("MAKE", "MFLAGS"))}
        for tagged in (False, True):
            with self.subTest(tagged=tagged):
                d = planted_repository(tagged)
                r = subprocess.run(["bash", "-c", step[0]], cwd=d, env=env, capture_output=True, text=True)
                if tagged:
                    self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
                    self.assertIn("every launch requirement is tested or excepted", r.stdout)
                else:
                    self.assertNotEqual(r.returncode, 0, r.stdout)
                    self.assertIn("p1-launch: FR-GL-02", r.stdout)
                self.assertTrue((d / ".data/trace/report.json").exists())
                shutil.rmtree(d)


if __name__ == "__main__":
    unittest.main()
