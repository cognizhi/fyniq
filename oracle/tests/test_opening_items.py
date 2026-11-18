"""Opening open items, worked by hand (opening_invoice@1, opening_bill@1; FR-GL-06, Appendix B.2 "Opening open items",
PRD-001 rev 0.257) [accountant to confirm].

An invoice issued, or a bill received, before the books' start and unpaid then posts at the books' start its total on
1100 or 2010 at its rate, against the opening balance clearing account (3900) in the base currency, with no revenue,
cost or GST. Every expected figure is worked out in the comment beside it from the PRD, never by running the oracle or
the engine (QA-ENG-01).
"""

from builders import P1_ACCOUNTS, P1_SETTINGS, bill, invoice, invoice_line, lines_of, p1_post, settlement

SETTINGS = {**P1_SETTINGS, "books_start": "2026-01-01", "accounts": {**P1_SETTINGS["accounts"], "opening_clearing": "3900"}}
ACCOUNTS = [*P1_ACCOUNTS, {"code": "3900", "type": "equity", "group": False, "active": True}]


def post(*documents):
    return p1_post(*documents, settings=SETTINGS, accounts=ACCOUNTS)


def posted_on(result, document):
    return next(e["posting_date"] for e in result["entries"] if e["document"] == document and e["kind"] == "post")


# req: FR-GL-06
def test_an_opening_invoice_posts_its_total_at_the_books_start_and_a_receipt_settles_it():
    # USD 1,000.00 at 9% issued on 20 Dec 2025, registered: GST 90.00, total USD 1,090.00 × 1.35 = S$1,471.50 on 1100,
    # and on 3900 the other way, on 1 Jan 2026. No 4010 and no 2200. Paid on 21 Jan at 1.36: USD 1,090.00 × 1.36 =
    # S$1,482.40 into the wallet, 1100 takes its S$1,471.50 and 7030 the gain of S$10.90.
    inv = invoice("INV-1", "2025-12-20", "USD", invoice_line("4010", "1000.00"), rate="1.35", opening_item=True)
    pay = settlement("customer_receipt", "RC-1", "2026-01-21", "1021", "USD", ("INV-1", "1090.00"), rate="1.36")
    result, ledger = post(inv, pay)
    assert not ledger.rejected, dict(ledger.reasons)
    assert lines_of(result, "INV-1") == [("1100", "USD", "1090.00", "1471.50"), ("3900", "SGD", "-1471.50", "-1471.50")]
    assert posted_on(result, "INV-1") == "2026-01-01"
    assert lines_of(result, "RC-1") == [("1021", "USD", "1090.00", "1482.40"), ("1100", "USD", "-1090.00", "-1471.50"),
                                        ("7030", "SGD", "-10.90", "-10.90")]


# req: FR-GL-06
def test_an_opening_bill_posts_its_total_with_the_gst_its_supplier_stated():
    # S$500.00 with GST 45.00 stated, received on 28 Dec 2025: S$545.00 on 2010 and the other way on 3900, on 1 Jan 2026.
    doc = bill("BILL-1", "2025-12-28", "SGD", {"account": "6010", "gst_code": "TX", "amount": "500.00", "gst": "45.00"}, opening_item=True)
    result, ledger = post(doc)
    assert not ledger.rejected, dict(ledger.reasons)
    assert lines_of(result, "BILL-1") == [("2010", "SGD", "-545.00", "-545.00"), ("3900", "SGD", "545.00", "545.00")]
    assert posted_on(result, "BILL-1") == "2026-01-01"


# req: FR-GL-06
def test_an_opening_item_comes_before_the_books_start_and_spreads_nothing():
    on_start = invoice("INV-1", "2026-01-01", "SGD", invoice_line("4010", "100.00", "ZR"), opening_item=True)
    spread = invoice("INV-2", "2025-12-01", "SGD", invoice_line("4010", "1200.00", "ZR", spread_months=12), opening_item=True)
    withheld = bill("BILL-1", "2025-12-01", "SGD", {"account": "6010", "gst_code": "OP", "amount": "100.00"}, opening_item=True,
                    withholding={"percent": "15"})
    late_credit = invoice("CN-2", "2026-01-05", "SGD", invoice_line("4010", "10.00", "ZR"), kind="credit_note", opening_item=True)
    _, ledger = post(on_start, spread, withheld, late_credit)
    assert set(ledger.rejected) == {"INV-1", "INV-2", "BILL-1", "CN-2"}
    _, ledger = p1_post(invoice("INV-3", "2025-12-01", "SGD", invoice_line("4010", "100.00", "ZR"), opening_item=True),
                        settings=P1_SETTINGS)
    assert set(ledger.rejected) == {"INV-3"}  # no books start to post at


# req: FR-GL-06
def test_a_credit_open_at_the_books_start_posts_the_other_way():
    # rev 0.261: a zero-rated credit note of S$1,090.00 open on 1 Jan 2026 credits 1100 and debits 3900 at the books'
    # start; a supplier's credit of S$218.00, GST 18.00 stated, debits 2010 and credits 3900.
    cn = invoice("CN-1", "2025-12-20", "SGD", invoice_line("4010", "1090.00", "ZR"), kind="credit_note", opening_item=True)
    vc = bill("VC-1", "2025-12-22", "SGD", {"account": "6010", "gst_code": "TX", "amount": "200.00", "gst": "18.00"}, kind="vendor_credit",
              opening_item=True)
    result, ledger = post(cn, vc)
    assert not ledger.rejected, dict(ledger.reasons)
    assert lines_of(result, "CN-1") == [("1100", "SGD", "-1090.00", "-1090.00"), ("3900", "SGD", "1090.00", "1090.00")]
    assert lines_of(result, "VC-1") == [("2010", "SGD", "218.00", "218.00"), ("3900", "SGD", "-218.00", "-218.00")]
    assert posted_on(result, "CN-1") == posted_on(result, "VC-1") == "2026-01-01"
