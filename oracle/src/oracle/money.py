"""Exact money for the oracle: the input precision boundary and the named rounding policies.

Written from PRD-001 §10.3 (ENG-10, ENG-11, ENG-44), §10.4 and §11.3 ("Money representation").

* No binary floating point anywhere (ENG-10). Every value is a ``decimal.Decimal`` and every
  operation names its context. ``EXACT`` traps any rounding, so arithmetic is either exact or
  raises; ``FloatOperation`` is trapped too, so mixing in a binary floating-point number raises.
* The only place a value is rounded is a named §10.4 policy below. P0 posts manual journals, which
  need one: ``RND-FX``.
"""

from __future__ import annotations

import re
from decimal import (
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
