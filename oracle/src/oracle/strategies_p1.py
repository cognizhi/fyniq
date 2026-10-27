"""Hypothesis strategies for scenarios with P1 sources (QA-ENG-02), one group of kinds per source.

Not part of the gate's profile yet: ``cmd/scenario-run`` does not read the P1 shapes, so
``make differential`` draws from these only for the sources named in ``FYNIQ_DIFFERENTIAL_SOURCES``
(test/differential/README.md, "P1 sources"). The oracle's own properties use them now.

As for manual journals, each document is built to an *intent* (``post`` or ``reject``), never to an
expected figure. Only shapes whose rule PRD-001 settles are generated; the readings the PRD leaves
open (README, "P1 readings") are covered by the pending golden cases instead. For bank lines that
means: lines in the base currency (the currency of a foreign line's parts is a reading), and fees
only on a transfer paid from a base-currency account.
"""

from __future__ import annotations

from datetime import date, timedelta
from decimal import Decimal
from typing import Any

from hypothesis import strategies as st

from oracle import money
from oracle.scenario import SCENARIO_SCHEMA
from oracle.strategies import POST, REJECT, Intent

SOURCES = ("bank",)

CURRENCIES = {"SGD": 2, "USD": 2, "JPY": 0, "KWD": 3}
MONEY_ACCOUNTS = {"1010": "SGD", "1011": "SGD", "1021": "USD", "1022": "JPY", "1023": "KWD"}
SYSTEM = {
    "receivable": "1100",
    "payable": "2010",
    "gst": "2200",
    "prepayments": "1210",
    "deferred_revenue": "2400",
    "fx": "7030",
    "bank_charges": "6460",
}
EXPENSES = ["6010", "6020", "6110", "6460", "7030"]
REVENUES = ["4010", "4020", "4030"]
OTHER = ["1500", "2050", "2999"]  # an asset, an accrual, suspense: P&L-free parts
_DATES = st.dates(date(2025, 6, 1), date(2027, 3, 31))
_CODES_IN = ["SR", "SR", "ZR", "ES", "OS", "NA"]
_CODES_OUT = ["TX", "TX", "IM", "ZP", "BL", "NR", "EP", "OP", "NA"]

BANK_POSTING = ["bank-line", "bank-line", "bank-split", "bank-spread", "transfer-same", "transfer-cross"]
BANK_REJECTED = ["bank-parts-mismatch", "bank-part-on-money", "bank-part-on-control", "bank-spread-not-p-and-l",
                 "transfer-unbalanced", "transfer-missing-rate"]
KINDS = {"bank": BANK_POSTING * 3 + BANK_REJECTED}


def _accounts() -> list[dict[str, Any]]:
    def account(code: str, kind: str) -> dict[str, Any]:
        return {"code": code, "type": kind, "group": False, "active": True}

    chart = [account(code, "asset") for code in [*MONEY_ACCOUNTS, "1100", "1210", "1500"]]
    chart += [account(code, "liability") for code in ["2010", "2050", "2200", "2400", "2999"]]
    chart += [account(code, "revenue") for code in REVENUES]
    chart += [account(code, "expense") for code in EXPENSES]
    return chart


@st.composite
def _settings(draw: st.DrawFn) -> dict[str, Any]:
    gst: dict[str, Any] = {"rates": [{"from": "2023-01-01", "percent": "8"}, {"from": "2024-01-01", "percent": "9"}]}
    registration = draw(st.sampled_from(["always", "always", "from", "window", "never"]))
    if registration == "always":
        gst["registered_from"] = "2020-01-01"
    elif registration == "from":
        gst["registered_from"] = draw(_DATES).isoformat()
    elif registration == "window":
        start = draw(_DATES)
        gst["registered_from"] = start.isoformat()
        gst["registered_to"] = (start + timedelta(days=draw(st.integers(0, 300)))).isoformat()
    if draw(st.booleans()):
        gst["per_line"] = draw(st.booleans())
    return {"accounts": dict(SYSTEM), "money_accounts": dict(MONEY_ACCOUNTS), "gst": gst}


@st.composite
def _cents(draw: st.DrawFn, minor: int = 2, digits: int = 7) -> Decimal:
    """A positive amount, log-uniform up to ``digits`` integer digits."""
    size = draw(st.integers(1, digits + minor))
    return Decimal(draw(st.integers(10 ** (size - 1), 10**size - 1))).scaleb(-minor, context=money.EXACT)


def _text(value: Decimal, minor: int) -> str:
    return money.fmt(value, minor)


@st.composite
def _split(draw: st.DrawFn, total: Decimal, parts: int, minor: int) -> list[Decimal]:
    """``total`` (positive) in ``parts`` positive pieces at the minor unit."""
    units = int(total.scaleb(minor, context=money.EXACT))
    parts = min(parts, units)
    cuts = sorted(draw(st.lists(st.integers(1, units - 1), min_size=parts - 1, max_size=parts - 1, unique=True))) \
        if parts > 1 else []
    edges = [0, *cuts, units]
    return [Decimal(b - a).scaleb(-minor, context=money.EXACT) for a, b in zip(edges, edges[1:], strict=False)]


@st.composite
def _rate(draw: st.DrawFn) -> str:
    decimals = draw(st.integers(0, 6))
    return format(Decimal(draw(st.integers(1, 10 ** (decimals + 2)))).scaleb(-decimals, context=money.EXACT), "f")


@st.composite
def _bank_document(draw: st.DrawFn, doc_id: str, day: date, kind: str) -> dict[str, Any]:
    if kind.startswith("transfer"):
        return draw(_transfer(doc_id, day, kind))
    sign = draw(st.sampled_from([1, -1]))
    account = draw(st.sampled_from(["1010", "1011"]))
    gross = draw(_cents())
    pieces = draw(_split(gross, draw(st.integers(2, 4)) if kind == "bank-split" else 1, 2))
    if kind == "bank-parts-mismatch":
        pieces[0] = money.EXACT.add(pieces[0], Decimal("0.01"))
    category = EXPENSES if sign < 0 else REVENUES
    codes = _CODES_OUT if sign < 0 else _CODES_IN
    parts = []
    for i, piece in enumerate(pieces):
        target = draw(st.sampled_from(category + OTHER if kind in ("bank-line", "bank-split") else category))
        if kind == "bank-part-on-money" and i == 0:
            target = "1011" if account == "1010" else "1010"
        if kind == "bank-part-on-control" and i == 0:
            target = draw(st.sampled_from(["1100", "2010"]))
        part = {"account": target, "amount": _text(sign * piece, 2), "gst_code": draw(st.sampled_from(codes))}
        if kind == "bank-spread" or (kind == "bank-spread-not-p-and-l" and i == 0) or \
                (kind == "bank-split" and draw(st.integers(0, 3)) == 0 and target in category):
            part["spread_months"] = draw(st.sampled_from([1, 2, 3, 6, 12, 24, 120]))
        if kind == "bank-spread-not-p-and-l" and i == 0:
            part["account"] = draw(st.sampled_from(OTHER + (REVENUES if sign < 0 else EXPENSES)))
        parts.append(part)
    return {"type": "bank_transaction", "id": doc_id, "version": 1, "date": day.isoformat(), "account": account,
            "currency": "SGD", "amount": _text(sign * gross, 2), "parts": parts}


@st.composite
def _transfer(draw: st.DrawFn, doc_id: str, day: date, kind: str) -> dict[str, Any]:
    def leg(account: str, amount: Decimal, rate: str | None) -> dict[str, Any]:
        currency = MONEY_ACCOUNTS[account]
        result = {"account": account, "currency": currency, "amount": _text(amount, CURRENCIES[currency])}
        if rate is not None:
            result["fx_rate"], result["fx_rate_source"] = rate, "document"
        return result

    if kind in ("transfer-same", "transfer-unbalanced"):
        paying, receiving = draw(st.permutations(["1010", "1011"]))
        amount = draw(_cents())
        received = money.EXACT.subtract(amount, Decimal("0.01")) if kind == "transfer-unbalanced" else amount
        if not received > 0:
            received = money.EXACT.add(amount, Decimal("0.01"))
        return {"type": "bank_transfer", "id": doc_id, "version": 1, "date": day.isoformat(),
                "from": leg(paying, amount, None), "to": leg(receiving, received, None)}
    foreign = draw(st.sampled_from(["1021", "1022", "1023"]))
    base_side = draw(st.sampled_from(["1010", "1011"]))
    pays_foreign = draw(st.booleans())
    paying, receiving = (foreign, base_side) if pays_foreign else (base_side, foreign)
    paid = draw(_cents(CURRENCIES[MONEY_ACCOUNTS[paying]]))
    received = draw(_cents(CURRENCIES[MONEY_ACCOUNTS[receiving]]))
    rate = None if kind == "transfer-missing-rate" else draw(_rate())
    doc = {"type": "bank_transfer", "id": doc_id, "version": 1, "date": day.isoformat(),
           "from": leg(paying, paid, rate if pays_foreign else None),
           "to": leg(receiving, received, None if pays_foreign else rate)}
    if not pays_foreign and draw(st.booleans()) and paid > Decimal("0.01"):
        doc["fee"] = _text(draw(_split(paid, 2, 2))[0], 2)  # a fee below what was paid
    return doc


@st.composite
def p1_scenarios_with_intent(
    draw: st.DrawFn, sources: tuple[str, ...] = SOURCES, name: str = "generated-p1"
) -> tuple[dict[str, Any], Intent]:
    """A scenario of P1 documents from ``sources`` (with SGD as the base) and each one's intent."""
    kinds = [kind for source in sources for kind in KINDS[source]]
    documents, intent = [], {}
    for i in range(draw(st.integers(1, 8))):
        kind = draw(st.sampled_from(kinds))
        doc_id = f"P1-{i + 1:04d}"
        documents.append(draw(_bank_document(doc_id, draw(_DATES), kind)))
        intent[doc_id] = REJECT if kind in BANK_REJECTED else POST
    last = max(date.fromisoformat(d["date"]) for d in documents)
    scenario = {
        "schema": SCENARIO_SCHEMA,
        "name": name,
        "base_currency": "SGD",
        "currencies": dict(CURRENCIES),
        "as_of": (last + timedelta(days=draw(st.integers(0, 400)))).isoformat(),
        "settings": draw(_settings()),
        "accounts": _accounts(),
        "documents": documents,
        "reversals": [],
    }
    return scenario, Intent(intent)


def p1_scenarios(sources: tuple[str, ...] = SOURCES, name: str = "generated-p1") -> st.SearchStrategy[dict[str, Any]]:
    return p1_scenarios_with_intent(sources, name).map(lambda pair: pair[0])
