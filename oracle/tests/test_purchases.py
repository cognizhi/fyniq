"""Bills, vendor credits, payments of bills and bill spreads, worked by hand (bill@1,
vendor_credit@1, bill_payment@1).

Every expected figure is worked out in the comment beside it from PRD-001 §10.2, Appendix B.2, ENG-42,
FR-BNK-08 and the rows of rev 0.74 to 0.93, never by running the oracle or the engine (QA-ENG-01).
"""

from decimal import Decimal as D

import pytest
from builders import P1_SETTINGS, bill, bill_line, lines_of, p1_post, settlement

from oracle import money

G4_023 = bill("BILL-1", "2026-03-05", "USD", bill_line("6010", "1000.00", "TX", "90.00", gst_base="121.50"), rate="1.34")


# req: RND-LINE
def test_rnd_line_rounds_quantity_times_price_once():
    # 3 × 12.345678 = 37.037034 -> 37.04; 2.5 × 0.01 = 0.025 -> 0.03 (half away from zero).
    assert money.rnd_line(D("3"), D("12.345678"), 2) == D("37.04")
    assert money.rnd_line(D("2.5"), D("0.01"), 2) == D("0.03")
    # With a 12.5% discount: 3 × 10.00 × 87.5 ÷ 100 = 26.25 exactly; 1 × 0.10 × 95 ÷ 100 = 0.095 -> 0.10.
    assert money.rnd_line(D("3"), D("10.00"), 2, D("12.5")) == D("26.25")
    assert money.rnd_line(D("1"), D("0.10"), 2, D("5")) == D("0.10")


# req: RND-LINE
def test_a_later_part_of_a_line_is_what_it_adds_to_rnd_line():
    # 0.333333 a unit: 1 unit -> 0.33; then 2 more: RND-LINE(3) = 0.999999 -> 1.00, less 0.33 = 0.67.
    assert money.rnd_line_part(D("0"), D("1"), D("0.333333"), 2) == D("0.33")
    assert money.rnd_line_part(D("1"), D("2"), D("0.333333"), 2) == D("0.67")


# req: RND-GST-SHARE
@pytest.mark.parametrize(("gst", "share", "claim"), [("9.00", "60", "5.40"), ("0.05", "50", "0.03"),
                                                     ("9.00", "100", "9.00"), ("9.00", "0", "0.00")])
def test_rnd_gst_share(gst, share, claim):
    # 9.00 × 60% = 5.40; 0.05 × 50% = 0.025 -> 0.03; 100 claims all; 0 none.
    assert money.rnd_gst_share(D(gst), D(share), 2) == D(claim)


# req: RND-WHT
def test_rnd_wht_rounds_the_gross_down_to_the_dollar_first():
    # ⌊1,234.56⌋ = 1,234 × 15% = 185.10; ⌊999.99⌋ = 999 × 17% = 169.83.
    assert money.rnd_wht(D("1234.56"), D("15"), 2) == D("185.10")
    assert money.rnd_wht(D("999.99"), D("17"), 2) == D("169.83")


# req: RND-DISCOUNT
def test_rnd_discount():
    # 1,090.00 × 2% = 21.80; 333.33 × 1.5% = 4.99995 -> 5.00.
    assert money.rnd_discount(D("1090.00"), D("2"), 2) == D("21.80")
    assert money.rnd_discount(D("333.33"), D("1.5"), 2) == D("5.00")


# req: ENG-42
def test_g4_023_a_usd_bill_claims_the_sgd_gst_its_invoice_states():
    # Net 1,000.00 × 1.34 = 1,340.00; AP 1,090.00 × 1.34 = 1,460.60; 2200 121.50 as stated (not 120.60);
    # 7030 -(1,340.00 + 121.50 - 1,460.60) = -0.90.
    result, _ = p1_post(G4_023)
    assert lines_of(result, "BILL-1") == [
        ("2010", "USD", "-1090.00", "-1460.60"),
        ("2200", "SGD", "121.50", "121.50"),
        ("6010", "USD", "1000.00", "1340.00"),
        ("7030", "SGD", "-0.90", "-0.90"),
    ]


# req: ENG-42
def test_a_usd_bill_without_sgd_gst_stated_keeps_the_gst_in_the_cost():
    # No gst_base: nothing claimed; the cost is 1,090.00 × 1.34 = 1,460.60, as is AP.
    result, _ = p1_post(bill("BILL-1", "2026-03-05", "USD", bill_line("6010", "1000.00", "TX", "90.00"), rate="1.34"))
    assert lines_of(result, "BILL-1") == [("2010", "USD", "-1090.00", "-1460.60"), ("6010", "USD", "1090.00", "1460.60")]


# req: QA-ENG-01
def test_g4_024_an_sgd_bill_claims_the_gst_as_stated():
    # The supplier rounded per line: 3 × 0.90 = 2.70 stated (the subtotal method gives 2.71); claimed as
    # stated. Cost 30.15; AP 32.85.
    lines = [bill_line("6010", "10.05", "TX", "0.90") for _ in range(3)]
    result, _ = p1_post(bill("BILL-1", "2026-03-05", "SGD", *lines))
    assert lines_of(result, "BILL-1") == [
        ("2010", "SGD", "-32.85", "-32.85"),
        ("2200", "SGD", "2.70", "2.70"),
        ("6010", "SGD", "30.15", "30.15"),
    ]


# req: RND-GST-SHARE
def test_a_business_share_claims_part_of_the_gst_and_keeps_the_rest_in_the_cost():
    # 9.00 × 60% = 5.40 claimed; 3.60 stays: cost 103.60; AP 109.00.
    result, _ = p1_post(bill("BILL-1", "2026-03-05", "SGD", bill_line("6010", "100.00", "TX", "9.00", business_share="60")))
    assert lines_of(result, "BILL-1") == [
        ("2010", "SGD", "-109.00", "-109.00"),
        ("2200", "SGD", "5.40", "5.40"),
        ("6010", "SGD", "103.60", "103.60"),
    ]


# req: RND-GST-SHARE
def test_a_business_share_of_a_usd_bill():
    # Claim 12.15 × 50% = 6.075 -> 6.08 SGD; the cost keeps 9.00 - 4.50 = USD 4.50: USD 104.50 × 1.35 =
    # 141.075 -> 141.08; AP 109.00 × 1.35 = 147.15; 7030 -(141.08 + 6.08 - 147.15) = -0.01.
    result, _ = p1_post(bill("BILL-1", "2026-03-05", "USD",
                             bill_line("6010", "100.00", "TX", "9.00", gst_base="12.15", business_share="50"), rate="1.35"))
    assert lines_of(result, "BILL-1") == [
        ("2010", "USD", "-109.00", "-147.15"),
        ("2200", "SGD", "6.08", "6.08"),
        ("6010", "USD", "104.50", "141.08"),
        ("7030", "SGD", "-0.01", "-0.01"),
    ]


# req: QA-ENG-01
def test_no_input_tax_before_registration_or_on_bl():
    # 31 Dec 2024 is before registration; BL is blocked (GST stays in the cost either way).
    result, _ = p1_post(
        bill("BILL-1", "2024-12-31", "SGD", bill_line("6010", "100.00", "TX", "9.00")),
        bill("BILL-2", "2026-03-05", "SGD", bill_line("6010", "100.00", "BL", "9.00")),
    )
    for doc in ("BILL-1", "BILL-2"):
        assert lines_of(result, doc) == [("2010", "SGD", "-109.00", "-109.00"), ("6010", "SGD", "109.00", "109.00")]


# req: RND-LINE
def test_a_bill_line_by_quantity():
    # 3 × 12.345678 = 37.04; a purchase order's later part, 2 more at 0.333333 after 1: 0.67.
    result, _ = p1_post(bill("BILL-1", "2026-03-05", "SGD",
                             bill_line("6010", None, "NR", quantity="3", unit_price="12.345678"),
                             bill_line("6110", None, "NR", quantity="2", unit_price="0.333333", quantity_before="1")))
    assert lines_of(result, "BILL-1") == [
        ("2010", "SGD", "-37.71", "-37.71"),
        ("6010", "SGD", "37.04", "37.04"),
        ("6110", "SGD", "0.67", "0.67"),
    ]


# req: FR-GL-03
def test_a_bill_spread_from_before_its_date_releases_the_past_months_on_its_date():
    # 1,200.00 + GST 108.00, spread 12 months from January; billed 15 March. 1210 takes 1,200.00; ALLOC-EVEN
    # gives 100.00 a month. January, February and March are dated 15 March (one entry, 300.00); April 1 Apr;
    # as_of 30 April leaves May to December to come.
    doc = bill("BILL-1", "2026-03-15", "SGD",
               bill_line("6010", "1200.00", "TX", "108.00", spread_months=12, spread_from="2026-01-01"))
    result, _ = p1_post(doc, as_of="2026-04-30")
    assert lines_of(result, "BILL-1") == [
        ("1210", "SGD", "1200.00", "1200.00"),
        ("2010", "SGD", "-1308.00", "-1308.00"),
        ("2200", "SGD", "108.00", "108.00"),
    ]
    assert [(e["kind"], e["posting_date"]) for e in result["entries"]] == [
        ("post", "2026-03-15"), ("release", "2026-03-15"), ("release", "2026-04-01")]
    assert lines_of(result, "BILL-1", "release") == [("1210", "SGD", "-300.00", "-300.00"), ("6010", "SGD", "300.00", "300.00")]


# req: FR-GL-03
def test_a_spread_usd_line_defers_its_base_cost():
    # USD 120.00 × 1.3333 = 159.996 -> 160.00 on 1210 (base currency, never revalued); 3 months of 53.33,
    # 53.34, 53.33 (ALLOC-EVEN: 53.333 -> 53.33, 106.667 -> 106.67, 160.00).
    doc = bill("BILL-1", "2026-03-15", "USD", bill_line("6010", "120.00", "NR", spread_months=3), rate="1.3333")
    result, _ = p1_post(doc, as_of="2026-05-31")
    assert lines_of(result, "BILL-1") == [("1210", "SGD", "160.00", "160.00"), ("2010", "USD", "-120.00", "-160.00")]
    releases = [e for e in result["entries"] if e["kind"] == "release"]
    assert [(e["posting_date"], e["lines"][1]["amount"]) for e in releases] == [
        ("2026-03-15", "53.33"), ("2026-04-01", "53.34"), ("2026-05-01", "53.33")]


# req: RND-WHT
def test_withholding_on_an_sgd_bill():
    # ⌊1,234.56⌋ × 15% = 185.10 to 2330; AP 1,234.56 - 185.10 = 1,049.46.
    result, _ = p1_post(bill("BILL-1", "2026-03-05", "SGD", bill_line("6010", "1234.56", "OP"), withholding={"percent": "15"}))
    assert lines_of(result, "BILL-1") == [
        ("2010", "SGD", "-1049.46", "-1049.46"),
        ("2330", "SGD", "-185.10", "-185.10"),
        ("6010", "SGD", "1234.56", "1234.56"),
    ]


# req: RND-WHT
def test_withholding_on_a_usd_bill_puts_the_cents_between_on_7030():
    # Gross 1,000.00 × 1.3456 = 1,345.60, ⌊⌋ 1,345 × 10% = 134.50 to 2330. The supplier's own: USD 100.00
    # withheld, AP USD 900.00 × 1.3456 = 1,211.04; 7030 -(1,345.60 - 134.50 - 1,211.04) = -0.06.
    result, _ = p1_post(bill("BILL-1", "2026-03-05", "USD", bill_line("6010", "1000.00", "OP"), rate="1.3456",
                             withholding={"percent": "10"}))
    assert lines_of(result, "BILL-1") == [
        ("2010", "USD", "-900.00", "-1211.04"),
        ("2330", "SGD", "-134.50", "-134.50"),
        ("6010", "USD", "1000.00", "1345.60"),
        ("7030", "SGD", "-0.06", "-0.06"),
    ]


# req: QA-ENG-01
def test_a_vendor_credit_posts_a_bills_legs_negated():
    result, _ = p1_post(bill("VC-1", "2026-03-05", "SGD", bill_line("6010", "50.00", "TX", "4.50"), kind="vendor_credit"))
    assert lines_of(result, "VC-1") == [
        ("2010", "SGD", "54.50", "54.50"),
        ("2200", "SGD", "-4.50", "-4.50"),
        ("6010", "SGD", "-50.00", "-50.00"),
    ]


SGD_BILL = bill("BILL-1", "2026-03-05", "SGD", bill_line("6010", "1000.00", "TX", "90.00"))


# req: QA-ENG-01
def test_a_bill_paid_in_two_parts():
    result, _ = p1_post(
        SGD_BILL,
        settlement("bill_payment", "PAY-1", "2026-03-10", "1010", "SGD", ("BILL-1", "500.00")),
        settlement("bill_payment", "PAY-2", "2026-04-10", "1010", "SGD", ("BILL-1", "590.00")),
    )
    assert lines_of(result, "PAY-1") == [("1010", "SGD", "-500.00", "-500.00"), ("2010", "SGD", "500.00", "500.00")]
    assert lines_of(result, "PAY-2") == [("1010", "SGD", "-590.00", "-590.00"), ("2010", "SGD", "590.00", "590.00")]


# req: ENG-41
def test_the_usd_bill_paid_from_the_usd_wallet_at_1_30():
    # Rev 0.77's case: Dr 2010 1,460.60 (its stored rate), Cr 1021 1,090.00 × 1.30 = 1,417.00, Cr 7030 43.60.
    result, _ = p1_post(G4_023, settlement("bill_payment", "PAY-1", "2026-03-20", "1021", "USD", ("BILL-1", "1090.00"),
                                           rate="1.30"))
    assert lines_of(result, "PAY-1") == [
        ("1021", "USD", "-1090.00", "-1417.00"),
        ("2010", "USD", "1090.00", "1460.60"),
        ("7030", "SGD", "-43.60", "-43.60"),
    ]


# req: INV-05
def test_the_last_part_clears_the_residual_rnd_fx_leaves():
    # A USD 0.03 bill at 1.5: AP 0.045 -> 0.05. Paid USD 0.01 three times at 1.5: the first two clear
    # 0.015 -> 0.02 each; the third settles what is left, so it clears 0.05 - 0.04 = 0.01; it paid 0.02
    # (0.015 -> 0.02), so 7030 takes -(0.01 - 0.02) = 0.01.
    small = bill("BILL-1", "2026-03-05", "USD", bill_line("6010", "0.03", "NR"), rate="1.5")
    pays = [settlement("bill_payment", f"PAY-{i}", "2026-03-10", "1021", "USD", ("BILL-1", "0.01"), rate="1.5")
            for i in (1, 2, 3)]
    result, _ = p1_post(small, *pays)
    assert lines_of(result, "PAY-1") == [("1021", "USD", "-0.01", "-0.02"), ("2010", "USD", "0.01", "0.02")]
    assert lines_of(result, "PAY-3") == [
        ("1021", "USD", "-0.01", "-0.02"),
        ("2010", "USD", "0.01", "0.01"),
        ("7030", "SGD", "0.01", "0.01"),
    ]
    assert result["trial_balance"]["2026-03"]["6010"] == "0.05"
    assert "2010" not in result["trial_balance"]["2026-03"]  # cleared to nothing


# req: ENG-41
def test_a_usd_bill_paid_from_an_sgd_account():
    # What the account paid, 1,420.00, settles the USD 1,090.00 carried at 1,460.60; 7030 -40.60.
    result, _ = p1_post(G4_023, settlement("bill_payment", "PAY-1", "2026-03-20", "1010", "USD", ("BILL-1", "1090.00"),
                                           base_amount="1420.00"))
    assert lines_of(result, "PAY-1") == [
        ("1010", "SGD", "-1420.00", "-1420.00"),
        ("2010", "USD", "1090.00", "1460.60"),
        ("7030", "SGD", "-40.60", "-40.60"),
    ]


# req: FR-BNK-08
def test_a_part_recorded_in_full_within_tolerance_clears_the_difference_to_7030():
    # Paid 1,089.50 of 1,090.00 in full: the difference 0.50 is within S$1.00; a gain on a bill.
    result, _ = p1_post(SGD_BILL, settlement("bill_payment", "PAY-1", "2026-03-10", "1010", "SGD", ("BILL-1", "1089.50", True)))
    assert lines_of(result, "PAY-1") == [
        ("1010", "SGD", "-1089.50", "-1089.50"),
        ("2010", "SGD", "1090.00", "1090.00"),
        ("7030", "SGD", "-0.50", "-0.50"),
    ]


# req: FR-BNK-08
def test_a_foreign_bill_in_full_takes_the_greater_of_a_dollar_and_three_percent():
    # USD 1,090.00 carried at 1,460.60; paid USD 1,060.00 in full at 1.34 (1,420.40): the difference
    # 1,460.60 - 1,420.40 = 40.20 is within 3% × 1,460.60 = 43.818 and S$50.00.
    result, _ = p1_post(G4_023, settlement("bill_payment", "PAY-1", "2026-03-20", "1021", "USD",
                                           ("BILL-1", "1060.00", True), rate="1.34"))
    assert lines_of(result, "PAY-1") == [
        ("1021", "USD", "-1060.00", "-1420.40"),
        ("2010", "USD", "1090.00", "1460.60"),
        ("7030", "SGD", "-40.20", "-40.20"),
    ]


# req: FR-BNK-08
def test_differences_beyond_tolerance_and_the_caps_are_refused():
    # 2.00 on an SGD bill is above S$1.00. With a monthly cap of S$50.00, March's first 40.20 posts and a
    # second (80.40 in the month) is refused; April's posts. On a USD 10,000.00 bill at 1.34 (13,400.00),
    # 3% is 402.00, but a difference of 60.00 is above the S$50.00 a document.
    def usd(doc_id, net="1000.00", gst="90.00", gst_base="121.50"):
        return bill(doc_id, "2026-03-05", "USD", bill_line("6010", net, "TX", gst, gst_base=gst_base), rate="1.34")

    settings = {**P1_SETTINGS, "tolerance": {"per_month": "50.00"}}
    result, ledger = p1_post(
        SGD_BILL, usd("BILL-2"), usd("BILL-3"), usd("BILL-4"),
        settlement("bill_payment", "PAY-1", "2026-03-10", "1010", "SGD", ("BILL-1", "1088.00", True)),
        settlement("bill_payment", "PAY-2", "2026-03-20", "1021", "USD", ("BILL-2", "1060.00", True), rate="1.34"),
        settlement("bill_payment", "PAY-3", "2026-03-21", "1021", "USD", ("BILL-3", "1060.00", True), rate="1.34"),
        settlement("bill_payment", "PAY-4", "2026-04-01", "1021", "USD", ("BILL-4", "1060.00", True), rate="1.34"),
        settings=settings,
    )
    assert ledger.reasons == [("PAY-1", "beyond-tolerance"), ("PAY-3", "beyond-tolerance")]
    big = usd("BILL-5", "10000.00", "900.00", "1206.00")  # AP 10,900.00 × 1.34 = 14,606.00
    over = settlement("bill_payment", "PAY-5", "2026-03-20", "1021", "USD", ("BILL-5", "10855.23", True), rate="1.34")
    # 14,606.00 - 10,855.23 × 1.34 (14,546.0082 -> 14,546.01) = 59.99: within 3% (438.18), above S$50.00.
    _refused([big, over], "PAY-5", "beyond-tolerance")


# req: FR-AR-18
def test_an_overpayment_kept_unapplied_is_a_credit_with_the_supplier():
    result, _ = p1_post(SGD_BILL, settlement("bill_payment", "PAY-1", "2026-03-10", "1010", "SGD", ("BILL-1", "100.00"),
                                             unapplied="20.00"))
    assert lines_of(result, "PAY-1") == [("1010", "SGD", "-120.00", "-120.00"), ("2010", "SGD", "120.00", "120.00")]


def _refused(documents, doc_id, reason, **options):
    result, ledger = p1_post(*documents, **options)
    assert doc_id in result["rejected"]
    assert dict(ledger.reasons)[doc_id] == reason


# req: QA-ENG-01
@pytest.mark.parametrize(
    ("document", "reason"),
    [
        (bill("BILL-9", "2026-03-05", "SGD"), "no-lines"),
        (bill("BILL-9", "2026-03-05", "SGD", bill_line("6010", "-10.00", "NR")), "total-not-positive"),
        (bill("BILL-9", "2026-03-05", "SGD", bill_line("6010", "10.00", "NR", quantity="1")), "amount-or-quantity"),
        (bill("BILL-9", "2026-03-05", "SGD", bill_line("6010", None, "NR", quantity="1", unit_price="1.0000001")),
         "bad-quantity"),
        (bill("BILL-9", "2026-03-05", "SGD", bill_line("6010", "10.00", "TX", "0.90", gst_base="0.90")),
         "gst-base-on-base-bill"),
        (bill("BILL-9", "2026-03-05", "SGD", bill_line("6010", "10.00", "TX", "0.90", business_share="101")), "bad-percent"),
        (bill("BILL-9", "2026-03-05", "SGD", bill_line("1500", "10.00", "TX", spread_months=12)), "spread-not-an-expense"),
        (bill("VC-9", "2026-03-05", "SGD", bill_line("6010", "10.00", "TX", spread_months=12), kind="vendor_credit"),
         "spread-on-credit"),
        (bill("BILL-9", "2026-03-05", "SGD", bill_line("6010", "10.00", "NR", spread_from="2026-03-01")),
         "spread-from-without-months"),
        (bill("BILL-9", "2026-03-05", "USD", bill_line("6010", "10.00", "NR")), "missing-rate"),
        (bill("BILL-9", "2026-03-05", "SGD", bill_line("3010", "10.00", "NR")), "share-capital-account"),
    ],
)
def test_a_bill_that_breaks_a_rule_is_rejected(document, reason):
    _refused([document], document["id"], reason)


VC = bill("VC-1", "2026-03-05", "SGD", bill_line("6010", "50.00", "NR"), kind="vendor_credit")


# req: QA-ENG-01
@pytest.mark.parametrize(
    ("payment", "reason"),
    [
        (settlement("bill_payment", "PAY-9", "2026-03-10", "1010", "SGD", ("BILL-1", "1090.01")), "more-than-open"),
        (settlement("bill_payment", "PAY-9", "2026-03-10", "1010", "SGD", ("BILL-7", "1.00")), "not-a-posted-bill"),
        (settlement("bill_payment", "PAY-9", "2026-03-10", "1010", "SGD", ("VC-1", "1.00")), "not-a-posted-bill"),
        (settlement("bill_payment", "PAY-9", "2026-03-10", "1010", "SGD", ("BILL-1", "1.00"), ("BILL-1", "1.00")),
         "document-twice"),
        (settlement("bill_payment", "PAY-9", "2026-03-10", "1010", "SGD"), "nothing-settled"),
        (settlement("bill_payment", "PAY-9", "2026-03-10", "1021", "SGD", ("BILL-1", "1.00")), "money-account-currency"),
        (settlement("bill_payment", "PAY-9", "2026-03-10", "1010", "SGD", ("BILL-1", "1.00"), base_amount="1.00"),
         "base-amount-not-needed"),
        (settlement("bill_payment", "PAY-9", "2026-03-10", "1500", "SGD", ("BILL-1", "1.00")), "not-a-money-account"),
        (settlement("bill_payment", "PAY-9", "2026-03-10", "1010", "SGD", ("BILL-1", "0.00")), "zero-amount"),
    ],
)
def test_a_payment_that_breaks_a_rule_is_rejected(payment, reason):
    _refused([SGD_BILL, VC, payment], "PAY-9", reason)


# req: QA-ENG-01
def test_a_base_account_paying_foreign_bills_needs_what_it_paid_or_a_rate():
    neither = settlement("bill_payment", "PAY-9", "2026-03-10", "1010", "USD", ("BILL-1", "1.00"))
    both = settlement("bill_payment", "PAY-8", "2026-03-10", "1010", "USD", ("BILL-1", "1.00"), rate="1.3",
                      base_amount="1.30")
    _refused([G4_023, neither], "PAY-9", "base-amount-or-rate")
    _refused([G4_023, both], "PAY-8", "base-amount-or-rate")


# req: QA-ENG-01
def test_a_rejected_payment_changes_no_open_amount():
    # PAY-1 is refused (more than open), so PAY-2 still finds all 1,090.00 open.
    result, _ = p1_post(
        SGD_BILL,
        settlement("bill_payment", "PAY-1", "2026-03-10", "1010", "SGD", ("BILL-1", "1000.00"), ("BILL-1", "100.00")),
        settlement("bill_payment", "PAY-2", "2026-03-11", "1010", "SGD", ("BILL-1", "1090.00")),
    )
    assert result["rejected"] == ["PAY-1"]
    assert lines_of(result, "PAY-2") == [("1010", "SGD", "-1090.00", "-1090.00"), ("2010", "SGD", "1090.00", "1090.00")]
