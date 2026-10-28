"""Bank, card and cash lines and transfers between money accounts (``bank_transaction@1``,
``bank_transfer@1``), from PRD-001 Appendix B.1, §10.2 and the rows of rev 0.140 and 0.141.

**A bank line** (``bank_transaction``) is categorised into one or more parts:

* the money account takes the line's signed gross (money in positive), ``RND-FX`` at the line's rate
  when it is in another currency;
* each part, in the line's direction and never on a money account (that is a transfer, FR-BNK-09) or
  on accounts receivable or payable (that is a match, FR-BNK-07), is converted on its own (``RND-FX``);
  2200 takes −GST, where GST = ``RND-GST-INCL`` of the part's base amount when the organisation is
  registered on the line's date and the code carries GST; the part's account takes the rest, −(base −
  GST), in the base currency (the oracle's reading: "the rest" is a base amount);
* a part spread over n months (an expense on money out, revenue on money in) puts the rest on 1210
  (money out) or 2400 (money in) instead, released to the part's account monthly from the posting
  month under ``ALLOC-EVEN`` (B.1: a bank line has no service period);
* the cent ``RND-FX`` leaves between the parts and the line goes to 7030.

**A transfer** (``bank_transfer``) is recorded once: Dr the receiving account with what it received,
Cr the paying account with what it paid. Across currencies each leg is at its own currency's rate for
the day (``RND-FX``); a fee the provider reports, in the paying currency and part of what was paid,
goes to the bank charges account (a setting, 6460 by default); whatever difference is left goes to
7030 (the owner's decision of 25 Oct 2026, rev 0.140; the accountant confirms it). In one currency
what was paid must be what was received plus the fee.
"""

from __future__ import annotations

from typing import Any

from oracle import money
from oracle.book import Posting, Rejected
from oracle.rules import Rules, month_start
from oracle.scenario import Source


def transaction(r: Rules, doc: Source, books: dict[str, Any]) -> Posting:
    d = doc.data
    currency = d["currency"]
    amount = r.amount(d["amount"], currency)
    sign = 1 if amount > 0 else -1
    rate = r.rate(d, currency)
    r.money_account(d["account"], currency)
    if not d["parts"]:
        raise Rejected("no-parts")

    parts = []
    for part in d["parts"]:
        value = r.amount(part["amount"], currency, sign=sign)  # each part has the line's direction
        account = r.account(part["account"])
        if part["account"] in r.settings.money_accounts:
            raise Rejected("part-on-money-account")  # a transfer (FR-BNK-09)
        if r.is_system(part["account"], "receivable", "payable"):
            raise Rejected("part-on-receivable-or-payable")  # a match (FR-BNK-07)
        percent = r.gst_percent(part["gst_code"], doc.date)
        months = r.spread_months(part["spread_months"]) if "spread_months" in part else None
        if months is not None and account.type != ("expense" if sign < 0 else "revenue"):
            raise Rejected("spread-not-p-and-l")  # B.1: P&L category only, 1210 out / 2400 in
        parts.append((part["account"], value, percent, months))
    if money.total([value for _, value, _, _ in parts]) != amount:
        raise Rejected("parts-do-not-add-up")

    lines = [r.line(d["account"], currency, amount, r.fx(amount, rate))]
    scheduled = []
    for account, value, percent, months in parts:
        base = r.fx(value, rate)
        gst = money.rnd_gst_incl(base, percent, r.base_minor) if not percent.is_zero() else money.ZERO
        rest = money.EXACT.subtract(base, gst)
        if not gst.is_zero():
            lines.append(r.base_line(r.system("gst"), money.negate(gst)))
        if months is None:
            lines.append(r.base_line(account, money.negate(rest)))
            continue
        deferral = r.system("prepayments" if sign < 0 else "deferred_revenue")
        lines.append(r.base_line(deferral, money.negate(rest)))
        scheduled += r.releases(money.negate(rest), months, month_start(doc.date), doc.date, account, deferral)

    leftover = money.total([ln.base_amount for ln in lines])
    if not leftover.is_zero():  # only across currencies: RND-FX of the parts against the line's
        lines.append(r.balance(r.system("fx"), lines))
    return Posting(lines, scheduled)


def transfer(r: Rules, doc: Source, books: dict[str, Any]) -> Posting:
    d = doc.data
    paying, receiving = d["from"], d["to"]
    paid_in, received_in = paying["currency"], receiving["currency"]
    paid = r.amount(paying["amount"], paid_in, sign=1)
    received = r.amount(receiving["amount"], received_in, sign=1)
    paid_rate = r.rate(paying, paid_in)
    received_rate = r.rate(receiving, received_in)
    r.money_account(paying["account"], paid_in)
    r.money_account(receiving["account"], received_in)
    if paying["account"] == receiving["account"]:
        raise Rejected("transfer-to-itself")
    fee = r.amount(d["fee"], paid_in, sign=1) if "fee" in d else None
    if fee is not None and fee >= paid:
        raise Rejected("fee-not-below-paid")
    if paid_in == received_in and paid != money.EXACT.add(received, fee or money.ZERO):
        raise Rejected("transfer-does-not-balance")

    lines = [
        r.line(receiving["account"], received_in, received, r.fx(received, received_rate)),
        r.line(paying["account"], paid_in, money.negate(paid), money.negate(r.fx(paid, paid_rate))),
    ]
    if fee is not None:
        lines.append(r.line(r.system("bank_charges"), paid_in, fee, r.fx(fee, paid_rate)))
    if not money.total([ln.base_amount for ln in lines]).is_zero():
        lines.append(r.balance(r.system("fx"), lines))  # what is left of the conversion
    return Posting(lines)
