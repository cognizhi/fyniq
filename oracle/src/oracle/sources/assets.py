"""The fixed asset register's depreciation and disposals (``asset_depreciation@1``,
``asset_disposal@1``), from PRD-001 Appendix B.4, §10.2 ("Depreciation", "Asset disposal"), FR-FA-01,
FR-FA-02, FR-FA-03 and the rows of rev 0.149, 0.150 and 0.151.

**A registered asset** (``fixed_asset``) posts nothing: its cost was posted by the bill or bank line it
came from. It names its cost account (an asset account), its cost and residual value in the base
currency, its useful life in months and, optionally, the day depreciation starts (by default its own
date, the purchase). Straight-line depreciation (the P1 method) is scheduled from it:

* monthly = ``ALLOC-EVEN``(cost − residual, life): month k of the life takes the k-th part;
* the start month is the depreciate-from date's, or the purchase month; the end is start + life − 1,
  or the month before a disposal;
* a month posts from the later of the start and the books' start (earlier depreciation is part of the
  opening balances), dated the month's last day (rev 0.150), once that day is on or before ``as_of``:
  Dr 6610, Cr 1590 (the settings ``depreciation`` and ``accumulated_depreciation``).

**A disposal** (``asset_disposal``) removes the asset on its date: Cr its cost account with its cost,
Dr 1590 with its accumulated depreciation (every month from its start to the month before the disposal,
the opening balances' included), Dr the proceeds and the gain or loss, whatever balances, to the
setting ``disposal`` (7050). Proceeds are money received into a base-currency money account, which
bears GST (2200, ``RND-GST-INCL`` of the amount) when registered and the code carries it, or the amount a
sales invoice to the buyer credited to the disposal clearing account (the setting ``disposal_clearing``,
1160), which the disposal debits and which bears no GST here (the invoice charged its own). A disposal
with no proceeds is a write-off. Its depreciation stops: no month from the disposal's month on posts.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from datetime import date, timedelta
from decimal import Decimal
from typing import Any

from oracle import money
from oracle.book import Posting, Rejected, Scheduled
from oracle.rules import Rules, add_months, month_start
from oracle.scenario import Source

MAX_LIFE_MONTHS = 1200


@dataclass
class Asset:
    """A registered asset: its cost account, cost and its months of depreciation (start, part)."""

    account: str
    cost: Decimal
    registered: date
    months: list[tuple[date, Decimal]]
    scheduled: list[Scheduled] = field(default_factory=list)
    disposed: bool = False


def _last_day(month: date) -> date:
    return add_months(month, 1) - timedelta(days=1)


def fixed_asset(r: Rules, doc: Source, books: dict[str, Any]) -> Posting:
    d = doc.data
    account = r.account(d["account"])
    if account.type != "asset":
        raise Rejected("not-an-asset-account")
    cost = r.amount(d["cost"], r.base, sign=1)
    residual = r.amount(d.get("residual", "0"), r.base, zero=True)
    if residual < 0 or not residual < cost:
        raise Rejected("residual-not-below-cost")
    life = d["life_months"]
    if not 1 <= life <= MAX_LIFE_MONTHS:
        raise Rejected("life-months")
    start = month_start(date.fromisoformat(d["depreciate_from"]) if "depreciate_from" in d else doc.date)
    parts = money.alloc_even(money.EXACT.subtract(cost, residual), life, r.base_minor)
    months = [(add_months(start, k), part) for k, part in enumerate(parts)]
    depreciation, accumulated = r.system("depreciation"), r.system("accumulated_depreciation")
    books_start = r.settings.books_start
    first = month_start(books_start) if books_start is not None else None
    scheduled = [
        Scheduled("depreciation", _last_day(month), (r.base_line(depreciation, part),
                                                     r.base_line(accumulated, money.negate(part))))
        for month, part in months
        if not part.is_zero() and (first is None or month >= first)
    ]
    return Posting([], scheduled, facts=Asset(d["account"], cost, doc.date, months, scheduled))


def asset_disposal(r: Rules, doc: Source, books: dict[str, Any]) -> Posting:
    d = doc.data
    asset = books.get(d["asset"])
    if not isinstance(asset, Asset):
        raise Rejected("not-a-registered-asset")
    if asset.disposed:
        raise Rejected("already-disposed")
    if doc.date < asset.registered:
        raise Rejected("disposal-before-asset")
    disposal_month = month_start(doc.date)
    accumulated = money.total([part for month, part in asset.months if month < disposal_month])

    lines = [
        r.base_line(asset.account, money.negate(asset.cost)),
        r.base_line(r.system("accumulated_depreciation"), accumulated),
    ]
    if "proceeds" in d:
        proceeds = d["proceeds"]
        amount = r.amount(proceeds["amount"], r.base, sign=1)
        clearing = r.settings.accounts.get("disposal_clearing")
        if proceeds["account"] == clearing and clearing is not None:
            if "gst_code" in proceeds:
                raise Rejected("gst-on-invoiced-proceeds")  # the invoice charged its own
            r.system("disposal_clearing")
            lines.append(r.base_line(proceeds["account"], amount))
        else:
            r.money_account(proceeds["account"], r.base)
            percent = r.gst_percent(proceeds.get("gst_code", "NA"), doc.date)
            gst = money.rnd_gst_incl(amount, percent, r.base_minor) if not percent.is_zero() else money.ZERO
            lines.append(r.base_line(proceeds["account"], amount))
            if not gst.is_zero():
                lines.append(r.base_line(r.system("gst"), money.negate(gst)))
    lines.append(r.balance(r.system("disposal"), lines))

    def apply() -> None:
        asset.disposed = True
        asset.scheduled[:] = [s for s in asset.scheduled if s.posting_date < disposal_month]

    return Posting(lines, effects=[apply])
