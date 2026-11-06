"""The merge of an account into another (``account_merge@1``), from PRD-001 FR-COA-04 and Appendix B.8 (rev 0.174), as
test/differential/README.md, "Account merges", states it.

The merge of ``account`` into ``into`` posts one entry on ``date`` that moves what ``account`` holds into ``into``,
each currency and dimension set apart, at its recorded base value (ENG-14: never converted again):

* **What it holds.** A balance-sheet account (asset, liability, equity) holds the sum of its lines dated on or before
  ``date``; an income or expense account the sum of its lines dated from the first day of the financial year that
  holds ``date`` to ``date``, since earlier years are in retained earnings. The first financial year runs from
  ``settings.books_start`` to ``settings.first_year_end``, each later one from the day after the last ends to one year
  later less a day (the scenario reader refuses a merge of an income or expense account without both). The lines are
  those of the entries posted before the merge: the documents before it in the scenario (reversals and edits come
  after every document, so none counts) and their scheduled entries dated on or before ``date`` (releases,
  depreciation and journals' automatic reversals).
* **The lines.** For each currency and dimension set, with amount A and base amount B: in the base currency, −A on
  ``account`` and +A on ``into``; in another currency with A ≠ 0, ∓A at rate r with base ∓``RND-FX``(A × r), and the base
  left, d = B − ``RND-FX``(A × r), when not zero, ∓d in the base currency; with A = 0, all of B as d. r is ``RND-RATE``
  of B ÷ A (rounded once, half away from zero, to 10 decimals), or 1 when A and B differ in sign or that rate is not
  one a stored rate can hold (0 once rounded, or 10^10 or more). Each line keeps the holding's dimension values (the
  system's posting: an account that requires a dimension takes them as they come). A holding of amount and base 0
  moves nothing, and an account holding nothing posts no entry.
* **After it**, posted or holding nothing, ``account`` is inactive: the ledger rejects a later document with a line on
  it, and a void or an edit of a journal whose current entry, or whose new version, has one.

It is rejected whole when (1) ``account`` and ``into`` are one account, or either is not in ``accounts``, is a group,
is inactive (merged before), has a ``role``, is named in ``settings.accounts``, is a money account or is marked
``revalue`` (the product merges a revalued account only into another, FR-COA-04; the format leaves that out); (2) their types
differ; (3) ``date`` is after ``as_of`` or before ``settings.books_start``; (4) an entry posted before it has a line on
``account`` dated after ``date``; (5) something still posts to ``account`` after ``date`` by itself: a release of a
spread of a document before it, an automatic reversal of a manual journal before it, or a fixed asset before it whose
cost is on ``account`` and that is not disposed of on or before ``date``.
"""

from __future__ import annotations

from collections.abc import Iterable
from datetime import date, timedelta
from decimal import Decimal
from typing import Any

from oracle import money
from oracle.book import PostedLine, Posting, Rejected
from oracle.rules import Rules
from oracle.scenario import Document, Source
from oracle.sources.assets import Asset

BALANCE_SHEET = frozenset({"asset", "liability", "equity"})


def _one_year_later(day: date) -> date:
    """The same day a year later; 29 February's is 1 March in a common year (the day after its 28 February)."""
    try:
        return day.replace(year=day.year + 1)
    except ValueError:
        return date(day.year + 1, 3, 1)


def year_start(books_start: date, first_year_end: date, day: date) -> date:
    """The first day of the financial year that holds ``day`` (on or after ``books_start``; FR-ORG-02).

    The first year runs from the books' start to the first year end, which may make it short or long; each later one
    from the day after the last ends to one year later less a day.
    """
    if day <= first_year_end:
        return books_start
    start = first_year_end + timedelta(days=1)
    while True:
        end = _one_year_later(start) - timedelta(days=1)
        if day <= end:
            return start
        start = end + timedelta(days=1)


def _on(lines: Iterable[PostedLine], code: str) -> bool:
    return any(ln.account == code for ln in lines)


def _check_account(r: Rules, code: str) -> None:
    """Rule 1 for one side of the merge."""
    account = r.scenario.accounts.get(code)
    if account is None:
        raise Rejected("unknown-account")
    if account.group:
        raise Rejected("group-account")
    if not account.active or code in r.merged:  # an account merged before it is inactive
        raise Rejected("inactive-account")
    if account.role is not None:
        raise Rejected("account-with-role")
    if code in r.settings.accounts.values():
        raise Rejected("system-account")
    if code in r.settings.money_accounts:
        raise Rejected("money-account")
    if account.revalue:  # the format leaves merges of revalued accounts out for now
        raise Rejected("revalued-account")


def account_merge(r: Rules, doc: Source, books: dict[str, Any]) -> Posting:
    d = doc.data
    code, into, day = d["account"], d["into"], doc.date

    # Rule 1.
    if code == into:
        raise Rejected("same-account")
    _check_account(r, code)
    _check_account(r, into)
    merged = r.scenario.accounts[code]
    # Rule 2.
    if merged.type != r.scenario.accounts[into].type:
        raise Rejected("types-differ")
    # Rule 3 (the scenario reader already refuses a document dated after as_of: the scenario is invalid).
    as_of, books_start = r.scenario.as_of, r.settings.books_start
    if as_of is not None and day > as_of:
        raise Rejected("after-as-of")
    if books_start is not None and day < books_start:
        raise Rejected("before-books-start")

    # Rule 4: the entries posted before the merge are the documents' before it (r.posted).
    if any(entry.posting_date > day and _on(entry.lines, code) for entry in r.posted):
        raise Rejected("posted-after-merge-date")

    # Rule 5, and the scheduled entries dated on or before the merge, which it moves.
    dated: list[tuple[date, PostedLine]] = [(e.posting_date, ln) for e in r.posted for ln in e.lines]
    for _, items in r.scheduled:  # a release of a spread (or depreciation, on system accounts only)
        for scheduled in items:
            if scheduled.posting_date > day:
                if _on(scheduled.lines, code):
                    raise Rejected("scheduled-after-merge-date")
            else:
                dated.extend((scheduled.posting_date, ln) for ln in scheduled.lines)
    reversing = {doc_.id: doc_ for doc_ in r.scenario.documents
                 if isinstance(doc_, Document) and doc_.reverse_on is not None}
    for entry in r.posted:  # a posted manual journal before it that reverses by itself (FR-GL-02)
        journal = reversing.get(entry.document)
        if journal is None or journal.reverse_on is None:
            continue
        if journal.reverse_on > day:
            if _on(entry.lines, code):
                raise Rejected("auto-reversal-after-merge-date")
        else:
            dated.extend((journal.reverse_on, PostedLine(ln.account, ln.currency, money.negate(ln.amount),
                                                         money.negate(ln.base_amount), ln.party, ln.dimensions))
                         for ln in entry.lines)
    for fact in books.values():  # a fixed asset before it whose cost is on the account
        if isinstance(fact, Asset) and fact.account == code and (fact.disposed_on is None or fact.disposed_on > day):
            raise Rejected("asset-not-disposed")

    # What it holds, per currency and dimension set.
    start: date | None = None
    if merged.type not in BALANCE_SHEET:
        first_year_end = r.settings.first_year_end
        assert books_start is not None and first_year_end is not None  # the scenario reader requires both
        start = year_start(books_start, first_year_end, day)
    holdings: dict[tuple[str, tuple[tuple[str, str], ...]], tuple[Decimal, Decimal]] = {}
    for posted_on, ln in dated:
        if ln.account != code or posted_on > day or (start is not None and posted_on < start):
            continue
        key = (ln.currency, tuple(sorted(ln.dimensions)))
        amount, base = holdings.get(key, (money.ZERO, money.ZERO))
        holdings[key] = (money.EXACT.add(amount, ln.amount), money.EXACT.add(base, ln.base_amount))

    lines: list[PostedLine] = []

    def move(currency: str, amount: Decimal, base: Decimal, values: tuple[tuple[str, str], ...]) -> None:
        lines.append(PostedLine(code, currency, money.negate(amount), money.negate(base), None, values))
        lines.append(PostedLine(into, currency, amount, base, None, values))

    for (currency, values), (amount, base) in sorted(holdings.items()):
        if amount.is_zero() and base.is_zero():
            continue
        if currency == r.base:
            move(currency, amount, base, values)  # a base-currency line's base amount is its amount
            continue
        left = base
        if not amount.is_zero():
            rate = money.rnd_rate(base, amount)  # RND-RATE of B ÷ A, base per one unit
            differ = (base > 0 and amount < 0) or (base < 0 and amount > 0)
            if differ or not money.ZERO < rate < money.RATE_LIMIT:  # 0 once rounded, or 10^10 or more
                rate = Decimal(1)
            converted = money.rnd_fx(amount, rate, r.base_minor)
            move(currency, amount, converted, values)
            left = money.EXACT.subtract(base, converted)
        if not left.is_zero():  # the base left, all of B when A = 0
            move(r.base, left, left, values)

    def apply() -> None:
        r.merged.add(code)  # the ledger's own set: inactive from now on

    return Posting(lines, effects=[apply])
