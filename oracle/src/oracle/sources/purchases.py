"""Bills, vendor credits and payments of bills (``bill@1``, ``vendor_credit@1``, ``bill_payment@1``)
and the releases of a bill's spread lines, from PRD-001 §10.2 ("Bill", "Supplier payment"), Appendix
B.2, ENG-42, FR-AP-02, FR-AP-03, FR-AP-19, FR-AP-20, FR-GL-03 and the rows of rev 0.76 to 0.93.

**A bill** has a currency (and its rate), and lines, each with its account, its net (an amount, or a
quantity × unit price under ``RND-LINE``, with the quantity billed before for a purchase order's
later part), a GST code and the GST its tax invoice states (in the bill's currency; for a bill in
another currency, also the GST stated in the base currency, ``gst_base``). It posts:

* each line's **cost**, its net plus whatever GST is not claimed, at ``RND-FX`` to its account, or,
  spread over n months (an expense line only), to 1210 in the base currency, released to the account
  monthly from the month its service starts (by default the bill's) under ``ALLOC-EVEN``;
* 2200 with the input tax claimed: a line's GST is claimable when its code has an input-tax box (TX,
  IM) and the organisation is registered on the bill's date (P1's form of FR-AP-20); of it the line's
  business share is claimed (``RND-GST-SHARE``, 100 by default). A base-currency bill claims its stated
  GST; a bill in another currency claims the share of ``gst_base`` (ENG-42, never a recomputation at the
  book rate), its cost keeping the share's remainder of its own GST, and with no ``gst_base`` claims
  nothing (the GST stays in the cost);
* 2010 with the total (the nets and the stated GST) at ``RND-FX``, less what is withheld;
* withholding tax (FR-AP-19, ``RND-WHT``) when the bill says so: 2330 takes round(⌊net total in the base
  currency⌋ × rate ÷ 100); 2010 is the total less the supplier's own amount withheld (round(net × rate
  ÷ 100) in a bill's other currency);
* 7030 with what balances the entry in the base currency (rev 0.74: net + stated GST − gross, and the
  cents ``RND-FX`` leaves).

**A vendor credit** posts the legs of a bill of the same content, every amount and base amount negated
(rev 0.80); it is never spread and withholds nothing. **A payment of bills** settles posted bills
(:mod:`oracle.sources.settle`) on 2010.
"""

from __future__ import annotations

from datetime import date
from decimal import Decimal
from typing import Any

from oracle import money
from oracle.book import Posting, Rejected
from oracle.rules import Rules, month_start
from oracle.scenario import Source
from oracle.sources.settle import OpenItem, settle

CLAIMABLE = frozenset({"TX", "IM"})  # the codes with an input-tax box (Appendix B.6, rev 0.76)


def line_net(r: Rules, line: Any, currency: str, minor: int, *, discount_allowed: bool) -> Decimal:
    """A document line's net in its currency: its amount, or RND-LINE of quantity × unit price."""
    has_amount = "amount" in line
    has_quantity = "quantity" in line or "unit_price" in line
    if has_amount == has_quantity:
        raise Rejected("amount-or-quantity")
    if has_amount:
        if "discount" in line or "quantity_before" in line:
            raise Rejected("amount-or-quantity")
        return r.amount(line["amount"], currency)
    if "quantity" not in line or "unit_price" not in line:
        raise Rejected("amount-or-quantity")
    quantity = r.quantity(line["quantity"])
    price = r.quantity(line["unit_price"])
    if "discount" in line:
        if not discount_allowed:
            raise Rejected("discount-not-allowed")
        discount = r.percent(line["discount"])
        net = money.rnd_line(quantity, price, minor, discount)
    elif "quantity_before" in line:
        net = money.rnd_line_part(r.quantity(line["quantity_before"], zero=True), quantity, price, minor)
    else:
        net = money.rnd_line(quantity, price, minor)
    if net.is_zero():
        raise Rejected("zero-amount")
    return net


def _bill_lines(r: Rules, doc: Source, *, credit: bool) -> Posting:
    d = doc.data
    currency = d["currency"]
    minor = r.minor(currency)
    rate = r.rate(d, currency)
    foreign = currency != r.base
    if not d["lines"]:
        raise Rejected("no-lines")
    registered = r.registered(doc.date)

    lines, scheduled = [], []
    nets, gsts = [], []
    for raw in d["lines"]:
        account = r.account(raw["account"])
        net = line_net(r, raw, currency, minor, discount_allowed=False)
        code = r.gst_code(raw["gst_code"])
        gst = r.amount(raw.get("gst", "0"), currency, zero=True)
        share = r.percent(raw.get("business_share", "100"))
        if not foreign and "gst_base" in raw:
            raise Rejected("gst-base-on-base-bill")
        claimable = code in CLAIMABLE and registered and not gst.is_zero()
        if foreign:
            stated = r.amount(raw["gst_base"], r.base, zero=True) if "gst_base" in raw else None
            if claimable and stated is not None:
                claim = money.rnd_gst_share(stated, share, r.base_minor)
                kept = money.EXACT.subtract(gst, money.rnd_gst_share(gst, share, minor))
            else:
                claim, kept = money.ZERO, gst
        else:
            claim = money.rnd_gst_share(gst, share, r.base_minor) if claimable else money.ZERO
            kept = money.EXACT.subtract(gst, claim)
        cost = money.EXACT.add(net, kept)
        cost_base = r.fx(cost, rate)
        if not claim.is_zero():
            lines.append(r.base_line(r.system("gst"), claim))
        if "spread_months" in raw:
            if credit:
                raise Rejected("spread-on-credit")  # LEDGER-28
            months = r.spread_months(raw["spread_months"])
            if account.type != "expense" or not cost_base > 0:
                raise Rejected("spread-not-an-expense")  # LEDGER-28: an expense line with a cost
            start = month_start(date.fromisoformat(raw["spread_from"]) if "spread_from" in raw else doc.date)
            prepayments = r.system("prepayments")
            lines.append(r.base_line(prepayments, cost_base))
            scheduled += r.releases(cost_base, months, start, doc.date, raw["account"], prepayments)
        else:
            if "spread_from" in raw:
                raise Rejected("spread-from-without-months")
            lines.append(r.line(raw["account"], currency, cost, cost_base))
        nets.append(net)
        gsts.append(gst)

    total = money.total(nets + gsts)
    if not total > 0:
        raise Rejected("total-not-positive")
    owed = total
    if "withholding" in d:
        if credit:
            raise Rejected("withholding-on-credit")
        percent = r.percent(d["withholding"]["percent"])
        net_total = money.total(nets)
        tax = money.rnd_wht(r.fx(net_total, rate), percent, r.base_minor)
        withheld = tax if not foreign else money.rnd_withheld(net_total, percent, minor)
        owed = money.EXACT.subtract(total, withheld)
        lines.append(r.base_line(r.system("withholding_tax"), money.negate(tax)))
    payable = r.system("payable")
    ap_base = r.fx(owed, rate)
    lines.append(r.line(payable, currency, money.negate(owed), money.negate(ap_base)))
    if not money.total([ln.base_amount for ln in lines]).is_zero():
        lines.append(r.balance(r.system("fx"), lines))
    item = OpenItem("vendor_credit" if credit else "bill", payable, currency, rate, owed, ap_base)
    return Posting(lines, scheduled, facts=item)


def bill(r: Rules, doc: Source, books: dict[str, Any]) -> Posting:
    return _bill_lines(r, doc, credit=False)


def vendor_credit(r: Rules, doc: Source, books: dict[str, Any]) -> Posting:
    posting = _bill_lines(r, doc, credit=True)
    posting.lines = [
        r.line(ln.account, ln.currency, money.negate(ln.amount), money.negate(ln.base_amount)) for ln in posting.lines
    ]
    return posting


def bill_payment(r: Rules, doc: Source, books: dict[str, Any]) -> Posting:
    return settle(r, doc, books, settles="bill", sign=1)
