"""What every P1 rule set checks and computes the same way, written from PRD-001 §10 and Appendix B.

A rule set (:mod:`oracle.sources`) reads one document of its source and either returns what it posts
or raises :class:`~oracle.book.Rejected` with a reason for people. The checks here are the ones every
source shares:

* **The input boundary** (ENG-44): amounts at most at their currency's minor units, rates plain and
  positive with at most 10 decimals, as for a manual journal; nothing is rounded to make it fit.
* **Rates** (ENG-03, ENG-40, ENG-13): an amount in a currency other than the base has its rate (the
  rate cascade's result is given in the scenario with its source); a base-currency amount has none.
* **Accounts** (INV-04, INV-13): every account a line posts to exists, is active and is a leaf, and
  only the register of share issues reaches share capital, so no P1 source does (rev 0.49).
* **System accounts** (Appendix B.5, FR-COA-03): read from the settings by key; a document that needs
  one the organisation has not set is rejected.
* **GST** (Appendix B conventions): the rate is the one in force at the tax point when the organisation
  is GST-registered then and the code is standard-rated (SR, TX, IM); otherwise 0. The codes are
  Appendix B.6's twelve.
* **Spreads** (FR-GL-03, Appendix B.1 and B.2, ENG-05): the deferral account takes the whole amount when
  the document posts, and the releases follow under ``ALLOC-EVEN``, one a month, each dated the first
  day of its month or, for a month that starts before the document, the document's date.
"""

from __future__ import annotations

from collections.abc import Mapping
from datetime import date
from decimal import Decimal
from typing import Any

from oracle import money
from oracle.book import PostedLine, Rejected, Scheduled
from oracle.scenario import Account, Scenario

GST_CODES = frozenset({"SR", "ZR", "ES", "OS", "TX", "IM", "ZP", "BL", "NR", "EP", "OP", "NA"})
STANDARD_RATED = frozenset({"SR", "TX", "IM"})
MAX_SPREAD_MONTHS = 120  # FR-GL-03: 1 to 120 months


def add_months(first: date, months: int) -> date:
    """The first day of the month ``months`` after ``first``'s month."""
    index = first.year * 12 + first.month - 1 + months
    return date(index // 12, index % 12 + 1, 1)


def month_start(day: date) -> date:
    return day.replace(day=1)


class Rules:
    """The shared checks and conversions, for one scenario."""

    def __init__(self, scenario: Scenario) -> None:
        if scenario.settings is None:  # the scenario reader refuses a P1 source without settings
            raise RuntimeError("a P1 rule set needs the scenario's settings")
        self.scenario = scenario
        self.settings = scenario.settings
        self.base = scenario.base_currency
        self.base_minor = scenario.currencies[self.base]

    # ---------------------------------------------------------------- the input boundary
    def minor(self, currency: str) -> int:
        minor = self.scenario.currencies.get(currency)
        if minor is None:
            raise Rejected("unknown-currency")
        return minor

    def amount(self, text: str, currency: str, *, sign: int | None = None, zero: bool = False) -> Decimal:
        """An entered amount in ``currency`` (ENG-44); ``sign`` 1 or -1 requires that sign."""
        try:
            value = money.parse_amount(text, self.minor(currency))
        except money.FormatError as exc:
            raise Rejected(exc.reason) from exc
        if value.is_zero():
            if not zero:
                raise Rejected("zero-amount")
        elif sign is not None and (value > 0) != (sign > 0):
            raise Rejected("wrong-sign")
        return value

    def rate(self, holder: Mapping[str, Any], currency: str) -> Decimal | None:
        """The rate on ``holder`` (a document or one of its parts), checked against its currency."""
        text = holder.get("fx_rate")
        if currency == self.base:
            if text is not None:
                raise Rejected("rate-on-base-amount")  # ENG-03
            return None
        if text is None:
            raise Rejected("missing-rate")  # ENG-40: no rate anywhere, FX-01
        try:
            return money.parse_rate(text)
        except money.FormatError as exc:
            raise Rejected(exc.reason) from exc

    def fx(self, amount: Decimal, rate: Decimal | None) -> Decimal:
        """``RND-FX`` to the base currency (a base-currency amount is its own base amount)."""
        base = amount if rate is None else money.rnd_fx(amount, rate, self.base_minor)
        if base.copy_abs() >= money.AMOUNT_LIMIT:
            raise Rejected("base-out-of-range")  # ENG-44: never truncated
        return base

    # ---------------------------------------------------------------- accounts
    def account(self, code: str) -> Account:
        """An account a line may post to (INV-04, INV-13)."""
        account = self.scenario.accounts.get(code)
        if account is None:
            raise Rejected("unknown-account")
        if account.group:
            raise Rejected("group-account")
        if not account.active:
            raise Rejected("inactive-account")
        if account.role == "share-capital":
            raise Rejected("share-capital-account")
        return account

    def system(self, key: str) -> str:
        """The organisation's system account ``key`` (Appendix B.5), postable."""
        code = self.settings.accounts.get(key)
        if code is None:
            raise Rejected(f"no-{key.replace('_', '-')}-account")
        self.account(code)
        return code

    def is_system(self, code: str, *keys: str) -> bool:
        return any(self.settings.accounts.get(key) == code for key in keys)

    def money_account(self, code: str, currency: str | None = None) -> str:
        """A money account (FR-BNK-01) and its currency; ``currency``, when given, must be it."""
        held = self.settings.money_accounts.get(code)
        if held is None:
            raise Rejected("not-a-money-account")
        self.account(code)
        if currency is not None and held != currency:
            raise Rejected("money-account-currency")
        return held

    # ---------------------------------------------------------------- GST
    def gst_code(self, code: str) -> str:
        if code not in GST_CODES:
            raise Rejected("unknown-gst-code")
        return code

    def registered(self, day: date) -> bool:
        gst = self.settings.gst
        return (
            gst.registered_from is not None
            and gst.registered_from <= day
            and (gst.registered_to is None or day <= gst.registered_to)
        )

    def gst_percent(self, code: str, day: date) -> Decimal:
        """The GST rate (a percentage) for ``code`` at the tax point ``day``, or 0 (Appendix B)."""
        self.gst_code(code)
        if code not in STANDARD_RATED or not self.registered(day):
            return money.ZERO
        in_force = [r.percent for r in self.settings.gst.rates if r.start <= day]
        if not in_force:
            raise Rejected("no-gst-rate")
        return in_force[-1]

    # ---------------------------------------------------------------- lines
    def line(self, account: str, currency: str, amount: Decimal, base_amount: Decimal) -> PostedLine:
        return PostedLine(account, currency, amount, base_amount)

    def base_line(self, account: str, base_amount: Decimal) -> PostedLine:
        """A line in the base currency: its amount is its base amount."""
        return PostedLine(account, self.base, base_amount, base_amount)

    def balance(self, account: str, lines: list[PostedLine]) -> PostedLine:
        """The base-currency line on ``account`` that balances ``lines`` in base (INV-01)."""
        return self.base_line(account, money.negate(money.total([ln.base_amount for ln in lines])))

    # ---------------------------------------------------------------- spreads
    def spread_months(self, raw: Any) -> int:
        if not isinstance(raw, int) or not 1 <= raw <= MAX_SPREAD_MONTHS:
            raise Rejected("spread-months")  # FR-GL-03: 1 to 120 months
        return raw

    def releases(
        self, amount: Decimal, months: int, first_month: date, document_date: date, category: str, deferral: str
    ) -> list[Scheduled]:
        """The monthly releases of ``amount`` from ``deferral`` to ``category`` (``ALLOC-EVEN``).

        ``amount`` is signed as the category would have taken it unspread (a debit for an expense);
        the deferral took it when the document posted, and month k moves its part to the category,
        dated the first day of its month or the document's date if later. A month whose part is zero
        posts nothing (a release has no zero line).
        """
        scheduled = []
        for k, part in enumerate(money.alloc_even(amount, months, self.base_minor)):
            if part.is_zero():
                continue
            day = max(add_months(first_month, k), document_date)
            scheduled.append(
                Scheduled("release", day, (self.base_line(category, part), self.base_line(deferral, money.negate(part))))
            )
        return scheduled
