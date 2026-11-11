"""Hypothesis strategies for fyniq-purge@1 documents (AUD-10; p1-audit-explorer design D7b, QA-ENG-02).

Harness code: it draws ledgers within purge.schema.json's limits for the differential run; it never computes an
expected figure. A ledger has a chart with receivables and payables, accounts kept by person (the director's loan, share application monies) and by instrument (SAFEs), entries in the base
currency and in others, some with a base amount that no rate gives (a revaluation's or a
rounding's), dimension sets, source documents shared by several entries so their open items net, reversals (of
reversals too) across the boundary, and entries a register or the bank names.
"""

from __future__ import annotations

from datetime import date, timedelta
from typing import Any

from hypothesis import HealthCheck, Phase, given, settings
from hypothesis import seed as hypothesis_seed
from hypothesis import strategies as st

BASE = "SGD"
CURRENCIES = {"SGD": 2, "USD": 2, "JPY": 0, "EUR": 2}
CHART = [
    {"code": "1010", "type": "asset"},
    {"code": "1020", "type": "asset"},
    {"code": "1030", "type": "asset"},
    {"code": "1100", "type": "asset", "role": "accounts-receivable"},
    {"code": "1210", "type": "asset", "role": "prepayments"},
    {"code": "2010", "type": "liability", "role": "accounts-payable"},
    {"code": "2200", "type": "liability", "role": "gst-control"},
    {"code": "2500", "type": "liability", "role": "director-loan"},
    {"code": "2600", "type": "liability"},
    {"code": "2700", "type": "liability", "role": "convertible-instruments"},
    {"code": "2800", "type": "liability", "role": "share-application-monies"},
    {"code": "2999", "type": "liability", "role": "suspense"},
    {"code": "3010", "type": "equity", "role": "share-capital"},
    {"code": "3100", "type": "equity", "role": "retained-earnings"},
    {"code": "4010", "type": "revenue"},
    {"code": "4020", "type": "revenue"},
    {"code": "6010", "type": "expense"},
    {"code": "6020", "type": "expense"},
    {"code": "7030", "type": "expense", "role": "fx-rounding"},
]
CODES = [a["code"] for a in CHART]
SETS = ["", "", "dept=eng", "dept=sales", "dept=eng,proj=apollo"]
PEOPLE = {"2500": ["P-1", "P-2"], "2800": ["P-1", "P-3"]}  # accounts whose lines name a person
INSTRUMENTS = {"2700": ["S-1", "S-2"]}  # accounts whose lines name an instrument
SOURCES = ["invoice", "bill", "customer_receipt", "bill_payment", "journal", "share_issue_cash", "bank_transaction"]
# A rate per unit, in ten-thousandths of the base currency: what a person would type from a bank's quote.
RATES = {"USD": (12000, 14500), "EUR": (13500, 15500), "JPY": (80, 120)}


def _money(units: int, places: int) -> str:
    sign = "-" if units < 0 else ""
    units = abs(units)
    if places == 0:
        return f"{sign}{units}"
    whole, part = divmod(units, 10**places)
    return f"{sign}{whole}.{part:0{places}d}"


def _rounded(amount_units: int, places: int, rate: int) -> int:
    """The base amount's cents for an amount at a rate in ten-thousandths, half away from zero (input only)."""
    num = amount_units * rate * 10**2
    den = 10**places * 10_000
    q, r = divmod(abs(num), den)
    if 2 * r >= den:
        q += 1
    return q if num >= 0 else -q


@st.composite
def _line(draw: st.DrawFn) -> dict[str, Any]:
    account = draw(st.sampled_from(CODES[:-1] + ["7030"]))
    currency = draw(st.sampled_from(["SGD", "SGD", "USD", "JPY", "EUR"]))
    places = CURRENCIES[currency]
    units = draw(st.integers(-5_000_000, 5_000_000).filter(lambda n: n != 0))
    if currency == BASE:
        base = units
    else:
        shape = draw(st.sampled_from(["rate", "rate", "rate", "base-only", "any"]))
        if shape == "base-only":
            units, base = 0, draw(st.integers(-50_000, 50_000))
        elif shape == "any":
            base = draw(st.integers(-5_000_000, 5_000_000))
        else:
            low, high = RATES[currency]
            base = _rounded(units, places, draw(st.integers(low, high)))
    return {"account": account, "dimension_set": draw(st.sampled_from(SETS)), "currency": currency, **draw(_tags(account)),
            "amount": _money(units, places), "base_amount": _money(base, 2), "_base": base}


@st.composite
def _tags(draw: st.DrawFn, account: str) -> dict[str, str]:
    """The person or instrument a line on account names, as the database requires."""
    if account in PEOPLE:
        return {"party": draw(st.sampled_from(PEOPLE[account]))}
    if account in INSTRUMENTS:
        return {"instrument": draw(st.sampled_from(INSTRUMENTS[account]))}
    return {}


@st.composite
def purges(draw: st.DrawFn) -> dict[str, Any]:
    fy_month = draw(st.integers(1, 12))
    boundary = date(draw(st.integers(2021, 2024)), fy_month, 1)
    first = boundary - timedelta(days=draw(st.integers(30, 800)))
    last = boundary + timedelta(days=draw(st.integers(0, 600)))
    count = draw(st.integers(1, 24))
    entries: list[dict[str, Any]] = []
    reversed_ids: set[str] = set()
    for i in range(count):
        key = f"E{i + 1}"
        candidates = [e for e in entries if e["id"] not in reversed_ids]
        if candidates and draw(st.integers(0, 4)) == 0:
            original = draw(st.sampled_from(candidates))
            reversed_ids.add(original["id"])
            day = date.fromisoformat(original["posting_date"]) + timedelta(days=draw(st.integers(0, 500)))
            lines = [{**line, "amount": _flip(line["amount"]), "base_amount": _flip(line["base_amount"])} for line in original["lines"]]
            entries.append({"id": key, "source": dict(original["source"]), "posting_date": min(day, last).isoformat(),
                            "reverses": original["id"], "lines": lines})
            continue
        lines = draw(st.lists(_line(), min_size=1, max_size=4))
        owed = -sum(line.pop("_base") for line in lines)
        balancer = draw(st.sampled_from(CODES))
        if owed != 0 or len(lines) == 1:
            lines.append({"account": balancer, "dimension_set": draw(st.sampled_from(SETS)), "currency": BASE, **draw(_tags(balancer)),
                          "amount": _money(owed, 2), "base_amount": _money(owed, 2)})
        kind = draw(st.sampled_from(SOURCES))
        entry: dict[str, Any] = {
            "id": key,
            "source": {"type": kind, "id": f"{kind[:3].upper()}-{draw(st.integers(1, 6))}"},
            "posting_date": (first + timedelta(days=draw(st.integers(0, (last - first).days)))).isoformat(),
            "lines": lines,
        }
        if draw(st.integers(0, 9)) == 0:
            entry["named"] = True
        entries.append(entry)
    return {
        "schema": "fyniq-purge@1",
        "name": "purge",
        "base_currency": BASE,
        "currencies": dict(CURRENCIES),
        "fy_start_month": fy_month,
        "boundary": boundary.isoformat(),
        "retained_earnings": "3100",
        "accounts": [{k: v for k, v in a.items() if v != ""} for a in CHART],
        "entries": entries,
    }


def _flip(text: str) -> str:
    if text.startswith("-"):
        return text[1:]
    return text if set(text) <= {"0", "."} else "-" + text


def generate_purges(seed: int, count: int) -> list[dict[str, Any]]:
    """``count`` distinct purge documents, the same for the same seed and Hypothesis version, named
    ``purge-<seed>-<i>``."""
    found: dict[str, dict[str, Any]] = {}
    for attempt in range(100):
        batch: list[dict[str, Any]] = []

        @hypothesis_seed(seed * 1_000_003 + attempt)
        @settings(max_examples=count - len(found), database=None, deadline=None, phases=[Phase.generate],
                  suppress_health_check=list(HealthCheck))
        @given(purges())
        def collect(doc: dict[str, Any]) -> None:
            batch.append(doc)

        collect()
        for doc in batch:
            found.setdefault(repr(doc), doc)
        if len(found) >= count:
            break
    else:  # pragma: no cover - the strategy cannot be this narrow
        raise RuntimeError(f"the purge strategy produced only {len(found)} distinct documents")
    return [{**doc, "name": f"purge-{seed}-{i:04d}"} for i, doc in enumerate(list(found.values())[:count])]
