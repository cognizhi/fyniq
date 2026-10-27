"""Bank lines, their spreads and transfers (bank_transaction@1, bank_transfer@1), worked by hand.

Every expected figure is worked out in the comment beside it from PRD-001 Appendix B.1, §10.2 and
rev 0.140/0.141, never by running the oracle or the engine (QA-ENG-01). A P1 entry is written in its
canonical form: one line per account and currency, sorted (test/differential/README.md).
"""

from decimal import Decimal as D

import pytest
from builders import P1_SETTINGS, bank_line, leg, lines_of, p1_post, part, transfer

from oracle import money


# req: ALLOC-EVEN
def test_g4_015_one_dollar_over_six_months():
    # Cumulative: 0.17, 0.33, 0.50, 0.67, 0.83, 1.00 -> 0.17, 0.16, 0.17, 0.17, 0.16, 0.17.
    parts = money.alloc_even(D("1.00"), 6, 2)
    assert parts == [D(x) for x in ("0.17", "0.16", "0.17", "0.17", "0.16", "0.17")]
    assert money.alloc_even(D("-1.00"), 6, 2) == [-p for p in parts]


# req: ALLOC-EVEN
def test_alloc_even_in_whole_yen_and_one_period():
    # 100 yen over 3: cumulative 33.33 -> 33, 66.67 -> 67, 100 -> 33, 34, 33.
    assert money.alloc_even(D("100"), 3, 0) == [D("33"), D("34"), D("33")]
    assert money.alloc_even(D("12.34"), 1, 2) == [D("12.34")]


# req: QA-ENG-01
def test_money_out_with_input_gst():
    # SGD -109.00 on TX at 9%: GST = 109 × 9 ÷ 109 = 9.00; the expense takes 100.00.
    result, _ = p1_post(bank_line("BT-1", "2026-03-05", "1010", "SGD", "-109.00", part("6010", "-109.00", "TX")))
    assert result["rejected"] == []
    assert lines_of(result, "BT-1") == [
        ("1010", "SGD", "-109.00", "-109.00"),
        ("2200", "SGD", "9.00", "9.00"),
        ("6010", "SGD", "100.00", "100.00"),
    ]


# req: QA-ENG-01
def test_money_in_with_output_gst_that_does_not_divide_evenly():
    # SGD 100.00 on SR: GST = 900 ÷ 109 = 8.2568... -> 8.26; revenue 91.74.
    result, _ = p1_post(bank_line("BT-1", "2026-03-05", "1010", "SGD", "100.00", part("4010", "100.00", "SR")))
    assert lines_of(result, "BT-1") == [
        ("1010", "SGD", "100.00", "100.00"),
        ("2200", "SGD", "-8.26", "-8.26"),
        ("4010", "SGD", "-91.74", "-91.74"),
    ]


# req: QA-ENG-01
def test_no_gst_before_registration_or_on_a_code_without_it():
    # Registered from 1 Jan 2025: a line on 31 Dec 2024 takes no GST, and neither does ZP.
    result, _ = p1_post(
        bank_line("BT-1", "2024-12-31", "1010", "SGD", "-109.00", part("6010", "-109.00", "TX")),
        bank_line("BT-2", "2026-03-05", "1010", "SGD", "-109.00", part("6010", "-109.00", "ZP")),
    )
    assert lines_of(result, "BT-1") == [("1010", "SGD", "-109.00", "-109.00"), ("6010", "SGD", "109.00", "109.00")]
    assert lines_of(result, "BT-2") == [("1010", "SGD", "-109.00", "-109.00"), ("6010", "SGD", "109.00", "109.00")]


# req: QA-ENG-01
def test_the_rate_in_force_on_the_lines_date():
    # 8% in 2023: SGD -108.00 on TX: 864 ÷ 108 = 8.00. (Registered from 2023 in these settings.)
    settings = {**P1_SETTINGS, "gst": {**P1_SETTINGS["gst"], "registered_from": "2023-01-01"}}
    result, _ = p1_post(
        bank_line("BT-1", "2023-06-30", "1010", "SGD", "-108.00", part("6010", "-108.00", "TX")), settings=settings
    )
    assert ("2200", "SGD", "8.00", "8.00") in lines_of(result, "BT-1")


# req: QA-ENG-01
def test_a_split_foreign_line_puts_the_rnd_fx_cent_on_7030():
    # USD -100.01 at 1.3335: the line -133.363335 -> -133.36; the parts -50.00 -> -66.675 -> -66.68 and
    # -50.01 -> -66.688335 -> -66.69, together -133.37; the cent between, -133.36 + 133.37 = 0.01, is
    # balanced by 7030 -0.01. The parts' lines are in SGD: "the rest" is a base amount.
    result, _ = p1_post(
        bank_line("BT-1", "2026-03-05", "1021", "USD", "-100.01",
                  part("6010", "-50.00"), part("6110", "-50.01"), rate="1.3335")
    )
    assert lines_of(result, "BT-1") == [
        ("1021", "USD", "-100.01", "-133.36"),
        ("6010", "SGD", "66.68", "66.68"),
        ("6110", "SGD", "66.69", "66.69"),
        ("7030", "SGD", "-0.01", "-0.01"),
    ]


# req: QA-ENG-01
def test_gst_on_a_foreign_line_is_extracted_from_its_base_amount():
    # USD -109.00 at 1.35 = -147.15; TX: GST = 147.15 × 9 ÷ 109 = 12.15 exactly; the expense 135.00.
    result, _ = p1_post(
        bank_line("BT-1", "2026-03-05", "1021", "USD", "-109.00", part("6010", "-109.00", "TX"), rate="1.35")
    )
    assert lines_of(result, "BT-1") == [
        ("1021", "USD", "-109.00", "-147.15"),
        ("2200", "SGD", "12.15", "12.15"),
        ("6010", "SGD", "135.00", "135.00"),
    ]


# req: FR-GL-03
def test_money_in_spread_over_three_months_releases_monthly_up_to_as_of():
    # SGD 327.00 on SR: GST 27.00, revenue 300.00 deferred to 2400 on 10 Mar. ALLOC-EVEN gives 100.00 a
    # month, released from the posting month: 10 Mar (the first of March is before the line), 1 Apr
    # and 1 May. With as_of 15 Apr the May release has not come.
    doc = bank_line("BT-1", "2026-03-10", "1010", "SGD", "327.00", part("4010", "327.00", "SR", spread_months=3))
    result, _ = p1_post(doc, as_of="2026-04-15")
    assert lines_of(result, "BT-1") == [
        ("1010", "SGD", "327.00", "327.00"),
        ("2200", "SGD", "-27.00", "-27.00"),
        ("2400", "SGD", "-300.00", "-300.00"),
    ]
    assert [(e["kind"], e["posting_date"]) for e in result["entries"]] == [
        ("post", "2026-03-10"),
        ("release", "2026-03-10"),
        ("release", "2026-04-01"),
    ]
    assert lines_of(result, "BT-1", "release") == [("2400", "SGD", "100.00", "100.00"), ("4010", "SGD", "-100.00", "-100.00")]
    assert result["trial_balance"] == {
        "2026-03": {"1010": "327.00", "2200": "-27.00", "2400": "-200.00", "4010": "-100.00"},
        "2026-04": {"2400": "100.00", "4010": "-100.00"},
    }


# req: FR-GL-03
def test_money_out_spread_goes_through_prepayments_and_never_releases_a_zero_month():
    # SGD -0.02 on 6010 (no GST) over 3 months: 1210 takes 0.02; ALLOC-EVEN's cumulative amounts are
    # 0.0067 -> 0.01, 0.0133 -> 0.01 and 0.02, so the parts are 0.01, 0.00, 0.01: February releases
    # nothing. January's is dated the line's day, 31 Jan.
    doc = bank_line("BT-1", "2026-01-31", "1010", "SGD", "-0.02", part("6010", "-0.02", spread_months=3))
    result, _ = p1_post(doc)
    assert lines_of(result, "BT-1") == [("1010", "SGD", "-0.02", "-0.02"), ("1210", "SGD", "0.02", "0.02")]
    assert [(e["kind"], e["posting_date"]) for e in result["entries"]] == [
        ("post", "2026-01-31"),
        ("release", "2026-01-31"),
        ("release", "2026-03-01"),
    ]
    assert result["trial_balance"]["2026-03"] == {"1210": "-0.01", "6010": "0.01"}


# req: FR-BNK-09
def test_a_same_currency_transfer_is_recorded_once():
    result, _ = p1_post(transfer("TR-1", "2026-03-05", leg("1010", "SGD", "500.00"), leg("1011", "SGD", "500.00")))
    assert lines_of(result, "TR-1") == [("1010", "SGD", "-500.00", "-500.00"), ("1011", "SGD", "500.00", "500.00")]


# req: FR-BNK-09
def test_a_wise_sgd_to_usd_conversion_with_a_fee():
    # Paid SGD 1,000.00, of which a SGD 4.50 fee; received USD 738.00 at the day's 1.3485 = 995.193 ->
    # 995.19. 7030 takes what is left: -(995.19 - 1,000.00 + 4.50) = 0.31.
    result, _ = p1_post(
        transfer("TR-1", "2026-03-05", leg("1010", "SGD", "1000.00"), leg("1021", "USD", "738.00", "1.3485"), fee="4.50")
    )
    assert lines_of(result, "TR-1") == [
        ("1010", "SGD", "-1000.00", "-1000.00"),
        ("1021", "USD", "738.00", "995.19"),
        ("6460", "SGD", "4.50", "4.50"),
        ("7030", "SGD", "0.31", "0.31"),
    ]


# req: FR-BNK-09
def test_a_usd_to_sgd_conversion_without_a_fee():
    # Paid USD 100.00 at 1.3501 = 135.01; received SGD 134.20; 7030 takes 0.81 (a loss).
    result, _ = p1_post(transfer("TR-1", "2026-03-05", leg("1021", "USD", "100.00", "1.3501"), leg("1010", "SGD", "134.20")))
    assert lines_of(result, "TR-1") == [
        ("1010", "SGD", "134.20", "134.20"),
        ("1021", "USD", "-100.00", "-135.01"),
        ("7030", "SGD", "0.81", "0.81"),
    ]


def _refused(document, reason, **options):
    result, ledger = p1_post(document, **options)
    assert result["rejected"] == [document["id"]]
    assert result["entries"] == [] and result["trial_balance"] == {}
    assert ledger.reasons == [(document["id"], reason)]


# req: QA-ENG-01
@pytest.mark.parametrize(
    ("document", "reason"),
    [
        (bank_line("BT-1", "2026-03-05", "1010", "SGD", "-10.00"), "no-parts"),
        (bank_line("BT-1", "2026-03-05", "1010", "SGD", "-10.00", part("6010", "-9.00")), "parts-do-not-add-up"),
        (bank_line("BT-1", "2026-03-05", "1010", "SGD", "-10.00", part("6010", "-11.00"), part("6110", "1.00")),
         "wrong-sign"),
        (bank_line("BT-1", "2026-03-05", "1010", "SGD", "-10.00", part("1011", "-10.00")), "part-on-money-account"),
        (bank_line("BT-1", "2026-03-05", "1010", "SGD", "10.00", part("1100", "10.00")), "part-on-receivable-or-payable"),
        (bank_line("BT-1", "2026-03-05", "1010", "SGD", "-10.00", part("2010", "-10.00")), "part-on-receivable-or-payable"),
        (bank_line("BT-1", "2026-03-05", "1010", "SGD", "10.00", part("3010", "10.00")), "share-capital-account"),
        (bank_line("BT-1", "2026-03-05", "1010", "SGD", "10.00", part("4000", "10.00")), "group-account"),
        (bank_line("BT-1", "2026-03-05", "1010", "SGD", "-10.00", part("6010", "-10.00", "XX")), "unknown-gst-code"),
        (bank_line("BT-1", "2026-03-05", "1010", "SGD", "-10.00", part("1500", "-10.00", spread_months=12)),
         "spread-not-p-and-l"),
        (bank_line("BT-1", "2026-03-05", "1010", "SGD", "10.00", part("6010", "10.00", spread_months=12)),
         "spread-not-p-and-l"),
        (bank_line("BT-1", "2026-03-05", "1010", "SGD", "-10.00", part("6010", "-10.00", spread_months=0)), "spread-months"),
        (bank_line("BT-1", "2026-03-05", "1010", "SGD", "-10.00", part("6010", "-10.00", spread_months=121)),
         "spread-months"),
        (bank_line("BT-1", "2026-03-05", "1010", "SGD", "0.00", part("6010", "0.00")), "zero-amount"),
        (bank_line("BT-1", "2026-03-05", "1010", "SGD", "-10.001", part("6010", "-10.001")), "amount-too-many-decimals"),
        (bank_line("BT-1", "2026-03-05", "1021", "USD", "-10.00", part("6010", "-10.00")), "missing-rate"),
        (bank_line("BT-1", "2026-03-05", "1010", "SGD", "-10.00", part("6010", "-10.00"), rate="1"), "rate-on-base-amount"),
        (bank_line("BT-1", "2026-03-05", "1500", "SGD", "-10.00", part("6010", "-10.00")), "not-a-money-account"),
        (bank_line("BT-1", "2026-03-05", "1021", "SGD", "-10.00", part("6010", "-10.00")), "money-account-currency"),
        (bank_line("BT-1", "2026-03-05", "1090", "SGD", "-10.00", part("6010", "-10.00")), "inactive-account"),
        (bank_line("BT-1", "2026-03-05", "1010", "XTS", "-10.00", part("6010", "-10.00")), "unknown-currency"),
    ],
)
def test_a_bank_line_that_breaks_a_rule_is_rejected(document, reason):
    _refused(document, reason)


# req: QA-ENG-01
def test_a_line_whose_gst_needs_a_rate_not_in_force_is_rejected():
    settings = {**P1_SETTINGS, "gst": {"registered_from": "2025-01-01", "rates": [{"from": "2027-01-01", "percent": "9"}]}}
    _refused(bank_line("BT-1", "2026-03-05", "1010", "SGD", "-10.90", part("6010", "-10.90", "TX")), "no-gst-rate",
             settings=settings)


# req: QA-ENG-01
def test_a_line_needing_a_system_account_the_organisation_has_not_set_is_rejected():
    settings = {**P1_SETTINGS, "accounts": {k: v for k, v in P1_SETTINGS["accounts"].items() if k != "gst"}}
    _refused(bank_line("BT-1", "2026-03-05", "1010", "SGD", "-10.90", part("6010", "-10.90", "TX")), "no-gst-account",
             settings=settings)


# req: FR-BNK-09
@pytest.mark.parametrize(
    ("document", "reason"),
    [
        (transfer("TR-1", "2026-03-05", leg("1010", "SGD", "5.00"), leg("1010", "SGD", "5.00")), "transfer-to-itself"),
        (transfer("TR-1", "2026-03-05", leg("1010", "SGD", "5.00"), leg("1011", "SGD", "4.00")), "transfer-does-not-balance"),
        (transfer("TR-1", "2026-03-05", leg("1010", "SGD", "5.00"), leg("1011", "SGD", "4.00"), fee="1.00"), None),
        (transfer("TR-1", "2026-03-05", leg("1010", "SGD", "5.00"), leg("1021", "USD", "4.00", "1.25"), fee="5.00"),
         "fee-not-below-paid"),
        (transfer("TR-1", "2026-03-05", leg("1010", "SGD", "5.00"), leg("1021", "USD", "4.00")), "missing-rate"),
        (transfer("TR-1", "2026-03-05", leg("1010", "SGD", "-5.00"), leg("1011", "SGD", "-5.00")), "wrong-sign"),
        (transfer("TR-1", "2026-03-05", leg("1500", "SGD", "5.00"), leg("1011", "SGD", "5.00")), "not-a-money-account"),
    ],
)
def test_a_transfer_that_breaks_a_rule_is_rejected(document, reason):
    if reason is None:  # paid = received + fee in one currency: it posts
        result, _ = p1_post(document)
        assert lines_of(result, "TR-1") == [
            ("1010", "SGD", "-5.00", "-5.00"),
            ("1011", "SGD", "4.00", "4.00"),
            ("6460", "SGD", "1.00", "1.00"),
        ]
        return
    _refused(document, reason)
