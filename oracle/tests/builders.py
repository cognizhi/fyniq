"""Small builders for hand-written scenarios. They build input only, never an expected figure."""

from __future__ import annotations

from typing import Any

from oracle.ledger import Ledger
from oracle.scenario import scenario_from_json

CURRENCIES = {"SGD": 2, "USD": 2, "EUR": 2, "JPY": 0, "KWD": 3}

ACCOUNTS = [
    {"code": "1000", "type": "asset", "group": True, "active": True},  # a group (parent) account
    {"code": "1010", "type": "asset", "group": False, "active": True},
    {"code": "1020", "type": "asset", "group": False, "active": True},
    {"code": "1090", "type": "asset", "group": False, "active": False},  # deactivated
    {"code": "2010", "type": "liability", "group": False, "active": True},
    {"code": "4010", "type": "revenue", "group": False, "active": True},
    {"code": "6010", "type": "expense", "group": False, "active": True},
]


def line(
    account: str, currency: str, amount: str, rate: str | None = None, source: str | None = "document"
) -> dict:
    result = {"account": account, "currency": currency, "amount": amount}
    if rate is not None:
        result["fx_rate"] = rate
        if source is not None:
            result["fx_rate_source"] = source
    return result


def journal(doc_id: str, day: str, *lines: dict) -> dict:
    return {"type": "manual_journal", "id": doc_id, "version": 1, "date": day, "memo": "", "lines": list(lines)}


def scenario(
    *documents: dict,
    reversals: tuple = (),
    base: str = "SGD",
    currencies: dict | None = None,
    accounts: list[dict] | None = None,
) -> dict[str, Any]:
    """A scenario with the standard accounts, plus ``accounts`` (more of them) when given."""
    return {
        "schema": "fyniq-scenario@1",
        "name": "hand-worked",
        "base_currency": base,
        "currencies": dict(currencies or CURRENCIES),
        "accounts": ACCOUNTS + list(accounts or []),
        "documents": list(documents),
        "reversals": [{"document": d, "date": day} for d, day in reversals],
    }


def post(*documents: dict, **options: Any) -> tuple[dict[str, Any], Ledger]:
    """Run the oracle on a hand-written scenario; returns its result and the ledger (for reasons)."""
    ledger = Ledger(scenario_from_json(scenario(*documents, **options)))
    return ledger.result(), ledger


# ---------------------------------------------------------------------------- P1 sources
# A small chart with the system accounts of Appendix B.5, and money accounts in SGD and USD.
P1_ACCOUNTS = [
    {"code": "1010", "type": "asset", "group": False, "active": True},  # SGD bank
    {"code": "1011", "type": "asset", "group": False, "active": True},  # SGD savings
    {"code": "1021", "type": "asset", "group": False, "active": True},  # USD wallet
    {"code": "1090", "type": "asset", "group": False, "active": False},  # a closed bank account
    {"code": "1100", "type": "asset", "group": False, "active": True},
    {"code": "1210", "type": "asset", "group": False, "active": True},
    {"code": "1500", "type": "asset", "group": False, "active": True},  # equipment
    {"code": "2010", "type": "liability", "group": False, "active": True},
    {"code": "2200", "type": "liability", "group": False, "active": True},
    {"code": "2400", "type": "liability", "group": False, "active": True},
    {"code": "2999", "type": "liability", "group": False, "active": True},
    {"code": "3010", "type": "equity", "group": False, "active": True, "role": "share-capital"},
    {"code": "4000", "type": "revenue", "group": True, "active": True},
    {"code": "4010", "type": "revenue", "group": False, "active": True},
    {"code": "6010", "type": "expense", "group": False, "active": True},
    {"code": "6110", "type": "expense", "group": False, "active": True},
    {"code": "6460", "type": "expense", "group": False, "active": True},
    {"code": "7030", "type": "expense", "group": False, "active": True},
]

P1_SETTINGS = {
    "accounts": {
        "receivable": "1100",
        "payable": "2010",
        "gst": "2200",
        "prepayments": "1210",
        "deferred_revenue": "2400",
        "fx": "7030",
        "bank_charges": "6460",
    },
    "money_accounts": {"1010": "SGD", "1011": "SGD", "1021": "USD", "1090": "SGD"},
    "gst": {"registered_from": "2025-01-01", "rates": [{"from": "2023-01-01", "percent": "8"}, {"from": "2024-01-01", "percent": "9"}]},
}


def p1_scenario(
    *documents: dict,
    as_of: str = "2026-12-31",
    settings: dict | None = None,
    accounts: list[dict] | None = None,
    currencies: dict | None = None,
    base: str = "SGD",
) -> dict[str, Any]:
    return {
        "schema": "fyniq-scenario@1",
        "name": "hand-worked-p1",
        "base_currency": base,
        "currencies": dict(currencies or CURRENCIES),
        "as_of": as_of,
        "settings": settings if settings is not None else P1_SETTINGS,
        "accounts": accounts if accounts is not None else P1_ACCOUNTS,
        "documents": list(documents),
        "reversals": [],
    }


def p1_post(*documents: dict, **options: Any) -> tuple[dict[str, Any], Ledger]:
    ledger = Ledger(scenario_from_json(p1_scenario(*documents, **options)))
    return ledger.result(), ledger


def lines_of(result: dict, document: str, kind: str = "post") -> list[tuple[str, str, str, str]]:
    """The lines of the document's first entry of ``kind``, as (account, currency, amount, base)."""
    for entry in result["entries"]:
        if entry["document"] == document and entry["kind"] == kind:
            return [(ln["account"], ln["currency"], ln["amount"], ln["base_amount"]) for ln in entry["lines"]]
    raise AssertionError(f"no {kind} entry for {document}")


def bank_line(doc_id: str, day: str, account: str, currency: str, amount: str, *parts: dict, rate: str | None = None) -> dict:
    doc = {"type": "bank_transaction", "id": doc_id, "version": 1, "date": day, "account": account,
           "currency": currency, "amount": amount, "parts": list(parts)}
    if rate is not None:
        doc["fx_rate"], doc["fx_rate_source"] = rate, "document"
    return doc


def part(account: str, amount: str, gst_code: str = "NA", spread_months: int | None = None) -> dict:
    result = {"account": account, "amount": amount, "gst_code": gst_code}
    if spread_months is not None:
        result["spread_months"] = spread_months
    return result


def leg(account: str, currency: str, amount: str, rate: str | None = None) -> dict:
    result = {"account": account, "currency": currency, "amount": amount}
    if rate is not None:
        result["fx_rate"], result["fx_rate_source"] = rate, "document"
    return result


def transfer(doc_id: str, day: str, paying: dict, receiving: dict, fee: str | None = None) -> dict:
    doc = {"type": "bank_transfer", "id": doc_id, "version": 1, "date": day, "from": paying, "to": receiving}
    if fee is not None:
        doc["fee"] = fee
    return doc
