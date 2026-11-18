"""The release policy for calculation defects (scripts/release_policy.py; p1-launch-readiness task 3.4, design D9).

Run with `make script-tests` (part of `make check`)."""
import pathlib
import subprocess
import sys
import tempfile
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))
import release_policy  # noqa: E402

SCRIPT = pathlib.Path(__file__).resolve().parent / "release_policy.py"


class TheRule(unittest.TestCase):
    """A fix that changes calculation code adds or changes a test in the same pull request; nothing else is asked.
    # req: QA-REL-01
    """

    def test_a_fix_of_the_engine_without_a_test_fails(self):
        problems = release_policy.judge(["fix(engine): settle a payment whose reference has a non-breaking space"],
                                        ["internal/domain/ledger/settle.go"])
        self.assertTrue(problems)
        self.assertIn("internal/domain/ledger/settle.go", "\n".join(problems))

    def test_a_fix_with_its_test_passes(self):
        for test in ("internal/domain/ledger/settle_test.go", "oracle/tests/test_settlement.py",
                     "test/differential/golden/bank.json", "packs/sg-gst/2026.2/pinned/rates.csv",
                     "apps/web/src/format/amount.test.ts"):
            changed = ["internal/domain/ledger/settle.go", test]
            self.assertEqual(release_policy.judge(["fix(engine): settle it"], changed), [], test)

    def test_the_oracle_and_the_packs_are_calculation_code(self):
        for code in ("oracle/src/oracle/ledger.py", "packs/sg-cpf/2027.1/rates.csv"):
            self.assertTrue(release_policy.judge(["fix(oracle,packs): round as the policy says"], [code]), code)

    def test_a_fix_elsewhere_and_other_types_pass(self):
        rule = ["internal/domain/ledger/rule.go"]
        self.assertEqual(release_policy.judge(["fix(web): the button's label"], ["apps/web/src/screens/Home.tsx"]), [])
        self.assertEqual(release_policy.judge(["feat(ledger): a new posting rule"], rule), [])
        self.assertEqual(release_policy.judge(["refactor(engine): one function"], rule), [])

    def test_the_title_of_a_squashed_pull_request_counts(self):
        self.assertTrue(release_policy.judge(["feat(ledger): first part"], ["internal/domain/ledger/rule.go"],
                                             "fix(ledger): the rounding of a credit line"))
        self.assertTrue(release_policy.judge(["fix(ledger)!: round per line"], ["internal/domain/rounding/gst.go"]))


class OnARepository(unittest.TestCase):
    """The script reads a range of commits as CI gives it, leaving merge commits out.
    # req: QA-REL-01
    """

    def git(self, *args):
        subprocess.run(["git", "-C", self.dir, "-c", "user.name=t", "-c", "user.email=t@example.com",
                        "-c", "commit.gpgsign=false", *args], check=True, capture_output=True)

    def commit(self, path, subject):
        p = pathlib.Path(self.dir, path)
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_text(subject)
        self.git("add", path)
        self.git("commit", "-q", "-m", subject)

    def run_policy(self, rev_range):
        return subprocess.run([sys.executable, str(SCRIPT), rev_range], cwd=self.dir, capture_output=True, text=True)

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.dir = self.tmp.name
        self.git("init", "-q", "-b", "main")
        self.commit("README.md", "docs: start")

    def tearDown(self):
        self.tmp.cleanup()

    def test_a_branch_with_a_fix_and_then_its_test(self):
        self.git("checkout", "-q", "-b", "fix")
        self.commit("internal/domain/ledger/settle.go", "fix(engine): settle it")
        r = self.run_policy("main..HEAD")
        self.assertEqual(r.returncode, 1, r.stdout)
        self.commit("internal/domain/ledger/settle_test.go", "test(engine): the case that failed")
        r = self.run_policy("main..HEAD")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)


if __name__ == "__main__":
    unittest.main()
