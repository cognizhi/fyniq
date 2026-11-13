"""Write test/migration/workbook-parallel.xlsx: the sample workbook at the end of a parallel month (p1-workbook-migration
task 6.1, design D11 M5).

M5 uploads the workbook as it stands at the end of the parallel month, recorded in both the workbook and Fyniq, and
reconciles it the same way as M4. The workbook's sample stops at 30 September 2026, so this script derives the parallel
month's workbook from the pinned fixture (test/migration/workbook-sample.xlsx) by what October records in it, the
workbook's own way:

* the Settings sheet's "as of" moves to 31 October 2026;
* the co-working membership of the Recurring sheet's first row, S$2,180.00 on 1 October, from 1020 Bank - Operating
  account to 6310 Rent & co-working, is October's one transaction;
* the sample's three spreads release their October month, as the workbook recognises a spread's months as they pass:
  INV-0111's annual plan, S$24,000.00 over twelve months from April, S$2,000.00 from 2400 Deferred revenue to 4010
  Subscription revenue; the business insurance premium, S$2,400.00 before GST over twelve months, S$200.00 from 1210
  Prepayments to 6450 Insurance; the design tool's licence, S$1,200.00 before GST over twelve months, S$100.00 from 1210 to
  6210 Software & SaaS subscriptions.

Each moves the Trial Balance sheet's October movement of its accounts, their FY movement, and their closings from October
on.

Everything else is the sample as it was. The data is synthetic (DEV-08). Run it from this repository:

    python3 -I scripts/workbook_parallel_fixture.py [--out test/migration]

The tests never run it; a test checks the file against the hash recorded in test/migration/workbook-parallel.json.
"""

from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import json
import re
import sys
from decimal import Decimal
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))  # the sample fixture's reader and writer, beside this script
import workbook_migration_fixture as fixture  # noqa: E402

AS_OF = dt.date(2026, 10, 31)
OCTOBER = [("1020", Decimal("-2180")), ("6310", Decimal("2180")),  # the membership
           ("2400", Decimal("2000")), ("4010", Decimal("-2000")),  # INV-0111's month
           ("1210", Decimal("-300")), ("6450", Decimal("200")), ("6210", Decimal("100"))]  # the insurance's and the licence's


def serial(d: dt.date) -> str:
    """A date as the workbook keeps it: days since 30 December 1899."""
    return str((d - dt.date(1899, 12, 30)).days)


def column(ref: str) -> str:
    return re.match(r"[A-Z]+", ref).group(0)


def number(value: str) -> Decimal:
    return Decimal(value) if value else Decimal(0)


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--out", type=Path, default=Path("test/migration"))
    a = ap.parse_args()
    base = a.out / "workbook-sample.xlsx"
    sheets = fixture.read_sheets(base)

    # Settings: the "as of" date in column C of the row labelled in column B.
    settings = sheets["Settings"]
    for i, (r, cells) in enumerate(settings):
        if any(column(ref) == "B" and v.startswith("Report 'as of' date") for ref, _, v in cells):
            settings[i] = (r, [(ref, "n", serial(AS_OF)) if column(ref) == "C" else (ref, k, v) for ref, k, v in cells])
            break
    else:
        raise SystemExit("the Settings sheet has no \"Report 'as of' date\"")

    # Trial Balance: its headers on row 7, the month columns between "Balance at FY start" and "FY movement", the
    # closings after "FY movement" in the same order.
    tb = sheets["Trial Balance"]
    head = next(cells for r, cells in tb if r == "7")
    names = {v: column(ref) for ref, _, v in head}
    order = [column(ref) for ref, _, _ in head]
    at_start, fy = order.index(names["Balance at FY start"]), order.index(names["FY movement"])
    months = order[at_start + 1:fy]
    october = 9  # the financial year runs January to December: October is its tenth month
    moved = {months[october], names["FY movement"]} | set(order[fy + 1 + october:fy + 1 + len(months)])
    code = names["Code"]
    done = set()
    for i, (r, cells) in enumerate(tb):
        account = next((v for ref, _, v in cells if column(ref) == code), "")
        delta = dict(OCTOBER).get(account)
        if delta is None:
            continue
        have = {column(ref) for ref, _, _ in cells}
        out = [(ref, "n", str(number(v) + delta)) if column(ref) in moved else (ref, k, v) for ref, k, v in cells]
        out += [(f"{c}{r}", "n", str(delta)) for c in sorted(moved - have)]
        tb[i] = (r, sorted(out, key=lambda x: (len(column(x[0])), column(x[0]))))
        done.add(account)
    if done != {c for c, _ in OCTOBER}:
        raise SystemExit(f"the Trial Balance has no rows for {sorted({c for c, _ in OCTOBER} - done)}")

    dest = a.out / "workbook-parallel.xlsx"
    fixture.write_xlsx(sheets, dest)
    meta = {
        "base": {"file": base.name, "sha256": hashlib.sha256(base.read_bytes()).hexdigest()},
        "as_of": AS_OF.isoformat(),
        "october": [{"account": c, "movement": str(d)} for c, d in OCTOBER],
        "fixture": {"file": dest.name, "sha256": hashlib.sha256(dest.read_bytes()).hexdigest()},
        "data": "synthetic (DEV-08): the workbook's sample company, with its parallel month derived from it",
    }
    (a.out / "workbook-parallel.json").write_text(json.dumps(meta, indent=2) + "\n")
    print(f"wrote {dest} ({dest.stat().st_size} bytes) from {base.name}", file=sys.stderr)


if __name__ == "__main__":
    main()
