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
"""

from __future__ import annotations

import re
from typing import Any

from oracle import money
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
    ])
