"""The exit waterfall's reference (FR-EQ-29), worked by hand, and the cases the engine's own test reads (test/differential/waterfalls/cases.json).

The hand-worked cases are those the engine's test pins (internal/domain/equity/waterfall_test.go), worked here from the rules by trying every
set of conversions. cases.json is the reference's answer to generated structures: the engine must give the same, to the cent.
"""

import json
import random
from pathlib import Path

from oracle.waterfall import waterfall

CASES = Path(__file__).resolve().parents[2] / "test" / "differential" / "waterfalls" / "cases.json"


def _h(i, shares, invested="0.00"):
    return {"id": i, "shares": shares, "invested": invested}


def _ord(shares=8_000_000):
    return {"id": "ord", "preference": False, "participating": False, "multiple": "0", "seniority": 0, "holders": [_h("f1", shares * 3 // 4), _h("f2", shares // 4)]}


def _a():
    return {"id": "a", "preference": True, "participating": False, "multiple": "1", "seniority": 1, "holders": [_h("x", 1_500_000, "1500000.00"), _h("y", 500_000, "500000.00")]}


def totals(result):
    return {c["id"]: c["total"] for c in result["classes"]}


# req: FR-EQ-29
# req: QA-ENG-01
def test_the_preference_or_the_conversion_whichever_pays_a_class_more():
    for exit_value, want in {"5000000": {"ord": "3000000.00", "a": "2000000.00"}, "20000000": {"ord": "16000000.00", "a": "4000000.00"},
                              "1000000": {"ord": "0.00", "a": "1000000.00"}, "10000000": {"ord": "8000000.00", "a": "2000000.00"}}.items():
        assert totals(waterfall(exit_value, [_ord(), _a()])) == want
    r = waterfall("5000000", [_ord(), _a()])
    assert [h["total"] for h in r["classes"][0]["holders"]] == ["2250000.00", "750000.00"]
    assert [h["total"] for h in r["classes"][1]["holders"]] == ["1500000.00", "500000.00"]
    assert r["as_converted"][1]["total"] == "1000000.00" and r["as_converted"][0]["total"] == "4000000.00"
    assert waterfall("20000000", [_ord(), _a()])["classes"][1]["converts"] is True


# req: FR-EQ-29
def test_participation_seniority_pari_passu_and_the_remainder():
    ordinary = {"id": "ord", "preference": False, "participating": False, "multiple": "0", "seniority": 0, "holders": [_h("f", 9_000_000)]}
    b = {"id": "b", "preference": True, "participating": True, "multiple": "1", "seniority": 1, "holders": [_h("i", 1_000_000, "1000000.00")]}
    assert totals(waterfall("10000000", [ordinary, b])) == {"ord": "8100000.00", "b": "1900000.00"}

    def pair(sa, sb):
        o = {"id": "ord", "preference": False, "participating": False, "multiple": "0", "seniority": 0, "holders": [_h("f", 10_000_000)]}
        a = {"id": "a", "preference": True, "participating": False, "multiple": "1", "seniority": sa, "holders": [_h("x", 2_000_000, "2000000.00")]}
        c = {"id": "b", "preference": True, "participating": False, "multiple": "1", "seniority": sb, "holders": [_h("y", 3_000_000, "3000000.00")]}
        return [o, a, c]

    assert totals(waterfall("4000000", pair(1, 1))) == {"ord": "0.00", "a": "1600000.00", "b": "2400000.00"}
    assert totals(waterfall("4000000", pair(1, 2))) == {"ord": "0.00", "a": "2000000.00", "b": "2000000.00"}
    assert totals(waterfall("6000000", pair(1, 2))) == {"ord": "1000000.00", "a": "2000000.00", "b": "3000000.00"}
    one = {"id": "ord", "preference": False, "participating": False, "multiple": "0", "seniority": 0, "holders": [_h("p", 1), _h("q", 2)]}
    assert [h["total"] for h in waterfall("1000", [one])["classes"][0]["holders"]] == ["333.33", "666.67"]
    capped = dict(b, capped=True)
    assert waterfall("1000", [ordinary, capped]) is None


def _generated(seed=29, count=60):
    rng = random.Random(seed)
    out = []
    for n in range(count):
        classes = [{"id": "ord", "name": "Ordinary", "preference": False, "participating": False, "multiple": "0", "seniority": 0,
                    "holders": [_h("f1", rng.randrange(1, 5_000_000)), _h("f2", rng.randrange(1, 3_000_000))]}]
        for k in range(rng.randrange(1, 4)):
            invested = rng.randrange(100_000, 5_000_000)
            shares = rng.randrange(100_000, 3_000_000)
            split = rng.randrange(1, 100)
            classes.append({"id": f"p{k}", "name": f"Series {k}", "preference": True, "participating": rng.random() < 0.3,
                            "multiple": rng.choice(["1", "1", "1.5", "2"]), "seniority": rng.randrange(1, 3),
                            "holders": [_h(f"p{k}a", max(1, shares * split // 100), f"{invested * split // 100}.00"),
                                        _h(f"p{k}b", max(1, shares - shares * split // 100), f"{invested - invested * split // 100}.00")]})
        exit_value = f"{rng.randrange(0, 60_000_000)}.{rng.randrange(0, 100):02d}"
        result = waterfall(exit_value, classes)
        out.append({"exit": exit_value, "classes": classes, "result": result})
    return out


# req: FR-EQ-29
# req: QA-ENG-02
def test_the_cases_the_engine_reads_are_the_references_answers():
    cases = _generated()
    for c in cases:
        assert round(sum(int(x["total"].replace(".", "")) for x in c["result"]["classes"])) == int(c["exit"].replace(".", ""))
    assert json.loads(CASES.read_text()) == cases, "run: python3 -m oracle.waterfall_cases to refresh test/differential/waterfalls/cases.json"
