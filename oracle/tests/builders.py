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
    {"code": "1160", "type": "asset", "group": False, "active": True},  # asset disposal clearing
    {"code": "1210", "type": "asset", "group": False, "active": True},
    {"code": "1250", "type": "asset", "group": False, "active": True},  # staff advances
    {"code": "1500", "type": "asset", "group": False, "active": True},  # equipment
    {"code": "1590", "type": "asset", "group": False, "active": True},  # accumulated depreciation
    {"code": "2010", "type": "liability", "group": False, "active": True},
    {"code": "2150", "type": "liability", "group": False, "active": True},
    {"code": "2200", "type": "liability", "group": False, "active": True},
    {"code": "2330", "type": "liability", "group": False, "active": True},
    {"code": "2400", "type": "liability", "group": False, "active": True},
    {"code": "2410", "type": "liability", "group": False, "active": True},
    {"code": "2999", "type": "liability", "group": False, "active": True},
    {"code": "3010", "type": "equity", "group": False, "active": True, "role": "share-capital"},
    {"code": "4000", "type": "revenue", "group": True, "active": True},
    {"code": "4010", "type": "revenue", "group": False, "active": True},
    {"code": "4020", "type": "revenue", "group": False, "active": True},
    {"code": "4095", "type": "revenue", "group": False, "active": True},  # discounts allowed
    {"code": "6010", "type": "expense", "group": False, "active": True},
    {"code": "6110", "type": "expense", "group": False, "active": True},
    {"code": "6460", "type": "expense", "group": False, "active": True},
    {"code": "6610", "type": "expense", "group": False, "active": True},  # depreciation
    {"code": "6900", "type": "expense", "group": False, "active": True},  # bad debts
    {"code": "7030", "type": "expense", "group": False, "active": True},
    {"code": "7050", "type": "expense", "group": False, "active": True},  # gain or loss on disposal
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
        "withholding_tax": "2330",
        "customer_deposits": "2410",
        "claims_payable": "2150",
        "advances": "1250",
        "depreciation": "6610",
        "accumulated_depreciation": "1590",
        "disposal": "7050",
        "disposal_clearing": "1160",
    },
    "mileage_rates": [{"vehicle": "car", "from": "2026-01-01", "rate": "0.60"},
                      {"vehicle": "motorcycle", "from": "2026-01-01", "rate": "0.25"}],
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


def bill_line(account: str, amount: str | None = None, gst_code: str = "TX", gst: str | None = None, **more: Any) -> dict:
    result: dict[str, Any] = {"account": account, "gst_code": gst_code}
    if amount is not None:
        result["amount"] = amount
    if gst is not None:
        result["gst"] = gst
    result.update(more)
    return result


def bill(doc_id: str, day: str, currency: str, *lines: dict, rate: str | None = None, kind: str = "bill",
         **more: Any) -> dict:
    doc: dict[str, Any] = {"type": kind, "id": doc_id, "version": 1, "date": day, "currency": currency,
                           "lines": list(lines)}
    if rate is not None:
        doc["fx_rate"], doc["fx_rate_source"] = rate, "document"
    doc.update(more)
    return doc


def settlement(kind: str, doc_id: str, day: str, account: str, currency: str, *parts: tuple, rate: str | None = None,
               **more: Any) -> dict:
    """``parts``: (document, amount) or (document, amount, in_full)."""
    doc: dict[str, Any] = {"type": kind, "id": doc_id, "version": 1, "date": day, "account": account,
                           "currency": currency, "parts": []}
    for p in parts:
        item = {"document": p[0], "amount": p[1]}
        if len(p) > 2:
            item["in_full"] = p[2]
        doc["parts"].append(item)
    if rate is not None:
        doc["fx_rate"], doc["fx_rate_source"] = rate, "document"
    doc.update(more)
    return doc


def invoice_line(account: str, amount: str | None = None, gst_code: str = "SR", **more: Any) -> dict:
    result: dict[str, Any] = {"account": account, "gst_code": gst_code}
    if amount is not None:
        result["amount"] = amount
    result.update(more)
    return result


def invoice(doc_id: str, day: str, currency: str, *lines: dict, rate: str | None = None, kind: str = "invoice",
            **more: Any) -> dict:
    return bill(doc_id, day, currency, *lines, rate=rate, kind=kind, **more)


def deposit(doc_id: str, day: str, account: str, currency: str, amount: str, kind: str = "part_payment",
            gst_code: str | None = "SR", rate: str | None = None) -> dict:
    doc: dict[str, Any] = {"type": "customer_deposit", "id": doc_id, "version": 1, "date": day, "account": account,
                           "currency": currency, "amount": amount, "kind": kind}
    if gst_code is not None:
        doc["gst_code"] = gst_code
    if rate is not None:
        doc["fx_rate"], doc["fx_rate_source"] = rate, "document"
    return doc


def claim(doc_id: str, day: str, currency: str, *lines: dict, rate: str | None = None, **more: Any) -> dict:
    return bill(doc_id, day, currency, *lines, rate=rate, kind="expense_claim", **more)


def trip(account: str, distance: str, vehicle: str = "car") -> dict:
    return {"account": account, "distance": distance, "vehicle": vehicle}


def advance(doc_id: str, day: str, amount: str, account: str = "1010", refund_of: str | None = None) -> dict:
    doc: dict[str, Any] = {"type": "cash_advance", "id": doc_id, "version": 1, "date": day, "account": account,
                           "amount": amount}
    if refund_of is not None:
        doc["refund_of"] = refund_of
    return doc


def asset(doc_id: str, day: str, cost: str, life: int, account: str = "1500", **more: Any) -> dict:
    return {"type": "fixed_asset", "id": doc_id, "version": 1, "date": day, "account": account, "cost": cost,
            "life_months": life, **more}


def disposal(doc_id: str, day: str, asset_id: str, account: str | None = None, amount: str | None = None,
             gst_code: str | None = None) -> dict:
    doc: dict[str, Any] = {"type": "asset_disposal", "id": doc_id, "version": 1, "date": day, "asset": asset_id}
    if account is not None:
        doc["proceeds"] = {"account": account, "amount": amount}
        if gst_code is not None:
            doc["proceeds"]["gst_code"] = gst_code
    return doc
