"""Expense claims, mileage, reimbursements and cash advances, worked by hand (expense_claim@1,
claim_reimbursement@1, cash_advance@1, RND-MILEAGE).

Every expected figure is worked out in the comment beside it from PRD-001 §10.2, §10.4, FR-AP-07,
FR-AP-16, FR-AP-17 and FR-AP-20, never by running the oracle or the engine (QA-ENG-01). The mileage
rates are the recommended S$0.60 a km for a car and S$0.25 for a motorcycle (rev 0.151) [verify].
"""

from decimal import Decimal as D

import pytest
from builders import P1_SETTINGS, advance, bill_line, claim, lines_of, p1_post, settlement, trip

from oracle import money


# req: RND-MILEAGE
@pytest.mark.parametrize(
    ("km", "rate", "amount"),
    [
        ("12.34", "0.60", "7.38"),  # 12.3 km × 0.60 = 7.38
        ("12.35", "0.60", "7.44"),  # 12.35 -> 12.4 km (half up) × 0.60 = 7.44
        ("7.25", "0.25", "1.83"),  # 7.3 km × 0.25 = 1.825 -> 1.83
        ("0.04", "0.60", "0.00"),  # 0.0 km
        ("100", "0.60", "60.00"),
    ],
)
def test_rnd_mileage_rounds_the_distance_to_a_tenth_then_the_amount_to_the_cent(km, rate, amount):
    assert money.rnd_mileage(D(km), D(rate), 2) == D(amount)


# req: FR-AP-07
def test_a_claim_with_a_receipt_and_a_trip():
    # Receipt 100.00 + GST 9.00 on TX: 2200 9.00, 6010 100.00. Trip 12.35 km by car: 7.44 to 6110, no GST.
    # 2150 owes 116.44.
    result, _ = p1_post(claim("EC-1", "2026-03-05", "SGD", bill_line("6010", "100.00", "TX", "9.00"), trip("6110", "12.35")))
    assert lines_of(result, "EC-1") == [
        ("2150", "SGD", "-116.44", "-116.44"),
        ("2200", "SGD", "9.00", "9.00"),
        ("6010", "SGD", "100.00", "100.00"),
        ("6110", "SGD", "7.44", "7.44"),
    ]


# req: FR-AP-16
def test_the_mileage_rate_is_the_vehicles_in_force_on_the_claims_date():
    # Motorcycle 7.25 km at 0.25 = 1.83; from 1 Jul 2026 a new car rate of 0.70: 10 km = 7.00.
    settings = {**P1_SETTINGS, "mileage_rates": [*P1_SETTINGS["mileage_rates"],
                                                 {"vehicle": "car", "from": "2026-07-01", "rate": "0.70"}]}
    result, _ = p1_post(
        claim("EC-1", "2026-06-30", "SGD", trip("6110", "7.25", "motorcycle"), trip("6110", "10")),
        claim("EC-2", "2026-07-01", "SGD", trip("6110", "10")),
        settings=settings,
    )
    assert lines_of(result, "EC-1") == [("2150", "SGD", "-7.83", "-7.83"), ("6110", "SGD", "7.83", "7.83")]
    assert lines_of(result, "EC-2") == [("2150", "SGD", "-7.00", "-7.00"), ("6110", "SGD", "7.00", "7.00")]


# req: FR-AP-17
def test_a_usd_claim_is_reimbursed_in_sgd_at_the_cascades_rate():
    # USD 50.00 at 1.35 = 67.50 owed on 2150; reimbursed from the SGD account at 1.33: 66.50; 7030 -1.00.
    result, _ = p1_post(
        claim("EC-1", "2026-03-05", "USD", bill_line("6010", "50.00", "OP"), rate="1.35"),
        settlement("claim_reimbursement", "RE-1", "2026-03-31", "1010", "USD", ("EC-1", "50.00"), rate="1.33"),
    )
    assert lines_of(result, "EC-1") == [("2150", "USD", "-50.00", "-67.50"), ("6010", "USD", "50.00", "67.50")]
    assert lines_of(result, "RE-1") == [
        ("1010", "SGD", "-66.50", "-66.50"),
        ("2150", "USD", "50.00", "67.50"),
        ("7030", "SGD", "-1.00", "-1.00"),
    ]


# req: FR-AP-17
def test_an_advance_settled_by_claims_and_its_remainder_refunded():
    # 500.00 advanced. A claim of 116.44 settles against it: 1250 -116.44, nothing to 2150. A second of
    # 450.00 takes the 383.56 left and owes 66.44 on 2150. A refund then finds nothing left.
    result, ledger = p1_post(
        advance("ADV-1", "2026-03-01", "500.00"),
        claim("EC-1", "2026-03-05", "SGD", bill_line("6010", "100.00", "TX", "9.00"), trip("6110", "12.35"),
              advance="ADV-1"),
        claim("EC-2", "2026-03-06", "SGD", bill_line("6010", "450.00", "OP"), advance="ADV-1"),
        advance("ADV-1R", "2026-03-31", "0.01", refund_of="ADV-1"),
    )
    assert lines_of(result, "ADV-1") == [("1010", "SGD", "-500.00", "-500.00"), ("1250", "SGD", "500.00", "500.00")]
    assert lines_of(result, "EC-1") == [
        ("1250", "SGD", "-116.44", "-116.44"),
        ("2200", "SGD", "9.00", "9.00"),
        ("6010", "SGD", "100.00", "100.00"),
        ("6110", "SGD", "7.44", "7.44"),
    ]
    assert lines_of(result, "EC-2") == [
        ("1250", "SGD", "-383.56", "-383.56"),
        ("2150", "SGD", "-66.44", "-66.44"),
        ("6010", "SGD", "450.00", "450.00"),
    ]
    assert ledger.reasons == [("ADV-1R", "more-than-left-of-advance")]


# req: FR-AP-17
def test_what_is_left_of_an_advance_comes_back():
    result, _ = p1_post(advance("ADV-1", "2026-03-01", "500.00"),
                        advance("ADV-1R", "2026-03-31", "120.00", refund_of="ADV-1"))
    assert lines_of(result, "ADV-1R") == [("1010", "SGD", "120.00", "120.00"), ("1250", "SGD", "-120.00", "-120.00")]


# req: FR-AP-07
def test_a_reimbursement_pays_what_is_owed():
    result, _ = p1_post(
        claim("EC-1", "2026-03-05", "SGD", trip("6110", "100")),
        settlement("claim_reimbursement", "RE-1", "2026-03-31", "1010", "SGD", ("EC-1", "60.00")),
    )
    assert lines_of(result, "RE-1") == [("1010", "SGD", "-60.00", "-60.00"), ("2150", "SGD", "60.00", "60.00")]


def _refused(documents, doc_id, reason):
    result, ledger = p1_post(*documents)
    assert doc_id in result["rejected"]
    assert dict(ledger.reasons)[doc_id] == reason


EC = claim("EC-1", "2026-03-05", "SGD", trip("6110", "100"))


# req: QA-ENG-01
@pytest.mark.parametrize(
    ("document", "reason"),
    [
        (claim("EC-9", "2026-03-05", "USD", trip("6110", "10"), rate="1.3"), "mileage-in-another-currency"),
        (claim("EC-9", "2025-12-31", "SGD", trip("6110", "10")), "no-mileage-rate"),
        (claim("EC-9", "2026-03-05", "SGD", trip("6110", "10", "van")), "no-mileage-rate"),
        (claim("EC-9", "2026-03-05", "SGD", trip("6110", "0.04")), "zero-amount"),
        (claim("EC-9", "2026-03-05", "SGD", {"account": "6110", "gst_code": "NA", "amount": "1.00", "vehicle": "car"}),
         "vehicle-without-distance"),
        (claim("EC-9", "2026-03-05", "SGD"), "no-lines"),
        (claim("EC-9", "2026-03-05", "SGD", trip("6110", "10"), advance="EC-1"), "not-a-posted-advance"),
        (claim("EC-9", "2026-03-05", "USD", bill_line("6010", "1.00", "OP"), rate="1.3", advance="ADV-1"),
         "advance-currency"),
        (advance("ADV-9", "2026-03-05", "10.00", account="1021"), "money-account-currency"),
        (advance("ADV-9", "2026-03-05", "10.00", refund_of="EC-1"), "not-a-posted-advance"),
        (settlement("claim_reimbursement", "RE-9", "2026-03-31", "1010", "SGD", ("EC-1", "59.50", True)),
         "in-full-not-allowed"),
        (settlement("claim_reimbursement", "RE-9", "2026-03-31", "1010", "SGD", ("EC-1", "50.00"), unapplied="1.00"),
         "unapplied-not-allowed"),
        (settlement("claim_reimbursement", "RE-9", "2026-03-31", "1010", "SGD", ("EC-1", "60.01")), "more-than-open"),
    ],
)
def test_a_claim_document_that_breaks_a_rule_is_rejected(document, reason):
    _refused([advance("ADV-1", "2026-03-01", "500.00"), EC, document], document["id"], reason)
