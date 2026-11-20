"""Allocation of a shared cost over target dimension values, worked out from the rule and not from the engine (QA-ENG-01; PRD-001 FR-DIM-03, ALLOC-PRORATA; p2-reporting-planning design D15).

The rule. A shared cost (an amount in a currency with minor units, positive or negative) is split over the targets of an allocation rule by weights; what the key cannot place
is a last part of its own, *unallocated*, which stays on the source account and is never handed to a target by rounding. ``ALLOC-PRORATA`` is the largest-remainder method:
work in whole minor units of the amount's size; each part gets the whole minor units of its exact share, then the minor units left over go one each to the parts whose exact
shares had the largest fractional remainders, a tie going to the earlier part (the unallocated part is last of all). The parts add to the whole exactly, and a negative
amount is the mirror image of the positive one.

Three keys give the weights. *fixed*: percentages above nothing with at most two decimals, adding to at most 100; a weight is the percentage in hundredths, and the 100.00 the
percentages leave is the unallocated weight. *quantity* (usage): zero or more with at most six decimals; a weight is the quantity in millionths. *weights*: whole numbers (the
headcount of each target department; the people with no department are the unallocated weight).
"""

import re
from decimal import Decimal
from fractions import Fraction

from oracle.money import fmt

PERCENT_TOTAL = 10000
QUANTITY_SCALE = 6
_PLAIN = re.compile(r"[0-9]+(\.[0-9]+)?")


class AllocationRefused(ValueError):
    """The weights cannot be worked from: the engine refuses the same inputs."""


def _scaled(text: str, scale: int) -> int | None:
    """A plain non-negative decimal with at most ``scale`` decimals as a whole number of 10^-scale; None for anything else."""
    text = text.strip()
    if not _PLAIN.fullmatch(text):
        return None
    whole, _, frac = text.partition(".")
    if len(frac) > scale:
        return None
    return int(whole + frac.ljust(scale, "0"))


def percent_units(percents: list[str]) -> tuple[list[int], int]:
    """Weights in hundredths of a percent, and the unallocated weight: what 100.00 is left after them."""
    units: list[int] = []
    for p in percents:
        n = _scaled(p, 2)
        if n is None or n == 0:
            raise AllocationRefused("a percentage is above nothing, with at most two decimals")
        units.append(n)
    if sum(units) > PERCENT_TOTAL:
        raise AllocationRefused("the percentages add up to more than 100")
    return units, PERCENT_TOTAL - sum(units)


def quantity_units(quantities: list[str]) -> list[int]:
    """Weights in millionths for quantities of zero or more with at most six decimals."""
    out = []
    for q in quantities:
        n = _scaled(q, QUANTITY_SCALE)
        if n is None:
            raise AllocationRefused("a quantity is zero or more, with at most six decimals")
        out.append(n)
    return out


def alloc_prorata(minor: int, weights: list[int]) -> list[int]:
    """``ALLOC-PRORATA`` on whole minor units: ``minor`` (any sign) split by non-negative whole-number weights."""
    if not weights or any(w < 0 for w in weights) or sum(weights) == 0:
        raise AllocationRefused("weights of zero or more, some above nothing")
    sign, size, total = (-1 if minor < 0 else 1), abs(minor), sum(weights)
    exact = [Fraction(size * w, total) for w in weights]
    whole = [int(x) for x in exact]  # the whole minor units of each exact share (never negative)
    left = size - sum(whole)
    # the largest fractional remainders take the units left over, the earlier part first among equals
    by_remainder = sorted(range(len(weights)), key=lambda i: (-(exact[i] - whole[i]), i))
    for i in by_remainder[:left]:
        whole[i] += 1
    return [sign * w for w in whole]


def split(amount: Decimal, minor_units: int, units: list[int], unplaced: int) -> dict[str, list[str] | str]:
    """The targets' parts and the unallocated part of ``amount``, as the text a case writes them in."""
    minor = int(amount.scaleb(minor_units))
    parts = alloc_prorata(minor, units + [unplaced])
    return {"parts": [fmt(Decimal(p).scaleb(-minor_units), minor_units) for p in parts[:-1]],
            "unallocated": fmt(Decimal(parts[-1]).scaleb(-minor_units), minor_units)}


def case_split(case: dict) -> dict[str, list[str] | str]:
    """A case of the shared file as ``split`` works it: its key turns the targets into weights."""
    kind, targets = case["key"], case["targets"]
    if kind == "percent":
        units, unplaced = percent_units(targets)
    elif kind == "quantity":
        units, unplaced = quantity_units(targets), sum(quantity_units([case["unplaced"]])) if "unplaced" in case else 0
    elif kind == "weights":
        units, unplaced = [int(t) for t in targets], int(case.get("unplaced", "0"))
    else:
        raise AllocationRefused(f"no key {kind!r}")
    return split(Decimal(case["amount"]), case["minor_units"], units, unplaced)
