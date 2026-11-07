"""Write test/planning/workbook-sample.json from the workbook's reference model (p1-planning task 2.4, design D9).

The workbook (zh-sheet-accounting) carries an independent reference model of its planning sheets, tests/ref_planning.py,
fed with its sample company (src/cfh/sample.py) and its reference ledger (tests/reference.py). This script runs that
model on the sample and on the workbook's second data set (tests/test_cov_planning.py, worst_case_data, restated
below as edits of the sample) and writes, for each:

* the fyniq-forecast@1 document of the same inputs (test/differential/README.md, "Forecasts"), with the books' figures
  the reference ledger gives at the last actual month and the workbook's statutory settings;
* the reference model's outputs, as the decimal text of its binary floats;
* the feasibility study's inputs and outputs, for the feasibility calculator (task 7.2).

Run it from this repository with a checkout of zh-sheet-accounting at the commit to pin; the commit is recorded:

    python3 -I scripts/workbook_planning_fixture.py <path to zh-sheet-accounting> [--out test/planning/workbook-sample.json]

It is run by hand, when the workbook's reference model changes; the test that reads the fixture never runs it.
"""

from __future__ import annotations

import argparse
import datetime as dt
import json
import subprocess
import sys
from decimal import Decimal
from pathlib import Path


def text(x) -> str:
    """A number's decimal text: an int as it is, a float as the shortest decimal that reads back as it (repr)."""
    if isinstance(x, bool):
        raise TypeError(x)
    if isinstance(x, int):
        return str(x)
    d = Decimal(repr(float(x)))
    s = format(d, "f")
    if "." in s:
        s = s.rstrip("0").rstrip(".")
    return "0" if s in ("-0", "") else s


def cents(x) -> str:
    """An amount the books hold in cents, from the reference ledger's float sum."""
    return format(Decimal(repr(float(x))).quantize(Decimal("0.01")), "f")


def month(d: dt.date) -> str:
    return f"{d.year:04d}-{d.month:02d}"


def worst_case(data: dict) -> dict:
    """tests/test_cov_planning.py, worst_case_data(), at the pinned commit: the sample with the Worst scenario, a 30-month
    horizon, an equipment purchase that takes cash below zero, losses brought forward, two start-up years, a one-off
    50% rebate, tax paid 3 months after each year end, revenue-linked expense lines, a leaver, an unpaid advisor, an
    intern at the SDL minimum, a salary above the CPF ceiling and a SAFE (its feasibility edits too)."""
    data["settings"].update(S_SUTEYears=2, S_LossBF=50000, S_TaxLag=3, S_Rebate=0.5, S_RebateCap=40000,
                            S_RptMonth=dt.date(2026, 8, 15))
    fc = data["forecast"]
    fc.update(scenario="Worst", horizon=30, cash_adj=-740000, buffer=3, new0=28, new_growth=0.05, arpa=360, other=1500,
              coll0=0.5, coll1=0.4, coll2=0.1)
    fc["opex"]["Sales & marketing"] = (2500, 0.02, 0.03)
    fc["opex"]["Other staff costs (benefits, recruitment, training)"] = (300, 0.01, 0.01)
    hc = fc["headcount"]
    hc[3] = dict(hc[3], end=dt.date(2028, 2, 1))
    hc += [dict(role="Intern (hire)", func="R&D", start=dt.date(2026, 9, 1), end=dt.date(2027, 2, 1), salary=600, cpf="No"),
           dict(role="Head of sales (hire)", func="S&M", start=dt.date(2027, 4, 1), salary=9000, cpf="Yes"),
           dict(role="Advisor (unpaid)", func="G&A", start=dt.date(2026, 10, 1))]
    fc["capex"].append(dict(item="Data centre kit", month=dt.date(2026, 11, 1), amount=150000, life=60))
    fc["funding"].append(dict(type="SAFE", month=dt.date(2027, 3, 1), amount=150000))
    data["feasibility"].update(horizon=10, rate=0.12, max_payback=5, salvage=20000, life=12, units=60, fixed=150000)
    return data


def document(name: str, data: dict, RP, Ledger, eom) -> tuple[dict, dict]:
    """The forecast document of a data set, and the reference model's outputs for it."""
    s = RP.settings(data)
    ledger = Ledger(data)
    out = RP.forecast(data, ledger)
    fc = dict(RP.FC_DEFAULTS)
    fc.update(data["forecast"])
    if "raise" in fc:
        fc["raise_"] = fc["raise"]
    start, horizon = out["start"], out["horizon"]
    idx = out["scen_idx"]
    levers = {k: v[idx - 1] for k, v in RP.LEVERS.items()}

    # Existing assets' depreciation in each forecast month, as the reference model works it, in cents: what the
    # fixed-asset register's schedule gives the engine.
    assets = []
    for a in data.get("assets", []):
        monthly = (a["cost"] - (a.get("resid") or 0)) / a["life"]
        first = eom(a.get("depstart") or a["date"])
        last = RP.add_months(first, a["life"] - 1)
        if a.get("disp"):
            last = min(last, RP.add_months(eom(a["disp"]), -1))
        assets.append((first, last, monthly))
    depreciation = []
    for n in range(1, horizon + 1):
        m_end = eom(RP.first_of_month(start, n - 1))
        depreciation.append(cents(sum(m for f, l, m in assets if f <= m_end <= l)))

    year1_ya = RP.first_of_month(start, 11).year + 1
    rebates = {}
    if s.get("S_Rebate"):
        rebates[str(year1_ya)] = {"percent": text(s["S_Rebate"]), "cap": text(s["S_RebateCap"])}
    doc = {
        "schema": "fyniq-forecast@1",
        "name": f"workbook-{name}",
        "currency": "SGD",
        "minor_units": 2,
        "start": month(start),
        "horizon": horizon,
        "cash_adjustment": cents(fc.get("cash_adj") or 0),
        "buffer_months": text(fc["buffer"]),
        "books": {"cash": cents(out["open_cash_act"]), "receivables": cents(out["open_ar"]),
                  "payables": cents(out["open_ap"]), "depreciation": depreciation},
        "revenue": {"customers": text(fc["cust0"]), "new_customers": text(fc["new0"]), "new_growth": text(fc["new_growth"]),
                    "churn": text(fc["churn"]), "arpa": text(fc["arpa"]), "price_increase": text(fc["price_inc"]),
                    "services": text(fc["serv0"]), "services_growth": text(fc["serv_growth"]), "other": text(fc["other"]),
                    "collection": [text(fc["coll0"]), text(fc["coll1"]), text(fc["coll2"])]},
        "costs": {"hosting": text(fc["host_pct"]), "ai": text(fc["ai_pct"]), "contractors": text(fc["contr_pct"]),
                  "fees": text(fc["fee_pct"]), "salary_increase": text(fc["raise_"])},
        "opex": [{"label": label, "group": "sales_marketing" if label == "Sales & marketing" else "",
                  "base": text(base), "growth": text(growth), "percent": text(pct)}
                 for label, (base, growth, pct) in fc.get("opex", {}).items()],
        "headcount": [{"role": h.get("role", ""), "function": h["func"], "start": month(h["start"]),
                       "end": month(h["end"]) if h.get("end") else None, "salary": text(h.get("salary") or 0),
                       "cpf": h.get("cpf") == "Yes"} for h in fc.get("headcount", [])],
        "capex": [{"amount": text(c["amount"]), "month": month(c["month"]), "life": int(c["life"])} for c in fc.get("capex", [])],
        "funding": [{"type": f["type"], "month": month(f["month"]), "amount": text(f["amount"]),
                     "rate": text(f["rate"]) if f["type"] == "Loan" else "",
                     "tenor": int(f.get("tenor") or 1) if f["type"] == "Loan" else 0} for f in fc.get("funding", [])],
        "tax": {"start_up_years": int(s["S_SUTEYears"]), "losses_brought_forward": text(s["S_LossBF"]),
                "payment_lag": int(s["S_TaxLag"])},
        "scenarios": [{"name": fc["scenario"], "levers": {
            "new_customers": text(levers["LV_New"]), "churn": text(levers["LV_Churn"]), "price": text(levers["LV_Price"]),
            "services": text(levers["LV_Serv"]), "opex": text(levers["LV_Cost"]), "direct_costs": text(levers["LV_COGS"]),
            "fund_amount": text(levers["LV_FundAmt"]), "hire_delay": int(levers["LV_HireDelay"]),
            "fund_delay": int(levers["LV_FundDelay"])}, "buffer_months": None}],
        "statutory": {"employer_cpf": text(s["CPF_ER_55"]), "ow_ceiling": text(s["S_OWCeiling"]),
                      "sdl": {"rate": text(s["S_SDLRate"]), "minimum": text(s["S_SDLMin"]), "maximum": text(s["S_SDLMax"])},
                      "tax_rate": text(s["S_TaxRate"]),
                      "exemptions": {"sute": [[text(s["S_SUTE1Amt"]), text(s["S_SUTE1Pct"])], [text(s["S_SUTE2Amt"]), text(s["S_SUTE2Pct"])]],
                                     "pte": [[text(s["S_PTE1Amt"]), text(s["S_PTE1Pct"])], [text(s["S_PTE2Amt"]), text(s["S_PTE2Pct"])]]},
                      "rebates": rebates},
    }

    def value(v):
        if v == RP.BLANK:
            return None
        if isinstance(v, dt.date):
            return month(v)
        if isinstance(v, str):
            return v
        return text(v)

    rows = {k: [value(x) for x in v[:horizon]] for k, v in out["rows"].items() if k not in ("date",)}
    reference = {
        "rows": rows,
        "taxes": {k: [value(v[y]) for y in sorted(v)] for k, v in out["tax"].items()},
        "summary": {k: [value(x) for x in v] for k, v in out["summary"].items()},
        "key": {k: value(v) for k, v in out["key"].items()},
        "opex_labels": [o["label"] for o in out["opex"]],
    }
    return doc, reference


def feasibility(data: dict, RP) -> dict:
    s = RP.settings(data)
    fe = data["feasibility"]
    mults = [0.8, 0.9, 1.0, 1.1, 1.2]
    rates = [0.08, 0.10, 0.12, 0.15, 0.20, 0.25, 0.30, 0.40]
    out = RP.feasibility_sheet(fe, s["S_TaxRate"], mults, rates)
    keep = {k: v for k, v in out.items() if k.startswith("FE_") or k in ("fcf", "pv", "cum", "cumpv", "grid", "npv_at")}

    def value(v):
        if isinstance(v, list):
            return [value(x) for x in v]
        if v == RP.BLANK:
            return None
        if isinstance(v, str):
            return v
        return text(v)

    return {"inputs": {k: (v if isinstance(v, str) else text(v)) for k, v in fe.items()}, "tax_rate": text(s["S_TaxRate"]),
            "multipliers": [text(m) for m in mults], "rates": [text(r) for r in rates],
            "reference": {k: value(v) for k, v in keep.items()}}


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("workbook", help="a checkout of zh-sheet-accounting")
    parser.add_argument("--out", default="test/planning/workbook-sample.json")
    args = parser.parse_args(argv)
    repo = Path(args.workbook).resolve()
    sys.path[:0] = [str(repo / "tests"), str(repo / "src")]
    import ref_planning as RP  # noqa: E402 - the workbook's reference model
    from cfh.sample import sample_data  # noqa: E402
    from reference import Ledger, eom  # noqa: E402

    commit = subprocess.run(["git", "-C", str(repo), "rev-parse", "HEAD"], capture_output=True, text=True, check=True).stdout.strip()
    cases = []
    for name, data in (("sample", sample_data()), ("worst-case", worst_case(sample_data()))):
        doc, reference = document(name, data, RP, Ledger, eom)
        cases.append({"name": name, "forecast": doc, "reference": reference, "feasibility": feasibility(data, RP)})
    fixture = {
        "source": {"repository": "cognizhi/zh-sheet-accounting", "commit": commit,
                   "model": "tests/ref_planning.py", "data": "src/cfh/sample.py; tests/test_cov_planning.py worst_case_data",
                   "written_by": "scripts/workbook_planning_fixture.py"},
        "cases": cases,
    }
    out = Path(args.out)
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(json.dumps(fixture, indent=1, ensure_ascii=False) + "\n", encoding="utf-8")
    print(f"wrote {out} from zh-sheet-accounting {commit[:12]}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
