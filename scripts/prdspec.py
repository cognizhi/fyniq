#!/usr/bin/env python3
"""PRD → OpenSpec tooling (fyniq-openspec skill): extract, map, packet, coverage, stale, stamp.

A PRD stays the source of truth; `openspec/specs/` is derived from it. The conversion itself is
done by an agent following the fyniq-openspec skill. This script does the deterministic parts:
it extracts every traceable anchor from a PRD (requirement IDs, named policies, task types,
numbered checks, acceptance scenarios, and the ID-less sections that must be cited), maps each
anchor to its capability, and checks that the specs cover every anchor exactly once, in the
right capability, with the right Source line, and that no PRD row changed since it was
converted. Standard library only.

    python3 scripts/prdspec.py extract [--json]            # every anchor and its capability
    python3 scripts/prdspec.py map                         # anchors per capability; exit 1 if unmapped
    python3 scripts/prdspec.py packet <capability>         # the PRD text an agent converts
    python3 scripts/prdspec.py show <anchor>               # one anchor's PRD row
    python3 scripts/prdspec.py coverage [--summary]        # exit 1 on any coverage error
    python3 scripts/prdspec.py stale [--summary]           # exit 1 if a PRD row changed since stamp
    python3 scripts/prdspec.py stamp (--all | <anchor>…)   # record the rows the specs now match

Every PRD has a source file `openspec/sources/<prd>.json` (capability rules, cited sections,
exclusions) and a lock file `openspec/sources/<prd>.lock.json` that only `stamp` writes.
Add `--source <file>` before the command to work on one PRD only.
"""
import hashlib
import json
import pathlib
import re
import signal
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
OPENSPEC = ROOT / "openspec"
SOURCES = OPENSPEC / "sources"
SPECS = OPENSPEC / "specs"
CHANGES = OPENSPEC / "changes"

ID = r"[A-Z][A-Z0-9]*(?:-[A-Z0-9]+)*-\d+[a-z]?"     # FR-GL-08, NFR-PERF-01, INV-01, G4-001
POLICY = r"(?:RND|ALLOC)(?:-[A-Z]+)+"                # RND-FX, ALLOC-EVEN
TASK = r"[a-z]+\.[a-z_]+"                            # bank.categorise_lines
CHECK = r"[A-Z]\.\d+ #\d+"                           # C.2 #44
HEADING_RE = re.compile(r"^(#{2,4}) (.+?)\s*$")
FENCE_RE = re.compile(r"^\s*(```|~~~)")
# "8.1", "8.18.1", "15." (numbered); "G.4", "F.7a" (appendix subsections); "Appendix E" (a bare
# letter only after "Appendix", so a heading such as "A note on …" is not read as section A).
SECTION_NO_RE = re.compile(
    r"^(?:Appendix ([A-Z])|(\d+(?:\.\d+)*|[A-Z]\.\d+[a-z]?(?:\.\d+)*))\.?(?=\s|$)")
SEPARATOR_RE = re.compile(r"^\|[\s:|-]+\|\s*$")
REQ_HEADER_RE = re.compile(r"^### Requirement: (.+?)\s*$")
SCN_HEADER_RE = re.compile(r"^#### Scenario: (.+?)\s*$")
DELTA_RE = re.compile(r"^## (ADDED|MODIFIED|REMOVED|RENAMED) Requirements\s*$")
SOURCE_LINE_RE = re.compile(r"^Source: (\S+) §(\S+?)(?=\s|$|·)")


class Source:
    """One PRD and its conversion rules (openspec/sources/<prd>.json)."""

    def __init__(self, path):
        self.path = path
        cfg = json.loads(path.read_text(encoding="utf-8"))
        self.doc = cfg["doc"]
        self.prd = ROOT / cfg["prd"]
        self.rules = cfg.get("rules", [])
        self.sections = cfg.get("sections", {})
        self.excluded = cfg.get("excluded", {})
        self.scenario_patterns = [re.compile(p) for p in cfg.get("scenario_patterns", [])]
        self.check_sections = set(cfg.get("check_sections", []))
        self.task_sections = set(cfg.get("task_sections", []))
        self.lock_path = path.with_name(path.stem + ".lock.json")
        self.anchors = {}
        self._extract()

    # --- extraction -----------------------------------------------------------------------

    def _row_anchor(self, first, section):
        f = first.strip().strip("*`").strip()
        if section in self.check_sections and f.isdigit():
            return f"{section} #{f}", "requirement"
        if any(p.fullmatch(f) for p in self.scenario_patterns):
            return f, "scenario"
        if re.fullmatch(POLICY, f):
            return f, "requirement"
        if section in self.task_sections:
            m = re.match(rf"({TASK})(?:\s*\(.*\))?$", f.replace("`", ""))   # "kb.propose_page (P2, rev 0.4)"
            if m:
                return m.group(1), "requirement"
        if re.fullmatch(ID, f):
            return f, "requirement"
        return None, None

    def _extract(self):
        lines = self.prd.read_text(encoding="utf-8").split("\n")
        fenced = fence_mask(lines)
        headings = []                                   # (line index, level, section, title)
        for i, line in enumerate(lines):
            m = HEADING_RE.match(line)
            if m and not fenced[i]:
                title = m.group(2)
                sm = SECTION_NO_RE.match(title)
                headings.append((i, len(m.group(1)), (sm.group(1) or sm.group(2)) if sm else None,
                                 title))
        section, title, header, level = None, None, None, 0
        hpos = 0
        anchor_lines = set()
        for i, line in enumerate(lines):
            while hpos < len(headings) and headings[hpos][0] <= i:
                _, lvl, sec, ttl = headings[hpos]
                if sec:
                    section, title, level = sec, ttl, lvl
                elif lvl <= level:                      # e.g. "## Revision history" ends §J.4
                    section, title, level = None, None, 0
                hpos += 1
            if fenced[i] or not line.startswith("|"):
                header = None
                continue
            if SEPARATOR_RE.match(line):
                continue
            cells = split_cells(line)
            if i + 1 < len(lines) and SEPARATOR_RE.match(lines[i + 1]):
                header = cells
                continue
            if header is None:
                continue
            anchor, kind = self._row_anchor(cells[0], section)
            if not anchor:
                continue
            fields = dict(zip(header, cells))
            item = {
                "anchor": anchor, "kind": kind, "doc": self.doc, "section": section,
                "section_title": title, "line": i + 1, "header": header, "cells": cells,
                "pri": fields.get("Pri"), "phase": fields.get("Phase"),
                "trace": find_anchors(fields.get("Trace", ""), self.scenario_patterns),
                "hash": digest(line.rstrip()),
            }
            if anchor in self.anchors:
                prev = self.anchors[anchor]["line"]
                raise SystemExit(f"✖ {self.doc}: {anchor} is defined twice (lines {prev} and {i + 1})")
            self.anchors[anchor] = item
            anchor_lines.add(i)
        for sec in self.sections:
            text = section_text(lines, headings, sec, anchor_lines)
            if text is None:
                raise SystemExit(f"✖ {self.path.name}: section {sec} not found in {self.doc}")
            start = next(h for h in headings if h[2] == sec)
            self.anchors[f"§{sec}"] = {
                "anchor": f"§{sec}", "kind": "section", "doc": self.doc, "section": sec,
                "section_title": start[3], "line": start[0] + 1, "text": text,
                "pri": None, "phase": None, "trace": [], "hash": digest(text),
            }
        for item in self.anchors.values():
            item["capability"] = self._capability(item)

    def _capability(self, item):
        if item["kind"] == "section":
            return self.sections[item["section"]]
        for rule in self.rules:
            if "match" in rule and not re.fullmatch(rule["match"], item["anchor"]):
                continue
            if "section" in rule and rule["section"] != item["section"]:
                continue
            return rule["capability"]
        if item["kind"] == "scenario":
            for target in item["trace"]:
                t = self.anchors.get(target)
                if t and t["kind"] == "requirement" and target not in self.excluded:
                    item["target"] = target
                    return self._capability(t)
        return None

    def source_line(self, item):
        parts = [f"Source: {self.doc} §{item['section']}"]
        if item.get("pri"):
            parts.append(f"Priority {item['pri']}")
        if item.get("phase"):
            parts.append(f"Phase {item['phase']}")
        return " · ".join(parts)

    def lock(self):
        if self.lock_path.exists():
            return json.loads(self.lock_path.read_text(encoding="utf-8"))
        return {}

    def write_lock(self, lock):
        text = json.dumps(dict(sorted(lock.items())), indent=1, ensure_ascii=False) + "\n"
        self.lock_path.write_text(text, encoding="utf-8")


def fence_mask(lines):
    """True for every line inside (or opening/closing) a fenced code block."""
    mask, fence = [], None
    for line in lines:
        m = FENCE_RE.match(line)
        if fence is None and m:
            fence = m.group(1)
            mask.append(True)
        elif fence is not None:
            mask.append(True)
            if line.strip().startswith(fence):
                fence = None
        else:
            mask.append(False)
    return mask


def split_cells(line):
    body = line.strip()
    body = body[1:] if body.startswith("|") else body
    body = body[:-1] if body.endswith("|") else body
    return [c.strip().replace("\\|", "|") for c in re.split(r"(?<!\\)\|", body)]


def find_anchors(text, scenario_patterns=()):
    extra = "|".join(p.pattern for p in scenario_patterns)
    pattern = rf"(?:{CHECK}|{POLICY}|{ID}{'|' + extra if extra else ''})"
    return re.findall(rf"(?<![\w.-]){pattern}(?![\w-])", text)


def section_text(lines, headings, sec, skip):
    for n, (i, level, s, _) in enumerate(headings):
        if s != sec:
            continue
        end = len(lines)
        for j, lvl, _, _ in headings[n + 1:]:
            if lvl <= level:
                end = j
                break
        body = [lines[k].rstrip() for k in range(i, end) if k not in skip]
        return "\n".join(body).strip()
    return None


def digest(text):
    return hashlib.sha256(text.encode("utf-8")).hexdigest()[:16]


def load_sources(only=None):
    if only:
        return [Source(pathlib.Path(only).resolve())]
    paths = sorted(p for p in SOURCES.glob("*.json") if not p.name.endswith(".lock.json"))
    if not paths:
        raise SystemExit("✖ no PRD source files in openspec/sources/ (see the fyniq-openspec skill)")
    return [Source(p) for p in paths]


# --- reading the specs -----------------------------------------------------------------------

def anchor_at_start(name, sources):
    """The anchor a requirement or scenario name starts with, or None."""
    extra = "|".join(p.pattern for s in sources for p in s.scenario_patterns)
    pattern = rf"^(?:{CHECK}|{POLICY}|{ID}{'|' + extra if extra else ''})(?=\s|$)"
    m = re.match(pattern, name)
    if m:
        return m.group(0)
    # A `word.word` token is an anchor only when it is a known task type, so ordinary names
    # such as "info.version follows the platform" stay unanchored.
    m = re.match(rf"^{TASK}(?=\s|$)", name)
    if m and any(m.group(0) in s.anchors for s in sources):
        return m.group(0)
    return None


def parse_spec(path, capability, sources, where):
    """Yield one record per requirement and per scenario header in a spec or delta file."""
    lines = path.read_text(encoding="utf-8").split("\n")
    delta, req = None, None
    for i, line in enumerate(lines):
        m = DELTA_RE.match(line)
        if m:
            delta = m.group(1)
            continue
        m = REQ_HEADER_RE.match(line)
        if m:
            name = m.group(1)
            block = []
            for nxt in lines[i + 1:]:
                if nxt.startswith("#### ") or nxt.startswith("### ") or nxt.startswith("## "):
                    break
                block.append(nxt)
            req = {"type": "requirement", "name": name, "anchor": anchor_at_start(name, sources),
                   "capability": capability, "file": path, "line": i + 1, "where": where,
                   "delta": delta, "body": block}
            yield req
            continue
        m = SCN_HEADER_RE.match(line)
        if m:
            name = m.group(1)
            yield {"type": "scenario", "name": name, "anchor": anchor_at_start(name, sources),
                   "capability": capability, "file": path, "line": i + 1, "where": where,
                   "delta": delta, "requirement": req["anchor"] if req else None}


def read_specs(sources):
    records = []
    for path in sorted(SPECS.rglob("spec.md")):
        cap = path.parent.relative_to(SPECS).as_posix()
        records += parse_spec(path, cap, sources, "main")
    if CHANGES.exists():
        for change in sorted(p for p in CHANGES.iterdir() if p.is_dir() and p.name != "archive"):
            root = change / "specs"
            for path in sorted(root.rglob("spec.md")) if root.exists() else []:
                cap = path.parent.relative_to(root).as_posix()
                records += parse_spec(path, cap, sources, f"change:{change.name}")
    return records


# --- commands -------------------------------------------------------------------------------

def rel(path):
    return pathlib.Path(path).relative_to(ROOT).as_posix()


def cmd_extract(sources, args):
    items = [dict(a, source=s.doc) for s in sources for a in s.anchors.values()]
    if "--json" in args:
        for item in items:
            item.pop("text", None)
        print(json.dumps(items, indent=1, ensure_ascii=False))
        return 0
    for item in items:
        print(f"{item['doc']}  {item['kind']:<11} {item['anchor']:<24} §{item['section'] or '?':<8} "
              f"{item['capability'] or '— unmapped —'}")
    return 0


def cmd_map(sources, args):
    caps, unmapped = {}, []
    for s in sources:
        for a in s.anchors.values():
            if a["anchor"] in s.excluded:
                continue
            if not a["capability"]:
                unmapped.append(f"{s.doc} {a['anchor']} (§{a['section']}, line {a['line']})")
                continue
            c = caps.setdefault(a["capability"], {"requirement": 0, "scenario": 0, "section": 0})
            c[a["kind"]] += 1
    print(f"{'capability':<44} req  scn  sec")
    for cap in sorted(caps):
        c = caps[cap]
        print(f"{cap:<44} {c['requirement']:>3}  {c['scenario']:>3}  {c['section']:>3}")
    total = {k: sum(c[k] for c in caps.values()) for k in ("requirement", "scenario", "section")}
    print(f"{len(caps)} capabilities · {total['requirement']} requirements · "
          f"{total['scenario']} scenarios · {total['section']} cited sections")
    for u in unmapped:
        print(f"✖ unmapped: {u}")
    return 1 if unmapped else 0


def describe(s, a):
    out = [f"### {a['anchor']}  ({a['kind']}, {s.doc} §{a['section']} {a['section_title']}, "
           f"line {a['line']})"]
    if a["kind"] == "section":
        out += ["", a["text"], ""]
        return out
    if a["kind"] == "requirement":
        out.append(f"Source line: {s.source_line(a)}")
    if a.get("target"):
        out.append(f"Attach as `#### Scenario: {a['anchor']} …` under requirement {a['target']}")
    for h, c in list(zip(a["header"], a["cells"]))[1:]:
        if c and h not in ("Pri", "Phase"):
            out.append(f"- **{h}**: {c}")
    out.append("")
    return out


def cmd_packet(sources, args):
    if not args:
        raise SystemExit("usage: prdspec.py packet <capability>")
    cap = args[0]
    out = [f"# Packet for `{cap}`", "",
           f"Write: openspec/specs/{cap}/spec.md (rules: .claude/skills/fyniq-openspec)", ""]
    found = False
    for kind, label in (("section", "Sections to convert and cite"),
                        ("requirement", "Requirements (one `### Requirement: <anchor> <title>` each)"),
                        ("scenario", "Acceptance scenarios (one `#### Scenario: <anchor> <title>` each)")):
        items = [(s, a) for s in sources for a in s.anchors.values()
                 if a["capability"] == cap and a["kind"] == kind and a["anchor"] not in s.excluded]
        if not items:
            continue
        found = True
        out += [f"## {label}", ""]
        for s, a in sorted(items, key=lambda x: x[1]["line"]):
            out += describe(s, a)
    excluded = [(s, a) for s in sources for a in s.anchors.values()
                if a["capability"] == cap and a["anchor"] in s.excluded]
    if excluded:
        out += ["## Excluded (do not convert)", ""]
        out += [f"- {a['anchor']}: {s.excluded[a['anchor']]}" for s, a in excluded]
    if not found:
        raise SystemExit(f"✖ no anchors map to capability {cap}")
    print("\n".join(out))
    return 0


def cmd_show(sources, args):
    if not args:
        raise SystemExit("usage: prdspec.py show <anchor>")
    for s in sources:
        a = s.anchors.get(args[0]) or s.anchors.get(f"§{args[0].lstrip('§')}")
        if a:
            print("\n".join(describe(s, a)))
            print(f"Capability: {a['capability']}")
            return 0
    raise SystemExit(f"✖ {args[0]} is not an anchor of any PRD source")


def cmd_coverage(sources, args):
    records = read_specs(sources)
    known = {a: (s, item) for s in sources for a, item in s.anchors.items()}
    docs = {s.doc: s for s in sources}
    errors, pending = [], []

    def err(code, msg):
        errors.append((code, msg))

    main = [r for r in records if r["where"] == "main"]
    added = [r for r in records if r["where"] != "main" and r["delta"] in ("ADDED", "MODIFIED")]
    for anchor, (s, a) in known.items():
        if a["kind"] == "section":
            continue
        want = a["kind"]
        hits = [r for r in main if r["anchor"] == anchor and r["type"] == want]
        if anchor in s.excluded:
            for r in hits:
                err("excluded", f"{anchor} is excluded ({s.excluded[anchor]}) but appears in "
                                f"{rel(r['file'])}:{r['line']}")
            continue
        if not hits:
            later = [r for r in added if r["anchor"] == anchor and r["type"] == want]
            if later:
                pending.append(f"{anchor} in {later[0]['where']}")
            else:
                err("missing", f"{anchor} ({s.doc} §{a['section']}) → {a['capability'] or 'unmapped'}")
            continue
        if len(hits) > 1:
            where = ", ".join(f"{rel(r['file'])}:{r['line']}" for r in hits)
            err("duplicate", f"{anchor} appears {len(hits)} times: {where}")
        for r in hits:
            if a["capability"] and r["capability"] != a["capability"]:
                err("misplaced", f"{anchor} is in {r['capability']}, mapped to {a['capability']}")
            if want == "scenario" and a.get("target") and r["requirement"] != a["target"]:
                err("misattached", f"{anchor} at {rel(r['file'])}:{r['line']} is under "
                                   f"{r['requirement'] or 'no anchored requirement'}; "
                                   f"it belongs under {a['target']}")
            if want == "requirement":
                check_source_line(r, s.source_line(a), err)
    for r in main:
        if r["type"] == "requirement" and r["anchor"] is None:
            check_derived_source(r, docs, err)
        if r["anchor"] and r["anchor"] not in known:
            err("unknown", f"{r['anchor']} at {rel(r['file'])}:{r['line']} is not in any PRD source")
    for s in sources:
        for sec, cap in s.sections.items():
            spec = SPECS / cap / "spec.md"
            cited = spec.exists() and re.search(
                rf"{re.escape(s.doc)} §{re.escape(sec)}(?![0-9a-z]|\.\d)", spec.read_text(encoding="utf-8"))
            if not cited:
                err("uncited", f"{s.doc} §{sec} must be cited in openspec/specs/{cap}/spec.md")
    reqs = sum(1 for r in main if r["type"] == "requirement")
    print(f"{reqs} requirements in {len({r['file'] for r in main})} main specs; "
          f"{len(pending)} anchors pending in active changes; {len(errors)} errors")
    summary = "--summary" in args
    for p in pending[:5] if summary else pending:
        print(f"  … pending  {p}")
    if summary and len(pending) > 5:
        print(f"  … {len(pending) - 5} more pending")
    report(errors, summary)
    return 1 if errors else 0


def report(problems, summary=True, sample=5):
    """Print (code, message) problems in full, or as counts per code with the first few of each."""
    problems = list(problems)
    if not summary:
        for code, msg in problems:
            print(f"✖ {code:<16} {msg}")
        return
    by_code = {}
    for code, msg in problems:
        by_code.setdefault(code, []).append(msg)
    for code, msgs in by_code.items():
        print(f"{code}: {len(msgs)}")
        for msg in msgs[:sample]:
            print(f"  ✖ {msg}")
        if len(msgs) > sample:
            print(f"  … {len(msgs) - sample} more (run without --summary to list them all)")


def check_source_line(r, expected, err):
    lines = [ln.strip() for ln in r["body"] if ln.strip().startswith("Source:")]
    if not lines:
        err("no-source", f"{r['anchor']} at {rel(r['file'])}:{r['line']}: add `{expected}`")
    elif expected not in lines:
        err("bad-source", f"{r['anchor']} at {rel(r['file'])}:{r['line']}: `{lines[0]}` "
                          f"should be `{expected}`")


def check_derived_source(r, docs, err):
    lines = [ln.strip() for ln in r["body"] if ln.strip().startswith("Source:")]
    m = SOURCE_LINE_RE.match(lines[0]) if lines else None
    if not m:
        err("no-source", f"'{r['name']}' at {rel(r['file'])}:{r['line']}: a requirement without "
                         f"an anchor needs `Source: <PRD> §<section>`")
    elif m.group(1) not in docs or f"§{m.group(2)}" not in docs[m.group(1)].anchors:
        err("bad-source", f"'{r['name']}' at {rel(r['file'])}:{r['line']}: {m.group(1)} "
                          f"§{m.group(2)} is not a cited section in openspec/sources/")


def cmd_stale(sources, args):
    problems = []
    for s in sources:
        lock = s.lock()
        current = {a: item for a, item in s.anchors.items() if a not in s.excluded}
        for anchor, item in current.items():
            if anchor not in lock:
                problems.append(("unstamped", f"{s.doc} {anchor} (§{item['section']}): convert "
                                              f"it, then `prdspec.py stamp {anchor}`"))
            elif lock[anchor] != item["hash"]:
                problems.append(("stale", f"{s.doc} {anchor} (§{item['section']}, line "
                                          f"{item['line']}) changed since it was converted → "
                                          f"update {item['capability']}, then stamp"))
        for anchor in sorted(set(lock) - set(current)):
            problems.append(("removed", f"{s.doc} {anchor} is stamped but no longer in the PRD "
                                        f"(withdrawn? add it to `excluded`, or remove it from "
                                        f"the specs), then stamp"))
    # Changed and removed rows first: they are the drift; unstamped rows are unfinished work.
    problems.sort(key=lambda p: p[0] == "unstamped")
    report(problems, "--summary" in args)
    if not problems:
        print(f"✔ specs match the PRD rows they were converted from "
              f"({sum(len(s.lock()) for s in sources)} anchors)")
    return 1 if problems else 0


def cmd_stamp(sources, args):
    if not args:
        raise SystemExit("usage: prdspec.py stamp (--all | <anchor>…)")
    records = read_specs(sources)
    covered = {r["anchor"] for r in records if r["where"] == "main" and r["anchor"]}
    if args != ["--all"]:
        unknown = [a for a in args if not any(a in s.anchors for s in sources)]
        if unknown:
            raise SystemExit(f"✖ not an anchor of any PRD source: {' '.join(unknown)}")
    for s in sources:
        lock = s.lock()
        wanted = s.anchors.keys() if args == ["--all"] else [a for a in args if a in s.anchors]
        stamped = skipped = 0
        for anchor in wanted:
            item = s.anchors[anchor]
            if anchor in s.excluded:
                lock.pop(anchor, None)
                continue
            if item["kind"] != "section" and anchor not in covered:
                skipped += 1
                if args != ["--all"]:
                    print(f"✖ {anchor} is not in openspec/specs/ yet; not stamped")
                continue
            lock[anchor] = item["hash"]
            stamped += 1
        if args == ["--all"]:
            for anchor in set(lock) - set(s.anchors):
                del lock[anchor]
        s.write_lock(lock)
        print(f"✔ {s.doc}: stamped {stamped} anchors in {rel(s.lock_path)}"
              + (f"; {skipped} not yet converted" if skipped else ""))
    return 0


COMMANDS = {"extract": cmd_extract, "map": cmd_map, "packet": cmd_packet, "show": cmd_show,
            "coverage": cmd_coverage, "stale": cmd_stale, "stamp": cmd_stamp}


def main(argv):
    only = None
    if len(argv) > 2 and argv[1] == "--source":
        only, argv = argv[2], [argv[0]] + argv[3:]
    if len(argv) < 2 or argv[1] not in COMMANDS:
        print(__doc__.strip())
        return 2
    return COMMANDS[argv[1]](load_sources(only), argv[2:])


if __name__ == "__main__":
    if hasattr(signal, "SIGPIPE"):                      # `prdspec.py extract | head` exits quietly
        signal.signal(signal.SIGPIPE, signal.SIG_DFL)
    sys.exit(main(sys.argv))
