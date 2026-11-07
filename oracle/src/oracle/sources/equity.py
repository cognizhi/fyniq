"""A cash issue of shares made effective from share application monies (``share_issue_cash@1``), from PRD-001 FR-EQ-17,
Appendix B.7 ("Share issue for cash", "Cash received before the allotment takes effect") and rev 0.205.

The document is the issue as the register makes it effective: the day ACRA's register of members shows the allotment
(``date``), its one subscriber (``person``, one of the scenario's ``people`` or ``subscribers``), the class's
share-capital account (``class``), the whole number of shares and the price per share in the base currency. Its amount is
``RND-LINE`` of shares x price.

The money came in earlier, to share application monies, the one account of role ``share-application-monies`` (a bank
line naming the subscriber, :mod:`oracle.sources.bank`). The issue posts Dr share application monies with the amount,
the line naming the subscriber, and Cr ``class`` with the same. It is never made effective unpaid: what the
subscriber's monies hold at the end of ``date`` is at least the amount, or it is rejected (test/differential/README.md,
"Cash share issues").

Once posted, the issue moves ``shares`` into the subscriber's holding of ``class`` on ``date``; a transfer
(:func:`share_transfer`) moves shares between holders and posts nothing (README, "Share transfers" and "Cap tables").
"""

from __future__ import annotations

import re
from typing import Any

from oracle import captable, money
from oracle.book import Posting, Rejected
from oracle.rules import Rules
from oracle.scenario import Source
from oracle.sources.founders import owed

_WHOLE_SHARES = re.compile(r"[1-9][0-9]{0,13}")


def share_issue_cash(r: Rules, doc: Source, books: dict[str, Any]) -> Posting:
    d = doc.data
    person = d["person"]
    if person not in r.scenario.people | r.scenario.subscribers:
        raise Rejected("not-a-subscriber")
    monies = [code for code, a in r.scenario.accounts.items()
              if a.role == "share-application-monies" and a.active and not a.group]
    if len(monies) != 1:
        raise Rejected("no-share-application-monies")
    share_class = r.scenario.accounts.get(d["class"])
    if share_class is None or share_class.role != "share-capital" or share_class.group or not share_class.active:
        raise Rejected("not-share-capital")
    if not _WHOLE_SHARES.fullmatch(d["shares"]):
        raise Rejected("bad-shares")
    price = r.quantity(d["price"])
    amount = money.rnd_line(r.quantity(d["shares"]), price, r.minor(r.base))
    if amount <= 0:
        raise Rejected("nothing-issued")
    if owed(r, person, monies[0], doc.date) < amount:  # the credit the subscriber's monies hold that day
        raise Rejected("unpaid")
    return Posting([
        r.line(monies[0], r.base, amount, amount, party=person),
        r.base_line(d["class"], money.negate(amount)),
    ], effects=[captable.move(books, person, d["class"], int(d["shares"]), doc.date)])


def share_transfer(r: Rules, doc: Source, books: dict[str, Any]) -> Posting:
    """``shares`` of ``class`` from ``seller`` to ``buyer`` on ``date``, the day ACRA's register shows it (FR-EQ-17).

    It posts no entry. It is refused when the seller's holding of ``class`` would fall below zero at the end of any day
    from ``date`` on, as the database refuses "a holding below zero on any day" (FY288).
    """
    d = doc.data
    holders = r.scenario.people | r.scenario.subscribers
    if d["seller"] not in holders or d["buyer"] not in holders:
        raise Rejected("not-a-holder")
    if d["seller"] == d["buyer"]:
        raise Rejected("same-holder")
    share_class = r.scenario.accounts.get(d["class"])
    if share_class is None or share_class.role != "share-capital" or share_class.group or not share_class.active:
        raise Rejected("not-share-capital")
    if not _WHOLE_SHARES.fullmatch(d["shares"]):
        raise Rejected("bad-shares")
    shares = int(d["shares"])
    if captable.lowest_from(books, d["seller"], d["class"], doc.date, -shares) < 0:
        raise Rejected("more-than-held")
    return Posting([], effects=[captable.move(books, d["seller"], d["class"], -shares, doc.date),
                                captable.move(books, d["buyer"], d["class"], shares, doc.date)])
