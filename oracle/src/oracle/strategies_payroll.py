"""Hypothesis strategies for scenarios of pay runs (``payroll_run@1``; ``make differential`` with ``FYNIQ_DIFFERENTIAL_SOURCES``
naming ``payroll``). A fixed small chart; one to three runs, each stating its wage month's full figures over one to four versions that
mostly count up, with some that repeat a figure (nothing posts), some stale (not newer than a posted version), departments on accounts a
salary may not post to, deductions above the pay and a posting date before the wage month. Money is drawn as cents (ENG-10).
"""

from __future__ import annotations

from datetime import date, timedelta
from typing import Any

from hypothesis import strategies as st

CHART = [
    {"code": "1010", "type": "asset", "group": False, "active": True},
    {"code": "1250", "type": "asset", "group": False, "active": True},
    {"code": "2100", "type": "liability", "group": False, "active": True},
    {"code": "2110", "type": "liability", "group": False, "active": True},
    {"code": "2150", "type": "liability", "group": False, "active": True},
    {"code": "2500", "type": "liability", "group": False, "active": True, "role": "director-loan"},
    {"code": "6011", "type": "expense", "group": False, "active": True},
    {"code": "6012", "type": "expense", "group": False, "active": True},
    {"code": "6013", "type": "expense", "group": False, "active": True},
    {"code": "6020", "type": "expense", "group": False, "active": True},
    {"code": "6030", "type": "expense", "group": False, "active": True},
]
SALARY = ["6011", "6012", "6013"]
BAD_SALARY = ["1010", "6020", "6030", "2100"]
DEDUCTION = ["1250", "2150", "2110"]
BAD_DEDUCTION = ["1010", "2500", "6013", "2100"]


def _text(cents: int) -> str:
    return f"{cents // 100}.{cents % 100:02d}"


@st.composite
def _figures(draw: st.DrawFn, bad: bool) -> dict[str, Any]:
    depts = draw(st.lists(st.sampled_from(SALARY), min_size=1, max_size=3, unique=True))
    grosses = [draw(st.integers(0, 3_000_000)) for _ in depts]
    if bad and draw(st.booleans()):
        depts[0] = draw(st.sampled_from(BAD_SALARY))
    gross = sum(grosses)
    employee = draw(st.integers(0, max(gross // 5, 0)))
    shg = draw(st.integers(0, 2_000))
    deductions = []
    left = max(gross - employee - shg, 0)
    for account in draw(st.lists(st.sampled_from(DEDUCTION), max_size=2, unique=True)):
        part = draw(st.integers(1, max(left, 1)))
        deductions.append({"account": account, "amount": _text(part)})
        left = max(left - part, 0)
    if bad and draw(st.booleans()):  # more than the pay, or to an account that cannot take it
        if draw(st.booleans()):
            deductions.append({"account": draw(st.sampled_from(DEDUCTION)), "amount": _text(gross + 1_000_000)})
        else:
            deductions.append({"account": draw(st.sampled_from(BAD_DEDUCTION)), "amount": _text(draw(st.integers(1, 5_000)))})
    doc: dict[str, Any] = {
        "departments": [{"account": a, "gross": _text(g)} for a, g in zip(depts, grosses)],
        "employee_cpf": _text(employee), "employer_cpf": _text(draw(st.integers(0, max(gross // 6, 0)))),
        "sdl": _text(draw(st.integers(0, 2_000))), "shg": _text(shg),
    }
    if deductions:
        doc["deductions"] = deductions
    if draw(st.booleans()):
        doc["reimbursements"] = _text(draw(st.integers(0, 80_000)))
    return doc


@st.composite
def payroll_scenarios(draw: st.DrawFn, name: str = "generated-payroll") -> dict[str, Any]:
    runs = draw(st.integers(1, 3))
    documents: list[dict[str, Any]] = []
    for r in range(runs):
        month = draw(st.integers(1, 11))
        wage_month = f"2026-{month:02d}"
        first_day = date(2026, month, 1)
        figures = draw(_figures(False))
        latest = 0
        for _ in range(draw(st.integers(1, 4))):
            kind = draw(st.sampled_from(["next", "next", "next", "repeat", "stale", "bad"]))
            version = latest + 1
            if kind == "stale" and latest:
                version = draw(st.integers(1, latest))
            if kind == "bad":
                doc_figures = draw(_figures(True))
            elif kind == "repeat":
                doc_figures = figures
            else:
                doc_figures = draw(_figures(False))
            posted_on = first_day + timedelta(days=draw(st.integers(27, 60)))
            if draw(st.integers(0, 9)) == 0:
                posted_on = first_day - timedelta(days=draw(st.integers(1, 20)))  # before the wage month
            posted_on = min(posted_on, date(2026, 12, 31))
            documents.append({"type": "payroll_run", "id": f"PR-{len(documents) + 1:04d}", "version": version, "date": posted_on.isoformat(),
                              "run": f"RUN-{r + 1}", "wage_month": wage_month, **doc_figures})
            if kind in ("next", "repeat"):
                latest = max(latest, version)
                figures = doc_figures
    return {
        "schema": "fyniq-scenario@1", "name": name, "base_currency": "SGD", "currencies": {"SGD": 2}, "as_of": "2026-12-31",
        "settings": {"accounts": {"claims_payable": "2150", "salaries_payable": "2100", "cpf_sdl_payable": "2110", "employer_cpf": "6020", "sdl": "6030"},
                     "money_accounts": {"1010": "SGD"}, "books_start": "2026-01-01"},
        "accounts": CHART, "documents": documents, "reversals": [],
    }
