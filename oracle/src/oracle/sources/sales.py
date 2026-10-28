"""Invoices, credit notes, customer receipts, deposits, deposit refunds and write-offs (``invoice@1``,
``credit_note@1``, ``customer_receipt@1``, ``customer_deposit@1``, ``deposit_refund@1``,
``write_off@1``) and the releases of an invoice's spread lines, from PRD-001 §10.2 ("Invoice",
"Customer payment", "Customer refund; deposit received and applied", "Bad-debt write-off"), Appendix
B.2, ENG-42, FR-AR-03, FR-AR-04, FR-AR-09, FR-AR-17, FR-AR-18, FR-GL-03 and the rows of rev 0.116 to
0.128.

**An invoice** has a currency (and its rate) and lines, each with its account, its net (an amount, or
``RND-LINE`` of quantity × unit price less a discount) and a GST code. It posts:

* each line's net at ``RND-FX`` to its account, or, spread over n months (a revenue line), to 2400 in
  the base currency, released to the account monthly from the month its service starts;
* 2200 with the output GST, when registered on its date: ``RND-GST-EXCL`` on the lines' base nets per
  GST code (per subtotal, or per line as the setting says), in the base currency (ENG-42: the nets in
  SGD, then ``RND-GST-EXCL``);
* 1100 with the customer's total in the invoice's currency, its nets and ``RND-GST-EXCL`` on them in
  that currency, at ``RND-FX``;
* 7030 with the cents between the two (rev 0.116).

An invoice may **deduct customer deposits** (rev 0.127): for each, the amount it takes (in the
deposit's currency, the invoice's), and, under ``RND-DEPOSIT``, its part of the deposit's base amount
and of the GST the deposit bore. 2410 takes the base part less the GST part; the output GST line is the
GST charged less the GST parts; 1100 takes the total less what the deposits take, at the invoice's rate
(no line when nothing is left); 7030 balances (the deposits' parts against what they take at the
invoice's rate, and the conversion's cents).

**A credit note** posts the legs of an invoice of the same content, negated (rev 0.123); never spread,
it deducts no deposit. Linked to an invoice (``invoice``), it adjusts GST at the rate and registration
of that invoice's date and is in its currency, dated on or after it; the credit notes linked to one
invoice credit at most its total together (rev 0.155, INVOICE-15).

**A customer receipt** settles posted invoices on 1100 (:mod:`oracle.sources.settle`, rev 0.120 and
0.121). **A customer deposit** (rev 0.126) is money in: the money account takes the gross at ``RND-FX``;
a part payment bears GST, ``RND-GST-INCL`` of the gross in the base currency when registered and the
code carries it, to 2200; 2410 takes the rest, in the base currency. A security deposit bears no GST
and names no code. **A deposit refund** (rev 0.128) pays back what is left of a deposit: 2410 takes its
base part less its GST part, 2200 the GST part, the money account what left it, and 7030 the realised
FX. **A write-off** (rev 0.122) clears an amount of an invoice from 1100 at its stored rate (all that is
left of its line when it takes all that is open) to the reason's account, in the base currency.
"""

from __future__ import annotations

from dataclasses import dataclass
from datetime import date
from decimal import Decimal
from typing import Any

from oracle import money
from oracle.book import Posting, Rejected
from oracle.rules import Rules, month_start
from oracle.scenario import Source
from oracle.sources.purchases import line_net
from oracle.sources.settle import OpenItem, settle


@dataclass
class Deposit:
    """A posted customer deposit and what is left of it (rev 0.126, RND-DEPOSIT)."""

    currency: str
    left: Decimal  # in its currency
    left_base: Decimal  # of its base amount (the gross at its rate)
    left_gst: Decimal  # of the GST it bore, in the base currency


def _invoice_lines(r: Rules, doc: Source, books: dict[str, Any], *, credit: bool) -> Posting:
    d = doc.data
    currency = d["currency"]
    minor = r.minor(currency)
    rate = r.rate(d, currency)
    if not d["lines"]:
        raise Rejected("no-lines")
    tax_day = doc.date
    if credit and "invoice" in d:
        invoice = books.get(d["invoice"])
        if not isinstance(invoice, OpenItem) or invoice.type != "invoice":
            raise Rejected("not-a-posted-invoice")
        if invoice.currency != currency:
            raise Rejected("document-currency")
        if invoice.date > doc.date:
            raise Rejected("credit-before-invoice")
        tax_day = invoice.date  # the supply it corrects (FR-AR-04, rev 0.123) [verify]

    lines, scheduled = [], []
    taxed_base: list[tuple[str, Decimal]] = []
    taxed: list[tuple[str, Decimal]] = []
    percents = set()
    nets = []
    for raw in d["lines"]:
        account = r.account(raw["account"])
        net = line_net(r, raw, currency, minor, discount_allowed=True)
        percent = r.gst_percent(raw["gst_code"], tax_day)
        net_base = r.fx(net, rate)
        if not percent.is_zero():
            percents.add(percent)
            taxed_base.append((raw["gst_code"], net_base))
            taxed.append((raw["gst_code"], net))
        if "spread_months" in raw:
            if credit:
                raise Rejected("spread-on-credit")
            months = r.spread_months(raw["spread_months"])
            if account.type != "revenue":
                raise Rejected("spread-not-revenue")
            deferred = r.system("deferred_revenue")
            start = month_start(date.fromisoformat(raw["spread_from"]) if "spread_from" in raw else doc.date)
            lines.append(r.base_line(deferred, money.negate(net_base)))
            scheduled += r.releases(money.negate(net_base), months, start, doc.date, raw["account"], deferred)
        else:
            if "spread_from" in raw:
                raise Rejected("spread-from-without-months")
            lines.append(r.line(raw["account"], currency, money.negate(net), money.negate(net_base)))
        nets.append(net)

    per_line = r.settings.gst.per_line
    gst_base = gst = money.ZERO
    if percents:
        (percent,) = percents  # one tax point, one rate
        gst_base = money.total(list(money.rnd_gst_excl(taxed_base, percent, r.base_minor, per_line).values()))
        gst = money.total(list(money.rnd_gst_excl(taxed, percent, minor, per_line).values()))
    total = money.EXACT.add(money.total(nets), gst)
    if not total > 0:
        raise Rejected("total-not-positive")

    # Deposits the invoice deducts (rev 0.127).
    takes: list[tuple[Deposit, Decimal, Decimal, Decimal]] = []  # (deposit, used, base part, GST part)
    seen: set[str] = set()
    for use in d.get("deposits", ()):
        if credit:
            raise Rejected("deposit-on-credit")
        deposit = books.get(use["deposit"])
        if not isinstance(deposit, Deposit):
            raise Rejected("not-a-posted-deposit")
        if use["deposit"] in seen:
            raise Rejected("deposit-twice")
        seen.add(use["deposit"])
        if deposit.currency != currency:
            raise Rejected("deposit-currency")
        used = r.amount(use["amount"], currency, sign=1)
        if used > deposit.left:
            raise Rejected("more-than-left-of-deposit")  # INVOICE-16
        takes.append((deposit, used, money.rnd_deposit(deposit.left_base, used, deposit.left, r.base_minor),
                      money.rnd_deposit(deposit.left_gst, used, deposit.left, r.base_minor)))
    taken = money.total([used for _, used, _, _ in takes])
    if taken > total:
        raise Rejected("deposits-above-total")  # LEDGER-33
    gst_borne = money.total([gst_part for *_, gst_part in takes])

    output = money.EXACT.subtract(gst_base, gst_borne)
    if not output.is_zero():
        lines.append(r.base_line(r.system("gst"), money.negate(output)))
    for _, _, base_part, gst_part in takes:
        lines.append(r.base_line(r.system("customer_deposits"), money.EXACT.subtract(base_part, gst_part)))
    receivable = r.system("receivable")
    owed = money.EXACT.subtract(total, taken)
    owed_base = r.fx(owed, rate)
    lines.append(r.line(receivable, currency, owed, owed_base))
    if not money.total([ln.base_amount for ln in lines]).is_zero():
        lines.append(r.balance(r.system("fx"), lines))

    def apply() -> None:
        for deposit, used, base_part, gst_part in takes:
            deposit.left = money.EXACT.subtract(deposit.left, used)
            deposit.left_base = money.EXACT.subtract(deposit.left_base, base_part)
            deposit.left_gst = money.EXACT.subtract(deposit.left_gst, gst_part)

    item = OpenItem("credit_note" if credit else "invoice", receivable, currency, rate, owed, owed_base,
                    date=doc.date, gross=total)
    return Posting(lines, scheduled, facts=item, effects=[apply])


def invoice(r: Rules, doc: Source, books: dict[str, Any]) -> Posting:
    return _invoice_lines(r, doc, books, credit=False)


def credit_note(r: Rules, doc: Source, books: dict[str, Any]) -> Posting:
    posting = _invoice_lines(r, doc, books, credit=True)
    if "invoice" in doc.data:
        linked = books[doc.data["invoice"]]
        credited = money.EXACT.add(linked.credited, posting.facts.gross)
        if credited > linked.gross:
            raise Rejected("credit-above-invoice")  # INVOICE-15: the linked credit notes together (rev 0.155)

        def credit() -> None:
            linked.credited = credited

        posting.effects.append(credit)
    posting.lines = [
        r.line(ln.account, ln.currency, money.negate(ln.amount), money.negate(ln.base_amount)) for ln in posting.lines
    ]
    return posting


def customer_receipt(r: Rules, doc: Source, books: dict[str, Any]) -> Posting:
    return settle(r, doc, books, settles="invoice", sign=-1)


def customer_deposit(r: Rules, doc: Source, books: dict[str, Any]) -> Posting:
    d = doc.data
    currency = d["currency"]
    amount = r.amount(d["amount"], currency, sign=1)
    rate = r.rate(d, currency)
    r.money_account(d["account"], currency)
    kind = d["kind"]
    if kind == "security":
        if "gst_code" in d:
            raise Rejected("gst-code-on-security-deposit")  # DEPOSIT-05
        percent = money.ZERO
    else:
        if "gst_code" not in d:
            raise Rejected("no-gst-code")
        percent = r.gst_percent(d["gst_code"], doc.date)
    base = r.fx(amount, rate)
    gst = money.rnd_gst_incl(base, percent, r.base_minor) if not percent.is_zero() else money.ZERO
    lines = [r.line(d["account"], currency, amount, base)]
    if not gst.is_zero():
        lines.append(r.base_line(r.system("gst"), money.negate(gst)))
    lines.append(r.base_line(r.system("customer_deposits"), money.negate(money.EXACT.subtract(base, gst))))
    return Posting(lines, facts=Deposit(currency, amount, base, gst))


def deposit_refund(r: Rules, doc: Source, books: dict[str, Any]) -> Posting:
    d = doc.data
    deposit = books.get(d["deposit"])
    if not isinstance(deposit, Deposit):
        raise Rejected("not-a-posted-deposit")
    currency = deposit.currency
    r.money_account(d["account"], currency)
    amount = r.amount(d["amount"], currency, sign=1)
    if amount > deposit.left:
        raise Rejected("more-than-left-of-deposit")  # APPLY-05
    rate = r.rate(d, currency)
    base_part = money.rnd_deposit(deposit.left_base, amount, deposit.left, r.base_minor)
    gst_part = money.rnd_deposit(deposit.left_gst, amount, deposit.left, r.base_minor)
    lines = [
        r.base_line(r.system("customer_deposits"), money.EXACT.subtract(base_part, gst_part)),
        r.line(d["account"], currency, money.negate(amount), money.negate(r.fx(amount, rate))),
    ]
    if not gst_part.is_zero():
        lines.append(r.base_line(r.system("gst"), gst_part))
    if not money.total([ln.base_amount for ln in lines]).is_zero():
        lines.append(r.balance(r.system("fx"), lines))

    def apply() -> None:
        deposit.left = money.EXACT.subtract(deposit.left, amount)
        deposit.left_base = money.EXACT.subtract(deposit.left_base, base_part)
        deposit.left_gst = money.EXACT.subtract(deposit.left_gst, gst_part)

    return Posting(lines, effects=[apply])


def write_off(r: Rules, doc: Source, books: dict[str, Any]) -> Posting:
    d = doc.data
    item = books.get(d["invoice"])
    if not isinstance(item, OpenItem) or item.type != "invoice":
        raise Rejected("not-a-posted-invoice")
    amount = r.amount(d["amount"], item.currency, sign=1)
    if amount > item.open:
        raise Rejected("more-than-open")  # WRITEOFF-04
    r.account(d["account"])
    carrying = item.open_base if amount == item.open else r.fx(amount, item.rate)
    lines = [
        r.line(item.control, item.currency, money.negate(amount), money.negate(carrying)),
        r.base_line(d["account"], carrying),
    ]

    def apply() -> None:
        item.settled = money.EXACT.add(item.settled, amount)
        item.settled_base = money.EXACT.add(item.settled_base, carrying)

    return Posting(lines, effects=[apply])
