"""Hypothesis strategies for ``fyniq-payroll-run@1`` documents (``make differential`` with ``FYNIQ_DIFFERENTIAL_SOURCES`` naming
``payroll-month``; p1-nemura-payroll 1.5, QA-ENG-02).

Harness code: it draws runs within payroll-run.schema.json's limits and never computes an expected figure. Money is drawn as cents
(ENG-10), with the wage-band edges of the CPF tables over-represented (49.99, 50.00, 50.01, 500.00, 500.01, 750.00, 750.01, 8,000.00, ...),
birthdays and permanent-resident anniversaries around the wage month (the 1st of a month, 29 February, the month before and after the
55th, 60th, 65th and 70th), year-to-date figures that need not agree with the month (a joiner, a pay change), additional wages with
the facts that class them either way, the deductions of every kind and a run's people more than once over the SDL's dollar.
"""

from __future__ import annotations

from calendar import monthrange
from datetime import date, timedelta
from typing import Any

from hypothesis import HealthCheck, Phase, given, settings
from hypothesis import seed as hypothesis_seed
from hypothesis import strategies as st

EDGES = [4999, 5000, 5001, 49999, 50000, 50001, 79999, 80000, 80001, 200000, 450000, 499999, 500000, 500001, 500100, 599999, 600000, 749999,
         750000, 750001, 750100, 799999, 800000, 800001, 1000000, 1000020, 2000000, 2000001, 2002000, 3004999, 3005000, 3500001, 4500000,
         4500001, 5000001, 7500001, 799999, 800000, 800001, 1000001, 1500001, 2500001, 4000001, 10000001, 10200000]
KINDS = ["absence", "damage_or_loss", "accommodation", "recovery", "cpf", "income_tax", "court_order", "cooperative", "other"]
FUNDS = ["", "CDAC", "ECF", "MBMF", "SINDA"]


def _text(cents: int) -> str:
    sign = "-" if cents < 0 else ""
    cents = abs(cents)
    return f"{sign}{cents // 100}.{cents % 100:02d}"


def _last(year: int, month: int) -> int:
    return monthrange(year, month)[1]


@st.composite
def _cents(draw: st.DrawFn, top: int = 1_500_000) -> int:
    return draw(st.one_of(st.sampled_from(EDGES), st.integers(0, top), st.integers(0, 60_000)))


@st.composite
def _birth(draw: st.DrawFn, month: date) -> str:
    """A birth date for a person near one of the CPF age bands for the wage month."""
    threshold = draw(st.one_of(st.sampled_from([55, 60, 65, 70]), st.integers(18, 78)))
    anchor = draw(st.sampled_from([0, 0, 1, -1, 2, -2]))  # months either side of the birthday's own month
    year, mon = month.year - threshold, month.month + anchor
    while mon < 1:
        year, mon = year - 1, mon + 12
    while mon > 12:
        year, mon = year + 1, mon - 12
    day = draw(st.sampled_from([1, 1, 2, 14, 15, 28, 29, 30, 31, _last(year, mon)]))
    day = min(day, _last(year, mon))
    if draw(st.integers(0, 40)) == 0:
        year, mon, day = draw(st.sampled_from([1952, 1956, 1960, 1968, 1972])), 2, 29  # a leap day
    return date(year, mon, day).isoformat()


@st.composite
def _pr_start(draw: st.DrawFn, month: date) -> str:
    """A permanent resident's start date, in or before the wage month, near an anniversary more often than not."""
    months_back = draw(st.one_of(st.sampled_from([0, 1, 11, 12, 13, 23, 24, 25, 35, 36, 37]), st.integers(0, 60)))
    total = month.year * 12 + (month.month - 1) - months_back
    year, mon = divmod(total, 12)
    mon += 1
    day = min(draw(st.sampled_from([1, 10, 15, 28, 29, 30, 31])), _last(year, mon))
    start = date(year, mon, day)
    return min(start, date(month.year, month.month, _last(month.year, month.month))).isoformat()


@st.composite
def _person(draw: st.DrawFn, month: date) -> dict[str, Any]:
    residency = draw(st.sampled_from(["citizen", "citizen", "permanent_resident", "permanent_resident", "foreigner"]))
    out: dict[str, Any] = {"residency": residency, "birth_date": draw(_birth(month))}
    if residency == "permanent_resident":
        if draw(st.integers(0, 9)) != 0:
            out["pr_start"] = draw(_pr_start(month))
        if draw(st.booleans()):
            out["joint_election"] = draw(st.booleans())
            if out["joint_election"] and draw(st.booleans()):
                out["employee_full_rate"] = True  # F/F: the full table's rates for the employee too
    if draw(st.integers(0, 19)) == 0:
        out["employee"] = False
    if draw(st.booleans()):
        out["shg_fund"] = draw(st.sampled_from(FUNDS))
    if draw(st.integers(0, 4)) == 0:
        out["shg_opt_out"] = True
    return out


@st.composite
def _items(draw: st.DrawFn, month: date) -> list[dict[str, Any]]:
    items: list[dict[str, Any]] = []
    basic = draw(_cents())
    items.append({"key": "basic", "name": "Basic salary", "amount": _text(basic), "cpf_wages": True, "suggested": "ordinary"})
    nxt = date(month.year + (month.month == 12), month.month % 12 + 1, 1)
    for i in range(draw(st.integers(0, 3))):
        kind = draw(st.sampled_from(["allowance", "unpaid", "bonus", "commission", "late", "nonwage", "overtime", "offcycle"]))
        key = f"{kind}{i}"
        if kind == "allowance" or kind == "overtime" or kind == "offcycle":
            items.append({"key": key, "name": kind, "amount": _text(draw(_cents(300_000))), "cpf_wages": True})
        elif kind == "unpaid":
            items.append({"key": key, "name": "Unpaid leave", "amount": _text(-draw(st.integers(0, max(basic, 0)))), "cpf_wages": True})
        elif kind == "nonwage":
            items.append({"key": key, "name": "Non-CPF allowance", "amount": _text(draw(_cents(200_000))), "cpf_wages": False})
        elif kind == "bonus":
            when = draw(st.sampled_from([month, nxt]))
            items.append({"key": key, "name": "Bonus", "amount": _text(draw(_cents(6_000_000))), "cpf_wages": True,
                          "for_month": draw(st.sampled_from([date(month.year - 1, 12, 1), date(month.year, 1, 1), month])).isoformat(),
                          "payable_on": when.replace(day=min(draw(st.sampled_from([1, 14, 15, 28])), _last(when.year, when.month))).isoformat(),
                          "suggested": "additional"})
        elif kind == "late":  # for an earlier month, paid now
            earlier = date(month.year - (month.month == 1), (month.month - 2) % 12 + 1, 1)
            items.append({"key": key, "name": "Arrears", "amount": _text(draw(_cents(500_000))), "cpf_wages": True, "for_month": earlier.isoformat(),
                          "payable_on": month.replace(day=draw(st.integers(1, _last(month.year, month.month)))).isoformat()})
        else:  # commission: for the month, paid within or after the 14th of the next
            items.append({"key": key, "name": "Commission", "amount": _text(draw(_cents(800_000))), "cpf_wages": True,
                          "for_month": month.isoformat(),
                          "payable_on": nxt.replace(day=draw(st.sampled_from([1, 13, 14, 15, 16, 28]))).isoformat(),
                          "suggested": draw(st.sampled_from(["", "ordinary", "additional"]))})
    return items


@st.composite
def _ytd(draw: st.DrawFn, month: date) -> dict[str, str]:
    typical = draw(st.integers(0, 800_000))
    elapsed = month.month - 1
    ow = draw(st.one_of(st.just(typical * elapsed), st.integers(0, 9_600_000), st.sampled_from([0, 8_800_000, 9_600_000])))
    paid = draw(st.one_of(st.just(0), st.integers(0, 10_000_000)))
    subject = draw(st.one_of(st.just(0), st.just(paid), st.integers(0, paid)))
    return {"ow": _text(ow), "aw_subject": _text(subject), "aw_paid": _text(paid)}


@st.composite
def _deductions(draw: st.DrawFn) -> list[dict[str, Any]]:
    out = []
    for i in range(draw(st.integers(0, 3))):
        kind = draw(st.sampled_from(KINDS))
        d: dict[str, Any] = {"key": f"d{i}", "kind": kind, "amount": _text(draw(st.integers(1, 800_000)))}
        if draw(st.booleans()):
            d["consent"] = draw(st.booleans())
        if kind == "recovery" and draw(st.booleans()):
            d["is_advance"] = True
            d["instalment"] = draw(st.sampled_from([1, 2, 3, 12, 13]))
        out.append(d)
    return out


@st.composite
def _overrides(draw: st.DrawFn) -> list[dict[str, str]]:
    """FR-PAY-06: one to three overrides, each of a different figure, in cents, with a reason of the length the rule asks for."""
    fields = draw(st.lists(st.sampled_from(["cpf_employee", "cpf_employer", "sdl", "shg"]), min_size=1, max_size=3, unique=True))
    return [{"field": f, "value": _text(draw(_cents(300_000))), "reason": draw(st.sampled_from(["The CPF Board's figure", "Agreed with the employee", "x" * 10, "y" * 500]))}
            for f in fields]


@st.composite
def _facts(draw: st.DrawFn, month: date, person: dict[str, Any]) -> dict[str, Any]:
    facts: dict[str, Any] = {"items": draw(_items(month))}
    if draw(st.integers(0, 7)) != 0:  # mostly known; a missing year to date is a refusal when additional wages are paid
        facts["ytd"] = draw(_ytd(month))
    if draw(st.integers(0, 5)) == 0 or month.month == 12 and draw(st.booleans()):
        facts["last_month"] = True
    if draw(st.integers(0, 3)) == 0:
        facts["reimbursements"] = [_text(draw(st.integers(1, 100_000))) for _ in range(draw(st.integers(1, 2)))]
    if draw(st.integers(0, 2)) == 0:
        facts["deductions"] = draw(_deductions())
    if draw(st.integers(0, 9)) == 0:
        facts["final"] = True
    if draw(st.integers(0, 6)) == 0:
        facts["held"] = _text(draw(st.integers(1, 500_000)))
    if draw(st.integers(0, 5)) == 0:
        facts["overrides"] = draw(_overrides())
    return facts


@st.composite
def payroll_runs(draw: st.DrawFn) -> dict[str, Any]:
    month = date(draw(st.sampled_from([2026, 2026, 2027])), draw(st.integers(1, 12)), 1)
    people = []
    for n in range(draw(st.integers(1, 4))):
        who = draw(_person(month))
        entry: dict[str, Any] = {"id": f"P{n + 1}", "person": who, **draw(_facts(month, who))}
        if draw(st.integers(0, 3)) == 0:  # a later run or a correction: the facts as last posted differ a little
            posted = draw(_facts(month, who))
            if draw(st.booleans()):
                posted["items"] = entry["items"][:1]
            entry["posted"] = posted
        people.append(entry)
    doc: dict[str, Any] = {"schema": "fyniq-payroll-run@1", "name": "run", "month": month.isoformat(), "people": people}
    if draw(st.integers(0, 3)) == 0:
        nxt = date(month.year + (month.month == 12), month.month % 12 + 1, 1)
        doc["pay_day"] = (month + timedelta(days=draw(st.integers(0, _last(month.year, month.month) - 1)))
                          if draw(st.booleans()) else nxt + timedelta(days=draw(st.integers(0, 14)))).isoformat()
    return doc


def generate(seed: int, count: int) -> list[dict[str, Any]]:
    """``count`` distinct runs, the same for the same seed and Hypothesis version, named ``payroll-<seed>-<i>``."""
    found: dict[str, dict[str, Any]] = {}
    for attempt in range(100):
        batch: list[dict[str, Any]] = []

        @hypothesis_seed(seed * 1_000_003 + attempt)
        @settings(max_examples=count - len(found) + 20, database=None, deadline=None, phases=[Phase.generate],
                  suppress_health_check=list(HealthCheck))
        @given(payroll_runs())
        def collect(doc: dict[str, Any]) -> None:
            batch.append(doc)

        collect()
        for doc in batch:
            found.setdefault(repr(doc), doc)
        if len(found) >= count:
            break
    else:  # pragma: no cover - the strategy cannot be this narrow
        raise RuntimeError(f"the payroll strategy produced only {len(found)} distinct documents")
    return [{**doc, "name": f"payroll-{seed}-{i:04d}"} for i, doc in enumerate(list(found.values())[:count])]
