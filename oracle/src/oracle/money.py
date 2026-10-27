"""Exact money for the oracle: the input precision boundary and the named rounding policies.

Written from PRD-001 §10.3 (ENG-10, ENG-11, ENG-44), §10.4 and §11.3 ("Money representation").

* No binary floating point anywhere (ENG-10). Every value is a ``decimal.Decimal`` and every
  operation names its context. ``EXACT`` traps any rounding, so arithmetic is either exact or
  raises; ``FloatOperation`` is trapped too, so mixing in a binary floating-point number raises.
* The only place a value is rounded is a named §10.4 policy below. P0 posts manual journals, which
  need one: ``RND-FX``. The P1 sources add the others, each under its policy name.
* A policy that divides (``RND-GST-INCL``, ``ALLOC-EVEN``, ...) rounds the exact quotient once
  through :func:`round_quotient`, which works on the integers behind the decimals, so a quotient that
  does not terminate (100 × 9 ÷ 109) is still rounded exactly.
"""

from __future__ import annotations

import re
from collections.abc import Iterable
from decimal import (
    ROUND_DOWN,
    ROUND_HALF_UP,
    Clamped,
    Context,
    Decimal,
    DivisionByZero,
    FloatOperation,
    Inexact,
    InvalidOperation,
    Overflow,
    Rounded,
    Subnormal,
    Underflow,
)

# 80 significant digits hold every product and sum the oracle forms: an amount has at most
# 16 + 4 digits (NUMERIC(20,4)), a rate at most 10 + 10 (NUMERIC(20,10)), so a product has at most
# 40, and a sum of many such values adds only a few more.
_PRECISION = 80

EXACT = Context(
    prec=_PRECISION,
    rounding=ROUND_HALF_UP,
    traps=[
        Clamped,
        DivisionByZero,
        FloatOperation,
        Inexact,
        InvalidOperation,
        Overflow,
        Rounded,
        Subnormal,
        Underflow,
    ],
)
"""Arithmetic that may not round. Any operation that would lose a digit raises."""

# §10.4: "Half-up" means half away from zero. decimal's ROUND_HALF_UP is exactly that
# (-0.125 -> -0.13). Banker's rounding (ROUND_HALF_EVEN) gives -0.12, and so does rounding half
# toward positive infinity, the other common reading of "half-up".
_HALF_AWAY_FROM_ZERO = Context(
    prec=_PRECISION,
    rounding=ROUND_HALF_UP,
    traps=[Clamped, DivisionByZero, FloatOperation, InvalidOperation, Overflow, Subnormal, Underflow],
)
"""The context of a named rounding policy: it may round (Inexact, Rounded), half away from zero."""

ZERO = Decimal(0)
HUNDRED = Decimal(100)

AMOUNT_INTEGER_DIGITS = 16
"""Amounts are NUMERIC(20,4): at most 16 integer digits (§11.3)."""

RATE_INTEGER_DIGITS = 10
RATE_DECIMALS = 10
"""FX rates are NUMERIC(20,10): at most 10 integer digits and 10 decimals (§11.3, ENG-11)."""

AMOUNT_LIMIT = Decimal((0, (1,), AMOUNT_INTEGER_DIGITS))  # 1E+16: |amount| must stay below it
RATE_LIMIT = Decimal((0, (1,), RATE_INTEGER_DIGITS))  # 1E+10

# Plain decimal strings only (ENG-12, ENG-44): ASCII digits, an optional leading minus, an optional
# fraction. No exponent, no "+", no grouping, no surrounding space, no NaN or Infinity, and no
# non-ASCII digits (Python's \d and Decimal() both accept those, so [0-9] is spelled out).
_AMOUNT = re.compile(r"-?[0-9]+(?:\.[0-9]+)?")
_RATE = re.compile(r"[0-9]+(?:\.[0-9]+)?")


class FormatError(ValueError):
    """A value breaks the input precision boundary (ENG-44); the API would refuse its document."""

    def __init__(self, reason: str) -> None:
        super().__init__(reason)
        self.reason = reason


def quantum(minor_units: int) -> Decimal:
    """One minor unit of a currency with ``minor_units`` decimals: 0.01 for SGD, 1 for JPY."""
    return Decimal((0, (1,), -minor_units))


def _decimals(text: str) -> int:
    """Decimal places as written: "100" has 0, "12345.0" has 1 (trailing zeros count)."""
    _, dot, fraction = text.partition(".")
    return len(fraction) if dot else 0


def parse_amount(text: str, minor_units: int) -> Decimal:
    """An entered amount: a plain decimal string at most at the currency's minor units (ENG-44).

    Fewer decimals are fine ("100" for USD is 100.00); more decimals are refused, never rounded,
    even when they are zeros ("12345.0" for JPY). The value must fit NUMERIC(20,4).
    """
    if not _AMOUNT.fullmatch(text):
        raise FormatError("amount-not-plain-decimal")
    if _decimals(text) > minor_units:
        raise FormatError("amount-too-many-decimals")
    value = Decimal(text)  # exact: the constructor never rounds
    if value.copy_abs() >= AMOUNT_LIMIT:
        raise FormatError("amount-out-of-range")
    return value


def parse_rate(text: str) -> Decimal:
    """An FX rate: a plain positive decimal string with at most 10 decimals (ENG-11, §11.3)."""
    if not _RATE.fullmatch(text):
        raise FormatError("rate-not-plain-decimal")
    if _decimals(text) > RATE_DECIMALS:
        raise FormatError("rate-too-many-decimals")
    value = Decimal(text)
    if value >= RATE_LIMIT:
        raise FormatError("rate-out-of-range")
    if not value > ZERO:
        raise FormatError("rate-not-positive")
    return value


def rnd_fx(amount: Decimal, rate: Decimal, base_minor_units: int) -> Decimal:
    """``RND-FX`` (§10.4): base amount = round(foreign amount × rate, base minor units), half-up.

    The product is formed exactly (EXACT traps any rounding); the one rounding step is the quantize
    to the **base** currency's minor unit, half away from zero.
    """
    product = EXACT.multiply(amount, rate)
    return product.quantize(quantum(base_minor_units), context=_HALF_AWAY_FROM_ZERO)


def total(values: list[Decimal] | tuple[Decimal, ...]) -> Decimal:
    """The exact sum of ``values`` (a sum of stored amounts never rounds: ``RND-DISPLAY``)."""
    result = ZERO
    for value in values:
        result = EXACT.add(result, value)
    return result


def negate(value: Decimal) -> Decimal:
    """``-value`` without a context (unary minus would apply the thread's context)."""
    return value.copy_negate()


def fmt(value: Decimal, minor_units: int) -> str:
    """A plain decimal string at exactly ``minor_units`` decimals: "100.00", "134", "2.263".

    Padding to the minor units is exact (EXACT raises if a digit would be lost, which would be a
    bug, not a rounding). Zero is never written as "-0.00".
    """
    value = value.quantize(quantum(minor_units), context=EXACT)
    if value.is_zero():
        value = value.copy_abs()
    return format(value, "f")


def round_quotient(numerator: Decimal, denominator: Decimal, minor_units: int) -> Decimal:
    """numerator ÷ denominator, rounded once to ``minor_units`` decimals, half away from zero.

    Exact for every input: both decimals are read as integer ratios, so the comparison with half a
    unit is made on integers, never on a rounded intermediate quotient.
    """
    if denominator.is_zero():
        raise ZeroDivisionError("round_quotient: zero denominator")
    pn, qn = numerator.as_integer_ratio()
    pd, qd = denominator.as_integer_ratio()
    num, den = pn * qd * 10**minor_units, qn * pd
    negative = (num < 0) != (den < 0)
    q, r = divmod(abs(num), abs(den))
    if 2 * r >= abs(den):  # half a unit or more: away from zero
        q += 1
    return Decimal(-q if negative else q).scaleb(-minor_units, context=EXACT)


def rnd_gst_incl(gross: Decimal, percent: Decimal, minor_units: int) -> Decimal:
    """``RND-GST-INCL`` (§10.4): GST in a GST-inclusive amount = round(gross × rate ÷ (1 + rate)).

    With the rate as a percentage p, gross × (p/100) ÷ (1 + p/100) = gross × p ÷ (100 + p); rounded
    once, half away from zero, to ``minor_units`` (the base currency's, rev 0.126). Net = gross − GST.
    """
    return round_quotient(EXACT.multiply(gross, percent), EXACT.add(HUNDRED, percent), minor_units)


def rnd_gst_excl(
    lines: Iterable[tuple[str, Decimal]], percent: Decimal, minor_units: int, per_line: bool = False
) -> dict[str, Decimal]:
    """``RND-GST-EXCL`` (§10.4): GST on a document's net amounts, by GST code.

    ``lines`` are (GST code, net) for the lines whose code carries GST at ``percent``. By default the
    GST is computed **per subtotal**: each code's nets are summed and round(subtotal × rate) taken
    once. With ``per_line`` (an organisation setting, applied consistently) each line's GST is
    rounded and the code's GST is their sum. Half away from zero, to ``minor_units``. The result
    keeps the codes in the order they first appear.
    """
    result: dict[str, Decimal] = {}
    if per_line:
        for code, net in lines:
            gst = round_quotient(EXACT.multiply(net, percent), HUNDRED, minor_units)
            result[code] = EXACT.add(result.get(code, ZERO), gst)
        return result
    subtotals: dict[str, Decimal] = {}
    for code, net in lines:
        subtotals[code] = EXACT.add(subtotals.get(code, ZERO), net)
    for code, subtotal in subtotals.items():
        result[code] = round_quotient(EXACT.multiply(subtotal, percent), HUNDRED, minor_units)
    return result


def alloc_even(amount: Decimal, periods: int, minor_units: int) -> list[Decimal]:
    """``ALLOC-EVEN`` (§10.4, rev 0.8): ``amount`` split into ``periods`` by cumulative rounding.

    Period k = round(amount × k ÷ n) − round(amount × (k − 1) ÷ n), each rounded half away from zero
    to the minor unit, so the parts sum exactly to the whole and a negative amount is the mirror image.
    """
    if periods < 1:
        raise ValueError("alloc_even: at least one period")
    parts: list[Decimal] = []
    before = ZERO.scaleb(-minor_units)
    for k in range(1, periods + 1):
        cumulative = round_quotient(EXACT.multiply(amount, Decimal(k)), Decimal(periods), minor_units)
        parts.append(EXACT.subtract(cumulative, before))
        before = cumulative
    return parts


def _percent_of(value: Decimal, percent: Decimal, minor_units: int) -> Decimal:
    return round_quotient(EXACT.multiply(value, percent), HUNDRED, minor_units)


def rnd_line(
    quantity: Decimal, unit_price: Decimal, minor_units: int, discount: Decimal = ZERO
) -> Decimal:
    """``RND-LINE`` (§10.4, rev 0.87 and 0.116): round(quantity × unit price × (100 − discount) ÷ 100).

    Computed exactly, then rounded once, half away from zero, to the currency's minor units, so a
    discount is never rounded apart from its line.
    """
    gross = EXACT.multiply(quantity, unit_price)
    return round_quotient(EXACT.multiply(gross, EXACT.subtract(HUNDRED, discount)), HUNDRED, minor_units)


def rnd_line_part(
    quantity_before: Decimal, quantity: Decimal, unit_price: Decimal, minor_units: int
) -> Decimal:
    """``RND-LINE`` of a part of a line billed after others (rev 0.87): RND-LINE of everything taken
    so far, this part included, less RND-LINE of what was taken before, so the parts add up exactly."""
    taken = EXACT.add(quantity_before, quantity)
    return EXACT.subtract(rnd_line(taken, unit_price, minor_units), rnd_line(quantity_before, unit_price, minor_units))


def rnd_gst_share(gst: Decimal, share: Decimal, minor_units: int) -> Decimal:
    """``RND-GST-SHARE`` (§10.4, rev 0.89): the input tax claimed of a line's GST for its business
    share = round(GST × share ÷ 100), half away from zero; 100 claims it whole and 0 claims none."""
    return _percent_of(gst, share, minor_units)


def rnd_wht(gross_base: Decimal, percent: Decimal, minor_units: int) -> Decimal:
    """``RND-WHT`` (§10.4, rev 0.90): withholding tax in the base currency = round(⌊gross⌋ × rate ÷
    100), where gross is the payment before GST in the base currency and ⌊⌋ rounds it down to the
    dollar (toward zero, as IRAS's form states it for an amount paid)."""
    whole = gross_base.to_integral_value(rounding=ROUND_DOWN)
    return _percent_of(whole, percent, minor_units)


def rnd_withheld(gross: Decimal, percent: Decimal, minor_units: int) -> Decimal:
    """The supplier's own amount withheld on a bill in another currency (rev 0.90, ``RND-WHT``):
    round(gross × rate ÷ 100, its minor units), half away from zero."""
    return _percent_of(gross, percent, minor_units)


def rnd_discount(total: Decimal, percent: Decimal, minor_units: int) -> Decimal:
    """``RND-DISCOUNT`` (§10.4, rev 0.93): an early-payment discount = round(total × percent ÷ 100),
    half away from zero, to the currency's minor units."""
    return _percent_of(total, percent, minor_units)


def rnd_deposit(left_base: Decimal, used: Decimal, left: Decimal, minor_units: int) -> Decimal:
    """``RND-DEPOSIT`` (§10.4, rev 0.127): the part of what is left of a deposit's base amount (or of
    its GST) that a use takes = round(left_base × used ÷ left), half away from zero; a use of all
    that is left takes all that is left."""
    if used == left:
        return left_base
    return round_quotient(EXACT.multiply(left_base, used), left, minor_units)


def rnd_rate(paid_base: Decimal, amount: Decimal) -> Decimal:
    """``RND-RATE`` (§10.4) of the rate a base-currency payment implies: round(paid ÷ amount, 10)."""
    return round_quotient(paid_base, amount, RATE_DECIMALS)
