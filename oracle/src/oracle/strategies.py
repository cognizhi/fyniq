"""Hypothesis strategies that generate ``fyniq-scenario@1`` scenarios (task 2.4, QA-ENG-02).

They aim at the places two implementations disagree: many currencies with 0 to 4 minor units, base
currencies other than SGD, long journals, amounts at the precision boundary (16 integer digits, one
minor unit), products that land exactly on half a minor unit (``RND-FX`` must round them away from
zero, also when negative), reversals across months, and journals that must be rejected, one for
each rule in :mod:`oracle.ledger`.

Each journal is built to an *intent*: it meets every rule by construction, it breaks exactly one, or
(for single-currency journals in a foreign currency) whether it balances in base is left to the
rounding. The intent is never an expected figure; the properties use it only to check that the
generator and the rules agree about which documents must post and which must not.

The shapes PRD-001 rev 0.24 decided (the "Decided in PRD-001 rev 0.24" table of
``test/differential/README.md``) each have a kind: a foreign line whose base amount rounds to zero
(posts, INV-05), a rate or rate source on a base-currency line (rejected, ENG-03), a base amount of
17 integer digits or more (rejected, ENG-44) and a line to a share-capital account (rejected,
INV-13); half of the charts have such an account. A reversal may be dated up to 20 days before its
document (rejected, FR-GL-12, and the document stays unreversed). Every rate carries its source, as
the format requires: ``document``, or ``base`` on a base-currency line built to be rejected.
"""

from __future__ import annotations

from dataclasses import dataclass
from datetime import date, timedelta
from decimal import Decimal
from typing import Any

from hypothesis import HealthCheck, Phase, assume, given, settings
from hypothesis import seed as hypothesis_seed
from hypothesis import strategies as st

from oracle import money
from oracle.scenario import SCENARIO_SCHEMA

# ISO 4217 minor units. CLF (4) reaches the scale of NUMERIC(20,4).
CURRENCIES: dict[str, int] = {
    "SGD": 2,
    "USD": 2,
    "EUR": 2,
    "GBP": 2,
    "MYR": 2,
    "CHF": 2,
    "JPY": 0,
    "KRW": 0,
    "VND": 0,
    "KWD": 3,
    "BHD": 3,
    "OMR": 3,
    "TND": 3,
    "CLF": 4,
}
_BASES = ["SGD"] * 4 + ["USD", "JPY", "KWD", "CLF"]
_UNKNOWN_CURRENCY = "XTS"  # ISO 4217's code for testing; never in a generated scenario
_UNKNOWN_ACCOUNT = "9999"  # generated codes are 1000 to 8999
_UNKNOWN_DOCUMENT = "JNL-9999"
_DATES = st.dates(date(2025, 11, 1), date(2027, 2, 28))  # crosses two year ends

POST, REJECT, OPEN = "post", "reject", "open"

_POSTING_KINDS = ["balanced"] * 14 + [
    "single-base", "long", "boundary", "half-unit", "half-unit", "base-rounds-to-zero"
]
_OPEN_KINDS = ["single-foreign", "single-foreign"]
_REJECTED_KINDS = [
    "one-line",
    "zero-line",
    "unknown-account",
    "inactive-account",
    "group-account",
    "missing-rate",
    "unbalanced-base",
    "unbalanced-currency",
    "unknown-currency",
    "too-many-decimals",
    "not-plain-amount",
    "bad-rate",
    "amount-out-of-range",
    "rate-on-base-line",
    "base-out-of-range",
    "share-capital-account",
]
KINDS = _POSTING_KINDS + _OPEN_KINDS + _REJECTED_KINDS

_NOT_PLAIN_AMOUNTS = [
    "1e3", "1E+2", "+5.00", " 5.00", "5.00 ", "1,000.00", "1_000", "NaN", "Infinity", "-Infinity",
    "0x1A", "", ".5", "5.", "--5",
    "\u22125.00",  # a minus sign, not a hyphen-minus
    "\u0661\u0662\u0663",  # Arabic-Indic digits: Decimal() reads them as 123
]
_BAD_RATES = ["1.00000000001", "0", "0.0000000000", "-1.25", "1e-3", "", "NaN", "10000000000", " 1.5", "1,5"]

# Half a minor unit: a = 2^i * 5^j minor units of the line currency, so (N + 1/2) base units / a is
# a terminating decimal. Rate decimals <= base minor - line minor + max(i + 1, j); the rate must keep
# at most 10 (ENG-11).
_HALF_DIVISORS = [(2**i) * (5**j) for i in range(8) for j in range(4)]


def _divisor_decimals(a: int) -> int:
    i = j = 0
    while a % 2 == 0:
        a //= 2
        i += 1
    while a % 5 == 0:
        a //= 5
        j += 1
    return max(i + 1, j)


@dataclass(frozen=True)
class Intent:
    """What the generator meant for each document: ``post``, ``reject`` or ``open``."""

    documents: dict[str, str]


@dataclass
class _Ctx:
    base: str
    currencies: dict[str, int]
    postable: list[str]
    inactive: str
    group: str
    share: str | None  # an account with the role share-capital, in half of the charts (INV-13)

    @property
    def base_minor(self) -> int:
        return self.currencies[self.base]

    @property
    def foreign(self) -> list[str]:
        return sorted(c for c in self.currencies if c != self.base)


def _value(units: int, minor: int) -> Decimal:
    return Decimal(units).scaleb(-minor, context=money.EXACT)


def _text(value: Decimal, minor: int) -> str:
    return money.fmt(value, minor)


def _short(text: str) -> str:
    """The same amount with fewer decimals, which the format allows: "100.00" -> "100"."""
    if "." not in text:
        return text
    text = text.rstrip("0").rstrip(".")
    return "0" if text in ("", "-", "-0") else text


@st.composite
def _units(draw: st.DrawFn, max_digits: int) -> int:
    """A non-zero signed count of minor units, log-uniform in size."""
    digits = draw(st.integers(1, max_digits))
    n = draw(st.integers(10 ** (digits - 1), 10**digits - 1))
    return -n if draw(st.booleans()) else n


@st.composite
def _rate(draw: st.DrawFn) -> Decimal:
    """A positive rate with 0 to 10 decimals, below 10^4."""
    decimals = draw(st.integers(0, 10))
    mantissa = draw(st.integers(1, 10 ** min(decimals + 4, 12) - 1))
    return _value(mantissa, decimals)


def _visible(amounts: list[Decimal], rate: Decimal, base_minor: int) -> Decimal:
    """Scale ``rate`` by tens until no amount's base rounds to zero (that shape has its own kind)."""
    while any(money.rnd_fx(a, rate, base_minor).is_zero() for a in amounts):
        rate = rate.scaleb(1, context=money.EXACT)
    return rate


def _rate_text(rate: Decimal) -> str:
    return format(rate, "f")


def _is_rate(rate: Decimal) -> bool:
    try:
        money.parse_rate(_rate_text(rate))
    except money.FormatError:
        return False
    return True


def _line(account: str, currency: str, amount: str, rate: Decimal | None = None) -> dict[str, str]:
    line = {"account": account, "currency": currency, "amount": amount}
    if rate is not None:
        line["fx_rate"] = _rate_text(rate)
        line["fx_rate_source"] = "document"
    return line


@st.composite
def _amount_text(draw: st.DrawFn, value: Decimal, minor: int) -> str:
    text = _text(value, minor)
    return _short(text) if draw(st.integers(0, 9)) == 0 else text


@st.composite
def _free_line(draw: st.DrawFn, ctx: _Ctx, currency: str | None = None, max_digits: int = 10) -> tuple[dict, Decimal]:
    """One line of a balanced journal and its base amount (by RND-FX), in range and non-zero."""
    account = draw(st.sampled_from(ctx.postable))
    if currency is None:
        currency = draw(st.sampled_from(sorted(ctx.currencies)))
    minor = ctx.currencies[currency]
    value = _value(draw(_units(max_digits + minor)), minor)
    text = draw(_amount_text(value, minor))
    if currency == ctx.base:
        return _line(account, currency, text), value
    rate = _visible([value], draw(_rate()), ctx.base_minor)
    return _line(account, currency, text, rate), money.rnd_fx(value, rate, ctx.base_minor)


def _with_plug(draw: st.DrawFn, ctx: _Ctx, lines: list[dict], bases: list[Decimal]) -> list[dict]:
    """Add a base-currency line that balances ``lines`` in base, at a random position."""
    plug = money.negate(money.total(bases))
    assume(not plug.is_zero())
    position = draw(st.integers(0, len(lines)))
    plug_line = _line(draw(st.sampled_from(ctx.postable)), ctx.base, _text(plug, ctx.base_minor))
    return [*lines[:position], plug_line, *lines[position:]]


@st.composite
def _balanced(
    draw: st.DrawFn, ctx: _Ctx, min_lines: int = 1, max_lines: int = 6, currency: str | None = None
) -> list[dict]:
    drawn = [draw(_free_line(ctx, currency)) for _ in range(draw(st.integers(min_lines, max_lines)))]
    return _with_plug(draw, ctx, [ln for ln, _ in drawn], [b for _, b in drawn])


@st.composite
def _single_foreign(draw: st.DrawFn, ctx: _Ctx, currency: str) -> list[dict]:
    """Balanced in one foreign currency; whether it balances in base is left to RND-FX."""
    minor = ctx.currencies[currency]
    values = [_value(draw(_units(10 + minor)), minor) for _ in range(draw(st.integers(1, 5)))]
    closing = money.negate(money.total(values))
    assume(not closing.is_zero())
    values.append(closing)
    if draw(st.booleans()):  # one rate for every line, scaled until each line shows in base
        shared = _visible(values, draw(_rate()), ctx.base_minor)
        rates = [shared] * len(values)
    else:
        rates = [_visible([value], draw(_rate()), ctx.base_minor) for value in values]
    return [
        _line(draw(st.sampled_from(ctx.postable)), currency, _text(value, minor), rate)
        for value, rate in zip(values, rates, strict=True)
    ]


@st.composite
def _boundary(draw: st.DrawFn, ctx: _Ctx) -> list[dict]:
    """Amounts at the precision boundary: 16 integer digits, or a single minor unit."""
    shape = draw(st.sampled_from(["base-max", "foreign-max", "one-unit"] if ctx.foreign else ["base-max"]))
    digits = money.AMOUNT_INTEGER_DIGITS
    if shape == "base-max":
        minor = ctx.base_minor
        largest = 10 ** (digits + minor) - 1  # 9999999999999999.99 in SGD
        units = largest if draw(st.booleans()) else draw(st.integers(10 ** (digits + minor - 1), largest))
        value = _value(units, minor)
        a, b = draw(st.lists(st.sampled_from(ctx.postable), min_size=2, max_size=2))
        sign = draw(st.booleans())
        first, second = (value, money.negate(value)) if sign else (money.negate(value), value)
        return [_line(a, ctx.base, _text(first, minor)), _line(b, ctx.base, _text(second, minor))]
    currency = draw(st.sampled_from(ctx.foreign))
    minor = ctx.currencies[currency]
    if shape == "foreign-max":
        units = draw(st.integers(10 ** (digits + minor - 1), 10 ** (digits + minor) - 1))
        value = _value(-units if draw(st.booleans()) else units, minor)
        rate = _value(draw(st.integers(1, 10**6 - 1)), 10)  # below 10^-4: the base stays under 10^12
    else:
        value = _value(draw(st.sampled_from([1, -1])), minor)
        rate = draw(_rate())
    rate = _visible([value], rate, ctx.base_minor)
    line = _line(draw(st.sampled_from(ctx.postable)), currency, _text(value, minor), rate)
    return _with_plug(draw, ctx, [line], [money.rnd_fx(value, rate, ctx.base_minor)])


@st.composite
def _half_unit(draw: st.DrawFn, ctx: _Ctx) -> list[dict]:
    """A foreign line whose product is exactly half a base minor unit past a whole one.

    amount = ±a minor units, rate = (N + 1/2) base units ÷ amount, so amount × rate = ±(N + 0.5)
    base units, which RND-FX rounds away from zero to ±(N + 1).
    """
    currency = draw(st.sampled_from(ctx.foreign))
    minor = ctx.currencies[currency]
    divisors = [a for a in _HALF_DIVISORS if ctx.base_minor - minor + _divisor_decimals(a) <= money.RATE_DECIMALS]
    a = draw(st.sampled_from(divisors))
    n = draw(st.integers(0, 10**5))
    amount = _value(a, minor)
    target = _value(2 * n + 1, ctx.base_minor)  # (N + 1/2) base units, doubled
    rate = money.EXACT.divide(target, money.EXACT.multiply(Decimal(2), amount)).normalize(money.EXACT)
    if rate.as_tuple().exponent > 0:  # 1E+2 -> 100, written out
        rate = rate.quantize(Decimal(1), context=money.EXACT)
    assume(_is_rate(rate))
    sign = draw(st.booleans())
    value = money.negate(amount) if sign else amount
    line = _line(draw(st.sampled_from(ctx.postable)), currency, _text(value, minor), rate)
    others = [draw(_free_line(ctx)) for _ in range(draw(st.integers(0, 3)))]
    lines = [line, *(ln for ln, _ in others)]
    bases = [money.rnd_fx(value, rate, ctx.base_minor), *(b for _, b in others)]
    return _with_plug(draw, ctx, lines, bases)


@st.composite
def _rounds_to_zero(draw: st.DrawFn, ctx: _Ctx) -> dict:
    """A non-zero foreign line whose base amount rounds to zero; it posts as 0.00 (INV-05, rev 0.24).

    amount = u line minor units and rate = k × 10^-10, so amount × rate = u × k × 10^-(line minor + 10),
    below half a base minor unit, 5 × 10^-(base minor + 1), when u × k < 5 × 10^(line minor + 9 −
    base minor). Half the time k is the largest such, so the product sits just under the half that
    RND-FX would round away from zero.
    """
    currency = draw(st.sampled_from(ctx.foreign))
    minor = ctx.currencies[currency]
    units = draw(_units(6))
    largest = (5 * 10 ** (minor + money.RATE_DECIMALS - 1 - ctx.base_minor) - 1) // abs(units)
    assume(largest >= 1)
    k = largest if draw(st.booleans()) else draw(st.integers(1, largest))
    rate = _value(k, money.RATE_DECIMALS)
    return _line(draw(st.sampled_from(ctx.postable)), currency, _text(_value(units, minor), minor), rate)


@st.composite
def _base_out_of_range(draw: st.DrawFn, ctx: _Ctx) -> list[dict]:
    """A foreign line whose amount fits but whose base amount has 17 integer digits or more (ENG-44).

    Rule 6 is the only rule it breaks: the line is balanced by its own negation (one currency,
    balanced in it and in base), or by base-currency lines that each fit. A quarter of the time the
    base amount is exactly 10^16, the first that does not fit NUMERIC(20,4).
    """
    currency = draw(st.sampled_from(ctx.foreign))
    minor = ctx.currencies[currency]
    if draw(st.integers(0, 3)) == 0:
        k = draw(st.integers(1, 9))
        rate, value = Decimal(10**k), Decimal(10 ** (money.AMOUNT_INTEGER_DIGITS - k))
    else:  # rate 5 to 10^10 with four decimals; amount = target ÷ rate stays below 10^16
        rate = _value(draw(st.integers(5 * 10**4, 10**14 - 1)), 4)
        target = Decimal(draw(st.integers(10**16 + 10**12, 4 * 10**16)))
        value = money.round_quotient(target, rate, minor)
    if draw(st.booleans()):
        value = money.negate(value)
    base = money.rnd_fx(value, rate, ctx.base_minor)
    line = _line(draw(st.sampled_from(ctx.postable)), currency, _text(value, minor), rate)
    if draw(st.booleans()):
        other = _line(draw(st.sampled_from(ctx.postable)), currency, _text(money.negate(value), minor), rate)
        return [line, other]
    count = int(money.EXACT.divide_int(base.copy_abs(), Decimal(9 * 10**15))) + 1  # each part below 10^16
    parts = money.alloc_even(money.negate(base), count, ctx.base_minor)
    others = [_line(draw(st.sampled_from(ctx.postable)), ctx.base, _text(part, ctx.base_minor)) for part in parts]
    return list(draw(st.permutations([line, *others])))


@st.composite
def _unbalanced_in_currency(draw: st.DrawFn, ctx: _Ctx, currency: str) -> list[dict]:
    """One foreign currency; balanced in base but off by one minor unit in its own currency.

    rate = 10^(line minor − base minor − 2), so one line minor unit is a hundredth of a base unit:
    100k units -> exactly k base units; 100k + 1 units -> k base units after RND-FX.
    """
    minor = ctx.currencies[currency]
    rate = Decimal((0, (1,), minor - ctx.base_minor - 2))
    k = draw(st.integers(1, 10**6))
    first, second = _value(100 * k, minor), _value(-(100 * k + 1), minor)
    if draw(st.booleans()):
        first, second = money.negate(first), money.negate(second)
    a, b = draw(st.sampled_from(ctx.postable)), draw(st.sampled_from(ctx.postable))
    return [_line(a, currency, _text(first, minor), rate), _line(b, currency, _text(second, minor), rate)]


@st.composite
def _foreign_journal(draw: st.DrawFn, ctx: _Ctx) -> tuple[list[dict], int]:
    """A balanced journal with at least one foreign line; returns the lines and that line's index."""
    line, base = draw(_free_line(ctx, draw(st.sampled_from(ctx.foreign))))
    others = [draw(_free_line(ctx)) for _ in range(draw(st.integers(0, 3)))]
    lines = _with_plug(draw, ctx, [line, *(ln for ln, _ in others)], [base, *(b for _, b in others)])
    return lines, lines.index(line)


@st.composite
def _journal(draw: st.DrawFn, ctx: _Ctx, kind: str) -> tuple[list[dict], str]:
    """The lines of one manual journal of ``kind``, and its intent."""
    foreign_kinds = ("missing-rate", "bad-rate", "half-unit", "single-foreign", "unbalanced-currency",
                     "base-rounds-to-zero", "base-out-of-range")
    if (kind in foreign_kinds and not ctx.foreign) or (kind == "share-capital-account" and ctx.share is None):
        kind = "balanced"
    if kind == "balanced":
        return draw(_balanced(ctx)), POST
    if kind == "single-base":
        return draw(_balanced(ctx, currency=ctx.base)), POST
    if kind == "long":
        return draw(_balanced(ctx, min_lines=20, max_lines=60)), POST
    if kind == "boundary":
        return draw(_boundary(ctx)), POST
    if kind == "half-unit":
        return draw(_half_unit(ctx)), POST
    if kind == "base-rounds-to-zero":
        lines = draw(_balanced(ctx))
        lines.insert(draw(st.integers(0, len(lines))), draw(_rounds_to_zero(ctx)))
        return lines, POST
    if kind == "base-out-of-range":
        return draw(_base_out_of_range(ctx)), REJECT
    if kind == "single-foreign":
        return draw(_single_foreign(ctx, draw(st.sampled_from(ctx.foreign)))), OPEN
    if kind == "unbalanced-currency":
        return draw(_unbalanced_in_currency(ctx, draw(st.sampled_from(ctx.foreign)))), REJECT
    if kind in ("missing-rate", "bad-rate"):
        lines, i = draw(_foreign_journal(ctx))
        if kind == "missing-rate":
            del lines[i]["fx_rate"], lines[i]["fx_rate_source"]
        else:
            lines[i]["fx_rate"] = draw(st.sampled_from(_BAD_RATES))
        return lines, REJECT

    lines = draw(_balanced(ctx))
    i = draw(st.integers(0, len(lines) - 1))
    if kind == "one-line":
        return [lines[i]], REJECT
    if kind == "zero-line":
        zero = draw(st.sampled_from([_text(money.ZERO, ctx.base_minor), "0", "-0"]))
        lines.insert(i, _line(draw(st.sampled_from(ctx.postable)), ctx.base, zero))
    elif kind == "unknown-account":
        lines[i]["account"] = _UNKNOWN_ACCOUNT
    elif kind == "inactive-account":
        lines[i]["account"] = ctx.inactive
    elif kind == "group-account":
        lines[i]["account"] = ctx.group
    elif kind == "share-capital-account":
        lines[i]["account"] = ctx.share
    elif kind == "rate-on-base-line":
        i = draw(st.sampled_from([k for k, ln in enumerate(lines) if ln["currency"] == ctx.base]))
        if draw(st.integers(0, 3)) > 0:  # a rate, which comes with its source; else the source alone
            lines[i]["fx_rate"] = _rate_text(Decimal(1) if draw(st.booleans()) else draw(_rate()))
        lines[i]["fx_rate_source"] = draw(st.sampled_from(["document", "base"]))
    elif kind == "unbalanced-base":
        plug = next(ln for ln in reversed(lines) if ln["currency"] == ctx.base and "fx_rate" not in ln)
        value = Decimal(plug["amount"])
        unit = money.quantum(ctx.base_minor)
        nudged = money.EXACT.add(value, unit) if value > 0 else money.EXACT.subtract(value, unit)
        plug["amount"] = _text(nudged, ctx.base_minor)
    elif kind == "unknown-currency":
        unused = sorted(set(CURRENCIES) - set(ctx.currencies)) or [_UNKNOWN_CURRENCY]
        lines[i]["currency"] = draw(st.sampled_from(unused))
    elif kind == "too-many-decimals":
        # One decimal more than the currency's minor units; "0" keeps the value but not the scale.
        whole, _, fraction = lines[i]["amount"].partition(".")
        minor = ctx.currencies[lines[i]["currency"]]
        lines[i]["amount"] = f"{whole}.{fraction.ljust(minor, '0')}{draw(st.sampled_from(['0', '1', '5']))}"
    elif kind == "not-plain-amount":
        lines[i]["amount"] = draw(st.sampled_from(_NOT_PLAIN_AMOUNTS))
    elif kind == "amount-out-of-range":
        minor = ctx.base_minor
        big = _value(10 ** (money.AMOUNT_INTEGER_DIGITS + minor) + draw(st.integers(0, 10**minor)), minor)
        a, b = draw(st.sampled_from(ctx.postable)), draw(st.sampled_from(ctx.postable))
        return [_line(a, ctx.base, _text(big, minor)), _line(b, ctx.base, _text(money.negate(big), minor))], REJECT
    else:  # pragma: no cover - a kind without a branch is a generator bug
        raise AssertionError(kind)
    return lines, REJECT


@st.composite
def _context(draw: st.DrawFn) -> tuple[_Ctx, list[dict]]:
    base = draw(st.sampled_from(_BASES))
    others = draw(st.lists(st.sampled_from(sorted(set(CURRENCIES) - {base})), max_size=8, unique=True))
    currencies = {base: CURRENCIES[base], **{c: CURRENCIES[c] for c in sorted(others)}}
    with_share = draw(st.booleans())
    codes = draw(st.lists(st.integers(1000, 8999).map(str), min_size=4 + with_share, max_size=12, unique=True))
    share = codes.pop() if with_share else None
    *postable, inactive, group = codes
    kinds = ["asset", "liability", "equity", "revenue", "expense"]
    accounts = [
        {"code": c, "type": draw(st.sampled_from(kinds)), "group": c == group, "active": c != inactive}
        for c in codes
    ]
    if share is not None:
        accounts.append({"code": share, "type": "equity", "group": False, "active": True, "role": "share-capital"})
    accounts = draw(st.permutations(accounts))
    return _Ctx(base, currencies, postable, inactive, group, share), accounts


@st.composite
def scenarios_with_intent(draw: st.DrawFn, name: str = "generated") -> tuple[dict[str, Any], Intent]:
    """A ``fyniq-scenario@1`` scenario and what each of its documents was built to do."""
    ctx, accounts = draw(_context())
    documents, intent = [], {}
    for i in range(draw(st.integers(0, 8))):
        doc_id = f"JNL-{i + 1:04d}"
        lines, intent[doc_id] = draw(_journal(ctx, draw(st.sampled_from(KINDS))))
        day = draw(_DATES).isoformat()
        documents.append(
            {"type": "manual_journal", "id": doc_id, "version": 1, "date": day, "memo": "", "lines": lines}
        )

    reversals = []
    for doc in documents:
        for _ in range(draw(st.sampled_from([0, 0, 0, 1, 1, 2]))):  # a second reversal must be rejected
            # Up to 20 days before the document: rejected, and the document stays unreversed (FR-GL-12).
            day = date.fromisoformat(doc["date"]) + timedelta(days=draw(st.integers(-20, 75)))
            reversals.append({"document": doc["id"], "date": day.isoformat()})
    if draw(st.integers(0, 4)) == 0:
        reversals.append({"document": _UNKNOWN_DOCUMENT, "date": draw(_DATES).isoformat()})
    reversals = draw(st.permutations(reversals))

    scenario = {
        "schema": SCENARIO_SCHEMA,
        "name": name,
        "base_currency": ctx.base,
        "currencies": ctx.currencies,
        "accounts": accounts,
        "documents": documents,
        "reversals": list(reversals),
    }
    return scenario, Intent(intent)


def scenarios(name: str = "generated") -> st.SearchStrategy[dict[str, Any]]:
    """Scenarios only (see :func:`scenarios_with_intent`)."""
    return scenarios_with_intent(name).map(lambda pair: pair[0])


def generate(seed: int, count: int) -> list[dict[str, Any]]:
    """``count`` distinct scenarios, the same for the same seed and Hypothesis version.

    Named ``gen-<seed>-<i>``. Hypothesis tries its simplest example first, so the first scenario is
    small; the rest vary.
    """
    found: dict[str, dict[str, Any]] = {}
    for attempt in range(100):
        for scenario in _collect(seed * 1_000_003 + attempt, count - len(found)):
            found.setdefault(repr(scenario), scenario)
        if len(found) >= count:
            break
    else:  # pragma: no cover - the strategy cannot be this narrow
        raise RuntimeError(f"the scenario strategy produced only {len(found)} distinct scenarios")
    return [{**scenario, "name": f"gen-{seed}-{i:04d}"} for i, scenario in enumerate(list(found.values())[:count])]


def _collect(seed: int, count: int) -> list[dict[str, Any]]:
    """Up to ``count`` scenarios from one seeded run of the generation phase (no shrinking)."""
    batch: list[dict[str, Any]] = []

    @hypothesis_seed(seed)
    @settings(
        max_examples=count,
        database=None,
        deadline=None,
        phases=[Phase.generate],
        suppress_health_check=list(HealthCheck),
    )
    @given(scenarios())
    def collect(scenario: dict[str, Any]) -> None:
        batch.append(scenario)

    collect()
    return batch
