"""A cost a founder paid before the books start (``pre_incorporation_cost@1``), from PRD-001 FR-EQ-03 and Appendix B.7
("Cost a founder paid personally, kept as a loan") and the founders' money design D7 (rev 0.167); and a director's fee
(``director_fee@1``), from FR-EQ-04, §10.2 and design D8; and a waiver of what the company owes one
(``director_settlement@1``), from FR-EQ-07, Appendix B.7 and design D10; a repayment through a payment batch
(``director_repayment@1``) and a conversion to shares from the register of share issues (``share_issue@1``), from
FR-EQ-05, FR-EQ-06 and designs D10 and D11.

The document is the cost as the accountant decided it: the date it posts on (``date``: the books start, or the day of
the decision when the books start's month is closed), the day it was paid (``incurred``, before the books start), the
director or shareholder who paid it (``person``, one of the scenario's ``people``), the account the accountant chose,
what they paid (``gross``, in the base currency), the GST the receipt shows (``gst``) and whether it is claimed
(``claim_gst``: only while the organisation is GST-registered on the posting date).

It posts Dr the account with the gross, or with the gross less the GST when the GST is claimed; Dr ``gst`` (2200) with
the GST when claimed; and Cr ``director_loan`` (2500) with the gross, the line naming the person who paid it.

A waiver, a conversion, and a repayment's settlements on one account together, draw on what the company owes the
person on that director's account at the end of the document's date, and never on more (test/differential/README.md,
"P1 readings"; FR-EQ-05 rev 0.168: a settlement is drafted for at most what is owed that day, and a batch refuses a
repayment of more, PB-25). Nothing owed refuses any amount.
"""

from __future__ import annotations

from datetime import date
from decimal import Decimal
from typing import Any

from oracle import captable, money
from oracle.book import Posting, Rejected
from oracle.rules import Rules
from oracle.scenario import Source

# The roles of the director's accounts, which a cost before the books start never posts to (B.7).
DIRECTOR_ROLES = frozenset({"director-loan", "deferred-remuneration"})


def owed(r: Rules, person: str, account: str, day: date) -> Decimal:
    """What the company owes ``person`` on the director's ``account`` at the end of ``day`` (README, "P1 readings").

    The credit, positive, of the lines naming the person on that account in the entries of the documents before this
    one in the scenario dated on or before ``day``: ``r.posted`` holds exactly those documents' entries, since
    reversals come after every document (so none counts) and so do scheduled entries. A debit, or no line, is nothing
    owed. Read in the base currency, as each person's totals are (FR-EQ-01).
    """
    credit = money.negate(money.total([
        ln.base_amount
        for entry in r.posted if entry.posting_date <= day
        for ln in entry.lines if ln.account == account and ln.party == person
    ]))
    return credit if credit > 0 else money.ZERO


def _at_most_owed(r: Rules, person: str, account: str, day: date, amount: Decimal) -> None:
    if amount > owed(r, person, account, day):
        raise Rejected("more-than-owed")


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


def director_waiver(r: Rules, doc: Source, books: dict[str, Any]) -> Posting:
    """A waiver of what the company owes a director or shareholder, posted on ``date``, the day it was approved.

    Dr ``account`` (2500 or 2120, a director's account) with ``amount``, the line naming ``person`` (one of the scenario's
    ``people``; B.7: waived); Cr ``credit`` (3300 Capital contribution, or an income account the accountant chose) with
    ``amount`` (FR-EQ-07). The credit is not a director's account, a money account or GST control. The amount is at
    most what is owed there (:func:`owed`).
    """
    d = doc.data
    person = d["person"]
    if person not in r.scenario.people:
        raise Rejected("not-a-director")
    if r.account(d["account"]).role not in DIRECTOR_ROLES:
        raise Rejected("not-a-directors-account")
    credit = r.account(d["credit"])
    if credit.role in DIRECTOR_ROLES or d["credit"] in r.settings.money_accounts or r.is_system(d["credit"], "gst"):
        raise Rejected("account-not-allowed")
    amount = r.amount(d["amount"], r.base, sign=1)
    _at_most_owed(r, person, d["account"], doc.date, amount)
    return Posting([
        r.line(d["account"], r.base, amount, amount, party=person),
        r.base_line(d["credit"], money.negate(amount)),
    ])


def director_repayment(r: Rules, doc: Source, books: dict[str, Any]) -> Posting:
    """What a payment batch's release repaid a director or shareholder, posted on ``date``, the batch's payment date.

    For each of ``settlements`` Dr its ``account`` (2500 or 2120, a director's account) with its ``amount``, the line naming
    ``person`` (one of the scenario's ``people``; B.7: paid out); Cr ``account``, a money account in the base currency, with
    the total (FR-EQ-05, FR-BNK-12). The settlements on one account total at most what is owed there (:func:`owed`).
    """
    d = doc.data
    person = d["person"]
    if person not in r.scenario.people:
        raise Rejected("not-a-director")
    r.money_account(d["account"], r.base)
    if not d["settlements"]:
        raise Rejected("no-settlements")
    lines = []
    total = money.ZERO
    by_account: dict[str, Decimal] = {}
    for s in d["settlements"]:
        if r.account(s["account"]).role not in DIRECTOR_ROLES:
            raise Rejected("not-a-directors-account")
        amount = r.amount(s["amount"], r.base, sign=1)
        lines.append(r.line(s["account"], r.base, amount, amount, party=person))
        total = money.EXACT.add(total, amount)
        by_account[s["account"]] = money.EXACT.add(by_account.get(s["account"], money.ZERO), amount)
    for account, settled in by_account.items():  # the settlements on one account together
        _at_most_owed(r, person, account, doc.date, settled)
    lines.append(r.base_line(d["account"], money.negate(total)))
    return Posting(lines)


def director_conversion(r: Rules, doc: Source, books: dict[str, Any]) -> Posting:
    """A founder's balance converted to shares from the register of share issues, posted on ``date``, the day ACRA's
    register shows the allotment (FR-EQ-06, Appendix B.7).

    ``RND-SHARE`` allots n whole shares for ``amount`` at ``price``; Dr ``account`` (2500 or 2120, a director's account)
    with the converted amount round(n x price), the line naming ``person``; Cr ``class`` (a share-capital account) with
    the converted amount, or under fair value with round(n x ``fair_value_price``), the difference to ``difference``, an
    income or expense account (Dr a loss, Cr a gain). The remainder stays owed. ``amount`` is at most what is owed on
    ``account`` (:func:`owed`).
    """
    d = doc.data
    person = d["person"]
    if person not in r.scenario.people:
        raise Rejected("not-a-director")
    if r.account(d["account"]).role not in DIRECTOR_ROLES:
        raise Rejected("not-a-directors-account")
    share_class = r.scenario.accounts.get(d["class"])
    if share_class is None or share_class.role != "share-capital" or share_class.group or not share_class.active:
        raise Rejected("not-share-capital")
    amount = r.amount(d["amount"], r.base, sign=1)
    price = r.quantity(d["price"])
    shares, converted, _ = money.rnd_share(amount, price, r.minor(r.base))
    if shares == 0:
        raise Rejected("no-whole-share")
    lines = [r.line(d["account"], r.base, converted, converted, party=person)]
    if "fair_value_price" not in d:
        lines.append(r.base_line(d["class"], money.negate(converted)))
    else:
        fair = r.quantity(d["fair_value_price"])
        equity = money.rnd_line(Decimal(shares), fair, r.minor(r.base))
        lines.append(r.base_line(d["class"], money.negate(equity)))
        if equity != converted:
            difference = r.account(d.get("difference", ""))
            if difference.type not in ("revenue", "expense") or d["difference"] in r.settings.money_accounts:
                raise Rejected("account-not-allowed")
            lines.append(r.base_line(d["difference"], money.EXACT.subtract(equity, converted)))
    _at_most_owed(r, person, d["account"], doc.date, amount)
    # The n shares join the person's holding of the class on the day (README, "Cap tables").
    return Posting(lines, effects=[captable.move(books, person, d["class"], shares, doc.date)])
