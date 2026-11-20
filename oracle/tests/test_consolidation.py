"""Consolidation of a group (QA-ENG-01; PRD-001 FR-ENT-06, RND-TRANSLATE): the hand-worked cases and the generated ones the Go engine reproduces
(internal/differential/consolidation_test.go), and the properties of the rules."""

import json
import random
from decimal import Decimal
from pathlib import Path

import pytest

from oracle.consolidation import ConsolidationRefused, run

FILES = Path(__file__).resolve().parents[2] / "test" / "differential" / "consolidations"
HAND, GENERATED = FILES / "hand.json", FILES / "generated.json"

_CHART = {"1000": "asset", "1100": "asset", "1350": "asset", "2000": "liability", "2350": "liability", "3010": "equity", "4000": "revenue", "4900": "revenue", "5000": "expense", "5900": "expense"}
_INTERCOMPANY = ("1350", "2350", "4900", "5900")
_CURRENCIES = {"SGD": 2, "MYR": 2, "USD": 2, "JPY": 0, "EUR": 2}
_MONTHS = ["2026-01", "2026-02", "2026-03", "2026-04", "2026-05", "2026-06"]


def _generated(count: int = 240, seed: int = 20261121) -> list[dict]:
    """Random groups: the oracle's answer to each is the case's want. Each month of each entity balances (a cash line takes what is left), as books do."""
    rng = random.Random(seed)
    cases = []
    for n in range(count):
        group = rng.choice(["SGD", "SGD", "USD", "JPY", "MYR"])
        last = rng.randrange(1, len(_MONTHS) + 1)
        months = _MONTHS[:last]
        first = months[rng.randrange(len(months))]
        count_entities = rng.randrange(1, 4)
        names = ["a", "b", "c"][:count_entities]
        specs, currencies = [], set()
        for name in names:
            currency = rng.choice(list(_CURRENCIES))
            if currency != group:
                currencies.add(currency)
            specs.append({"id": name, "currency": currency, "minor_units": _CURRENCIES[currency], "lines": []})
        by_id = {s["id"]: s for s in specs}
        for spec in specs:
            for month in months:
                for _ in range(rng.randrange(1, 6)):
                    account = rng.choice([a for a in _CHART if a != "1000"])
                    cp = ""
                    if account in _INTERCOMPANY and count_entities > 1:
                        cp = rng.choice([x for x in names if x != spec["id"]])
                    amount = Decimal(rng.randrange(-5_000_000, 5_000_000)).scaleb(-spec["minor_units"])
                    spec["lines"].append({"account": account, "type": _CHART[account], "counterparty": cp, "month": month, "amount": format(amount, "f")})
                    # an intercompany line is often mirrored on the other entity's books, in its currency, usually for about the same value
                    mirror = {"1350": "2350", "2350": "1350", "5900": "4900", "4900": "5900"}.get(account)
                    if cp and rng.random() < 0.6:
                        other = by_id[cp]
                        factor = Decimal(1) if other["currency"] == spec["currency"] else Decimal(rng.randrange(900, 1100)).scaleb(-3)
                        mirrored = (-amount * factor).quantize(Decimal(1).scaleb(-other["minor_units"]))
                        other["lines"].append({"account": mirror, "type": _CHART[mirror], "counterparty": spec["id"], "month": month, "amount": format(mirrored, "f")})
        entities = []
        for spec in specs:
            for month in months:
                balance = sum((Decimal(l["amount"]) for l in spec["lines"] if l["month"] == month), Decimal(0))
                spec["lines"].append({"account": "1000", "type": "asset", "counterparty": "", "month": month, "amount": format(-balance, "f")})
            entities.append(spec)
        rates = []
        for currency in sorted(currencies):
            for month in months:
                rates.append({"currency": currency, "month": month, "closing": _rate(rng), "average": _rate(rng)})
        case = {"name": f"generated {n}", "group_currency": group, "group_minor_units": _CURRENCIES[group], "from": first, "to": months[-1], "rates": rates,
                "entities": entities, "pairs": [{"a": "1350", "b": "2350"}, {"a": "5900", "b": "4900"}]}
        case["want"] = run(case)
        cases.append(case)
    return cases


def _rate(rng: random.Random) -> str:
    """A positive rate of up to ten decimals."""
    places = rng.randrange(0, 11)
    value = Decimal(rng.randrange(1, 10 ** min(places + 2, 12))).scaleb(-places)
    return format(value, "f")


def test_each_hand_worked_case_is_reproduced():  # req: FR-ENT-06, QA-ENG-02
    cases = json.loads(HAND.read_text())
    assert len(cases) >= 11
    for c in cases:
        assert run(c) == c["want"], c["name"]


def test_the_generated_cases_are_the_oracles_answers():  # req: FR-ENT-06, QA-ENG-02
    cases = json.loads(GENERATED.read_text())
    assert len(cases) >= 240
    for c in cases:
        assert run(c) == c["want"], c["name"]


def test_a_missing_rate_a_bad_month_and_a_two_type_account_are_refused():  # req: FR-ENT-05
    base = json.loads(HAND.read_text())[1]
    missing = {**base, "rates": []}
    with pytest.raises(ConsolidationRefused):
        run(missing)
    with pytest.raises(ConsolidationRefused):
        run({**base, "from": "2026-13"})
    twice = json.loads(json.dumps(base))
    twice["entities"][0]["lines"].append({"account": "1000", "type": "liability", "counterparty": "", "month": "2026-07", "amount": "1.00"})
    with pytest.raises(ConsolidationRefused):
        run(twice)


def test_every_translated_trial_balance_foots_and_the_eliminations_net_to_nothing():  # req: FR-ENT-06
    for c in _generated(60, seed=7):
        got = run(c)
        for e in got["entities"]:
            assert sum(Decimal(s.split("|")[3]) for s in e["lines"]) + Decimal(e["reserve"]) == 0, (c["name"], e["id"])
        assert sum(Decimal(s.split("|")[4]) for s in got["eliminations"]) == 0, c["name"]
        assert sum(Decimal(s.split("|")[2]) for s in got["consolidated"]) + Decimal(got["reserve"]) == 0, c["name"]


def test_what_is_matched_is_never_more_than_either_side_and_the_gap_is_what_is_left():  # req: FR-ENT-06
    for c in _generated(60, seed=11):
        for d in run(c)["differences"]:
            x_id, y_id, a, b, x, y, matched, gap = d.split("|")
            x, y, matched, gap = Decimal(x), Decimal(y), Decimal(matched), Decimal(gap)
            assert x + y == gap and gap != 0
            assert matched == 0 or (matched <= x and matched <= -y)
