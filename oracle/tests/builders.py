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
