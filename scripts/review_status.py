#!/usr/bin/env python3
"""The review register (PRD-001 FR-UX-41, QA-UI-09; PRD-002 NM-UX-06).

`docs/prd/reviews/review-status.json` lists each area of rules that awaits its second review
(the review plan in `docs/prd/reviews/second-review-plan-2026-10-14.md`) and the pages that show
the "Needs additional verification" banner until the area's review pull request merges. The
screen build (`docs/design/screens/build.py canvas`) and, from P1, the web app read it; this
script keeps it honest. Standard library only.

    python3 scripts/review_status.py check     # exit 1 on any register error (make review-check)
    python3 scripts/review_status.py list      # areas, their status and the pages that show the banner
"""
import json
import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
REGISTER = ROOT / "docs/prd/reviews/review-status.json"
PRDS = [ROOT / "docs/prd/prd-001-fyniq-platform.md", ROOT / "docs/prd/prd-002-nemura-people.md"]
SCREENS = ROOT / "docs/design/screens"
GUIDE = ROOT / "docs/user-guide"
SETTINGS_RE = re.compile(r"<!--\s*fyniq-screen\s+(\{.*?\})\s*-->", re.S)
SELF_SERVICE = {"employee", "linemanager", "leaver"}   # personas of the personal workspace and Nemura's Me pages
SELF_SERVICE_CANVAS = "nemura-me"


def load():
    return json.loads(REGISTER.read_text(encoding="utf-8"))


def prd_ids():
    ids = set()
    for prd in PRDS:
        ids.update(re.findall(r"^\| ([A-Z][A-Z0-9]*(?:-[A-Z0-9]+)*-\d+[a-z]?) \|", prd.read_text(encoding="utf-8"), re.M))
    return ids


def persona(file):
    path = SCREENS / "src" / f"{file}.html"
    if not path.exists():
        return None
    m = SETTINGS_RE.match(path.read_text(encoding="utf-8"))
    return json.loads(m.group(1)).get("persona") if m else None


def help_keys():
    keys = set()
    for page in GUIDE.rglob("*.md"):
        m = re.search(r"^key:\s*(\S+)", page.read_text(encoding="utf-8"), re.M)
        if m:
            keys.add(m.group(1))
    return keys


def errors(reg):
    out = []
    inv = {s["id"]: s for s in json.loads((SCREENS / "inventory.json").read_text(encoding="utf-8"))["screens"]}
    ids = prd_ids()
    for key in ("heading", "help_key", "never", "areas"):
        if key not in reg:
            out.append(f"register has no `{key}`")
    if reg.get("help_key") and reg["help_key"] not in help_keys():
        out.append(f"help key `{reg['help_key']}` has no page in docs/user-guide/ (front-matter `key:`)")
    never = set(reg.get("never", []))
    for sid in sorted(never - inv.keys()):
        out.append(f"`never` names unknown screen `{sid}`")
    seen = set()
    for area in reg.get("areas", []):
        aid = area.get("id", "?")
        if aid in seen:
            out.append(f"area `{aid}` appears twice")
        seen.add(aid)
        for key in ("title", "reviewer", "status", "short", "message", "requirements", "screens"):
            if not area.get(key):
                out.append(f"area `{aid}` has no `{key}`")
        status = area.get("status")
        if status not in ("pending", "reviewed"):
            out.append(f"area `{aid}`: status must be `pending` or `reviewed`, not `{status}`")
        if status == "reviewed" and not isinstance(area.get("pr"), int):
            out.append(f"area `{aid}` is reviewed but names no pull request number (`pr`)")
        reqs = set(area.get("requirements", []))
        for rid in sorted(reqs - ids):
            out.append(f"area `{aid}` names `{rid}`, which is not a requirement row in PRD-001 or PRD-002")
        for sid in area.get("screens", []):
            s = inv.get(sid)
            if not s:
                out.append(f"area `{aid}` lists unknown screen `{sid}` (docs/design/screens/inventory.json)")
                continue
            if sid in never:
                out.append(f"area `{aid}` lists `{sid}`, a page that never shows the banner (FR-UX-41)")
            if s.get("canvas") == SELF_SERVICE_CANVAS or persona(s["file"]) in SELF_SERVICE:
                out.append(f"area `{aid}` lists `{sid}`, a self-service page (FR-UX-39, NM-UX-06)")
            if not reqs & set(s.get("requirements", [])):
                out.append(f"area `{aid}` lists `{sid}`, which cites none of the area's requirements")
    return out


def banners(reg):
    """Screen ID → the pending areas whose banner it shows (used by build.py)."""
    shown = {}
    for area in reg["areas"]:
        if area["status"] == "pending":
            for sid in area["screens"]:
                shown.setdefault(sid, []).append(area)
    return shown


def main(argv):
    cmd = argv[1] if len(argv) > 1 else "check"
    reg = load()
    if cmd == "check":
        errs = errors(reg)
        for e in errs:
            print(f"✖ {e}")
        if errs:
            return 1
        shown = banners(reg)
        pending = [a["id"] for a in reg["areas"] if a["status"] == "pending"]
        print(f"✔ review register: {len(reg['areas'])} areas ({len(pending)} pending), "
              f"{len(shown)} pages show the banner")
        return 0
    if cmd == "list":
        for area in reg["areas"]:
            pr = f" · PR #{area['pr']}" if area.get("pr") else ""
            print(f"{area['id']}: {area['title']} · {area['reviewer']} · {area['status']}{pr}")
            if area["status"] == "pending":
                print("  " + ", ".join(area["screens"]))
        return 0
    print(__doc__)
    return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv))
