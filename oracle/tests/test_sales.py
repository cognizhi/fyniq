"""Invoices, credit notes, receipts, deposits, deposit refunds, write-offs and invoice spreads, worked
by hand (invoice@1, credit_note@1, customer_receipt@1, customer_deposit@1, deposit_refund@1,
write_off@1).

Every expected figure is worked out in the comment beside it from PRD-001 §10.2, Appendix B.2, ENG-42,
FR-AR-03/04/09/17/18, FR-BNK-08, G4-022 and G4-038, never by running the oracle or the engine
(QA-ENG-01).
"""

import pytest
from builders import P1_SETTINGS, deposit, invoice, invoice_line, lines_of, p1_post, settlement

THREE = [invoice_line("4010", "10.05") for _ in range(3)]


# req: RND-GST-EXCL
def test_an_invoices_gst_per_subtotal_by_default():
    # 30.15 × 9% = 2.7135 -> 2.71; the customer owes 32.86.
    result, _ = p1_post(invoice("INV-1", "2026-03-05", "SGD", *THREE))
    assert lines_of(result, "INV-1") == [
        ("1100", "SGD", "32.86", "32.86"),
        ("2200", "SGD", "-2.71", "-2.71"),
        ("4010", "SGD", "-30.15", "-30.15"),
    ]


# req: RND-GST-EXCL
def test_an_invoices_gst_per_line_when_the_organisation_says_so():
    # 0.9045 -> 0.90 a line, 2.70; the customer owes 32.85.
    settings = {**P1_SETTINGS, "gst": {**P1_SETTINGS["gst"], "per_line": True}}
    result, _ = p1_post(invoice("INV-1", "2026-03-05", "SGD", *THREE), settings=settings)
    assert lines_of(result, "INV-1") == [
        ("1100", "SGD", "32.85", "32.85"),
        ("2200", "SGD", "-2.70", "-2.70"),
        ("4010", "SGD", "-30.15", "-30.15"),
    ]


# req: ENG-42
def test_a_usd_invoice_reports_its_gst_from_the_sgd_nets_and_puts_the_cents_on_7030():
    # USD 10.05 × 1.5 = 15.075 -> 15.08; GST on the SGD net: 15.08 × 9% = 1.3572 -> 1.36. The customer's
    # total in USD: 10.05 + 0.9045 -> 0.90 = 10.95, × 1.5 = 16.425 -> 16.43. 7030: -(16.43 - 15.08 - 1.36)
    # = 0.01.
    result, _ = p1_post(invoice("INV-1", "2026-03-05", "USD", invoice_line("4010", "10.05"), rate="1.5"))
    assert lines_of(result, "INV-1") == [
        ("1100", "USD", "10.95", "16.43"),
        ("2200", "SGD", "-1.36", "-1.36"),
        ("4010", "USD", "-10.05", "-15.08"),
        ("7030", "SGD", "0.01", "0.01"),
    ]


# req: RND-LINE
def test_an_invoice_line_with_a_discount_rounds_once():
    # 3 × 10.00 × (100 - 12.5) ÷ 100 = 26.25; zero-rated.
    result, _ = p1_post(invoice("INV-1", "2026-03-05", "SGD",
                                invoice_line("4010", None, "ZR", quantity="3", unit_price="10.00", discount="12.5")))
    assert lines_of(result, "INV-1") == [("1100", "SGD", "26.25", "26.25"), ("4010", "SGD", "-26.25", "-26.25")]


# req: FR-GL-03
def test_g4_022_an_annual_subscription_spreads_its_revenue_and_keeps_its_gst():
    # 1,200.00 + 108.00 GST invoiced 15 January: all the GST in January; 2400 takes 1,200.00 and releases
    # 100.00 a month from January (dated 15 Jan, then 1 Feb and 1 Mar up to as_of).
    doc = invoice("INV-1", "2026-01-15", "SGD", invoice_line("4010", "1200.00", spread_months=12))
    result, _ = p1_post(doc, as_of="2026-03-31")
    assert lines_of(result, "INV-1") == [
        ("1100", "SGD", "1308.00", "1308.00"),
        ("2200", "SGD", "-108.00", "-108.00"),
        ("2400", "SGD", "-1200.00", "-1200.00"),
    ]
    assert [(e["kind"], e["posting_date"]) for e in result["entries"]] == [
        ("post", "2026-01-15"), ("release", "2026-01-15"), ("release", "2026-02-01"), ("release", "2026-03-01")]
    assert lines_of(result, "INV-1", "release") == [("2400", "SGD", "100.00", "100.00"), ("4010", "SGD", "-100.00", "-100.00")]


# req: QA-ENG-01
def test_a_credit_note_posts_an_invoices_legs_negated():
    # 50.00 + 4.50 GST, credited.
    result, _ = p1_post(invoice("CN-1", "2026-03-05", "SGD", invoice_line("4010", "50.00"), kind="credit_note"))
    assert lines_of(result, "CN-1") == [
        ("1100", "SGD", "-54.50", "-54.50"),
        ("2200", "SGD", "4.50", "4.50"),
        ("4010", "SGD", "50.00", "50.00"),
    ]


# req: FR-AR-04
def test_a_linked_credit_note_adjusts_gst_at_its_invoices_rate():
    # The invoice of 20 Dec 2023 charged 8%; its credit note of 10 Jan 2024 credits 8.00, not 9.00.
    settings = {**P1_SETTINGS, "gst": {**P1_SETTINGS["gst"], "registered_from": "2023-01-01"}}
    result, ledger = p1_post(
        invoice("INV-1", "2023-12-20", "SGD", invoice_line("4010", "100.00")),
        invoice("CN-1", "2024-01-10", "SGD", invoice_line("4010", "100.00"), kind="credit_note", invoice="INV-1"),
        invoice("CN-2", "2024-01-11", "SGD", invoice_line("4010", "100.01"), kind="credit_note", invoice="INV-1"),
        settings=settings,
    )
    assert lines_of(result, "CN-1") == [
        ("1100", "SGD", "-108.00", "-108.00"),
        ("2200", "SGD", "8.00", "8.00"),
        ("4010", "SGD", "100.00", "100.00"),
    ]
    assert ledger.reasons == [("CN-2", "credit-above-invoice")]  # 108.01 above the invoice's 108.00


G4_038 = invoice("INV-1", "2026-03-01", "USD", invoice_line("4010", "10000.00", "ZR"), rate="1.35")


# req: ENG-41
def test_g4_038_a_usd_receipt_posts_realised_fx_and_a_large_residual_stays_open():
    # USD 10,000.00 at 1.35 = 13,500.00. USD 9,710.00 received at 1.32 = 12,817.20; 1100 takes 9,710.00
    # at 1.35 = 13,108.50; 7030 the realised loss 291.30. The residual USD 290.00 is S$391.50 at the
    # document rate, above S$50.00, so recording it in full is refused.
    result, ledger = p1_post(
        G4_038,
        settlement("customer_receipt", "RC-1", "2026-03-20", "1021", "USD", ("INV-1", "9710.00"), rate="1.32"),
        settlement("customer_receipt", "RC-2", "2026-03-21", "1021", "USD", ("INV-1", "1.00", True), rate="1.32"),
    )
    assert lines_of(result, "RC-1") == [
        ("1021", "USD", "9710.00", "12817.20"),
        ("1100", "USD", "-9710.00", "-13108.50"),
        ("7030", "SGD", "291.30", "291.30"),
    ]
    assert ledger.reasons == [("RC-2", "beyond-tolerance")]


# req: FR-AR-17
def test_g4_038_the_residual_written_off_clears_its_carrying_amount():
    # After RC-1, USD 290.00 is open, carried at 13,500.00 - 13,108.50 = 391.50; written off in full to
    # 7030 (a cross-currency difference).
    result, _ = p1_post(
        G4_038,
        settlement("customer_receipt", "RC-1", "2026-03-20", "1021", "USD", ("INV-1", "9710.00"), rate="1.32"),
        {"type": "write_off", "id": "WO-1", "version": 1, "date": "2026-04-30", "invoice": "INV-1", "amount": "290.00",
         "account": "7030"},
    )
    assert lines_of(result, "WO-1") == [("1100", "USD", "-290.00", "-391.50"), ("7030", "SGD", "391.50", "391.50")]


# req: FR-AR-17
def test_a_partial_write_off_at_the_invoices_rate():
    # USD 100.00 of the USD 10,000.00 at 1.35 = 135.00 to bad debts.
    result, _ = p1_post(G4_038, {"type": "write_off", "id": "WO-1", "version": 1, "date": "2026-04-30",
                                 "invoice": "INV-1", "amount": "100.00", "account": "6900"})
    assert lines_of(result, "WO-1") == [("1100", "USD", "-100.00", "-135.00"), ("6900", "SGD", "135.00", "135.00")]


# req: FR-AR-09
def test_a_part_payment_deposit_bears_gst_and_a_security_deposit_does_not():
    # 1,090.00 on SR: GST 90.00, 2410 1,000.00. A security deposit of 500.00: no GST.
    result, _ = p1_post(
        deposit("DP-1", "2026-03-01", "1010", "SGD", "1090.00"),
        deposit("DP-2", "2026-03-01", "1010", "SGD", "500.00", kind="security", gst_code=None),
    )
    assert lines_of(result, "DP-1") == [
        ("1010", "SGD", "1090.00", "1090.00"),
        ("2200", "SGD", "-90.00", "-90.00"),
        ("2410", "SGD", "-1000.00", "-1000.00"),
    ]
    assert lines_of(result, "DP-2") == [("1010", "SGD", "500.00", "500.00"), ("2410", "SGD", "-500.00", "-500.00")]


# req: FR-AR-09
def test_an_invoice_deducting_a_whole_deposit_charges_gst_once():
    # Invoice 2,000.00 + 180.00 GST = 2,180.00, deducting all of DP-1 (1,090.00, of which GST 90.00):
    # 2410 takes 1,090.00 - 90.00 = 1,000.00; 2200 180.00 - 90.00 = 90.00; 1100 2,180.00 - 1,090.00 = 1,090.00.
    result, _ = p1_post(
        deposit("DP-1", "2026-03-01", "1010", "SGD", "1090.00"),
        invoice("INV-1", "2026-03-31", "SGD", invoice_line("4010", "2000.00"),
                deposits=[{"deposit": "DP-1", "amount": "1090.00"}]),
    )
    assert lines_of(result, "INV-1") == [
        ("1100", "SGD", "1090.00", "1090.00"),
        ("2200", "SGD", "-90.00", "-90.00"),
        ("2410", "SGD", "1000.00", "1000.00"),
        ("4010", "SGD", "-2000.00", "-2000.00"),
    ]


# req: RND-DEPOSIT
def test_uses_of_a_deposit_take_their_share_and_the_last_takes_what_is_left():
    # 100.00 of 1,090.00: its base 100.00 and GST 90 × 100 ÷ 1,090 = 8.2568... -> 8.26 (2410 91.74).
    # The rest, 990.00, takes all that is left: base 990.00 and GST 81.74 (2410 908.26).
    result, _ = p1_post(
        deposit("DP-1", "2026-03-01", "1010", "SGD", "1090.00"),
        invoice("INV-1", "2026-03-31", "SGD", invoice_line("4010", "100.00", "ZR"),
                deposits=[{"deposit": "DP-1", "amount": "100.00"}]),
        invoice("INV-2", "2026-04-30", "SGD", invoice_line("4010", "1000.00"),
                deposits=[{"deposit": "DP-1", "amount": "990.00"}]),
    )
    # INV-1, zero-rated: no GST charged, 8.26 borne by the deposit comes back off 2200.
    assert lines_of(result, "INV-1") == [
        ("2200", "SGD", "8.26", "8.26"),
        ("2410", "SGD", "91.74", "91.74"),
        ("4010", "SGD", "-100.00", "-100.00"),
    ]
    # INV-2: 1,000.00 + 90.00 = 1,090.00; output 90.00 - 81.74 = 8.26; 1100 1,090.00 - 990.00 = 100.00.
    assert lines_of(result, "INV-2") == [
        ("1100", "SGD", "100.00", "100.00"),
        ("2200", "SGD", "-8.26", "-8.26"),
        ("2410", "SGD", "908.26", "908.26"),
        ("4010", "SGD", "-1000.00", "-1000.00"),
    ]
    assert result["trial_balance"]["2026-03"]["2410"] == "-908.26"


# req: FR-AR-09
def test_a_usd_deposit_used_at_another_rate_puts_the_difference_on_7030():
    # USD 109.00 at 1.30 = 141.70, GST 141.70 × 9 ÷ 109 = 11.70, 2410 130.00. The invoice: USD 100.00 at
    # 1.35 = 135.00, GST 12.15 (USD 9.00), total USD 109.00, all taken by the deposit: 2410 130.00, output
    # 12.15 - 11.70 = 0.45, no 1100 line; 7030 balances: -(-135.00 - 0.45 + 130.00) = 5.45.
    result, _ = p1_post(
        deposit("DP-1", "2026-03-01", "1021", "USD", "109.00", rate="1.30"),
        invoice("INV-1", "2026-03-31", "USD", invoice_line("4010", "100.00"), rate="1.35",
                deposits=[{"deposit": "DP-1", "amount": "109.00"}]),
    )
    assert lines_of(result, "DP-1") == [
        ("1021", "USD", "109.00", "141.70"),
        ("2200", "SGD", "-11.70", "-11.70"),
        ("2410", "SGD", "-130.00", "-130.00"),
    ]
    assert lines_of(result, "INV-1") == [
        ("2200", "SGD", "-0.45", "-0.45"),
        ("2410", "SGD", "130.00", "130.00"),
        ("4010", "USD", "-100.00", "-135.00"),
        ("7030", "SGD", "5.45", "5.45"),
    ]


# req: FR-AR-09
def test_a_deposit_refund_returns_its_gst_part():
    # Half of DP-1 refunded: base 545.00, GST 45.00: 2410 500.00, 2200 45.00, 1010 -545.00. A USD deposit
    # refunded at 1.35: 2410 130.00, 2200 11.70, 1021 -147.15, 7030 5.45.
    result, _ = p1_post(
        deposit("DP-1", "2026-03-01", "1010", "SGD", "1090.00"),
        deposit("DP-2", "2026-03-01", "1021", "USD", "109.00", rate="1.30"),
        {"type": "deposit_refund", "id": "RF-1", "version": 1, "date": "2026-04-01", "deposit": "DP-1",
         "account": "1010", "amount": "545.00"},
        {"type": "deposit_refund", "id": "RF-2", "version": 1, "date": "2026-04-01", "deposit": "DP-2",
         "account": "1021", "amount": "109.00", "fx_rate": "1.35", "fx_rate_source": "document"},
    )
    assert lines_of(result, "RF-1") == [
        ("1010", "SGD", "-545.00", "-545.00"),
        ("2200", "SGD", "45.00", "45.00"),
        ("2410", "SGD", "500.00", "500.00"),
    ]
    assert lines_of(result, "RF-2") == [
        ("1021", "USD", "-109.00", "-147.15"),
        ("2200", "SGD", "11.70", "11.70"),
        ("2410", "SGD", "130.00", "130.00"),
        ("7030", "SGD", "5.45", "5.45"),
    ]


def _refused(documents, doc_id, reason):
    result, ledger = p1_post(*documents)
    assert doc_id in result["rejected"]
    assert dict(ledger.reasons)[doc_id] == reason


DP = deposit("DP-1", "2026-03-01", "1010", "SGD", "1090.00")
INV = invoice("INV-1", "2026-03-02", "SGD", invoice_line("4010", "100.00"))


# req: QA-ENG-01
@pytest.mark.parametrize(
    ("document", "reason"),
    [
        (invoice("INV-9", "2026-03-05", "SGD"), "no-lines"),
        (invoice("INV-9", "2026-03-05", "SGD", invoice_line("6010", "100.00", spread_months=12)), "spread-not-revenue"),
        (invoice("CN-9", "2026-03-05", "SGD", invoice_line("4010", "100.00", spread_months=12), kind="credit_note"),
         "spread-on-credit"),
        (invoice("INV-9", "2026-03-05", "SGD", invoice_line("4010", None, quantity="1", unit_price="1", discount="100.01")),
         "bad-percent"),
        (invoice("INV-9", "2026-03-05", "USD", invoice_line("4010", "100.00"), rate="1.3",
                 deposits=[{"deposit": "DP-1", "amount": "1.00"}]), "deposit-currency"),
        (invoice("INV-9", "2026-03-05", "SGD", invoice_line("4010", "2000.00"),
                 deposits=[{"deposit": "DP-1", "amount": "1090.01"}]), "more-than-left-of-deposit"),
        (invoice("INV-9", "2026-03-05", "SGD", invoice_line("4010", "100.00"),
                 deposits=[{"deposit": "DP-1", "amount": "109.01"}]), "deposits-above-total"),
        (invoice("INV-9", "2026-03-05", "SGD", invoice_line("4010", "100.00"),
                 deposits=[{"deposit": "INV-1", "amount": "1.00"}]), "not-a-posted-deposit"),
        (invoice("CN-9", "2026-03-05", "SGD", invoice_line("4010", "100.00"), kind="credit_note",
                 deposits=[{"deposit": "DP-1", "amount": "1.00"}]), "deposit-on-credit"),
        (invoice("CN-9", "2026-03-01", "SGD", invoice_line("4010", "1.00"), kind="credit_note", invoice="INV-1"),
         "credit-before-invoice"),
        (deposit("DP-9", "2026-03-05", "1010", "SGD", "10.00", kind="security"), "gst-code-on-security-deposit"),
        (deposit("DP-9", "2026-03-05", "1010", "SGD", "10.00", gst_code=None), "no-gst-code"),
        (deposit("DP-9", "2026-03-05", "1021", "SGD", "10.00"), "money-account-currency"),
        ({"type": "deposit_refund", "id": "RF-9", "version": 1, "date": "2026-03-05", "deposit": "DP-1",
          "account": "1010", "amount": "1090.01"}, "more-than-left-of-deposit"),
        ({"type": "write_off", "id": "WO-9", "version": 1, "date": "2026-03-05", "invoice": "INV-1", "amount": "109.01",
          "account": "6900"}, "more-than-open"),
        ({"type": "write_off", "id": "WO-9", "version": 1, "date": "2026-03-05", "invoice": "DP-1", "amount": "1.00",
          "account": "6900"}, "not-a-posted-invoice"),
        (settlement("customer_receipt", "RC-9", "2026-03-05", "1010", "SGD", ("DP-1", "1.00")), "not-a-posted-invoice"),
        (settlement("customer_receipt", "RC-9", "2026-03-05", "1010", "SGD", ("INV-1", "109.01")), "more-than-open"),
    ],
)
def test_a_sales_document_that_breaks_a_rule_is_rejected(document, reason):
    _refused([DP, INV, document], document["id"], reason)


# req: FR-AR-18
def test_an_overpayment_stays_a_customer_credit_on_1100():
    # 109.00 open; 120.00 received: 109.00 settles INV-1 and 11.00 stays unapplied on 1100.
    result, _ = p1_post(INV, settlement("customer_receipt", "RC-1", "2026-03-05", "1010", "SGD", ("INV-1", "109.00"),
                                        unapplied="11.00"))
    assert lines_of(result, "RC-1") == [("1010", "SGD", "120.00", "120.00"), ("1100", "SGD", "-120.00", "-120.00")]


# req: FR-BNK-08
def test_a_receipt_in_full_within_tolerance_is_a_loss_on_7030():
    # 108.50 received of 109.00, in full: 0.50 to 7030 as a loss.
    result, _ = p1_post(INV, settlement("customer_receipt", "RC-1", "2026-03-05", "1010", "SGD",
                                        ("INV-1", "108.50", True)))
    assert lines_of(result, "RC-1") == [
        ("1010", "SGD", "108.50", "108.50"),
        ("1100", "SGD", "-109.00", "-109.00"),
        ("7030", "SGD", "0.50", "0.50"),
    ]


# req: INV-05
def test_a_deposit_whose_base_rounds_to_nothing_is_refused():
    # USD 0.01 at 0.0001 is 0.000001 -> 0.00: only the money line would be left, so it cannot post
    # (test/differential/README.md, "P1 readings").
    result, ledger = p1_post(deposit("DP-1", "2026-03-01", "1021", "USD", "0.01", rate="0.0001"))
    assert result["rejected"] == ["DP-1"] and ledger.reasons == [("DP-1", "entry-rounds-to-one-line")]
