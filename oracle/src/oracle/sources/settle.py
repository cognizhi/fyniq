"""Settling posted documents with money: payments of bills, customer receipts and claim
reimbursements, from PRD-001 §10.2 ("Customer payment", "Supplier payment", "Settlement difference
within tolerance"), Appendix B.1 and B.2, FR-BNK-08, FR-AR-18, ENG-41 and the rows of rev 0.77,
0.120 and 0.121.

A settlement names a money account, the documents' currency and what it settles of each document
(its *parts*), and may keep what it paid beyond them as a credit (``unapplied``, FR-AR-18). It posts:

* the control account (2010 for bills, 1100 for invoices, 2150 for claims) with each part at the
  document's **stored rate** (``RND-FX``); the part that settles what is left of a document clears all
  that is left of its control line, so the ``RND-FX`` of its parts add up to it exactly (the residual
  line of rev 0.77, at most S$1.00);
* an unapplied amount on the control account at the settlement's rate (a credit);
* the money account with what it paid or received: the total at the settlement's rate when the
  account is in the documents' currency, or, for a base-currency account and documents in another
  currency, what it paid (``base_amount``) or the total at the given rate (the cascade's, for a claim);
* 7030 with what balances the entry in the base currency: each part's realised FX (ENG-41), the
  ``RND-FX`` cents between the lines and the difference of a part recorded in full.

A part recorded **in full** (rev 0.121) settles all that is open of its document though it pays less;
its difference, ``RND-FX`` of what is open less ``RND-FX`` of what is paid at the stored rate, must be
within the tolerances on the settlement's date: S$1.00 for a base-currency document, else the greater
of S$1.00 and 3% of the document's total at its stored rate; at most S$50.00 a document; and, with the
month's earlier differences, at most S$200.00 a month (the defaults of open question 32; the settings
``tolerance``). Each is compared exactly; a larger difference is refused (a write-off instead).
"""

from __future__ import annotations

from dataclasses import dataclass
from datetime import date
from decimal import Decimal
from typing import Any

from oracle import money
from oracle.book import Posting, Rejected
from oracle.rules import Rules
from oracle.scenario import Source

DEFAULT_TOLERANCE = {"base": "1.00", "percent": "3", "per_document": "50.00", "per_month": "200.00"}
_MONTH_DIFFERENCES = "_tolerance_by_month"  # the books' key for the month's differences so far


@dataclass
class OpenItem:
    """A posted document that settlements settle (FR-AR-18): its control line and what is left."""

    type: str
    control: str
    currency: str
    rate: Decimal | None
    total: Decimal  # what it owes in its currency, positive
    carrying: Decimal  # its control line's base amount, positive
    settled: Decimal = money.ZERO
    settled_base: Decimal = money.ZERO
    date: date | None = None  # an invoice's, for a credit note linked to it
    gross: Decimal | None = None  # an invoice's total before the deposits it deducts

    @property
    def open(self) -> Decimal:
        return money.EXACT.subtract(self.total, self.settled)

    @property
    def open_base(self) -> Decimal:
        return money.EXACT.subtract(self.carrying, self.settled_base)


def _tolerance(r: Rules) -> dict[str, Decimal]:
    raw = {**DEFAULT_TOLERANCE, **dict(r.settings.data.get("tolerance", {}))}
    return {key: Decimal(value) for key, value in raw.items()}


def settle(
    r: Rules,
    doc: Source,
    books: dict[str, Any],
    *,
    settles: str,
    sign: int,
    in_full_allowed: bool = True,
    unapplied_allowed: bool = True,
) -> Posting:
    """Post a settlement of documents of type ``settles``; ``sign`` is +1 when the control account is
    debited (paying bills or claims) and -1 when it is credited (receiving for invoices)."""
    d = doc.data
    currency = d["currency"]
    r.minor(currency)
    held = r.money_account(d["account"])
    if held not in (currency, r.base):
        raise Rejected("money-account-currency")
    base_account_for_foreign = held == r.base and currency != r.base
    rate: Decimal | None = None
    base_paid: Decimal | None = None
    if base_account_for_foreign:
        if ("base_amount" in d) == ("fx_rate" in d):
            raise Rejected("base-amount-or-rate")  # what the account paid, or the rate to convert at
        if "base_amount" in d:
            base_paid = r.amount(d["base_amount"], r.base, sign=1)
        else:
            rate = r.rate(d, currency)
    else:
        if "base_amount" in d:
            raise Rejected("base-amount-not-needed")
        rate = r.rate(d, currency)

    unapplied = money.ZERO
    if "unapplied" in d:
        if not unapplied_allowed:
            raise Rejected("unapplied-not-allowed")
        unapplied = r.amount(d["unapplied"], currency, sign=1)
    if not d["parts"] and unapplied.is_zero():
        raise Rejected("nothing-settled")

    tolerance = _tolerance(r)
    month = doc.date.strftime("%Y-%m")
    month_so_far = books.get(_MONTH_DIFFERENCES, {}).get(month, money.ZERO)
    parts: list[tuple[OpenItem, Decimal, Decimal, Decimal]] = []  # (item, settled, its carrying, paid)
    seen: set[str] = set()
    differences = money.ZERO
    for part in d["parts"]:
        item = books.get(part["document"])
        if not isinstance(item, OpenItem) or item.type != settles:
            raise Rejected(f"not-a-posted-{settles.replace('_', '-')}")
        if part["document"] in seen:
            raise Rejected("document-twice")
        seen.add(part["document"])
        if item.currency != currency:
            raise Rejected("document-currency")
        amount = r.amount(part["amount"], currency, sign=1)
        if amount > item.open:
            raise Rejected("more-than-open")
        in_full = part.get("in_full", False)
        if in_full and not in_full_allowed:
            raise Rejected("in-full-not-allowed")
        if in_full or amount == item.open:
            settled, carrying = item.open, item.open_base  # what is left, residual included
        else:
            settled, carrying = amount, r.fx(amount, item.rate)
        if in_full and amount != item.open:
            difference = money.EXACT.subtract(r.fx(item.open, item.rate), r.fx(amount, item.rate))
            limit = tolerance["base"]
            if item.currency != r.base:
                limit = max(limit, money.EXACT.divide(money.EXACT.multiply(item.carrying, tolerance["percent"]),
                                                      money.HUNDRED))
            month_total = money.EXACT.add(money.EXACT.add(month_so_far, differences), difference.copy_abs())
            if difference.copy_abs() > limit or difference.copy_abs() > tolerance["per_document"] \
                    or month_total > tolerance["per_month"]:
                raise Rejected("beyond-tolerance")
            differences = money.EXACT.add(differences, difference.copy_abs())
        parts.append((item, settled, carrying, amount))

    paid = money.total([amount for *_, amount in parts] + [unapplied])
    if base_paid is not None:
        paid_base = base_paid
        unapplied_rate = money.rnd_rate(base_paid, paid) if not unapplied.is_zero() else None
    else:
        paid_base = r.fx(paid, rate)
        unapplied_rate = rate

    lines = []
    for item, settled, carrying, _ in parts:
        lines.append(r.line(item.control, currency, sign * settled, sign * carrying))
    credit = None
    if not unapplied.is_zero():
        control = r.system(_CONTROL[settles])
        unapplied_base = r.fx(unapplied, unapplied_rate)
        lines.append(r.line(control, currency, sign * unapplied, sign * unapplied_base))
        credit = OpenItem(_CREDIT[settles], control, currency, unapplied_rate, unapplied, unapplied_base)
    money_currency = r.base if base_account_for_foreign else currency
    money_amount = paid_base if base_account_for_foreign else paid
    lines.append(r.line(d["account"], money_currency, -sign * money_amount, -sign * paid_base))
    if not money.total([ln.base_amount for ln in lines]).is_zero():
        lines.append(r.balance(r.system("fx"), lines))

    def apply() -> None:
        for item, settled, carrying, _ in parts:
            item.settled = money.EXACT.add(item.settled, settled)
            item.settled_base = money.EXACT.add(item.settled_base, carrying)
        if not differences.is_zero():
            by_month = books.setdefault(_MONTH_DIFFERENCES, {})
            by_month[month] = money.EXACT.add(by_month.get(month, money.ZERO), differences)

    return Posting(lines, facts=credit, effects=[apply])


_CONTROL = {"bill": "payable", "invoice": "receivable", "expense_claim": "claims_payable"}
_CREDIT = {"bill": "supplier_credit", "invoice": "customer_credit", "expense_claim": "claim_credit"}
