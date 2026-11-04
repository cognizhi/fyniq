"""The month-end revaluation of foreign-currency monetary items (FR-GL-04), written from PRD-001 (QA-ENG-01).

From FR-GL-04 (rev 0.8, 0.72, 0.73, 0.75, 0.92 and 0.119), INV-05, ENG-41, §10.2 ("FX revaluation (month end)") and
Appendix B.5 (7040), as test/differential/README.md, "Month-end revaluation", states them; never from the engine.

At each month's last day D on or before ``as_of`` the scheduler reads every entry posted so far dated on or before D
(the documents', the corrections' and the other scheduled entries', but no revaluation: each is reversed on the next
month's first day) and finds the open **items** on the accounts marked ``revalue``, each in one currency other than the
base:

* on the receivable and payable accounts, document by document (rev 0.92, 0.119): each posted invoice, credit note,
  bill, vendor credit and customer's or supplier's credit dated on or before D, at what is open of it on D and its
  carrying amount at its stored rate, signed as its line on the account; and what the account holds beyond them as
  one more item ("so 2010 ends the period at the rate");
* on every other marked account, its balance in the currency, one item per person on a director's account.

When every currency of an open item has a rate for D (rev 0.75), each item's change is ``RND-FX``(amount × rate) −
carrying amount, posted as a base-only line (INV-05) on its account in its currency, against the unrealised FX account
(7040) in the base currency; the entry is dated D and reversed on the next month's first day (``revaluation`` and
``revaluation_reversal``), each when on or before ``as_of``. Otherwise the month waits and posts nothing.
"""

from __future__ import annotations

from collections.abc import Iterable
from datetime import date, timedelta
from decimal import Decimal
from typing import Any

from oracle import money
from oracle.book import Entry, PostedLine, canonical
from oracle.scenario import Scenario
from oracle.sources.settle import OpenItem

# How a document's open item sits on its control account (README, "What a month's revaluation reads").
SIDE = {"invoice": 1, "vendor_credit": 1, "supplier_credit": 1, "bill": -1, "credit_note": -1, "customer_credit": -1}
DIRECTOR_ROLES = frozenset({"director-loan", "deferred-remuneration"})

Key = tuple[str, str, str | None]  # (account, currency, person)
Item = tuple[str, str, str | None, Decimal, Decimal]  # (account, currency, person, amount, carrying amount)


def document_id(day: date) -> str:
    """The revaluation's document: ``revaluation-YYYY-MM``, its month."""
    return f"revaluation-{day.year:04d}-{day.month:02d}"


def _month_end(year: int, month: int) -> date:
    following = date(year + month // 12, month % 12 + 1, 1)
    return following - timedelta(days=1)


def revaluations(scenario: Scenario, entries: Iterable[Entry], books: dict[str, Any]) -> list[Entry]:
    """Each month's revaluation and its reversal dated on or before ``as_of``, in date order.

    ``entries`` are every posted entry but a revaluation (the documents', the corrections' and the other scheduled
    entries', whatever their order); ``books`` what each posted document left (its open item).
    """
    flagged = {code for code, account in scenario.accounts.items() if account.revalue}
    entries = [e for e in entries if e.kind not in ("revaluation", "revaluation_reversal")]
    as_of, settings = scenario.as_of, scenario.settings
    if not flagged or not entries or as_of is None or settings is None:
        return []
    out: list[Entry] = []
    first = min(e.posting_date for e in entries)
    year, month = first.year, first.month
    while (end := _month_end(year, month)) <= as_of:
        lines = _month(scenario, flagged, entries, books, end)
        if lines:
            out.append(Entry(document_id(end), "revaluation", end, lines))
            following = end + timedelta(days=1)
            if following <= as_of:  # "the entry reverses on day 1 of the next period"
                out.append(Entry(document_id(end), "revaluation_reversal", following, tuple(
                    PostedLine(ln.account, ln.currency, money.negate(ln.amount), money.negate(ln.base_amount),
                               ln.party) for ln in lines)))
        year, month = (year + 1, 1) if month == 12 else (year, month + 1)
    return out


def items(scenario: Scenario, flagged: set[str], entries: Iterable[Entry], books: dict[str, Any], end: date) -> list[Item]:
    """The open items on the flagged accounts at the end of ``end`` (README, "What a month's revaluation reads")."""
    assert scenario.settings is not None
    base = scenario.base_currency
    balances: dict[Key, tuple[Decimal, Decimal]] = {}
    for entry in entries:
        if entry.posting_date > end:
            continue
        for ln in entry.lines:
            if ln.account in flagged and ln.currency != base:
                key = (ln.account, ln.currency, ln.party)
                amount, carrying = balances.get(key, (money.ZERO, money.ZERO))
                balances[key] = (money.EXACT.add(amount, ln.amount), money.EXACT.add(carrying, ln.base_amount))
    controls = {scenario.settings.accounts.get("receivable"), scenario.settings.accounts.get("payable")} - {None}
    found: list[Item] = []
    for (account, currency, person), (amount, carrying) in sorted(balances.items(), key=lambda kv: (
            kv[0][0], kv[0][1], kv[0][2] or "")):
        if account in controls:
            for doc in scenario.documents:
                item = books.get(doc.id)
                if not isinstance(item, OpenItem) or item.type not in SIDE or doc.date > end:
                    continue
                if item.control != account or item.currency != currency:
                    continue
                left, left_base = item.open_on(end)
                side = Decimal(SIDE[item.type])
                left, left_base = money.EXACT.multiply(left, side), money.EXACT.multiply(left_base, side)
                found.append((account, currency, None, left, left_base))
                amount = money.EXACT.subtract(amount, left)  # what the account holds beyond its documents
                carrying = money.EXACT.subtract(carrying, left_base)
        found.append((account, currency, person, amount, carrying))
    return [item for item in found if not (item[3].is_zero() and item[4].is_zero())]


def _month(scenario: Scenario, flagged: set[str], entries: list[Entry], books: dict[str, Any],
           end: date) -> tuple[PostedLine, ...]:
    """The month's revaluation entry in canonical form, or nothing (no open item, a missing rate, no change)."""
    assert scenario.settings is not None
    open_items = items(scenario, flagged, entries, books, end)
    rates = {r.currency: r.rate for r in scenario.settings.period_end_rates if r.day == end}
    if any(currency not in rates for _, currency, _, _, _ in open_items):
        return ()  # the run waits for every currency's rate for the period's last day (rev 0.75)
    base_minor = scenario.currencies[scenario.base_currency]
    lines = []
    for account, currency, person, amount, carrying in open_items:
        change = money.EXACT.subtract(money.rnd_fx(amount, rates[currency], base_minor), carrying)
        if not change.is_zero():
            lines.append(PostedLine(account, currency, money.ZERO, change, person))
    if not lines:
        return ()
    unrealised = scenario.settings.accounts["unrealised_fx"]
    gain = money.total([ln.base_amount for ln in lines])
    lines.append(PostedLine(unrealised, scenario.base_currency, money.negate(gain), money.negate(gain)))
    return canonical(lines)
