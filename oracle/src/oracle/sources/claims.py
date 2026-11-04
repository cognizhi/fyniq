"""Expense claims, their reimbursements and cash advances (``expense_claim@1`` and ``@2``,
``claim_reimbursement@1``, ``cash_advance@1``) and ``RND-MILEAGE``, from PRD-001 §10.2 ("Expense
claim", "Mileage claim", "Cash advance to an employee"), §10.4, FR-AP-07, FR-AP-16, FR-AP-17, FR-AP-20,
FR-EQ-02, Appendix B.7, UC-EXP-03 and the rows of rev 0.147, 0.148, 0.151, 0.167 and 0.169.

**A claim** has a currency (and its rate) and lines of two kinds:

* a receipt: its account, net, GST code and the GST stated, costed as a bill's line is (the input tax
  claimable on TX and IM when registered on the claim's date, its business share under
  ``RND-GST-SHARE``; FR-AP-20: "claims and card lines take the same rules");
* a trip (mileage): its account, its ``distance`` in km and its ``vehicle``; the amount is
  ``RND-MILEAGE`` of the distance at the organisation's rate per km for the vehicle in force on the
  claim's date (a policy setting, effective-dated), with no input GST (no tax invoice; reg. 27). A trip
  is in the base currency, so a claim with one is too.

It posts each line's cost to its account in the claim's currency at ``RND-FX`` and 2200 the input tax,
and credits what the employee is owed **in the base currency**: for each receipt line, the base amount the
claimant states they paid for it (``paid``) or else its net plus its stated GST at the claim's rate under
``RND-FX``, line by line, summed, and each trip's amount. Under SFRS(I) 1-21 the company owes the claimant
a fixed SGD amount, what they were out of pocket, so nothing on 2150 is foreign (FR-AP-17, rev 0.155's
"2150 at the claim's rate", rev 0.169's "or the SGD the claimant states they paid, with the difference to
7030 when the claim posts", decided by the owner [accountant to confirm]; superseding 2150 in the claim's
currency). 1250 takes it first, up to what is left of the cash advance it settles (a claim in any
currency: what it owes is in SGD, and UC-EXP-03's IDR receipts settle an SGD advance), then 2150 the
rest; 7030 takes what balances (the ``RND-FX`` cents between a line's parts, the stated GST in base
against the GST at the rate, and what was paid against the line at the rate).

**What the claimant paid** (``paid``, rev 0.169): only on a receipt line of a claim in another currency,
a plain base-currency amount above zero, net and GST together; it changes only what is owed, never the
line's cost (at the claim's rate) or its input tax (the share of ``gst_base``). No tolerance applies: any
``paid`` is owed as stated (README, "P1 readings" [accountant to confirm]). ``paid`` on a base-currency
claim or on a trip, or one that is not a valid base-currency amount or not above zero, refuses the claim.

**A claim kept as a loan** (``loan``: a director or shareholder, one of the scenario's ``people``;
FR-EQ-02, B.7 "Cost a founder paid personally, kept as a loan") credits the director's loan account
(2500) instead of 2150, as 2150 would be credited (in the base currency), naming the person: what the
claimant paid stays owed to them there. It settles no cash advance, and nothing reimburses it (it leaves
no open item on 2150).

**A reimbursement** pays claims in the base currency only, from a base-currency money account: the
settlement of :mod:`oracle.sources.settle` on 2150, at most what is owed, never in full and with nothing
unapplied, with no rate and so no FX (what is owed is already in SGD). One in another currency, or with
``fx_rate``, ``fx_rate_source`` or ``base_amount``, is refused whole. *Superseded:* a claim in another
currency reimbursed in SGD at the cascade's rate, its difference from the claim's rate to 7030.

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
    lines, owed, owed_base = [], money.ZERO, money.ZERO  # owed: in the claim's currency; owed_base: what 2150 takes
    for raw in d["lines"]:
        r.account(raw["account"])
        if "distance" in raw:
            if currency != r.base:
                raise Rejected("mileage-in-another-currency")
            if "paid" in raw:  # rev 0.169: what the claimant paid is a receipt's, in a claim in another currency
                raise Rejected("paid-on-trip")
            distance = r.quantity(raw["distance"])
            cost = money.rnd_mileage(distance, mileage_rate(r, raw["vehicle"], doc.date), r.base_minor)
            if cost.is_zero():
                raise Rejected("zero-amount")
            lines.append(r.base_line(raw["account"], cost))
            owed = money.EXACT.add(owed, cost)
            owed_base = money.EXACT.add(owed_base, cost)
            continue
        if "vehicle" in raw:
            raise Rejected("vehicle-without-distance")
        net, cost, claim, gst = cost_line(r, raw, currency, minor, registered)
        if not claim.is_zero():
            lines.append(r.base_line(r.system("gst"), claim))
        lines.append(r.line(raw["account"], currency, cost, r.fx(cost, rate)))
        owed = money.EXACT.add(owed, money.EXACT.add(net, gst))
        if "paid" in raw:
            # FR-AP-17 rev 0.169: "or the SGD the claimant states they paid", in place of the line at the rate; the
            # difference goes to 7030 with what balances below. A base-currency line's amount is already what was paid.
            if currency == r.base:
                raise Rejected("paid-on-base-claim")
            owed_base = money.EXACT.add(owed_base, r.amount(raw["paid"], r.base, sign=1))
        else:
            owed_base = money.EXACT.add(owed_base, r.fx(money.EXACT.add(net, gst), rate))  # line by line
    if not owed > 0:
        raise Rejected("total-not-positive")

    loan = d.get("loan")
    if loan is not None:
        if loan not in r.scenario.people:
            raise Rejected("loan-not-a-director")
        if "advance" in d:
            raise Rejected("loan-with-advance")
        payable = r.system("director_loan")
        lines.append(r.line(payable, r.base, money.negate(owed_base), money.negate(owed_base), party=loan))
        if not money.total([ln.base_amount for ln in lines]).is_zero():
            lines.append(r.balance(r.system("fx"), lines))
        return Posting(lines)

    advance, from_advance = None, money.ZERO
    if "advance" in d:
        advance = books.get(d["advance"])
        if not isinstance(advance, Advance):
            raise Rejected("not-a-posted-advance")
        # What a claim owes is in SGD whatever its currency (rev 0.169), so a foreign claim settles an SGD advance
        # as UC-EXP-03's IDR receipts do. *Superseded:* refused unless the claim is in the base currency.
        from_advance = min(owed_base, advance.left)
        if not from_advance.is_zero():
            lines.append(r.base_line(r.system("advances"), money.negate(from_advance)))
    payable = r.system("claims_payable")
    rest = money.EXACT.subtract(owed_base, from_advance)
    lines.append(r.base_line(payable, money.negate(rest)))
    if not money.total([ln.base_amount for ln in lines]).is_zero():
        lines.append(r.balance(r.system("fx"), lines))

    def apply() -> None:
        if advance is not None:
            advance.left = money.EXACT.subtract(advance.left, from_advance)

    return Posting(lines, facts=OpenItem("expense_claim", payable, r.base, None, rest, rest), effects=[apply])


def claim_reimbursement(r: Rules, doc: Source, books: dict[str, Any]) -> Posting:
    d = doc.data
    # FR-AP-17 (decided, [accountant to confirm]): what is owed is in SGD, so it is paid in SGD with no rate.
    if d["currency"] != r.base or any(key in d for key in ("fx_rate", "fx_rate_source", "base_amount")):
        raise Rejected("reimbursement-not-in-base")
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
