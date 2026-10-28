"""Properties of the oracle over generated scenarios (task 2.4's strategies).

They check the oracle against rules stated independently of its code: entries balance, a reversal
and its original net to zero, each month's trial balance nets to zero, a base amount is within half a
minor unit of amount × rate and rounds a half away from zero, and the documents the generator built to
post (or to be rejected) are posted (or rejected).
"""

from collections import Counter, defaultdict
from decimal import Decimal

from hypothesis import given

from oracle import money
from oracle.ledger import Ledger
from oracle.scenario import scenario_from_json
from oracle.strategies import POST, REJECT, generate, scenarios, scenarios_with_intent


def _d(text: str) -> Decimal:
    return Decimal(text)


def _sum(values) -> Decimal:
    return money.total([_d(v) for v in values])


def _foreign_rates(data):
    """(document id, line index) -> rate, for the lines that carry one."""
    return {
        (doc["id"], i): ln["fx_rate"]
        for doc in data["documents"]
        for i, ln in enumerate(doc["lines"])
        if "fx_rate" in ln
    }


# req: QA-ENG-01
@given(scenarios())
def test_every_entry_balances_and_has_no_zero_line(data):
    result = Ledger(scenario_from_json(data)).result()
    for entry in result["entries"]:
        lines = entry["lines"]
        assert len(lines) >= 2
        assert all(not _d(ln["amount"]).is_zero() for ln in lines)
        assert _sum(ln["base_amount"] for ln in lines).is_zero()
        if len({ln["currency"] for ln in lines}) == 1:
            assert _sum(ln["amount"] for ln in lines).is_zero()


# req: QA-ENG-01
@given(scenarios())
def test_a_base_amount_is_amount_times_rate_rounded_half_away_from_zero(data):
    scenario = scenario_from_json(data)
    result = Ledger(scenario).result()
    half = money.EXACT.divide(money.quantum(scenario.currencies[scenario.base_currency]), Decimal(2))
    rates = _foreign_rates(data)
    for entry in (e for e in result["entries"] if e["kind"] == "post"):
        for i, ln in enumerate(entry["lines"]):
            base = _d(ln["base_amount"])
            if ln["currency"] == scenario.base_currency:
                assert base == _d(ln["amount"])
                continue
            exact = money.EXACT.multiply(_d(ln["amount"]), _d(rates[(entry["document"], i)]))
            error = money.EXACT.subtract(base, exact).copy_abs()
            assert error <= half
            if error == half:  # a tie goes away from zero
                assert base.copy_abs() > exact.copy_abs()


# req: QA-ENG-01
@given(scenarios())
def test_a_reversal_negates_its_original_line_by_line(data):
    result = Ledger(scenario_from_json(data)).result()
    posts = {e["document"]: e for e in result["entries"] if e["kind"] == "post"}
    reversals = [e for e in result["entries"] if e["kind"] == "reversal"]
    assert len({e["document"] for e in reversals}) == len(reversals)  # at most once each
    for reversal in reversals:
        original = posts[reversal["document"]]
        assert len(reversal["lines"]) == len(original["lines"])
        for a, b in zip(original["lines"], reversal["lines"], strict=True):
            assert (a["account"], a["currency"]) == (b["account"], b["currency"])
            assert money.EXACT.add(_d(a["amount"]), _d(b["amount"])).is_zero()
            assert money.EXACT.add(_d(a["base_amount"]), _d(b["base_amount"])).is_zero()


# req: QA-ENG-01
@given(scenarios())
def test_the_trial_balance_is_the_month_sum_of_the_entries_and_nets_to_zero(data):
    result = Ledger(scenario_from_json(data)).result()
    expected: dict[str, dict[str, Decimal]] = defaultdict(lambda: defaultdict(lambda: money.ZERO))
    for entry in result["entries"]:
        month = entry["posting_date"][:7]
        expected[month]  # a posting month is listed even when everything in it nets to zero
        for ln in entry["lines"]:
            expected[month][ln["account"]] = money.EXACT.add(expected[month][ln["account"]], _d(ln["base_amount"]))
    tb = result["trial_balance"]
    assert set(tb) == set(expected)
    for month, accounts in expected.items():
        assert {a: _d(v) for a, v in tb[month].items()} == {a: v for a, v in accounts.items() if not v.is_zero()}
        assert _sum(tb[month].values()).is_zero()


# req: QA-ENG-01
@given(scenarios_with_intent())
def test_documents_built_to_post_post_and_those_built_to_fail_are_rejected(pair):
    data, intent = pair
    result = Ledger(scenario_from_json(data)).result()
    posted = {e["document"] for e in result["entries"] if e["kind"] == "post"}
    rejected = set(result["rejected"])
    for doc_id, meant in intent.documents.items():
        if meant == POST:
            assert doc_id in posted and doc_id not in rejected, doc_id
        elif meant == REJECT:
            assert doc_id in rejected and doc_id not in posted, doc_id

    # Reversals: the first of a posted document posts; every other one is refused.
    listed = Counter(r["document"] for r in data["reversals"])
    reversed_ = Counter(e["document"] for e in result["entries"] if e["kind"] == "reversal")
    refused = Counter(key.removesuffix("/reversal") for key in result["rejected"] if key.endswith("/reversal"))
    for doc_id, count in listed.items():
        first_posts = 1 if doc_id in posted else 0
        assert reversed_[doc_id] == first_posts
        assert refused[doc_id] == count - first_posts


# req: QA-ENG-01
@given(scenarios())
def test_the_oracle_is_deterministic(data):
    assert Ledger(scenario_from_json(data)).result() == Ledger(scenario_from_json(data)).result()


# req: QA-ENG-01
def test_the_generator_is_reproducible_and_covers_its_targets():
    # Hypothesis also draws on constants in the oracle's own modules (5% of draws), so what a seed
    # gives changes with the oracle's source and with the modules a process has loaded. The targets
    # are checked on enough scenarios not to hinge on that.
    first = generate(seed=11, count=120)
    assert first == generate(seed=11, count=120)
    assert first != generate(seed=12, count=40)
    assert [s["name"] for s in first] == [f"gen-11-{i:04d}" for i in range(120)]

    bases = {s["base_currency"] for s in first}
    minors = {minor for s in first for minor in s["currencies"].values()}
    lines = [ln for s in first for doc in s["documents"] for ln in doc["lines"]]
    sixteen = [ln for ln in lines if len(ln["amount"].lstrip("-").partition(".")[0]) == money.AMOUNT_INTEGER_DIGITS]
    assert len(bases) >= 3 and minors == {0, 2, 3, 4}
    assert max(len(doc["lines"]) for s in first for doc in s["documents"]) >= 20
    assert sixteen
    results = [Ledger(scenario_from_json(s)).result() for s in first]
    assert any(r["rejected"] for r in results)
    assert any(e["kind"] == "reversal" for r in results for e in r["entries"])


# req: QA-ENG-01
@given(scenarios())
def test_the_generator_does_not_yet_produce_the_shapes_rev_0_24_decided(data):
    # test/differential/README.md, "Decided in PRD-001 rev 0.24": the shapes marked "not generated".
    base = data["base_currency"]
    dates = {doc["id"]: doc["date"] for doc in data["documents"]}
    for doc in data["documents"]:
        for ln in doc["lines"]:
            if ln["currency"] == base:
                assert "fx_rate" not in ln and "fx_rate_source" not in ln
            assert ("fx_rate" in ln) == ("fx_rate_source" in ln)
            assert ln.get("fx_rate_source", "document") == "document"
        assert doc["version"] == 1
    for reversal in data["reversals"]:
        if reversal["document"] in dates:
            assert reversal["date"] >= dates[reversal["document"]]
    assert all("role" not in account for account in data["accounts"])
    ledger = Ledger(scenario_from_json(data))
    assert "base-out-of-range" not in {reason for _, reason in ledger.reasons}
    for entry in ledger.entries:
        assert all(not ln.base_amount.is_zero() for ln in entry.lines)
