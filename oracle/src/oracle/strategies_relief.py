"""Hypothesis strategies for scenarios of GST bad debt relief (``bad_debt_relief@1``; ``make differential`` with ``FYNIQ_DIFFERENTIAL_SOURCES``
naming ``relief``). A fixed small chart and a GST-registered organisation; one to four invoices (base currency or USD, standard-rated with a
few zero-rated), each written off in one to three parts, each part then claimed (most of them), with some claims repeated, some for a
write-off that never posted, some after a credit note credited the invoice. Money is drawn as cents (ENG-10). The claim names only its
write-off: each side works out the GST, so the amount is the thing the two are compared on.
"""

from __future__ import annotations

from datetime import date, timedelta
from decimal import Decimal
from typing import Any

from hypothesis import strategies as st

CHART = [
    {"code": "1010", "type": "asset", "group": False, "active": True},
    {"code": "1021", "type": "asset", "group": False, "active": True},
    {"code": "1100", "type": "asset", "group": False, "active": True},
    {"code": "2200", "type": "liability", "group": False, "active": True},
    {"code": "2400", "type": "liability", "group": False, "active": True},
    {"code": "2410", "type": "liability", "group": False, "active": True},
    {"code": "4010", "type": "revenue", "group": False, "active": True},
    {"code": "6900", "type": "expense", "group": False, "active": True},
    {"code": "6910", "type": "expense", "group": False, "active": True},
    {"code": "7030", "type": "expense", "group": False, "active": True},
]
BAD_DEBT = ["6900", "6910", "7030"]


def _text(cents: int) -> str:
    return f"{cents // 100}.{cents % 100:02d}"


def _cents_of(text: str) -> int:
    return int((Decimal(text) * 100).to_integral_value())


@st.composite
def relief_scenarios(draw: st.DrawFn, name: str = "generated-relief") -> dict[str, Any]:
    documents: list[dict[str, Any]] = []
    posted_writes: list[str] = []
    day = date(2026, 3, 1)
    for n in range(1, draw(st.integers(1, 4)) + 1):
        currency = draw(st.sampled_from(["SGD", "SGD", "USD"]))
        code = draw(st.sampled_from(["SR", "SR", "SR", "ZR"]))
        net = draw(st.integers(1, 5_000_000))
        invoice: dict[str, Any] = {"type": "invoice", "id": f"INV-{n}", "version": 1, "date": day.isoformat(), "currency": currency,
                                   "lines": [{"account": "4010", "amount": _text(net), "gst_code": code}]}
        if currency == "USD":
            invoice["fx_rate"], invoice["fx_rate_source"] = draw(st.sampled_from(["1.35", "1.5", "1.3333", "0.7"])), "document"
        documents.append(invoice)
        # what the customer owes: the net and its 9% (rounded per subtotal, RND-GST-EXCL), worked out only to bound the parts: a part above what is
        # open is a refusal both sides agree on, so an over-estimate or an under-estimate only changes which documents post
        owed = net + (net * 9 + 50) // 100 if code == "SR" else net
        if draw(st.integers(0, 7)) == 0:  # a credit note linked to it: its claims are refused
            note: dict[str, Any] = {"type": "credit_note", "id": f"CN-{n}", "version": 1, "date": (day + timedelta(days=5)).isoformat(),
                                    "currency": currency, "invoice": f"INV-{n}",
                                    "lines": [{"account": "4010", "amount": _text(draw(st.integers(1, max(net // 2, 1)))), "gst_code": code}]}
            if currency == "USD":
                note["fx_rate"], note["fx_rate_source"] = invoice["fx_rate"], "document"
            documents.append(note)
        left = owed
        writes: list[str] = []
        for k in range(draw(st.integers(1, 3))):
            if left <= 0:
                break
            part = left if draw(st.booleans()) else draw(st.integers(1, left))
            if draw(st.integers(0, 15)) == 0:
                part = left + draw(st.integers(1, 100))  # more than is open: refused, and so is its claim
            wid = f"WO-{n}-{k + 1}"
            documents.append({"type": "write_off", "id": wid, "version": 1, "date": (day + timedelta(days=30 + k)).isoformat(), "invoice": f"INV-{n}",
                              "amount": _text(part), "account": draw(st.sampled_from(BAD_DEBT))})
            writes.append(wid)
            left = max(left - part, 0)
        posted_writes += writes
    for wid in posted_writes:
        if draw(st.integers(0, 9)) == 0:
            continue  # left unclaimed
        documents.append({"type": "bad_debt_relief", "id": f"BDR-{len(documents) + 1}", "version": 1,
                          "date": (day + timedelta(days=200 + draw(st.integers(0, 40)))).isoformat(), "write_off": wid})
        if draw(st.integers(0, 9)) == 0:
            documents.append({"type": "bad_debt_relief", "id": f"BDR-{len(documents) + 1}", "version": 1, "date": "2026-11-30", "write_off": wid})
    if draw(st.integers(0, 9)) == 0:
        documents.append({"type": "bad_debt_relief", "id": f"BDR-{len(documents) + 1}", "version": 1, "date": "2026-11-30", "write_off": "WO-NONE"})
    return {
        "schema": "fyniq-scenario@1", "name": name, "base_currency": "SGD", "currencies": {"SGD": 2, "USD": 2}, "as_of": "2026-12-31",
        "settings": {"accounts": {"receivable": "1100", "gst": "2200", "deferred_revenue": "2400", "fx": "7030", "customer_deposits": "2410"},
                     "money_accounts": {"1010": "SGD", "1021": "USD"},
                     "gst": {"registered_from": "2025-01-01", "rates": [{"from": "2024-01-01", "percent": "9"}]}},
        "accounts": CHART, "documents": documents, "reversals": [],
    }
