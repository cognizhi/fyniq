"""Revenue over time for a fixed-fee project, worked out from the rule and not from the engine (QA-ENG-01; PRD-001 FR-PRJ-11, RND-RECOGNISE).

The rule. Progress is cost to date over the estimated total cost (at most the whole). The revenue earned to date is the price times that progress, rounded once, to the
currency's minor units, half away from zero, on the cumulative figure and never per month (RND-RECOGNISE); a cost at or above the estimate earns the whole price. The
ledger already holds some revenue for the project: what was invoiced (a milestone invoice credits revenue as it posts) plus the earlier journals' adjustments. This month's
adjustment moves what the ledger holds to what is earned. What is earned less what was invoiced is the contract position: positive is an asset (earned and not yet
invoiced), negative a liability (invoiced and not yet earned). The journal moves the asset and the liability each from its position before it to its position after.
"""

from decimal import Decimal

from oracle.money import EXACT, fmt, rnd_percent, round_quotient


def earned_to_date(price: Decimal, estimate: Decimal, cost: Decimal, minor_units: int) -> Decimal:
    """``RND-RECOGNISE``: the price times cost over estimate, rounded once to ``minor_units`` half away from zero; the whole price once the estimate is reached."""
    if price < 0 or cost < 0 or estimate <= 0:
        raise ValueError("a fee of zero or more, a cost of zero or more and an estimate above nothing")
    if cost >= estimate:
        return price
    return round_quotient(EXACT.multiply(price, cost), estimate, minor_units)


def _split(net: Decimal) -> tuple[Decimal, Decimal]:
    """The asset and the liability of a contract position: the position when above nothing, its size when below."""
    return (net, Decimal(0)) if net > 0 else (Decimal(0), -net)


def position(price: Decimal, estimate: Decimal, cost: Decimal, billed: Decimal, held: Decimal, minor_units: int) -> dict[str, str | bool]:
    """The month's figures as the text a case writes them in."""
    earned = earned_to_date(price, estimate, cost, minor_units)
    asset_after, liability_after = _split(EXACT.subtract(earned, billed))
    asset_before, liability_before = _split(EXACT.subtract(held, billed))
    reached = cost >= estimate
    progress = "100.0" if reached else str(rnd_percent(cost, estimate))
    out = {
        "progress": progress,
        "overrun": reached,
        "earned": earned,
        "adjustment": EXACT.subtract(earned, held),
        "asset": asset_after,
        "liability": liability_after,
        "asset_change": EXACT.subtract(asset_after, asset_before),
        "liability_change": EXACT.subtract(liability_after, liability_before),
    }
    return {k: (fmt(v, minor_units) if isinstance(v, Decimal) else v) for k, v in out.items()}
