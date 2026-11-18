#!/usr/bin/env python3
"""Requirement traceability (PRD-001 QA-TRACE-01; p1-launch-readiness task 1.2, design D2).

Reads the requirement tables of PRD-001 and PRD-002 (through scripts/prdspec.py's extractor) and the `req:` tags of the
tests, and lists the requirements no test references.

    python3 scripts/trace_requirements.py report              # the summary and .data/trace/report.json (make trace)
    python3 scripts/trace_requirements.py gate --phase launch # exit 1 while a launch requirement is untested (make trace-gate)
    python3 scripts/trace_requirements.py gate --phase P1     # the phase gate: every M row of P1

A row's priority is M, S or C; a conditional one ("M before GST registration; else S") counts as M, and a table with no
priority column (the task types) as M. Its Phase cell may name several phases ("P1 (files) / P2 (APIs)"), each counted,
or "As FR-AR-12", that row's. A row the script cannot parse, or whose text records a phase move ("(rev 0.7: P1") its
Phase cell does not show, fails it (G4-001). The PRD-001 payroll rows Nemura runs take the phase PRD-002's delivery map
(NM-PAY-07) gives them. INV-* rows count where QA-ENG-05's attack tests tag them, and CTL-* rows where the control
registry (compliance/controls/) runs a check for them. S and C rows are reported and never block.

Tags: `// req: <anchor>` in Go and TypeScript tests, `# req:` in Python and `-- req:` in SQL, in test files only. A tag
names an anchor (FR-GL-08, C.2 #32, RND-FX, sec.create_task), or a capability and a requirement's title, with a
scenario's after it (`governance/company-secretary FR-CS-05 Documents drafted`). Standard library only.
"""
import argparse
import collections
import json
import pathlib
import re
import subprocess
import sys

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))
import prdspec  # noqa: E402

ROOT = prdspec.ROOT
REPORT = ROOT / ".data/trace/report.json"
SUBSET = ROOT / "docs/prd/plans/launch-subset.json"
EXCEPTIONS = ROOT / "docs/prd/plans/launch-exceptions.json"
CONTROLS = ROOT / "compliance/controls"
PRD2 = ROOT / "docs/prd/prd-002-nemura-people.md"

TAG = re.compile(r"(?://|#|--)\s*req:\s*(.+?)\s*$")
# The test files tags count in (design D2).
TEST_FILE = re.compile(r"(_test\.go|\.test\.tsx?|^apps/web/e2e/.+\.spec\.ts|^apps/web/journeys/.+\.spec\.ts|^apps/web/visual/.+\.spec\.ts|\.stories\.tsx|"
                       r"^oracle/(?:.+/)?test_[^/]+\.py|^test/.+\.py|^scripts/test_[^/]+\.py|^db/.+_test\.sql)$")
PHASE = re.compile(r"\bP[0-3]\b")
MOVE = re.compile(r"\(rev 0\.\d+: (P[0-3])\b")
CAPABILITY = re.compile(r"^((?:[a-z0-9-]+/)+[a-z0-9-]+):? (.+)$")


class TraceError(Exception):
    """A row the extractor cannot parse, or a recorded phase move its Phase cell does not show (G4-001)."""


def priority(item):
    """M, S or C: a conditional priority counts as M, a table with no priority column as M."""
    if "Pri" not in item["header"]:
        return "M"
    cell = (item["pri"] or "").strip()
    tokens = re.findall(r"\b[MSC]\b", cell)
    if not tokens:
        raise TraceError(f"{item['doc']} {item['anchor']} (line {item['line']}): the priority {cell!r} is not M, S or C")
    return "M" if "M" in tokens else tokens[0]


def phases(item, anchors, seen=()):
    """The phases a row's Phase cell names; "As FR-AR-12" is that row's."""
    cell = (item["phase"] or "").strip()
    m = re.match(r"^As (\S+)$", cell)
    if m:
        ref = anchors.get(m.group(1))
        if ref is None or ref["anchor"] in seen:
            raise TraceError(f"{item['doc']} {item['anchor']} (line {item['line']}): its phase {cell!r} names no row")
        return phases(ref, anchors, seen + (item["anchor"],))
    found = set(PHASE.findall(cell))
    if not found:
        raise TraceError(f"{item['doc']} {item['anchor']} (line {item['line']}): the Phase cell {cell!r} names no phase")
    return found


def delivery_map(text=None):
    """The PRD-001 payroll rows Nemura runs, and their phases in Nemura (PRD-002 NM-PAY-07's table)."""
    text = PRD2.read_text(encoding="utf-8") if text is None else text
    out = collections.defaultdict(set)
    start = text.find("**Delivery map (NM-PAY-07)**")
    if start < 0:
        return out
    for line in text[start:].split("\n")[2:]:
        if not line.startswith("|"):
            if out:
                break
            continue
        cells = prdspec.split_cells(line)
        if len(cells) < 4 or set(cells[0]) <= set("-: "):
            continue
        for rid in re.findall(r"FR-PAY-\d+", cells[0]):
            out[rid] |= set(PHASE.findall(cells[3]))
    return out


def rows(sources=None, prd2=None):
    """Every requirement row of both PRDs: its anchor, document, priority, phases and line. Raises TraceError on a row
    it cannot parse."""
    sources = prdspec.load_sources() if sources is None else sources
    nemura = delivery_map(prd2)
    out = {}
    for src in sources:
        for a in src.anchors.values():
            if a["kind"] != "requirement" or a["anchor"] in src.excluded or "Phase" not in a["header"]:
                continue
            text = " ".join(c for h, c in zip(a["header"], a["cells"]) if h != "Phase")
            ph = phases(a, src.anchors)
            for moved in MOVE.findall(text):
                if moved not in ph:
                    raise TraceError(f"{a['doc']} {a['anchor']} (line {a['line']}): its text records a move to {moved}, "
                                     f"and its Phase cell says {a['phase']!r} (G4-001)")
            doc = a["doc"]
            if a["anchor"] in nemura:
                ph, doc = nemura[a["anchor"]] or ph, "PRD-002 (NM-PAY-07)"
            out[a["anchor"]] = dict(anchor=a["anchor"], doc=doc, pri=priority(a), phases=sorted(ph), line=a["line"])
    return out


def test_files():
    files = subprocess.run(["git", "ls-files"], cwd=ROOT, capture_output=True, text=True, check=True).stdout.split("\n")
    return [f for f in files if f and TEST_FILE.search(f)]


def tags(files=None, read=None):
    """Each tag's text, with the files that carry it."""
    files = test_files() if files is None else files
    read = read or (lambda f: (ROOT / f).read_text(encoding="utf-8", errors="replace"))
    out = collections.defaultdict(set)
    for f in files:
        for line in read(f).split("\n"):
            m = TAG.search(line)
            if m:
                out[m.group(1).rstrip("`*").strip()].add(f)
    return out


def covered(tagged, sources=None):
    """What the tags cover: anchors, and capability-and-title labels ("cap title")."""
    sources = prdspec.load_sources() if sources is None else sources
    anchors, titled = set(), set()
    for text in tagged:
        parts = [p.strip() for p in text.split(",")]
        if len(parts) > 1 and all(prdspec.anchor_at_start(p, sources) == p for p in parts):
            anchors.update(parts)
            continue
        m = CAPABILITY.match(text)
        rest = m.group(2) if m else text
        a = prdspec.anchor_at_start(rest, sources)
        if a:
            anchors.add(a)
        if m:
            titled.add(f"{m.group(1)} {m.group(2)}")
    return anchors, titled


def controls():
    """Each control's checks' statuses, from the control registry."""
    out = {}
    for p in sorted(CONTROLS.glob("*.yaml")):
        text = p.read_text(encoding="utf-8")
        out[p.stem] = re.findall(r"^\s+status: (\w+)", text, re.M)
    return out


def is_covered(label, anchors, titled, ctl, sources):
    """Whether a requirement's label (an anchor, or "capability: title") is proved: tagged, or a CTL-* row with a
    running check in the control registry."""
    m = re.match(r"^((?:[a-z0-9-]+/)+[a-z0-9-]+): (.+)$", label)
    head = m.group(2) if m else label
    a = prdspec.anchor_at_start(head, sources) or (label if not m else None)
    if a and a.startswith("CTL-"):
        return "running" in ctl.get(a, [])
    if a and a in anchors:
        return True
    if m:
        key = f"{m.group(1)} {m.group(2)}"
        return any(t == key or t.startswith(key + " ") for t in titled)
    return False


def phase_gate(phase, req_rows, anchors, titled, ctl, sources):
    """The rows of a phase no test references: the M ones block, S and C are reported. INV-* rows are QA-ENG-05's."""
    out = {"blocking": [], "reported": []}
    for r in sorted(req_rows.values(), key=lambda r: (r["doc"], r["anchor"])):
        if phase not in r["phases"] or r["anchor"].startswith("INV-"):
            continue
        if not is_covered(r["anchor"], anchors, titled, ctl, sources):
            out["blocking" if r["pri"] == "M" else "reported"].append(dict(r))
    return out


def launch_gate(subset, exceptions, req_rows, anchors, titled, ctl, sources):
    """The launch subset's requirements and scenarios no test references and no exception covers, by change. A PRD row
    of priority M, a check of Appendix C or an acceptance scenario blocks; an S or C row, or a requirement the specs
    draw from the PRD's prose with no ID ("capability: title"), is reported, as QA-TRACE-01 gates the PRD's rows."""
    excepted = {x["id"]: x for x in exceptions.get("exceptions", [])}
    out = {"blocking": collections.defaultdict(list), "reported": collections.defaultdict(list),
           "excepted": collections.defaultdict(list), "covered": 0}
    for c in subset["changes"]:
        for label in c["requirements"] + c["scenarios"]:
            if label in excepted:
                out["excepted"][c["id"]].append(dict(id=label, kind=excepted[label]["kind"]))
                continue
            if is_covered(label, anchors, titled, ctl, sources):
                out["covered"] += 1
                continue
            row = req_rows.get(label)
            prose = re.match(r"^(?:[a-z0-9-]+/)+[a-z0-9-]+: ", label) and not prdspec.anchor_at_start(label.split(": ", 1)[1], sources)
            (out["reported"] if prose or (row and row["pri"] != "M") else out["blocking"])[c["id"]].append(label)
    return out


def main(argv):
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("command", choices=["report", "gate"])
    ap.add_argument("--phase", default="launch", help="launch, or a phase such as P1")
    args = ap.parse_args(argv)
    sources = prdspec.load_sources()
    try:
        req_rows = rows(sources)
    except TraceError as e:
        print(f"✖ {e}")
        return 1
    tagged = tags()
    anchors, titled = covered(tagged, sources)
    ctl = controls()
    subset = json.loads(SUBSET.read_text(encoding="utf-8"))
    exceptions = json.loads(EXCEPTIONS.read_text(encoding="utf-8"))
    launch = launch_gate(subset, exceptions, req_rows, anchors, titled, ctl, sources)
    p1 = phase_gate("P1", req_rows, anchors, titled, ctl, sources)
    report = {"tags": len(tagged), "rows": len(req_rows),
              "launch": {"covered": launch["covered"], "blocking": launch["blocking"], "reported": launch["reported"],
                         "excepted": launch["excepted"]},
              "P1": {"blocking": [r["anchor"] for r in p1["blocking"]], "reported": [r["anchor"] for r in p1["reported"]]}}
    REPORT.parent.mkdir(parents=True, exist_ok=True)
    REPORT.write_text(json.dumps(report, indent=1, ensure_ascii=False) + "\n", encoding="utf-8")
    blocking = sum(len(v) for v in launch["blocking"].values())
    reported = sum(len(v) for v in launch["reported"].values())
    excepted = sum(len(v) for v in launch["excepted"].values())
    print(f"✔ trace: {len(req_rows)} requirement rows in both PRDs, {len(tagged)} tags; the launch: {launch['covered']} covered, "
          f"{blocking} untested, {reported} reported, {excepted} excepted; P1: {len(p1['blocking'])} untested M rows "
          f"({REPORT.relative_to(ROOT)})")
    if args.command == "report":
        return 0
    if args.phase == "launch":
        if blocking:
            print("✖ the launch gate: untested requirements and scenarios of the launch subset, by change:")
            for change, labels in sorted(launch["blocking"].items()):
                print(f"  {change}: {', '.join(labels)}")
            return 1
        print("✔ the launch gate: every launch requirement is tested or excepted")
        return 0
    gate = phase_gate(args.phase, req_rows, anchors, titled, ctl, sources)
    if gate["blocking"]:
        print(f"✖ the {args.phase} gate: untested M rows: {', '.join(r['anchor'] for r in gate['blocking'])}")
        return 1
    print(f"✔ the {args.phase} gate: every M row is tested")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
