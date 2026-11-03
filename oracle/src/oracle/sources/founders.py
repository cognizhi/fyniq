"""A cost a founder paid before the books start (``pre_incorporation_cost@1``), from PRD-001 FR-EQ-03 and Appendix B.7
("Cost a founder paid personally, kept as a loan") and the founders' money design D7 (rev 0.167); and a director's fee
(``director_fee@1``), from FR-EQ-04, §10.2 and design D8.

The document is the cost as the accountant decided it: the date it posts on (``date``: the books start, or the day of
the decision when the books start's month is closed), the day it was paid (``incurred``, before the books start), the
director or shareholder who paid it (``person``, one of the scenario's ``people``), the account the accountant chose,
what they paid (``gross``, in the base currency), the GST the receipt shows (``gst``) and whether it is claimed
(``claim_gst``: only while the organisation is GST-registered on the posting date).

It posts Dr the account with the gross, or with the gross less the GST when the GST is claimed; Dr ``gst`` (2200) with
the GST when claimed; and Cr ``director_loan`` (2500) with the gross, the line naming the person who paid it.
"""

from __future__ import annotations

from datetime import date
from typing import Any

from oracle import money
from oracle.book import Posting, Rejected
from oracle.rules import Rules
from oracle.scenario import Source

# The roles of the director's accounts, which a cost before the books start never posts to (B.7).
DIRECTOR_ROLES = frozenset({"director-loan", "deferred-remuneration"})


def pre_incorporation_cost(r: Rules, doc: Source, books: dict[str, Any]) -> Posting:
    d = doc.data
    start = r.settings.books_start
    if start is None:
        raise Rejected("no-books-start")
    if not date.fromisoformat(d["incurred"]) < start:
        raise Rejected("not-before-books-start")
    if doc.date < start:
        raise Rejected("posts-before-books-start")
    person = d["person"]
    if person not in r.scenario.people:
        raise Rejected("not-a-director")
    account = r.account(d["account"])
    if (account.role in DIRECTOR_ROLES or d["account"] in r.settings.money_accounts
            or r.is_system(d["account"], "gst")):
        raise Rejected("account-not-allowed")
    gross = r.amount(d["gross"], r.base, sign=1)
    gst = r.amount(d.get("gst", "0"), r.base, sign=1, zero=True)
    if not gst < gross:
        raise Rejected("gst-not-less-than-amount")
    claim = bool(d.get("claim_gst", False))
    if claim and (gst.is_zero() or not r.registered(doc.date)):
        raise Rejected("gst-not-claimable")

    cost = money.EXACT.subtract(gross, gst) if claim else gross
    lines = [r.base_line(d["account"], cost)]
    if claim:
        lines.append(r.base_line(r.system("gst"), gst))
    lines.append(r.line(r.system("director_loan"), r.base, money.negate(gross), money.negate(gross), party=person))
    return Posting(lines)


def director_fee(r: Rules, doc: Source, books: dict[str, Any]) -> Posting:
    """A director's fee the shareholders voted, posted on ``date``, the day the director became entitled to it.

    Dr ``account`` (6050 Directors' fees) with ``amount``, Cr ``deferred_remuneration`` (2120) with ``amount``, the line
    naming ``person``, a director, one of the scenario's ``people`` (FR-EQ-04, §10.2: "Dr 6050 / Cr 2120 to that person").
    """
    d = doc.data
    person = d["person"]
    if person not in r.scenario.people:
        raise Rejected("not-a-director")
    account = r.account(d["account"])
    if (account.role in DIRECTOR_ROLES or d["account"] in r.settings.money_accounts
            or r.is_system(d["account"], "gst")):
        raise Rejected("account-not-allowed")
    amount = r.amount(d["amount"], r.base, sign=1)
    return Posting([
        r.base_line(d["account"], amount),
        r.line(r.system("deferred_remuneration"), r.base, money.negate(amount), money.negate(amount), party=person),
    ])
