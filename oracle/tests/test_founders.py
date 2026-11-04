"""Director's accounts: each line names its person, and each person's totals (FR-EQ-01, B.7; PRD-001 rev 0.167).

Worked by hand. Mei Lin (P0001) lends S$1,500.00 into the bank on 5 January and is repaid S$400.00 on 9
January: on 2500 she has −1,500.00 and +400.00, a total of −1,100.00 (the company owes her S$1,100.00).
Arjun (P0002) pays S$300.00 of costs personally on 7 January, kept as a loan (−300.00 on 2500), and has
S$250.00 of fees owed on 2120 (−250.00). Mei Lin's loan journal is then reversed on 12 January, so her
total is +400.00: she owes the company S$400.00.
"""

from decimal import ROUND_HALF_UP
from decimal import Decimal as D

import pytest
from builders import P1_SETTINGS, journal, line, p1_post, p1_scenario, scenario
from hypothesis import given
from hypothesis import strategies as st

from oracle import money
from oracle.ledger import Ledger
from oracle.scenario import scenario_from_json

DIRECTOR_ACCOUNTS = [
    {"code": "2120", "type": "liability", "group": False, "active": True, "role": "deferred-remuneration"},
    {"code": "2500", "type": "liability", "group": False, "active": True, "role": "director-loan"},
]


def person(account: str, amount: str, party: str | None, origin: str | None) -> dict:
    out = line(account, "SGD", amount)
    if party is not None:
        out["party"] = party
    if origin is not None:
        out["origin"] = origin
    return out


def run(*documents: dict, reversals: tuple = ()) -> tuple[dict, Ledger]:
    data = scenario(*documents, reversals=reversals, accounts=DIRECTOR_ACCOUNTS)
    data["people"] = ["P0001", "P0002"]
    ledger = Ledger(scenario_from_json(data))
    return ledger.result(), ledger


LENT = journal("JNL-0001", "2026-01-05", line("1010", "SGD", "1500.00"), person("2500", "-1500.00", "P0001", "lent"))
COSTS = journal("JNL-0002", "2026-01-07", line("6010", "SGD", "300.00"), person("2500", "-300.00", "P0002", "paid_personally"))
REPAID = journal("JNL-0003", "2026-01-09", person("2500", "400.00", "P0001", "paid_out"), line("1010", "SGD", "-400.00"))
FEES = journal("JNL-0004", "2026-01-10", line("6010", "SGD", "250.00"), person("2120", "-250.00", "P0002", "fee"))


# req: FR-EQ-01
# req: QA-ENG-01
def test_each_persons_totals_on_the_directors_accounts():
    result, _ = run(LENT, COSTS, REPAID, FEES)
    assert result["rejected"] == []
    assert result["people"] == {"P0001": {"2500": "-1100.00"}, "P0002": {"2120": "-250.00", "2500": "-300.00"}}
    # The person is on the director's account's line, and only there.
    lent = result["entries"][0]["lines"]
    assert "party" not in lent[0] and lent[1]["party"] == "P0001"
    # The sub-ledger adds up to the accounts.
    assert result["trial_balance"]["2026-01"]["2500"] == "-1400.00"


# req: FR-EQ-01
# req: FR-GL-12
def test_a_reversal_keeps_the_person():
    result, _ = run(LENT, REPAID, reversals=(("JNL-0001", "2026-01-12"),))
    reversal = result["entries"][-1]
    assert reversal["kind"] == "reversal" and reversal["lines"][1]["party"] == "P0001"
    assert result["people"] == {"P0001": {"2500": "400.00"}}


# req: FR-EQ-01
def test_nobody_named_leaves_people_out():
    result, _ = run(journal("JNL-0001", "2026-01-05", line("1010", "SGD", "10.00"), line("4010", "SGD", "-10.00")))
    assert "people" not in result


# req: FR-EQ-01
# req: QA-ENG-01
@pytest.mark.parametrize(
    ("lines", "reason"),
    [
        ((line("1010", "SGD", "10.00"), person("2500", "-10.00", None, None)), "director-line-without-person"),
        ((line("1010", "SGD", "10.00"), person("2500", "-10.00", "P0009", "lent")), "director-line-without-person"),
        ((line("1010", "SGD", "10.00"), person("2500", "-10.00", "P0001", None)), "director-line-without-person"),
        ((line("1010", "SGD", "10.00"), person("2500", "-10.00", "P0001", "gift")), "director-line-without-person"),
        ((person("1010", "10.00", "P0001", "lent"), person("2500", "-10.00", "P0001", "lent")), "person-on-another-account"),
        ((person("1010", "10.00", None, "lent"), person("2500", "-10.00", "P0001", "lent")), "person-on-another-account"),
    ],
)
def test_a_line_without_its_person_or_a_person_elsewhere_is_rejected(lines, reason):
    result, ledger = run(journal("JNL-0001", "2026-01-05", *lines))
    assert result["rejected"] == ["JNL-0001"]
    assert result["entries"] == [] and "people" not in result
    assert ledger.reasons == [("JNL-0001", reason)]


def _cost(**more) -> dict:
    doc = {"type": "pre_incorporation_cost", "id": "PIC-9", "version": 1, "date": "2026-01-01", "incurred": "2025-12-01",
           "person": "P0001", "account": "6010", "gross": "109.00", "gst": "9.00"}
    doc.update(more)
    return doc


# req: FR-EQ-03
@pytest.mark.parametrize(
    ("change", "settings", "reason"),
    [
        ({}, {}, "no-books-start"),
        ({"incurred": "2026-01-01"}, {"books_start": "2026-01-01"}, "not-before-books-start"),
        ({"date": "2025-12-31"}, {"books_start": "2026-01-01"}, "posts-before-books-start"),
        ({"person": "P0009"}, {"books_start": "2026-01-01"}, "not-a-director"),
        ({"account": "2500"}, {"books_start": "2026-01-01"}, "account-not-allowed"),
        ({"account": "1010"}, {"books_start": "2026-01-01"}, "account-not-allowed"),
        ({"account": "2200"}, {"books_start": "2026-01-01"}, "account-not-allowed"),
        ({"gst": "109.00"}, {"books_start": "2026-01-01"}, "gst-not-less-than-amount"),
        ({"gross": "0.00"}, {"books_start": "2026-01-01"}, "zero-amount"),
        ({"gst": "0.00", "claim_gst": True}, {"books_start": "2026-01-01"}, "gst-not-claimable"),
        ({"claim_gst": True}, {"books_start": "2026-01-01", "gst": {"registered_from": "2026-02-01", "rates": [{"from": "2024-01-01", "percent": "9"}]}},
         "gst-not-claimable"),
    ],
)
def test_a_cost_before_the_books_start_that_breaks_a_rule_is_rejected(change, settings, reason):
    # D7: a cost paid before the books start, posted on or after it, by a director or shareholder, to an account that is
    # not a director's account, a money account or GST control; its GST claimed only while registered and when shown.
    _, ledger = p1_post(_cost(**change), settings={**P1_SETTINGS, **settings})
    assert ledger.reasons == [("PIC-9", reason)]


# req: FR-EQ-04
def test_a_directors_fee_is_owed_to_the_director_on_2120():
    # §10.2: a S$6,000.00 fee voted on 30 Nov: Dr 6050 6,000.00, Cr 2120 6,000.00 naming P0001.
    fee = {"type": "director_fee", "id": "DF-1", "version": 1, "date": "2026-11-30", "person": "P0001", "account": "6050", "amount": "6000.00"}
    result, _ = p1_post(fee)
    entry = next(e for e in result["entries"] if e["document"] == "DF-1")
    assert [(ln["account"], ln["amount"], ln.get("party")) for ln in entry["lines"]] == [("2120", "-6000.00", "P0001"), ("6050", "6000.00", None)]
    assert result["people"] == {"P0001": {"2120": "-6000.00"}}


# req: FR-EQ-04
@pytest.mark.parametrize(
    ("change", "reason"),
    [
        ({"person": "P0009"}, "not-a-director"),
        ({"account": "2120"}, "account-not-allowed"),
        ({"account": "1010"}, "account-not-allowed"),
        ({"amount": "0.00"}, "zero-amount"),
        ({"amount": "-5.00"}, "wrong-sign"),
    ],
)
def test_a_directors_fee_that_breaks_a_rule_is_rejected(change, reason):
    fee = {"type": "director_fee", "id": "DF-9", "version": 1, "date": "2026-11-30", "person": "P0001", "account": "6050", "amount": "6000.00"}
    fee.update(change)
    _, ledger = p1_post(fee)
    assert ledger.reasons == [("DF-9", reason)]


# req: FR-EQ-07
def test_a_waiver_credits_capital_and_clears_the_persons_account():
    # B.7: P0001 waives 400.00 of a 1,000.00 loan as a shareholder: Dr 2500 400.00 naming them, Cr 3300 400.00.
    loan = {"type": "manual_journal", "id": "JNL-1", "version": 1, "date": "2026-03-01", "memo": "",
            "lines": [{"account": "1010", "currency": "SGD", "amount": "1000.00"},
                      {"account": "2500", "currency": "SGD", "amount": "-1000.00", "party": "P0001", "origin": "lent"}]}
    waiver = {"type": "director_waiver", "id": "DS-1", "version": 1, "date": "2026-03-31", "person": "P0001", "account": "2500",
              "credit": "3300", "amount": "400.00"}
    result, ledger = p1_post(loan, waiver)
    assert ledger.reasons == []
    entry = next(e for e in result["entries"] if e["document"] == "DS-1")
    assert [(ln["account"], ln["amount"], ln.get("party")) for ln in entry["lines"]] == [("2500", "400.00", "P0001"), ("3300", "-400.00", None)]
    assert result["people"] == {"P0001": {"2500": "-600.00"}}


# req: FR-EQ-07
@pytest.mark.parametrize(
    ("change", "reason"),
    [
        ({"person": "P0009"}, "not-a-director"),
        ({"account": "6010"}, "not-a-directors-account"),
        ({"credit": "2120"}, "account-not-allowed"),
        ({"credit": "1010"}, "account-not-allowed"),
        ({"credit": "2200"}, "account-not-allowed"),
        ({"amount": "0.00"}, "zero-amount"),
    ],
)
def test_a_waiver_that_breaks_a_rule_is_rejected(change, reason):
    waiver = {"type": "director_waiver", "id": "DS-9", "version": 1, "date": "2026-03-31", "person": "P0001", "account": "2500",
              "credit": "3300", "amount": "10.00"}
    waiver.update(change)
    _, ledger = p1_post(waiver)
    assert ledger.reasons == [("DS-9", reason)]


# req: FR-EQ-05
# req: FR-BNK-12
def test_a_repayment_debits_each_settlements_account_and_credits_the_bank():
    # B.7: a batch repays P0001 600.00 of a loan and 250.00 of a fee: Dr 2500 600.00 and Dr 2120 250.00 naming them, Cr 1010.
    loan = {"type": "manual_journal", "id": "JNL-1", "version": 1, "date": "2026-03-01", "memo": "",
            "lines": [{"account": "1010", "currency": "SGD", "amount": "1000.00"},
                      {"account": "2500", "currency": "SGD", "amount": "-1000.00", "party": "P0001", "origin": "lent"}]}
    fee = {"type": "director_fee", "id": "DF-1", "version": 1, "date": "2026-03-02", "person": "P0001", "account": "6050", "amount": "250.00"}
    repaid = {"type": "director_repayment", "id": "DR-1", "version": 1, "date": "2026-03-31", "person": "P0001", "account": "1010",
              "settlements": [{"account": "2500", "amount": "600.00"}, {"account": "2120", "amount": "250.00"}]}
    result, ledger = p1_post(loan, fee, repaid)
    assert ledger.reasons == []
    entry = next(e for e in result["entries"] if e["document"] == "DR-1")
    assert [(ln["account"], ln["amount"], ln.get("party")) for ln in entry["lines"]] == [
        ("1010", "-850.00", None), ("2120", "250.00", "P0001"), ("2500", "600.00", "P0001")]
    assert result["people"] == {"P0001": {"2500": "-400.00"}}


# req: FR-EQ-05
@pytest.mark.parametrize(
    ("change", "reason"),
    [
        ({"person": "P0009"}, "not-a-director"),
        ({"account": "6010"}, "not-a-money-account"),
        ({"settlements": []}, "no-settlements"),
        ({"settlements": [{"account": "6010", "amount": "10.00"}]}, "not-a-directors-account"),
        ({"settlements": [{"account": "2500", "amount": "0.00"}]}, "zero-amount"),
    ],
)
def test_a_repayment_that_breaks_a_rule_is_rejected(change, reason):
    repaid = {"type": "director_repayment", "id": "DR-9", "version": 1, "date": "2026-03-31", "person": "P0001", "account": "1010",
              "settlements": [{"account": "2500", "amount": "10.00"}]}
    repaid.update(change)
    _, ledger = p1_post(repaid)
    assert ledger.reasons == [("DR-9", reason)]


# req: RND-SHARE
# req: FR-EQ-06
@pytest.mark.parametrize(
    ("amount", "price", "shares", "converted", "remainder"),
    [
        # Appendix G.3: 12,345.67 at 1.20 is 10,288 shares, 12,345.60 converted, 0.07 left owed; at 0.0375, 329,217
        # shares, 12,345.64 converted (329,217 x 0.0375 = 12,345.6375), 0.03 left, as 329,218 would round to 12,345.68.
        ("12345.67", "1.20", 10288, "12345.60", "0.07"),
        ("12345.67", "0.0375", 329217, "12345.64", "0.03"),
        # One more share that rounds down to the amount still fits: 3 x 0.333334 = 1.000002, which is 1.00.
        ("1.00", "0.333334", 3, "1.00", "0.00"),
        ("0.50", "1.00", 0, "0.00", "0.50"),
    ],
)
def test_rnd_share_allots_whole_shares_and_leaves_the_rest_owed(amount, price, shares, converted, remainder):
    assert money.rnd_share(D(amount), D(price), 2) == (shares, D(converted), D(remainder))


# req: RND-SHARE
@given(cents=st.integers(min_value=1, max_value=10**10), micros=st.integers(min_value=1, max_value=10**8))
def test_rnd_share_is_the_largest_whole_number_that_fits(cents, micros):
    amount, price = D(cents).scaleb(-2), D(micros).scaleb(-6)
    n, converted, remainder = money.rnd_share(amount, price, 2)
    worth = lambda k: (D(k) * price).quantize(D("0.01"), rounding=ROUND_HALF_UP)  # noqa: E731
    assert worth(n) == converted <= amount
    assert worth(n + 1) > amount
    assert converted + remainder == amount


# req: FR-EQ-06
# req: RND-SHARE
def test_a_conversion_allots_whole_shares_and_leaves_the_rest_owed():
    # Appendix G.3: 12,345.67 at 1.20 a share is 10,288 shares; Dr 2500 12,345.60 naming P0001, Cr 3010 12,345.60, and
    # 0.07 stays owed on 2500.
    loan = {"type": "manual_journal", "id": "JNL-1", "version": 1, "date": "2026-03-01", "memo": "",
            "lines": [{"account": "1010", "currency": "SGD", "amount": "12345.67"},
                      {"account": "2500", "currency": "SGD", "amount": "-12345.67", "party": "P0001", "origin": "lent"}]}
    converted = {"type": "director_conversion", "id": "SI-1", "version": 1, "date": "2026-03-31", "person": "P0001", "account": "2500",
                 "class": "3010", "amount": "12345.67", "price": "1.20"}
    result, ledger = p1_post(loan, converted)
    assert ledger.reasons == []
    entry = next(e for e in result["entries"] if e["document"] == "SI-1")
    assert [(ln["account"], ln["amount"], ln.get("party")) for ln in entry["lines"]] == [("2500", "12345.60", "P0001"), ("3010", "-12345.60", None)]
    assert result["people"] == {"P0001": {"2500": "-0.07"}}


# req: FR-EQ-06
def test_a_conversion_at_fair_value_puts_the_difference_in_profit_or_loss():
    # 10,288 shares at a fair value of 1.25 is 12,860.00: Cr 3010 12,860.00 and Dr 6010 514.40, the loss.
    converted = {"type": "director_conversion", "id": "SI-2", "version": 1, "date": "2026-03-31", "person": "P0001", "account": "2500",
                 "class": "3010", "amount": "12345.67", "price": "1.20", "fair_value_price": "1.25", "difference": "6010"}
    result, ledger = p1_post(_lent("JNL-1", "2026-03-01", "12345.67"), converted)
    assert ledger.reasons == []
    entry = next(e for e in result["entries"] if e["document"] == "SI-2")
    assert [(ln["account"], ln["amount"]) for ln in entry["lines"]] == [("2500", "12345.60"), ("3010", "-12860.00"), ("6010", "514.40")]


# req: FR-EQ-06
@pytest.mark.parametrize(
    ("change", "reason"),
    [
        ({"person": "P0009"}, "not-a-director"),
        ({"class": "6010"}, "not-share-capital"),
        ({"account": "6010"}, "not-a-directors-account"),
        ({"amount": "0.50"}, "no-whole-share"),
        ({"fair_value_price": "1.25"}, "unknown-account"),
        ({"fair_value_price": "1.25", "difference": "1010"}, "account-not-allowed"),
    ],
)
def test_a_conversion_that_breaks_a_rule_is_rejected(change, reason):
    converted = {"type": "director_conversion", "id": "SI-9", "version": 1, "date": "2026-03-31", "person": "P0001", "account": "2500",
                 "class": "3010", "amount": "10.00", "price": "1.00"}
    converted.update(change)
    _, ledger = p1_post(converted)
    assert ledger.reasons == [("SI-9", reason)]


# What is owed, and nothing more (README, "P1 readings"; FR-EQ-05 rev 0.168, PB-25). What the company owes a person on a
# director's account is the credit of the lines naming them there, from the documents before it in the scenario dated on
# or before its date; reversals come after every document, so none counts.


def _lent(doc_id: str, day: str, amount: str, who: str = "P0001", account: str = "2500") -> dict:
    return journal(doc_id, day, line("1010", "SGD", amount), person(account, f"-{amount}", who, "lent"))


def _waiver(amount: str, day: str = "2026-03-31", account: str = "2500") -> dict:
    return {"type": "director_waiver", "id": "DS-1", "version": 1, "date": day, "person": "P0001", "account": account,
            "credit": "3300", "amount": amount}


def _repayment(*settlements: tuple[str, str]) -> dict:
    return {"type": "director_repayment", "id": "DR-1", "version": 1, "date": "2026-03-31", "person": "P0001", "account": "1010",
            "settlements": [{"account": a, "amount": amount} for a, amount in settlements]}


def _conversion(amount: str, price: str = "1.20") -> dict:
    return {"type": "director_conversion", "id": "SI-1", "version": 1, "date": "2026-03-31", "person": "P0001", "account": "2500",
            "class": "3010", "amount": amount, "price": price}


# Before each test's document in the scenario: S$1,000.00 lent on 1 March, and S$500.00 more on 1 April, a credit dated
# after the document, which is not owed yet on 31 March.
OWED_BEFORE = (_lent("JNL-1", "2026-03-01", "1000.00"), _lent("JNL-2", "2026-04-01", "500.00"))


def _refused(ledger: Ledger, doc_id: str) -> bool:
    return (doc_id, "more-than-owed") in ledger.reasons


def _post_for_two(*documents: dict, reversals: tuple = ()) -> Ledger:
    data = p1_scenario(*documents, people=("P0001", "P0002"))
    data["reversals"] = [{"document": d, "date": day} for d, day in reversals]
    return Ledger(scenario_from_json(data))


# req: FR-EQ-07
# req: FR-EQ-05
@pytest.mark.parametrize(("amount", "posts"), [("1000.00", True), ("1000.01", False), ("1500.00", False)])
def test_a_waiver_draws_at_most_what_is_owed_at_the_end_of_its_date(amount, posts):
    result, ledger = p1_post(*OWED_BEFORE, _waiver(amount))
    assert ("DS-1" not in result["rejected"]) is posts
    assert _refused(ledger, "DS-1") is not posts
    if not posts:  # rejected whole: the waiver's lines are nowhere
        assert all(e["document"] != "DS-1" for e in result["entries"])


# req: FR-EQ-05
# req: FR-BNK-12
@pytest.mark.parametrize(
    ("settlements", "posts"),
    [
        ((("2500", "1000.00"), ("2120", "250.00")), True),  # each account at its limit
        ((("2500", "600.00"), ("2500", "400.00"), ("2120", "250.00")), True),  # two on 2500, together at the limit
        ((("2500", "1000.01"), ("2120", "250.00")), False),  # a cent above on 2500
        ((("2500", "600.00"), ("2500", "400.01")), False),  # each within, together a cent above
        ((("2500", "10.00"), ("2120", "250.01")), False),  # a cent above on 2120: the whole batch is refused
        ((("2500", "1500.00"),), False),  # the credit dated 1 April is not owed on 31 March
    ],
)
def test_a_repayments_settlements_on_each_account_together_are_at_most_what_is_owed(settlements, posts):
    fee = {"type": "director_fee", "id": "DF-1", "version": 1, "date": "2026-03-02", "person": "P0001", "account": "6050",
           "amount": "250.00"}
    result, ledger = p1_post(*OWED_BEFORE, fee, _repayment(*settlements))
    assert ("DR-1" not in result["rejected"]) is posts
    assert _refused(ledger, "DR-1") is not posts
    if not posts:
        assert result["people"] == {"P0001": {"2120": "-250.00", "2500": "-1500.00"}}


# req: FR-EQ-06
# req: FR-EQ-05
@pytest.mark.parametrize(
    ("amount", "posts"),
    [
        ("1000.00", True),  # 833 shares, 999.60 converted, 0.40 left owed
        # One cent above what is owed is refused, though its 833 shares would convert only 999.60: the limit is on the
        # amount (README, "Conversions": "`amount` is at most what is owed on `account`").
        ("1000.01", False),
        ("1500.00", False),
    ],
)
def test_a_conversion_converts_at_most_what_is_owed(amount, posts):
    result, ledger = p1_post(*OWED_BEFORE, _conversion(amount))
    assert ("SI-1" not in result["rejected"]) is posts
    assert _refused(ledger, "SI-1") is not posts


# req: FR-EQ-05
def test_what_is_owed_counts_only_the_documents_before_it_on_or_before_its_date():
    # A credit after the waiver in the scenario (though dated before it) does not count; nor does another person's, nor
    # one on the other director's account; a reversal comes after every document, so the reversed loan still counts.
    later = _lent("JNL-3", "2026-03-15", "500.00")
    others = (_lent("JNL-4", "2026-03-01", "500.00", who="P0002"), _lent("JNL-5", "2026-03-01", "500.00", account="2120"))
    ledger = _post_for_two(*OWED_BEFORE, *others, _waiver("1000.01"), later)
    assert ledger.reasons == [("DS-1", "more-than-owed")]
    ledger = _post_for_two(*OWED_BEFORE, _waiver("1000.00"), reversals=(("JNL-1", "2026-03-02"),))
    assert ledger.reasons == [] and ledger.rejected == []


# req: FR-EQ-05
@pytest.mark.parametrize("document", [_waiver("0.01"), _repayment(("2500", "0.01")), _conversion("1.20")])
def test_nothing_owed_refuses_any_amount(document):
    # P0001 lent 1,000.00 and was repaid 1,200.00 by journal: they owe the company 200.00, and are owed nothing.
    repaid = journal("JNL-9", "2026-03-10", person("2500", "1200.00", "P0001", "paid_out"), line("1010", "SGD", "-1200.00"))
    _, ledger = p1_post(OWED_BEFORE[0], repaid, document)
    assert ledger.reasons == [(document["id"], "more-than-owed")]
    _, ledger = p1_post(document)  # no line on the account at all
    assert ledger.reasons == [(document["id"], "more-than-owed")]
