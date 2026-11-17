#!/usr/bin/env python3
"""The release policy for calculation defects, as a check (PRD-001 QA-REL-01; p1-launch-readiness design D9).

A `fix` that changes how Fyniq calculates comes with its regression test. When a commit of the range, or the pull
request's title (which the squash merge takes), is a `fix`, and the range changes calculation code, the same range
must add or change a test:
  - calculation code is the domain packages (internal/domain/), the oracle's reference ledger (oracle/src/) and the
    statutory packs (packs/);
  - a test is a Go, TypeScript or SQL test file, the oracle's tests, the differential and golden scenarios under test/,
    or a pack's pinned cases.
A `fix` elsewhere, and any other type, passes. Merge commits are left out: what a restack brings from main is not the
pull request's.

  python3 scripts/release_policy.py origin/main..HEAD                          # exit 1 when a fix has no test
  python3 scripts/release_policy.py BASE..HEAD --title "fix(engine): …"        # as CI runs it on a pull request

The root-cause review that follows a calculation defect found after a release is the bug-fix skill's §7; its record's
template is .claude/skills/fyniq-bug-fix/templates/root-cause.md. Standard library only.
"""
from __future__ import annotations

import argparse
import re
import subprocess
import sys

FIX = re.compile(r"^fix(\([a-z,]+\))?!?: ")
CALCULATION = re.compile(r"^(internal/domain/|oracle/src/|packs/)")
TEST = re.compile(r"(_test\.go|\.test\.tsx?|_test\.sql)$|^oracle/tests/|^test/|^packs/[^/]+/[^/]+/pinned/")


def judge(subjects: list[str], files: list[str], title: str = "") -> list[str]:
    """What breaks the policy: the fixes, the calculation code they change and the missing test, or nothing."""
    fixes = [s for s in subjects if FIX.match(s)]
    if title and FIX.match(title):
        fixes.append(f"{title} (the pull request's title)")
    tests = [f for f in files if TEST.search(f)]
    calculation = [f for f in files if CALCULATION.match(f) and not TEST.search(f)]
    if not fixes or not calculation or tests:
        return []
    return ["✖ a fix changes calculation code without adding or changing a test (QA-REL-01):",
            *[f"    fix: {s}" for s in fixes],
            *[f"    changed: {f}" for f in calculation[:10]],
            *([f"    and {len(calculation) - 10} more"] if len(calculation) > 10 else []),
            "  Add the regression test that failed before the fix (the fyniq-bug-fix skill, §4), in the same pull"
            " request."]


def git(*args: str) -> str:
    return subprocess.run(["git", *args], check=True, capture_output=True, text=True).stdout


def range_of(rev_range: str) -> tuple[list[str], list[str]]:
    """The subjects of the range's commits, merges left out, and the files those commits change."""
    subjects = [s for s in git("log", "--no-merges", "--format=%s", rev_range).split("\n") if s]
    files = sorted({f for f in git("log", "--no-merges", "--name-only", "--format=", rev_range).split("\n") if f})
    return subjects, files


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("range", help="the commits to check, such as origin/main..HEAD")
    ap.add_argument("--title", default="", help="the pull request's title, which the squash merge takes")
    args = ap.parse_args()
    subjects, files = range_of(args.range)
    problems = judge(subjects, files, args.title)
    if problems:
        print("\n".join(problems))
        return 1
    print(f"✔ release policy: {len(subjects)} commit(s); no fix changes calculation code without a test")
    return 0


if __name__ == "__main__":
    sys.exit(main())
