#!/usr/bin/env python3
"""The evidence of a release (PRD-001 CMP-01, §14.1 item 3; p1-compliance-center design D3).

    python3 scripts/release-evidence.py publish vX.Y.Z   # .github/workflows/release-evidence.yml (GH_TOKEN, REPO)
    python3 scripts/release-evidence.py self-test         # the manifest from a fixture run (part of make check)

`publish` waits for the runs of each workflow in compliance/release-assets.json on the release's commit (pushed to
main), and refuses unless each succeeded: no release carries the evidence of a failing gate. It attaches each run's
artefacts to the GitHub release as <name>.zip and its logs as <logs>.zip, with `controls-evidence.json`: each check of
the registry that runs in CI, and the asset that holds its evidence. The Compliance Center links a check to the same
asset (compliance.ReleaseAsset reads the same table), so the release the platform runs shows its CI checks as passed.
"""

from __future__ import annotations

import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
CONTROLS = ROOT / "compliance" / "controls"
TABLE = ROOT / "compliance" / "release-assets.json"
WORKFLOWS = ROOT / ".github" / "workflows"
CI_KINDS = {"make", "test", "ci"}
WAIT = 4 * 3600  # CI on main takes about an hour; the job's own timeout is longer
TAG = re.compile(r"^v[0-9]+\.[0-9]+\.[0-9]+$")


def table() -> dict:
    return json.loads(TABLE.read_text())


def ci_checks(controls: Path = CONTROLS) -> list[dict]:
    """Every check of the registry that runs in CI: running, by a make target, a Go test or a CI job."""
    out = []
    for f in sorted(controls.glob("CTL-*.yaml")):
        text = f.read_text()
        body = text[text.index("checks:\n") + len("checks:\n"):]
        cur = None
        for line in body.splitlines():
            m = re.match(r"  - frequency: (\S+)$", line)
            if m:
                cur = {"control": f.stem, "frequency": m.group(1)}
                out.append(cur)
                continue
            m = re.match(r"    (status|how): (.*)$", line)
            if m and cur is not None:
                v = m.group(2)
                cur[m.group(1)] = json.loads(v) if v.startswith('"') else v
    return [c for c in out if c.get("status") == "running" and c.get("how", "").split(" ")[0] in CI_KINDS]


def asset(t: dict, how: str) -> str:
    """The release asset that holds a CI check's evidence, as compliance.ReleaseAssets.Asset names it."""
    name = t["by_how"].get(how) or t["by_kind"].get(how.split(" ")[0]) or t["otherwise"]
    return name + ".zip"


def manifest(t: dict, tag: str, commit: str, runs: dict, present: set[str], checks: list[dict]) -> dict:
    """The release's manifest, refusing a run that did not succeed and an asset the release would not hold."""
    problems = []
    for wf in t["workflows"]:
        run = runs.get(wf)
        if run is None:
            problems.append(f"{wf}: no run on {commit}")
        elif run.get("conclusion") != "success":
            problems.append(f"{wf}: its run on {commit} is {run.get('conclusion') or run.get('status')} ({run.get('html_url', '')})")
    entries = []
    for c in checks:
        a = asset(t, c["how"])
        if a not in present:
            problems.append(f"{c['control']} ({c['frequency']}) {c['how']}: the release holds no {a}")
        entries.append({"control": c["control"], "frequency": c["frequency"], "how": c["how"], "asset": a})
    if problems:
        raise SystemExit("✖ release evidence:\n  " + "\n  ".join(problems))
    return {"format": "fyniq-controls-evidence@1", "release": tag, "commit": commit,
            "runs": {wf: {"id": r["id"], "url": r.get("html_url", ""), "conclusion": r["conclusion"]} for wf, r in runs.items()},
            "checks": entries}


def gh(*args: str, out: Path | None = None) -> str:
    if out is not None:
        with out.open("wb") as f:
            subprocess.run(["gh", *args], check=True, stdout=f)
        return ""
    return subprocess.run(["gh", *args], check=True, capture_output=True, text=True).stdout


def publish(tag: str) -> None:
    if not TAG.match(tag):
        raise SystemExit(f"✖ release evidence: {tag!r} is not a release tag (vX.Y.Z)")
    repo, t = os.environ["REPO"], table()
    commit = gh("api", f"repos/{repo}/commits/{tag}", "--jq", ".sha").strip()
    runs, deadline = {}, time.time() + WAIT
    for wf in t["workflows"]:
        while True:
            found = json.loads(gh("api", f"repos/{repo}/actions/workflows/{wf}/runs?head_sha={commit}&event=push&branch=main&per_page=1"))
            run = (found.get("workflow_runs") or [None])[0]
            if run and run["status"] == "completed":
                runs[wf] = run
                break
            if time.time() > deadline:
                raise SystemExit(f"✖ release evidence: {wf} did not finish on {commit} within {WAIT // 3600} hours")
            time.sleep(60)
    manifest(t, tag, commit, runs, set(), [])  # refuse a failed run before downloading anything
    with tempfile.TemporaryDirectory() as tmp:
        work, out = Path(tmp) / "work", Path(tmp) / "out"
        out.mkdir()
        for wf, spec in t["workflows"].items():
            for name in spec["artefacts"]:
                gh("run", "download", str(runs[wf]["id"]), "--repo", repo, "--name", name, "--dir", str(work / name))
                shutil.make_archive(str(out / name), "zip", work / name)
            gh("api", f"repos/{repo}/actions/runs/{runs[wf]['id']}/logs", out=out / f"{spec['logs']}.zip")
        m = manifest(t, tag, commit, runs, {p.name for p in out.iterdir()}, ci_checks())
        (out / "controls-evidence.json").write_text(json.dumps(m, indent=2) + "\n")
        gh("release", "upload", tag, "--repo", repo, *sorted(str(p) for p in out.iterdir()))
    print(f"✔ {tag}: the evidence of {len(m['checks'])} CI checks attached ({', '.join(sorted(runs))})")


def uploaded(workflow: Path) -> set[str]:
    """The artefacts a workflow uploads."""
    src = workflow.read_text()
    return set(re.findall(r"uses: actions/upload-artifact@\S+[^\n]*\n(?:\s+#[^\n]*\n)?\s+with:\n(?:\s+[a-z-]+:[^\n]*\n)*?\s+name: ([a-z0-9-]+)", src))


def self_test() -> None:
    t, checks = table(), ci_checks()
    if len(checks) < 20:
        raise SystemExit(f"✖ release-evidence self-test: only {len(checks)} CI checks in the registry")
    # Each workflow uploads the artefacts the table names, and every asset a check names is one the release holds.
    for wf, spec in t["workflows"].items():
        missing = set(spec["artefacts"]) - uploaded(WORKFLOWS / wf)
        if missing:
            raise SystemExit(f"✖ release-evidence self-test: {wf} uploads no {sorted(missing)}")
    held = {f"{n}.zip" for s in t["workflows"].values() for n in [*s["artefacts"], s["logs"]]}
    named = {asset(t, c["how"]) for c in checks} | {f"{n}.zip" for n in [*t["by_how"].values(), *t["by_kind"].values(), t["otherwise"]]}
    if named - held:
        raise SystemExit(f"✖ release-evidence self-test: assets the release would not hold: {sorted(named - held)}")
    # A fixture run: both workflows succeeded and every asset is there.
    runs = {wf: {"id": i + 1, "status": "completed", "conclusion": "success", "html_url": f"https://example.test/runs/{i + 1}"}
            for i, wf in enumerate(t["workflows"])}
    m = manifest(t, "v1.2.0", "abc123", runs, held, checks)
    by = {(c["control"], c["frequency"], c["how"]): c["asset"] for c in m["checks"]}
    for key, want in {("CTL-CHG-03", "CI", "make scan"): "security-scan-reports.zip",
                      ("CTL-FIN-05", "CI", "make differential"): "ci-logs.zip",
                      ("CTL-IAM-06", "CI", "test TestAuthorisationMatrix"): "test-report.zip",
                      ("CTL-CHG-01", "D", "ci Repository settings and merge audit (CTL-CHG-01)"): "control-ctl-chg-01.zip"}.items():
        if by.get(key) != want:
            raise SystemExit(f"✖ release-evidence self-test: {key} has {by.get(key)}, want {want}")
    if len(m["checks"]) != len(checks) or m["release"] != "v1.2.0":
        raise SystemExit("✖ release-evidence self-test: the manifest lost a check")
    # A failed run, a run that never came and a missing artefact are each refused.
    for why, bad_runs, bad_held in [
        ("a failed CI run", {**runs, "ci.yml": {**runs["ci.yml"], "conclusion": "failure"}}, held),
        ("a cancelled controls run", {**runs, "controls.yml": {**runs["controls.yml"], "conclusion": "cancelled"}}, held),
        ("no CI run", {k: v for k, v in runs.items() if k != "ci.yml"}, held),
        ("no image reports", runs, held - {"image-reports.zip"}),
    ]:
        try:
            manifest(t, "v1.2.0", "abc123", bad_runs, bad_held, checks)
        except SystemExit:
            continue
        raise SystemExit(f"✖ release-evidence self-test: {why} was accepted")
    print(f"✔ release evidence: {len(checks)} CI checks, each with its release asset")


if __name__ == "__main__":
    if sys.argv[1:2] == ["self-test"]:
        self_test()
    elif sys.argv[1:2] == ["publish"] and len(sys.argv) == 3:
        publish(sys.argv[2])
    else:
        raise SystemExit(__doc__)
