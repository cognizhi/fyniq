"""Write test/migration/workbook-sample.xlsx from the workbook's synthetic sample (p1-workbook-migration design D12).

The workbook (zh-sheet-accounting) publishes dist/Cognizhi-Finance-Hub-Sample.xlsx, its sample company with every value
its engine calculated. The workbook import reads some twenty of its forty-two sheets; this script copies those sheets,
values only (no formulas, styles or macros), into a small workbook the import's tests read, and records where it came
from and its SHA-256 beside it in test/migration/workbook-sample.json. The data is synthetic (DEV-08).

Run it from this repository with a checkout of zh-sheet-accounting at the commit to pin; the commit is recorded:

    python3 -I scripts/workbook_migration_fixture.py <path to zh-sheet-accounting> [--out test/migration]

It is run by hand, when the workbook's sample changes; the tests that read the fixture never run it, and a test checks
the file against the recorded hash.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import posixpath
import subprocess
import sys
import zipfile
from pathlib import Path
from xml.etree import ElementTree as ET

# The sheets the import reads (design D5), the ones it reports as kept in the archive, and the workbook's own engine
# it reconciles with (design D8): Trial Balance, Checks and Carry Forward.
SHEETS = [
    "Settings", "Accounts", "Items", "Contacts", "Employees", "Lists",
    "Transactions", "Invoices", "Bills", "Quotes & Orders", "Doc Lines", "Payroll", "Timesheets", "Assets", "Journal",
    "Recurring", "Bank Rec", "Budget", "Forecast", "Funding",
    "Trial Balance", "Checks", "Carry Forward",
]

MAIN = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
REL = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
PKG = "http://schemas.openxmlformats.org/package/2006/relationships"
NS = {"m": MAIN, "r": REL, "p": PKG}


def text_of(el: ET.Element | None) -> str:
    """A string item's text: its own <t> and its runs' <t>s."""
    if el is None:
        return ""
    return "".join(t.text or "" for t in el.iter(f"{{{MAIN}}}t"))


def read_sheets(src: Path) -> dict[str, list[list[tuple[str, str, str]]]]:
    """Each wanted sheet's rows as (reference, kind, value) cells: kind "n" for a number, "s" for text."""
    z = zipfile.ZipFile(src)
    wb = ET.fromstring(z.read("xl/workbook.xml"))
    rels = ET.fromstring(z.read("xl/_rels/workbook.xml.rels"))
    targets = {r.get("Id"): r.get("Target") for r in rels.findall("p:Relationship", NS)}
    shared: list[str] = []
    if "xl/sharedStrings.xml" in z.namelist():
        sst = ET.fromstring(z.read("xl/sharedStrings.xml"))
        shared = [text_of(si) for si in sst.findall("m:si", NS)]
    out: dict[str, list[list[tuple[str, str, str]]]] = {}
    for sh in wb.findall("m:sheets/m:sheet", NS):
        name = sh.get("name")
        if name not in SHEETS:
            continue
        target = targets[sh.get(f"{{{REL}}}id")].lstrip("/")
        part = posixpath.normpath(target if target.startswith("xl/") else posixpath.join("xl", target))
        root = ET.fromstring(z.read(part))
        rows = []
        for row in root.findall("m:sheetData/m:row", NS):
            cells = []
            for c in row.findall("m:c", NS):
                kind, ref = c.get("t", "n"), c.get("r")
                v = c.find("m:v", NS)
                value = v.text if v is not None and v.text is not None else ""
                if kind == "s":
                    cells.append((ref, "s", shared[int(value)]))
                elif kind == "inlineStr":
                    cells.append((ref, "s", text_of(c.find("m:is", NS))))
                elif kind in ("str", "e", "b", "d"):
                    if value != "":
                        cells.append((ref, "s", value))
                elif value != "":
                    cells.append((ref, "n", value))
            if cells:
                rows.append((row.get("r"), cells))
        out[name] = rows
    missing = [s for s in SHEETS if s not in out]
    if missing:
        raise SystemExit(f"the workbook has no sheet {missing}")
    return out


def esc(s: str) -> str:
    return s.replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;").replace('"', "&quot;")


def write_xlsx(sheets: dict, dest: Path) -> None:
    """A values-only workbook: each cell a number or an inline string, in the wanted sheets' order."""
    names = [s for s in SHEETS if s in sheets]
    ct = ['<?xml version="1.0" encoding="UTF-8" standalone="yes"?>',
          '<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">',
          '<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>',
          '<Default Extension="xml" ContentType="application/xml"/>',
          '<Override PartName="/xl/workbook.xml" '
          'ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>']
    ct += [f'<Override PartName="/xl/worksheets/sheet{i}.xml" '
           'ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>'
           for i in range(1, len(names) + 1)]
    ct.append("</Types>")
    root_rels = ('<?xml version="1.0" encoding="UTF-8" standalone="yes"?>'
                 f'<Relationships xmlns="{PKG}"><Relationship Id="rId1" '
                 'Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" '
                 'Target="xl/workbook.xml"/></Relationships>')
    wb = (f'<?xml version="1.0" encoding="UTF-8" standalone="yes"?><workbook xmlns="{MAIN}" xmlns:r="{REL}"><sheets>'
          + "".join(f'<sheet name="{esc(n)}" sheetId="{i}" r:id="rId{i}"/>' for i, n in enumerate(names, 1))
          + "</sheets></workbook>")
    wb_rels = (f'<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="{PKG}">'
               + "".join(f'<Relationship Id="rId{i}" '
                         'Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" '
                         f'Target="worksheets/sheet{i}.xml"/>' for i in range(1, len(names) + 1))
               + "</Relationships>")
    fixed = (1980, 1, 1, 0, 0, 0)  # every entry dated the same, so the same input writes the same bytes
    with zipfile.ZipFile(dest, "w", zipfile.ZIP_DEFLATED) as z:
        def put(name: str, data: str) -> None:
            info = zipfile.ZipInfo(name, fixed)
            info.compress_type = zipfile.ZIP_DEFLATED
            z.writestr(info, data.encode("utf-8"))
        put("[Content_Types].xml", "".join(ct))
        put("_rels/.rels", root_rels)
        put("xl/workbook.xml", wb)
        put("xl/_rels/workbook.xml.rels", wb_rels)
        for i, n in enumerate(names, 1):
            body = []
            for r, cells in sheets[n]:
                xs = []
                for ref, kind, value in cells:
                    if kind == "n":
                        xs.append(f'<c r="{ref}"><v>{esc(value)}</v></c>')
                    else:
                        xs.append(f'<c r="{ref}" t="inlineStr"><is><t xml:space="preserve">{esc(value)}</t></is></c>')
                body.append(f'<row r="{r}">{"".join(xs)}</row>')
            put(f"xl/worksheets/sheet{i}.xml",
                f'<?xml version="1.0" encoding="UTF-8" standalone="yes"?><worksheet xmlns="{MAIN}"><sheetData>'
                + "".join(body) + "</sheetData></worksheet>")


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("repo", type=Path, help="a checkout of zh-sheet-accounting at the commit to pin")
    ap.add_argument("--out", type=Path, default=Path("test/migration"))
    a = ap.parse_args()
    src = a.repo / "dist" / "Cognizhi-Finance-Hub-Sample.xlsx"
    commit = subprocess.run(["git", "-C", str(a.repo), "rev-parse", "HEAD"], capture_output=True, text=True,
                            check=True).stdout.strip()
    a.out.mkdir(parents=True, exist_ok=True)
    dest = a.out / "workbook-sample.xlsx"
    write_xlsx(read_sheets(src), dest)
    digest = hashlib.sha256(dest.read_bytes()).hexdigest()
    meta = {
        "source": {"repository": "cognizhi/zh-sheet-accounting", "commit": commit,
                   "file": "dist/Cognizhi-Finance-Hub-Sample.xlsx",
                   "sha256": hashlib.sha256(src.read_bytes()).hexdigest()},
        "sheets": SHEETS,
        "fixture": {"file": dest.name, "sha256": digest},
        "data": "synthetic (DEV-08): the workbook's sample company",
    }
    (a.out / "workbook-sample.json").write_text(json.dumps(meta, indent=2) + "\n")
    print(f"wrote {dest} ({dest.stat().st_size} bytes) from zh-sheet-accounting {commit[:12]}", file=sys.stderr)


if __name__ == "__main__":
    main()
