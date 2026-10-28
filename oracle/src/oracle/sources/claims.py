"""Expense claims, their reimbursements and cash advances (``expense_claim@1``,
``claim_reimbursement@1``, ``cash_advance@1``) and ``RND-MILEAGE``, from PRD-001 §10.2 ("Expense
claim", "Mileage claim", "Cash advance to an employee"), §10.4, FR-AP-07, FR-AP-16, FR-AP-17, FR-AP-20
and the rows of rev 0.147, 0.148 and 0.151.

**A claim** has a currency (and its rate) and lines of two kinds:

* a receipt: its account, net, GST code and the GST stated, costed as a bill's line is (the input tax
  claimable on TX and IM when registered on the claim's date, its business share under
  ``RND-GST-SHARE``; FR-AP-20: "claims and card lines take the same rules");
* a trip (mileage): its account, its ``distance`` in km and its ``vehicle``; the amount is
  ``RND-MILEAGE`` of the distance at the organisation's rate per km for the vehicle in force on the
  claim's date (a policy setting, effective-dated), with no input GST (no tax invoice; reg. 27). A trip
  is in the base currency, so a claim with one is too.

It posts each line's cost to its account at ``RND-FX`` and 2200 the input tax, and credits what the
employee is owed: 1250 first, up to what is left of the cash advance it settles (in the base currency),
then 2150 with the rest at ``RND-FX``; 7030 takes what balances.

**A reimbursement** pays claims from a money account: the settlement of :mod:`oracle.sources.settle` on
2150, never in full and with nothing unapplied; a claim in another currency paid from a base-currency
account is reimbursed in SGD at the given rate (the cascade's), its difference from the claim's rate to
7030 (FR-AP-17).

**A cash advance** is paid from a base-currency money account: Dr 1250, Cr the money account; with
``refund_of`` it is what is left of an advance coming back: Dr the money account, Cr 1250, at most what
is left (FR-AP-17).
"""

from __future__ import annotations

from dataclasses import dataclass
from decimal import Decimal
from typing import Any

from oracle import money
from oracle.book import Posting, Rejected
from oracle.rules import Rules
from oracle.scenario import Source
from oracle.sources.purchases import cost_line
from oracle.sources.settle import OpenItem, settle


@dataclass
class Advance:
    """A cash advance paid to an employee, and what is left of it (FR-AP-17)."""

    left: Decimal


def mileage_rate(r: Rules, vehicle: str, day: Any) -> Decimal:
    rates = [row for row in r.settings.data.get("mileage_rates", ()) if row["vehicle"] == vehicle and
             row["from"] <= day.isoformat()]
    if not rates:
        raise Rejected("no-mileage-rate")
    return Decimal(max(rates, key=lambda row: row["from"])["rate"])


def expense_claim(r: Rules, doc: Source, books: dict[str, Any]) -> Posting:
    d = doc.data
    currency = d["currency"]
    minor = r.minor(currency)
    rate = r.rate(d, currency)
    if not d["lines"]:
        raise Rejected("no-lines")
    registered = r.registered(doc.date)
    lines, owed = [], money.ZERO
    for raw in d["lines"]:
        r.account(raw["account"])
        if "distance" in raw:
            if currency != r.base:
                raise Rejected("mileage-in-another-currency")
            distance = r.quantity(raw["distance"])
            cost = money.rnd_mileage(distance, mileage_rate(r, raw["vehicle"], doc.date), r.base_minor)
            if cost.is_zero():
                raise Rejected("zero-amount")
            lines.append(r.base_line(raw["account"], cost))
            owed = money.EXACT.add(owed, cost)
            continue
        if "vehicle" in raw:
            raise Rejected("vehicle-without-distance")
        net, cost, claim, gst = cost_line(r, raw, currency, minor, registered)
        if not claim.is_zero():
            lines.append(r.base_line(r.system("gst"), claim))
        lines.append(r.line(raw["account"], currency, cost, r.fx(cost, rate)))
        owed = money.EXACT.add(owed, money.EXACT.add(net, gst))
    if not owed > 0:
        raise Rejected("total-not-positive")

    advance, from_advance = None, money.ZERO
    if "advance" in d:
        advance = books.get(d["advance"])
        if not isinstance(advance, Advance):
            raise Rejected("not-a-posted-advance")
        if currency != r.base:
            raise Rejected("advance-currency")
        from_advance = min(owed, advance.left)
        if not from_advance.is_zero():
            lines.append(r.base_line(r.system("advances"), money.negate(from_advance)))
    payable = r.system("claims_payable")
    rest = money.EXACT.subtract(owed, from_advance)
    rest_base = r.fx(rest, rate)
    lines.append(r.line(payable, currency, money.negate(rest), money.negate(rest_base)))
    if not money.total([ln.base_amount for ln in lines]).is_zero():
        lines.append(r.balance(r.system("fx"), lines))

    def apply() -> None:
        if advance is not None:
            advance.left = money.EXACT.subtract(advance.left, from_advance)

    return Posting(lines, facts=OpenItem("expense_claim", payable, currency, rate, rest, rest_base), effects=[apply])


def claim_reimbursement(r: Rules, doc: Source, books: dict[str, Any]) -> Posting:
    return settle(r, doc, books, settles="expense_claim", sign=1, in_full_allowed=False, unapplied_allowed=False)


def cash_advance(r: Rules, doc: Source, books: dict[str, Any]) -> Posting:
    d = doc.data
    r.money_account(d["account"], r.base)
    amount = r.amount(d["amount"], r.base, sign=1)
    advances = r.system("advances")
    if "refund_of" not in d:
        lines = [r.base_line(advances, amount), r.base_line(d["account"], money.negate(amount))]
        return Posting(lines, facts=Advance(amount))
    advance = books.get(d["refund_of"])
    if not isinstance(advance, Advance):
        raise Rejected("not-a-posted-advance")
    if amount > advance.left:
        raise Rejected("more-than-left-of-advance")
    lines = [r.base_line(d["account"], amount), r.base_line(advances, money.negate(amount))]

    def apply() -> None:
        advance.left = money.EXACT.subtract(advance.left, amount)

    return Posting(lines, effects=[apply])
