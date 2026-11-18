"""scripts/kb.py and the Go lint (internal/domain/knowledge) agree on the shared fixtures (AI-52; p1-knowledge-base design D1).

Each directory of test/kb/fixtures/ is a small repository (kb/ and docs/user-guide/) with an expect.txt: one line per finding,
"error <RULE> <where>" or "warn <RULE> <where>". The Go test (internal/domain/knowledge/fixtures_test.go) reads the same
files, so a rule changed in one lint and not the other fails one of the two.

Run with `make script-tests` (part of `make check`). `python3 scripts/test_kb_fixtures.py --write` rewrites the expect files
from this lint's findings; review the diff.
"""
import os
import pathlib
import re
import subprocess
import sys
import unittest

ROOT = pathlib.Path(__file__).resolve().parent.parent
FIXTURES = ROOT / "test" / "kb" / "fixtures"

# kb.py's messages, as the rules the Go lint names them (internal/domain/knowledge/lint.go).
RULES = [
    (r"only Markdown files belong", "KB-FILE-TYPE"),
    (r"missing front matter|unreadable front-matter line", "KB-FRONT"),
    (r"duplicate key", "KB-DUPLICATE-KEY"),
    (r"front matter is missing", "KB-FIELD"),
    (r"type must be one of", "KB-TYPE"),
    (r"page belongs in kb/pages/", "KB-TYPE-DIR"),
    (r"summary is longer", "KB-SUMMARY"),
    (r"schema must be", "KB-SCHEMA"),
    (r"data_class must be", "KB-CLASS"),
    (r"taint must be", "KB-TAINT"),
    (r"status must be", "KB-STATUS"),
    (r"human_edited must be", "KB-HUMAN-EDITED"),
    (r"compiled_at must be", "KB-DATE"),
    (r"aliases must be", "KB-ALIASES"),
    (r"an untrusted page stays draft", "KB-UNTRUSTED"),
    (r"source line must be", "KB-SOURCE-LINE"),
    (r"is internal and may not be a source", "KB-SOURCE-BANNED"),
    (r"source not found", "KB-SOURCE-GONE"),
    (r"STALE", "KB-STALE"),
    (r"published, but its source", "KB-PUBLISH-DRAFT"),
    (r"the body should start with", "KB-TITLE"),
    (r"cites no source", "KB-SECTION-CITE"),
    (r"which is not in its sources", "KB-CITE-SOURCE"),
    (r"but the source has no such heading", "KB-CITE-ANCHOR"),
    (r"matches no page key", "KB-LINK"),
    (r"appears in none of its sources", "KB-NUMBER"),
    (r"orphan page", "KB-ORPHAN"),
    (r"source not compiled into any page", "KB-UNCOVERED"),
    (r"is out of date; run make kb-index", "KB-INDEX"),
]


def findings(case):
    out = subprocess.run([sys.executable, str(ROOT / "scripts" / "kb.py"), "lint"], capture_output=True, text=True,
                         env=dict(os.environ, KB_ROOT=str(case)))
    lines = set()
    for line in out.stdout.splitlines():
        m = re.match(r"^\s+(✖|⚠) (.*)$", line)
        if not m:
            continue
        level, text = ("error" if m.group(1) == "✖" else "warn"), m.group(2)
        where = "kb/index.md" if text.startswith("kb/index.md ") else text.split(": ", 1)[0]
        for pattern, rule in RULES:
            if re.search(pattern, text):
                lines.add(f"{level} {rule} {where}")
                break
        else:
            raise AssertionError(f"{case.name}: no rule for the message {text!r}")
    return lines


class TheLintsAgree(unittest.TestCase):
    """# req: AI-52"""

    def test_every_fixture_gives_its_expected_findings(self):
        cases = sorted(p for p in FIXTURES.iterdir() if p.is_dir())
        self.assertGreaterEqual(len(cases), 20)
        for case in cases:
            with self.subTest(case=case.name):
                want = set((case / "expect.txt").read_text().split("\n")) - {""}
                self.assertEqual(findings(case), want)


if __name__ == "__main__":
    if "--write" in sys.argv:
        for case in sorted(p for p in FIXTURES.iterdir() if p.is_dir()):
            (case / "expect.txt").write_text("".join(f"{l}\n" for l in sorted(findings(case))))
    else:
        unittest.main()
