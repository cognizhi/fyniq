"""Director's accounts: each line names its person, and each person's totals (FR-EQ-01, B.7; PRD-001 rev 0.167).

Worked by hand. Mei Lin (P0001) lends S$1,500.00 into the bank on 5 January and is repaid S$400.00 on 9
January: on 2500 she has −1,500.00 and +400.00, a total of −1,100.00 (the company owes her S$1,100.00).
Arjun (P0002) pays S$300.00 of costs personally on 7 January, kept as a loan (−300.00 on 2500), and has
S$250.00 of fees owed on 2120 (−250.00). Mei Lin's loan journal is then reversed on 12 January, so her
total is +400.00: she owes the company S$400.00.
"""

import pytest
from builders import P1_SETTINGS, journal, line, p1_post, scenario

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
