"""The exit waterfall (FR-EQ-29), written from PRD-001 FR-EQ-29 and p2-reporting-planning design D14, not from the engine.

An exit value is paid out to share classes. A preference class that is *non-participating* takes its preference (a multiple of
the money invested) or converts and takes its share as if ordinary, whichever pays it strictly more; a *participating* class takes
its preference and then shares the rest as converted, with no cap; preferences are paid in order of seniority (1 first), classes of
the same seniority pari passu in proportion to their preferences; ordinary shares and converted and participating classes share
what is left by shares. A capped participating preference is refused.

Where the engine converts classes one at a time, lowest preference per share first, this reference tries **every** set of
conversions and keeps the stable one: no class that converts would do better staying, and none that stays would do better
converting. Money is exact: Fractions for the sharing, a whole number of minor units for each amount, split by the largest
remainder with ties to the earlier (ALLOC-PRORATA).
"""

from __future__ import annotations

from fractions import Fraction
from itertools import product
from typing import Any

MINOR = 100  # SGD: two decimals; the cases are in a currency of 2 minor units


def _minor(text: str) -> int:
    whole, _, tail = text.partition(".")
    tail = (tail + "00")[:2]
    sign = -1 if whole.startswith("-") else 1
    return sign * (abs(int(whole)) * MINOR + int(tail))


def _plain(units: int) -> str:
    sign = "-" if units < 0 else ""
    units = abs(units)
    return f"{sign}{units // MINOR}.{units % MINOR:02d}"


def _split(amount: int, weights: list[int]) -> list[int]:
    """ALLOC-PRORATA: floor each share, then one unit each to the largest remainders, ties to the earlier."""
    total = sum(weights)
    base = [amount * w // total for w in weights]
    remainder = [amount * w - b * total for w, b in zip(weights, base)]
    left = amount - sum(base)
    for i in sorted(range(len(weights)), key=lambda k: (-remainder[k], k))[:left]:
        base[i] += 1
    return base


def _preference(cls: dict[str, Any]) -> int:
    invested = sum(_minor(h["invested"]) for h in cls["holders"])
    exact = Fraction(cls["multiple"]) * invested  # a decimal multiple, exact
    cut = exact.numerator // exact.denominator
    return cut + (1 if (exact - cut) >= Fraction(1, 2) else 0)  # half away from zero (amounts are not negative)


def _shares(cls: dict[str, Any]) -> int:
    return sum(h["shares"] for h in cls["holders"])


def _pay(exit_units: int, classes: list[dict[str, Any]], prefs: list[int], converted: frozenset[int]) -> tuple[list[int], list[int]]:
    taken = [0] * len(classes)
    rest = [0] * len(classes)
    remaining = exit_units
    paying = [i for i, c in enumerate(classes) if c["preference"] and i not in converted and prefs[i] > 0]
    for seniority in sorted({classes[i]["seniority"] for i in paying}):
        group = [i for i in paying if classes[i]["seniority"] == seniority]
        want = sum(prefs[i] for i in group)
        paid = min(want, remaining)
        for i, part in zip(group, _split(paid, [prefs[i] for i in group])):
            taken[i] = part
        remaining -= paid
    sharing = [i for i, c in enumerate(classes) if not c["preference"] or i in converted or c["participating"]]
    if sharing and remaining:
        for i, part in zip(sharing, _split(remaining, [_shares(classes[i]) for i in sharing])):
            rest[i] = part
    return taken, rest


def waterfall(exit_value: str, classes: list[dict[str, Any]]) -> dict[str, Any] | None:
    """The proceeds per class and holder, or None for a structure not modelled."""
    for c in classes:
        if c.get("capped") or (c["participating"] and not c["preference"]) or _shares(c) == 0:
            return None
    exit_units = _minor(exit_value)
    if exit_units < 0:
        return None
    prefs = [_preference(c) if c["preference"] else 0 for c in classes]
    candidates = [i for i, c in enumerate(classes) if c["preference"] and not c["participating"]]

    def total(i: int, s: frozenset[int]) -> int:
        taken, rest = _pay(exit_units, classes, prefs, s)
        return taken[i] + rest[i]

    stable: list[frozenset[int]] = []
    for flags in product([False, True], repeat=len(candidates)):
        s = frozenset(i for i, f in zip(candidates, flags) if f)
        ok = True
        for i in candidates:
            if i in s and not total(i, s) > total(i, s - {i}):
                ok = False
            if i not in s and total(i, s | {i}) > total(i, s):
                ok = False
        if ok:
            stable.append(s)
    chosen = min(stable, key=lambda s: (len(s), sorted(s)))
    taken, rest = _pay(exit_units, classes, prefs, chosen)
    every = frozenset(range(len(classes)))
    no_prefs = [0] * len(classes)
    a_taken, a_rest = _pay(exit_units, classes, no_prefs, every)

    def paid(i: int, pref: int, residual: int, converts: bool) -> dict[str, Any]:
        c = classes[i]
        parts = [0] * len(c["holders"])
        for amount, weights in ((pref, [_minor(h["invested"]) for h in c["holders"]]), (residual, [h["shares"] for h in c["holders"]])):
            if amount:
                parts = [p + q for p, q in zip(parts, _split(amount, weights))]
        return {"id": c["id"], "total": _plain(pref + residual), "preference": _plain(pref), "residual": _plain(residual), "converts": converts,
                "holders": [{"id": h["id"], "total": _plain(p)} for h, p in zip(c["holders"], parts)]}

    return {"classes": [paid(i, taken[i], rest[i], i in chosen) for i in range(len(classes))],
            "as_converted": [paid(i, a_taken[i], a_rest[i], False) for i in range(len(classes))]}
