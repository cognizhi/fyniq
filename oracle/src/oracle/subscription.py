"""The contracted MRR of subscription contracts, worked out from the rule and not from the engine (QA-ENG-01; PRD-001 FR-AR-15, FR-KPI-02).

The rule: a contract is in force from its start day. From then, each day is lived through in turn: an amendment dated that day takes effect (an upgrade or a downgrade sets the
monthly value, a cancellation ends the contract, a renewal starts it again at its value and lifts the agreed end), and the agreed end day, when no renewal has happened, is the
last day it is in force. The contracted MRR on a day is the sum, by currency, of the monthly values of the contracts in force that day, with how many they are. Nothing is
converted between currencies and nothing is rounded: the values are added as written.
"""

from collections import OrderedDict
from datetime import date, timedelta
from decimal import Decimal


def state_on(contract: dict, day: date) -> tuple[bool, Decimal]:
    """Whether ``contract`` is in force on ``day``, and its monthly value then, by living each day from its start."""
    start = date.fromisoformat(contract["starts_on"])
    end = date.fromisoformat(contract["ends_on"]) if contract.get("ends_on") else None
    if day < start:
        return False, Decimal("0.00")
    by_day: dict[date, list[dict]] = {}
    for a in contract.get("amendments", []):
        by_day.setdefault(date.fromisoformat(a["effective_on"]), []).append(a)
    value, in_force, renewed = Decimal(contract["mrr"]), True, False
    cursor = start
    while cursor <= day:
        for a in by_day.get(cursor, []):
            if a["kind"] == "cancel":
                in_force = False
            elif a["kind"] == "renew":
                in_force, renewed, value = True, True, Decimal(a["mrr"])
            else:
                in_force, value = True, Decimal(a["mrr"])
        cursor += timedelta(days=1)
    if in_force and not renewed and end is not None and day > end:
        in_force = False
    return in_force, (value if in_force else Decimal("0.00"))


def contracted_mrr(contracts: list[dict], day: date) -> list[dict]:
    """The monthly value of the contracts in force on ``day``, by currency in the order each currency first appears among the contracts, and how many are in force."""
    totals: "OrderedDict[str, dict]" = OrderedDict()
    for c in contracts:
        in_force, value = state_on(c, day)
        if not in_force:
            continue
        t = totals.setdefault(c["currency"], {"currency": c["currency"], "mrr": Decimal("0.00"), "active": 0})
        t["mrr"] += value
        t["active"] += 1
    # Currencies are listed in the order of their first contract, not of the first one in force.
    order = list(OrderedDict.fromkeys(c["currency"] for c in contracts))
    return [{"currency": cur, "mrr": f'{totals[cur]["mrr"]:.2f}', "active": totals[cur]["active"]} for cur in order if cur in totals]
