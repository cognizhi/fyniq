#!/usr/bin/env python3
"""The control registry (PRD-001 Appendix F) in compliance/controls/, one file per control.

    python3 scripts/controls.py check      # every Appendix F control has its file and the PRD's text (make check)
    python3 scripts/controls.py sync       # rewrite the PRD's fields in every file from Appendix F
    python3 scripts/controls.py generate   # write the files that are missing (a new control in the PRD)

A file's first fields (section to evidence) are the PRD's row, one JSON-quoted line each, so this script
reads them without a YAML library. `owner` and `checks` are ours: who answers for the control, and how
each frequency is checked here. The Go test compliance/registry_test.go validates the whole file
against compliance/control.schema.json and checks that every make target, job, test and CI job a check
names exists.
"""

from __future__ import annotations

import json
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
PRD = ROOT / "docs" / "prd" / "prd-001-fyniq-platform.md"
DIR = ROOT / "compliance" / "controls"
PRD_FIELDS = ["section", "control", "mapping", "automated_check", "frequency", "evidence"]

# Who answers for each area's controls (founder decision, 15 Oct 2026): roles of PRD-001 §7.1.
OWNERS = {"FIN": "Controller", "AUD": "Owner", "IAM": "Administrator", "CHG": "Owner", "DP": "DPO", "RES": "Owner",
          "AI": "Owner", "API": "Owner", "MCP": "Owner", "STAT": "Owner"}

# How P0 checks a control today, by frequency: "make <target>", "job <platform job>",
# "test <Go test>" or "ci <CI job name>", with its evidence. Used only to generate a missing file;
# afterwards the file is the record.
RUNNING = {
    ("CTL-FIN-01", "CI"): ("make test", "INV-01 trigger and property tests; CI log"),
    ("CTL-FIN-02", "CI"): ("make test", "grant snapshot and attack suites (QA-ENG-05); CI log"),
    ("CTL-FIN-03", "CI"): ("make test", "INV-03 trigger tests; CI log"),
    ("CTL-FIN-05", "CI"): ("make differential", "differential run; CI artefact shrunk-scenario on failure"),
    ("CTL-FIN-07", "CI"): ("test TestAuthorisationMatrix", "authorisation matrix (QA-SEC-03); CI log"),
    ("CTL-AUD-01", "CI"): ("make test", "AUD-08 same-transaction tests; CI log"),
    ("CTL-AUD-02", "N"): ("job audit.verify", "verification log and heartbeat (jobs heartbeat-check)"),
    ("CTL-IAM-05", "CI"): ("make test", "cross-tenant attack tests and the API's cross-tenant test; CI log"),
    ("CTL-IAM-06", "CI"): ("test TestAuthorisationMatrix", "authorisation matrix; CI log"),
    ("CTL-CHG-01", "CI"): ("ci Auto-approve development changes", "pull request reviews; the approval rule of .github/workflows/auto-approve.yml"),
    ("CTL-CHG-03", "CI"): ("make scan", "CI artefact security-scan-reports (90 days)"),
    ("CTL-CHG-08", "CI"): ("make images", "image sizes and hardening in the CI log; CI artefact image-reports"),
    ("CTL-CHG-09", "CI"): ("make images", "release images without local adapters (CI log); start-up refusal test in make test"),
    ("CTL-CHG-10", "CI"): ("ci Conventional Commits (REL-01)", "commit and title check; CI log"),
    ("CTL-CHG-11", "CI"): ("make scan", "actionlint and zizmor reports; the workflow rules in archtest"),
    ("CTL-DP-02", "CI"): ("test TestMaskedFieldsThroughTheAPI", "masking through the API on a planted capability; CI log"),
    ("CTL-AI-08", "CI"): ("make check", "kb-lint; CI log"),
    ("CTL-API-01", "CI"): ("make test", "contract golden, oasdiff, route check; Spectral in make api-check; CI log"),
}

# When the checks that do not run yet arrive.
INFRA = {"CTL-AUD-03", "CTL-AUD-04", "CTL-IAM-03", "CTL-IAM-04", "CTL-CHG-02", "CTL-CHG-05", "CTL-DP-01", "CTL-DP-07",
         "CTL-RES-01", "CTL-RES-03", "CTL-RES-05"}
IDENTITY = {"CTL-IAM-01", "CTL-IAM-02", "CTL-IAM-07", "CTL-API-02", "CTL-MCP-04"}
RUNNER = {("CTL-FIN-01", "N"), ("CTL-FIN-02", "N")}


def arrives(cid: str, freq: str) -> str:
    if (cid, freq) in RUNNER:
        return "p0-compliance-as-code (the nightly runner)"
    if cid in INFRA:
        return "p0-infra-opentofu"
    if cid in IDENTITY:
        return "p0-identity-oauth-first-run"
    if cid == "CTL-CHG-04":
        return "p0-infra-opentofu (signing) and P1 (Binary Authorization, SEC-22)"
    if cid == "CTL-CHG-01" and freq == "D":
        return "p0-compliance-as-code (the repository settings check)"
    if cid in ("CTL-CHG-06", "CTL-CHG-09") and freq == "E":
        return "p0-ci-images-release (the release workflow)"
    return "P1"


def cells(line: str) -> list[str]:
    return [c.strip() for c in line.strip().strip("|").split("|")]


def appendix_f() -> list[dict]:
    text = PRD.read_text()
    start = text.index("## Appendix F")
    end = text.index("\n## Appendix G", start)
    rows, section = [], ""
    for line in text[start:end].splitlines():
        m = re.match(r"^### (F\.[0-9a-z]+) ", line)
        if m:
            section = m[1]
            continue
        if line.startswith("| CTL-"):
            c = cells(line)
            if len(c) != 6:
                raise SystemExit(f"✖ Appendix F row {c[0]} has {len(c)} cells, not 6")
            rows.append({"id": c[0], "section": section, "control": c[1], "mapping": c[2], "automated_check": c[3],
                         "frequency": c[4], "evidence": c[5]})
    if len(rows) < 60:
        raise SystemExit(f"✖ read only {len(rows)} controls from Appendix F")
    return rows


def frequencies(cell: str) -> list[str]:
    """The frequency codes of a Freq. cell, without its notes: CI, D, N, W, M, Q, S (semi-annual), E."""
    depth, plain = 0, ""
    for ch in cell:
        depth += ch == "("
        if depth == 0:
            plain += ch
        depth -= ch == ")"
    out = []
    for token in (t.strip() for t in plain.split(",")):
        code = {"semi-annual": "S"}.get(token, token)
        if code not in ("CI", "D", "N", "W", "M", "Q", "S", "E"):
            raise SystemExit(f"✖ unknown frequency {token!r} in {cell!r}")
        if code not in out:
            out.append(code)
    return out


def area(cid: str) -> str:
    return cid.split("-")[1]


def render(row: dict) -> str:
    cid = row["id"]
    lines = [f"# {cid}: PRD-001 Appendix F ({row['section']}). The fields from section to evidence are the PRD's row,",
             "# kept equal to it by `make check` (scripts/controls.py sync rewrites them); owner and checks are ours.",
             f"id: {cid}"]
    lines += [f"{k}: {json.dumps(row[k], ensure_ascii=False)}" for k in PRD_FIELDS]
    lines += [f"owner: {OWNERS[area(cid)]}", "checks:"]
    for f in frequencies(row["frequency"]):
        if (cid, f) in RUNNING:
            how, evidence = RUNNING[(cid, f)]
            lines += [f"  - frequency: {f}", "    status: running", f"    how: {json.dumps(how)}", f"    evidence: {json.dumps(evidence)}"]
        else:
            lines += [f"  - frequency: {f}", "    status: planned", f"    arrives: {json.dumps(arrives(cid, f))}"]
    return "\n".join(lines) + "\n"


def prd_lines(path: Path) -> dict[str, str]:
    out = {}
    for line in path.read_text().splitlines():
        m = re.match(r"^([a-z_]+): (\".*\")$", line)
        if m and m[1] in PRD_FIELDS:
            out[m[1]] = json.loads(m[2])
    return out


def main() -> int:
    cmd = sys.argv[1] if len(sys.argv) > 1 else "check"
    rows = appendix_f()
    ids = {r["id"] for r in rows}
    DIR.mkdir(parents=True, exist_ok=True)
    if cmd == "generate":
        n = 0
        for r in rows:
            p = DIR / f"{r['id']}.yaml"
            if not p.exists():
                p.write_text(render(r))
                n += 1
        print(f"✔ wrote {n} new control file(s) in compliance/controls/")
        return 0
    problems = []
    for r in rows:
        p = DIR / f"{r['id']}.yaml"
        if not p.exists():
            problems.append(f"{r['id']} has no file: run python3 scripts/controls.py generate")
            continue
        have = prd_lines(p)
        for k in PRD_FIELDS:
            if have.get(k) != r[k]:
                if cmd == "sync":
                    text = p.read_text()
                    text = re.sub(rf"(?m)^{k}: \".*\"$", lambda _: f"{k}: {json.dumps(r[k], ensure_ascii=False)}", text)
                    p.write_text(text)
                else:
                    problems.append(f"{r['id']}: {k} differs from Appendix F: run python3 scripts/controls.py sync")
    for p in sorted(DIR.glob("*.yaml")):
        if p.stem not in ids:
            problems.append(f"{p.name}: Appendix F has no control {p.stem}; withdrawn controls are marked in the PRD, then removed here")
    if problems:
        print("✖ control registry:\n  " + "\n  ".join(problems))
        return 1
    print(f"✔ control registry: {len(rows)} controls of Appendix F, each with its file and the PRD's text")
    return 0


if __name__ == "__main__":
    sys.exit(main())
