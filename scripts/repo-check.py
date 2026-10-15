#!/usr/bin/env python3
"""CTL-CHG-01: protected branches and two-person review, checked from GitHub's API with a read-only token
(the `controls` workflow, daily and on every push to main).

  - main is protected and requires the merge gates (the required checks below);
  - every pull request merged into main in the last few days was approved, before it merged, by someone
    other than its author. An approval by the auto-approval workflow is reported as such: it stands for
    the founders' pre-production waiver (AGENTS.md) and ends before the first production deploy.

The report goes to .data/controls/repo-settings.json (the evidence). Needs `gh` and GH_TOKEN.
"""

from __future__ import annotations

import datetime as dt
import json
import os
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
REPO = os.environ.get("REPO", "cognizhi/fyniq")
REQUIRED = ["Conventional Commits (REL-01)", "Design tokens and configuration", "Go, web and checks (make test)",
            "Security scans (make scan)", "Images and local mode (make images, QA-LOCAL-01)"]
BOT = "github-actions[bot]"
WINDOW_DAYS = int(os.environ.get("WINDOW_DAYS", "3"))
# The audit starts after the bootstrap: pull requests #2 to #12 were merged by the founder with an
# administrator bypass while branch protection and the auto-approval rule were being set up (12 to
# 15 Oct 2026, #12 at 01:13 UTC); the pull request that added this check records them, and they are
# not re-checked every day.
AUDIT_FROM = dt.datetime(2026, 10, 15, 1, 14, tzinfo=dt.UTC)


def api(path: str) -> object:
    p = subprocess.run(["gh", "api", "-H", "Accept: application/vnd.github+json", path], capture_output=True, text=True)
    if p.returncode != 0:
        raise SystemExit(f"✖ gh api {path}: {p.stderr.strip()}")
    return json.loads(p.stdout)


# req: CTL-CHG-01
def main() -> int:
    problems, notes = [], []
    branch = api(f"repos/{REPO}/branches/main")
    checks = (branch.get("protection") or {}).get("required_status_checks") or {}
    contexts = checks.get("contexts") or []
    if not branch.get("protected"):
        problems.append("main is not protected (CTL-CHG-01): switch branch protection on")
    for c in REQUIRED:
        if c not in contexts:
            problems.append(f"main does not require the check {c!r}")
    since = max(dt.datetime.now(dt.UTC) - dt.timedelta(days=WINDOW_DAYS), AUDIT_FROM)
    audited = []
    for pr in api(f"repos/{REPO}/pulls?state=closed&base=main&sort=updated&direction=desc&per_page=50"):
        if not pr.get("merged_at"):
            continue
        merged = dt.datetime.fromisoformat(pr["merged_at"].replace("Z", "+00:00"))
        if merged < since:
            continue
        author = pr["user"]["login"]
        approvers = sorted({r["user"]["login"] for r in api(f"repos/{REPO}/pulls/{pr['number']}/reviews")
                            if r["state"] == "APPROVED" and r["user"]["login"] != author
                            and dt.datetime.fromisoformat(r["submitted_at"].replace("Z", "+00:00")) <= merged})
        audited.append({"number": pr["number"], "title": pr["title"], "author": author, "merged_at": pr["merged_at"],
                        "approved_by": approvers})
        if not approvers:
            problems.append(f"#{pr['number']} merged without an approval by someone other than its author ({author})")
        elif approvers == [BOT]:
            notes.append(f"#{pr['number']} was approved by the auto-approval workflow (pre-production waiver)")
    report = {"control": "CTL-CHG-01", "checked_at": dt.datetime.now(dt.UTC).isoformat(timespec="seconds"), "repository": REPO,
              "protected": branch.get("protected"), "required_checks": contexts, "window_days": WINDOW_DAYS,
              "merges": audited, "notes": notes, "problems": problems}
    out = ROOT / ".data" / "controls"
    out.mkdir(parents=True, exist_ok=True)
    (out / "repo-settings.json").write_text(json.dumps(report, indent=1))
    for n in notes:
        print(f"  note: {n}")
    if problems:
        print("✖ CTL-CHG-01:\n  " + "\n  ".join(problems))
        return 1
    print(f"✔ CTL-CHG-01: main is protected with {len(contexts)} required checks; {len(audited)} merge(s) in "
          f"{WINDOW_DAYS} days, each approved by someone other than its author")
    return 0


if __name__ == "__main__":
    sys.exit(main())
