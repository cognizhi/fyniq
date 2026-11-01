"""The pending golden scenarios (test/differential/pending/), figures worked out by hand.

They wait for cmd/scenario-run to read the P1 shapes (pending/README.md); the oracle must match the
hand-worked figures now. Every figure comes from the comment beside it (QA-ENG-01).
"""

from pathlib import Path

from oracle.ledger import Ledger
from oracle.scenario import load_scenario

PENDING = Path(__file__).resolve().parents[2] / "test" / "differential" / "pending"


def _run(name: str) -> dict:
    return Ledger(load_scenario(PENDING / f"{name}.json")).result()


def _lines(result: dict, document: str, kind: str = "post", posting_date: str | None = None) -> list[tuple]:
    for e in result["entries"]:
        if e["document"] == document and e["kind"] == kind and posting_date in (None, e["posting_date"]):
            return [(ln["account"], ln["currency"], ln["amount"], ln["base_amount"]) for ln in e["lines"]]
    raise AssertionError(f"no {kind} entry for {document}")


# req: QA-ENG-02
def test_every_pending_file_is_listed_in_its_readme():
    listed = (PENDING / "README.md").read_text(encoding="utf-8")
    for path in PENDING.glob("*.json"):
        assert f"`{path.name}`" in listed, path.name


# req: QA-ENG-02
def test_pending_bank():
    r = _run("bank")
    assert r["rejected"] == ["BT-0005", "BT-0006"]  # a part on a money account; a spread on an asset
    assert [(e["document"], e["kind"], e["posting_date"]) for e in r["entries"]] == [
        ("BT-0001", "post", "2026-03-05"),
        ("BT-0002", "post", "2026-03-10"),
        ("BT-0003", "post", "2026-03-12"),
        ("BT-0004", "post", "2026-03-20"),
        ("TR-0001", "post", "2026-04-02"),
        ("TR-0002", "post", "2026-04-03"),
        ("BT-0007", "post", "2026-04-07"),
        ("BT-0002", "release", "2026-03-10"),  # the posting month: the line's own date
        ("BT-0002", "release", "2026-04-01"),  # May's release (1 May) is after as_of
    ]
    # -109.00 on TX: 109 × 9 ÷ 109 = 9.00 of GST; 100.00 of expense.
    assert _lines(r, "BT-0001") == [("1010", "SGD", "-109.00", "-109.00"), ("2200", "SGD", "9.00", "9.00"),
                                    ("6010", "SGD", "100.00", "100.00")]
    # +327.00 on SR: GST 27.00; 300.00 deferred to 2400, released 100.00 a month (ALLOC-EVEN).
    assert _lines(r, "BT-0002") == [("1010", "SGD", "327.00", "327.00"), ("2200", "SGD", "-27.00", "-27.00"),
                                    ("2400", "SGD", "-300.00", "-300.00")]
    assert _lines(r, "BT-0002", "release", "2026-04-01") == [("2400", "SGD", "100.00", "100.00"),
                                                              ("4010", "SGD", "-100.00", "-100.00")]
    # USD -100.01 × 1.3335 = -133.36; parts -66.68 and -66.69 (-133.37); the cent to 7030.
    assert _lines(r, "BT-0003") == [("1021", "USD", "-100.01", "-133.36"), ("6010", "SGD", "66.68", "66.68"),
                                    ("6110", "SGD", "66.69", "66.69"), ("7030", "SGD", "-0.01", "-0.01")]
    # USD -109.00 × 1.35 = -147.15; GST 147.15 × 9 ÷ 109 = 12.15; expense 135.00.
    assert _lines(r, "BT-0004") == [("1021", "USD", "-109.00", "-147.15"), ("2200", "SGD", "12.15", "12.15"),
                                    ("6010", "SGD", "135.00", "135.00")]
    # USD 738.00 × 1.3485 = 995.193 -> 995.19; fee 4.50; 7030 -(995.19 - 1000.00 + 4.50) = 0.31.
    assert _lines(r, "TR-0002") == [("1010", "SGD", "-1000.00", "-1000.00"), ("1021", "USD", "738.00", "995.19"),
                                    ("6460", "SGD", "4.50", "4.50"), ("7030", "SGD", "0.31", "0.31")]
    assert r["trial_balance"] == {
        "2026-03": {
            "1010": "218.00",  # -109.00 + 327.00
            "1021": "-280.51",  # -133.36 - 147.15
            "2200": "-5.85",  # 9.00 - 27.00 + 12.15
            "2400": "-200.00",  # -300.00 + March's 100.00
            "4010": "-100.00",
            "6010": "301.68",  # 100.00 + 66.68 + 135.00
            "6110": "66.69",
            "7030": "-0.01",
        },
        "2026-04": {
            "1010": "-1550.00",  # -500.00 - 1,000.00 - 50.00
            "1011": "500.00",
            "1021": "995.19",
            "2400": "100.00",
            "2999": "50.00",
            "4010": "-100.00",
            "6460": "4.50",
            "7030": "0.31",
        },
    }


# req: QA-ENG-02
def test_pending_purchases():
    r = _run("purchases")
    assert r["rejected"] == ["BILL-0005", "PAY-0004"]  # a spread on an asset; 900.00 of 808.00 open
    assert [(e["document"], e["kind"], e["posting_date"]) for e in r["entries"]] == [
        ("BILL-0001", "post", "2026-03-05"),
        ("BILL-0002", "post", "2026-03-06"),
        ("BILL-0003", "post", "2026-03-15"),
        ("BILL-0004", "post", "2026-03-16"),
        ("VC-0001", "post", "2026-03-17"),
        ("PAY-0001", "post", "2026-03-20"),
        ("PAY-0002", "post", "2026-03-25"),
        ("PAY-0003", "post", "2026-04-02"),
        ("BILL-0003", "release", "2026-03-15"),  # January to March, on the bill's date
        ("BILL-0003", "release", "2026-04-01"),
    ]
    # G4-023: Dr 6010 1,340.00, Dr 2200 121.50 as stated, Cr 2010 1,460.60, Cr 7030 0.90.
    assert _lines(r, "BILL-0001") == [("2010", "USD", "-1090.00", "-1460.60"), ("2200", "SGD", "121.50", "121.50"),
                                      ("6010", "USD", "1000.00", "1340.00"), ("7030", "SGD", "-0.90", "-0.90")]
    # ⌊1,234.56⌋ × 15% = 185.10 withheld; 2010 the rest, 1,049.46.
    assert _lines(r, "BILL-0004") == [("2010", "SGD", "-1049.46", "-1049.46"), ("2330", "SGD", "-185.10", "-185.10"),
                                      ("6010", "SGD", "1234.56", "1234.56")]
    # Rev 0.77: Dr 2010 1,460.60, Cr 1021 1,090.00 × 1.30 = 1,417.00, Cr 7030 43.60.
    assert _lines(r, "PAY-0001") == [("1021", "USD", "-1090.00", "-1417.00"), ("2010", "USD", "1090.00", "1460.60"),
                                     ("7030", "SGD", "-43.60", "-43.60")]
    # 1,089.50 in full: 1,090.00 settled, the 0.50 within S$1.00 a gain on 7030.
    assert _lines(r, "PAY-0002") == [("1010", "SGD", "-1089.50", "-1089.50"), ("2010", "SGD", "1090.00", "1090.00"),
                                     ("7030", "SGD", "-0.50", "-0.50")]
    assert _lines(r, "BILL-0003", "release", "2026-03-15") == [("1210", "SGD", "-300.00", "-300.00"),
                                                                 ("6010", "SGD", "300.00", "300.00")]
    assert r["trial_balance"] == {
        "2026-03": {
            "1010": "-1089.50",
            "1021": "-1417.00",
            "1210": "900.00",  # 1,200.00 less three months of 100.00
            "2010": "-2302.96",  # BILL-0003 1,308.00 + BILL-0004 1,049.46 - VC-0001 54.50, still open
            "2200": "315.00",  # 121.50 + 90.00 + 108.00 - 4.50
            "2330": "-185.10",
            "6010": "3824.56",  # 1,340.00 + 1,000.00 + 300.00 + 1,234.56 - 50.00
            "7030": "-45.00",  # -0.90 - 43.60 - 0.50
        },
        "2026-04": {"1010": "-520.00", "1210": "-100.00", "2010": "520.00", "6010": "100.00"},
    }


# req: QA-ENG-02
def test_pending_sales():
    r = _run("sales")
    # CN-0003's 0.01 takes INV-0001's credit notes to 32.87 of its 32.86 (rev 0.155); USD 11.00 of 10.95
    # open; a spread on an expense.
    assert r["rejected"] == ["CN-0003", "RC-0003", "INV-0006"]
    assert [(e["document"], e["kind"], e["posting_date"]) for e in r["entries"]] == [
        ("DP-0001", "post", "2026-03-01"),
        ("INV-0005", "post", "2026-03-01"),
        ("INV-0001", "post", "2026-03-02"),
        ("INV-0002", "post", "2026-03-03"),
        ("INV-0003", "post", "2026-03-15"),
        ("RC-0001", "post", "2026-03-20"),
        ("INV-0004", "post", "2026-03-31"),
        ("CN-0001", "post", "2026-04-01"),
        ("RC-0002", "post", "2026-04-02"),
        ("CN-0002", "post", "2026-04-03"),
        ("RF-0001", "post", "2026-04-10"),
        ("WO-0001", "post", "2026-04-30"),
        ("INV-0003", "release", "2026-03-15"),
        ("INV-0003", "release", "2026-04-01"),
    ]
    # USD 10.05 at 1.5: 15.08 net, 1.36 GST on it, USD 10.95 owed (16.43), 7030 0.01.
    assert _lines(r, "INV-0002") == [("1100", "USD", "10.95", "16.43"), ("2200", "SGD", "-1.36", "-1.36"),
                                     ("4010", "USD", "-10.05", "-15.08"), ("7030", "SGD", "0.01", "0.01")]
    # G4-038: USD 9,710.00 at 1.32 = 12,817.20 against 13,108.50 at 1.35: 291.30 realised loss.
    assert _lines(r, "RC-0001") == [("1021", "USD", "9710.00", "12817.20"), ("1100", "USD", "-9710.00", "-13108.50"),
                                    ("7030", "SGD", "291.30", "291.30")]
    # 100.00 of the 1,090.00 deposit: GST 90 × 100 ÷ 1,090 = 8.26 back off 2200; 2410 91.74; nothing left owed.
    assert _lines(r, "INV-0004") == [("2200", "SGD", "8.26", "8.26"), ("2410", "SGD", "91.74", "91.74"),
                                     ("4020", "SGD", "-100.00", "-100.00")]
    # 545.00 of the 990.00 left: its base 545.00 and GST 81.74 × 545 ÷ 990 = 44.998... -> 45.00.
    assert _lines(r, "RF-0001") == [("1010", "SGD", "-545.00", "-545.00"), ("2200", "SGD", "45.00", "45.00"),
                                    ("2410", "SGD", "500.00", "500.00")]
    # INV-0001's 32.86 (30.15 + 2.71 GST per subtotal) less CN-0001's 10.95 leaves 21.91 to credit:
    # 20.10 + 1.81 (1.809) uses all of it.
    assert _lines(r, "CN-0002") == [("1100", "SGD", "-21.91", "-21.91"), ("2200", "SGD", "1.81", "1.81"),
                                    ("4010", "SGD", "20.10", "20.10")]
    # The USD 290.00 left, carried at 13,500.00 - 13,108.50 = 391.50, written off to 7030.
    assert _lines(r, "WO-0001") == [("1100", "USD", "-290.00", "-391.50"), ("7030", "SGD", "391.50", "391.50")]
    assert r["trial_balance"] == {
        "2026-03": {
            "1010": "1090.00",
            "1021": "12817.20",
            "1100": "1748.79",  # 32.86 + 16.43 + 1,308.00 + 13,500.00 - 13,108.50
            "2200": "-193.81",  # -90.00 - 2.71 - 1.36 - 108.00 + 8.26
            "2400": "-1100.00",  # -1,200.00 + March's 100.00
            "2410": "-908.26",  # -1,000.00 + 91.74
            "4010": "-13645.23",  # -30.15 - 15.08 - 13,500.00 - March's 100.00
            "4020": "-100.00",
            "7030": "291.31",  # 0.01 + 291.30
        },
        "2026-04": {
            "1010": "-512.64",  # 32.36 - 545.00
            "1100": "-457.22",  # -10.95 - 32.86 - 21.91 - 391.50
            "2200": "47.71",  # 0.90 + 1.81 + 45.00
            "2400": "100.00",
            "2410": "500.00",
            "4010": "-69.85",  # 10.05 + 20.10 - April's 100.00
            "7030": "392.00",  # 0.50 + 391.50
        },
    }


# req: QA-ENG-02
def test_pending_claims():
    r = _run("claims")
    assert r["rejected"] == ["RE-0003", "EC-0005"]  # EC-0004 is paid; a trip in USD
    assert [e["document"] for e in r["entries"]] == ["ADV-0001", "EC-0001", "EC-0002", "EC-0003", "EC-0004", "RE-0001",
                                                     "RE-0002", "ADV-0002", "ADV-0002R"]
    # 100.00 + 9.00 GST and 12.35 km -> 12.4 × 0.60 = 7.44: 116.44 taken from the 500.00 advance.
    assert _lines(r, "EC-0001") == [("1250", "SGD", "-116.44", "-116.44"), ("2200", "SGD", "9.00", "9.00"),
                                    ("6010", "SGD", "100.00", "100.00"), ("6110", "SGD", "7.44", "7.44")]
    # 450.00: the 383.56 left on the advance, 66.44 owed on 2150.
    assert _lines(r, "EC-0002") == [("1250", "SGD", "-383.56", "-383.56"), ("2150", "SGD", "-66.44", "-66.44"),
                                    ("6010", "SGD", "450.00", "450.00")]
    # 7.25 km -> 7.3 × 0.25 = 1.825 -> 1.83, and 100 km × 0.60 = 60.00.
    assert _lines(r, "EC-0004") == [("2150", "SGD", "-61.83", "-61.83"), ("6110", "SGD", "61.83", "61.83")]
    # USD 50.00 carried at 67.50, reimbursed at 1.33 = 66.50: 1.00 to 7030.
    assert _lines(r, "RE-0001") == [("1010", "SGD", "-66.50", "-66.50"), ("2150", "USD", "50.00", "67.50"),
                                    ("7030", "SGD", "-1.00", "-1.00")]
    assert r["trial_balance"] == {
        "2026-03": {
            "1010": "-694.77",  # -500.00 - 66.50 - 128.27; 1250 and 2150 are back at nothing
            "2200": "9.00",
            "6010": "617.50",  # 100.00 + 450.00 + 67.50
            "6110": "69.27",  # 7.44 + 61.83
            "7030": "-1.00",
        },
        "2026-04": {},  # 300.00 advanced and refunded
    }


# req: QA-ENG-02
def test_pending_assets():
    r = _run("assets")
    assert r["rejected"] == ["AD-0003", "FA-0005"]  # FA-0001 is gone; 6010 is not an asset account
    assert [(e["document"], e["kind"], e["posting_date"]) for e in r["entries"]] == [
        ("AD-0001", "post", "2026-04-10"),
        ("AD-0002", "post", "2026-05-05"),
        ("FA-0001", "depreciation", "2026-01-31"),
        ("FA-0002", "depreciation", "2026-01-31"),  # November and December 2025 are in the opening balances
        ("FA-0004", "depreciation", "2026-01-31"),
        ("FA-0001", "depreciation", "2026-02-28"),
        ("FA-0002", "depreciation", "2026-02-28"),
        ("FA-0004", "depreciation", "2026-02-28"),
        ("FA-0001", "depreciation", "2026-03-31"),
        ("FA-0002", "depreciation", "2026-03-31"),
        ("FA-0003", "depreciation", "2026-03-31"),  # from 1 March
        ("FA-0004", "depreciation", "2026-03-31"),
        ("FA-0002", "depreciation", "2026-04-30"),  # FA-0001 disposed of in April
        ("FA-0003", "depreciation", "2026-04-30"),
        ("FA-0004", "depreciation", "2026-04-30"),
        ("FA-0002", "depreciation", "2026-05-31"),  # FA-0004 disposed of in May
        ("FA-0003", "depreciation", "2026-05-31"),
    ]
    # 3,270.00 on SR: GST 270.00; accumulated January to March 300.00; carrying 3,300.00 against 3,000.00 net.
    assert _lines(r, "AD-0001") == [("1010", "SGD", "3270.00", "3270.00"), ("1500", "SGD", "-3600.00", "-3600.00"),
                                    ("1590", "SGD", "300.00", "300.00"), ("2200", "SGD", "-270.00", "-270.00"),
                                    ("7050", "SGD", "300.00", "300.00")]
    # (2,000.00 - 200.00) ÷ 18 = 100.00 a month, January to April 400.00; carrying 1,600.00 against 1,500.00.
    assert _lines(r, "AD-0002") == [("1160", "SGD", "1500.00", "1500.00"), ("1510", "SGD", "-2000.00", "-2000.00"),
                                    ("1590", "SGD", "400.00", "400.00"), ("7050", "SGD", "100.00", "100.00")]
    assert r["trial_balance"] == {
        "2026-01": {"1590": "-300.00", "6610": "300.00"},
        "2026-02": {"1590": "-300.00", "6610": "300.00"},
        "2026-03": {"1590": "-633.33", "6610": "633.33"},  # 100.00 × 3 + 333.33
        "2026-04": {"1010": "3270.00", "1500": "-3600.00", "1590": "-233.34", "2200": "-270.00",
                    "6610": "533.34", "7050": "300.00"},  # 1590: +300.00 cleared, -(100.00 + 333.34 + 100.00)
        "2026-05": {"1160": "1500.00", "1510": "-2000.00", "1590": "-33.33", "6610": "433.33",
                    "7050": "100.00"},  # 1590: +400.00 cleared, -(100.00 + 333.33)
    }


def _section(total: str, groups: dict[str, str] | None = None) -> dict:
    return {"total": total, "groups": groups or {}}


_NO_PROFIT = {
    "revenue": _section("0.00"), "cost_of_sales": _section("0.00"), "gross_profit": "0.00",
    "operating_expenses": {"total": "0.00", "groups": {}, "functions": {}}, "ebitda": "0.00",
    "depreciation_and_amortisation": _section("0.00"), "operating_profit": "0.00", "other_income": _section("0.00"),
    "finance_costs_and_other_expenses": _section("0.00"), "profit_before_tax": "0.00", "income_tax": "0.00",
    "net_profit": "0.00",
}  # no percentages: a revenue of zero has none (RND-PERCENT)
_NO_CASH_FLOW = {"net_profit": "0.00", "depreciation_and_amortisation": "0.00", "investing_and_financing_results": "0.00",
                 "working_capital": {}, "total": "0.00"}


# req: FR-RPT-01, INV-10, INV-11, RND-PERCENT, QA-ENG-02
def test_pending_statements():
    r = _run("statements")
    assert r["rejected"] == []
    assert len(r["entries"]) == 23  # 22 journals and JNL-0010's reversal on 5 February
    s = r["statements"]
    assert list(s) == ["2026-01", "2026-02", "2026-03", "2026-04"]  # March has no entry and is still stated

    jan = s["2026-01"]
    assert jan["profit_and_loss"] == {
        "revenue": _section("1200.00", {"Subscriptions": "1200.00"}),
        "cost_of_sales": _section("-199.80", {"Hosting": "-199.80"}),
        "gross_profit": "1000.20",  # 1,200.00 - 199.80
        "operating_expenses": {
            "total": "-3399.99",  # salaries 2,000.00, marketing 500.00, rent 800.00 (paid by a director), software 99.99
            "groups": {"6300": "-99.99", "Marketing": "-500.00", "Rent": "-800.00", "Salaries": "-2000.00"},
            "functions": {"R&D": "-2000.00", "S&M": "-500.00", "G&A": "-800.00", "Other": "-99.99"},  # 6300 has none
        },
        "ebitda": "-2399.79",  # 1,000.20 - 3,399.99; depreciation is below it
        "depreciation_and_amortisation": _section("-83.33", {"Depreciation & amortisation": "-83.33"}),
        "operating_profit": "-2483.12",
        "other_income": _section("0.00"),
        "finance_costs_and_other_expenses": _section("0.00"),
        "profit_before_tax": "-2483.12",
        "income_tax": "0.00",
        "net_profit": "-2483.12",
        "percent_of_revenue": {  # of 1,200.00, to 1 decimal, half away from zero
            "revenue": "100.0",
            "cost_of_sales": "-16.7",  # -16.65 exactly: away from zero
            "gross_profit": "83.4",  # 83.35 exactly
            "operating_expenses": "-283.3",  # -283.3325
            "ebitda": "-200.0",  # -199.9825
            "depreciation_and_amortisation": "-6.9",  # -6.94416…
            "operating_profit": "-206.9",  # -206.92666…
            "other_income": "0.0",
            "finance_costs_and_other_expenses": "0.0",
            "profit_before_tax": "-206.9",
            "income_tax": "0.0",
            "net_profit": "-206.9",
        },
    }
    # Bank 10,000.00 - 3,270.00 + 1,000.00 - 199.80 - 2,000.00 - 99.99 = 5,430.21; receivable 1,308.00 - 1,000.00;
    # the laptop 3,000.00 less 83.33; GST paid 270.00 less 108.00 charged is a debit under current liabilities;
    # the director's loan 10,000.00 + 800.00; retained earnings hold the month's loss.
    assert jan["balance_sheet"] == {
        "current_assets": _section("5738.21", {"Cash and bank": "5430.21", "Trade receivables": "308.00"}),
        "non_current_assets": _section("2916.67", {"Property, plant & equipment": "2916.67"}),
        "total_assets": "8654.88",
        "current_liabilities": _section("338.00", {"GST": "-162.00", "Trade payables": "500.00"}),
        "borrowings": _section("10800.00", {"Loan from director": "10800.00"}),
        "total_liabilities": "11138.00",
        "equity": _section("-2483.12", {"Retained earnings": "-2483.12"}),
        "total_liabilities_and_equity": "8654.88",
    }
    # JNL-0001 is financing (+10,000.00) and JNL-0002 investing (-3,270.00, its GST inside); JNL-0008 moved no money.
    # Operating: -2,483.12 + 83.33 depreciation + 800.00 (the rent in a financing entry) - 308.00 receivables
    # + 108.00 GST charged + 500.00 payables = -1,299.79, the operating entries' cash: 1,000.00 - 199.80 - 2,000.00 - 99.99.
    assert jan["cash_flow"] == {
        "operating": {"net_profit": "-2483.12", "depreciation_and_amortisation": "83.33",
                      "investing_and_financing_results": "800.00",
                      "working_capital": {"Receivables": "-308.00", "Payables & accruals": "500.00",
                                          "Tax liabilities": "108.00"},
                      "total": "-1299.79"},
        "investing": "-3270.00",
        "financing": "10000.00",
        "net_change": "5430.21",
        "opening_cash": "0.00",
        "closing_cash": "5430.21",
        "non_cash": [{"document": "JNL-0008", "kind": "post", "posting_date": "2026-01-28", "section": "financing"}],
    }
    assert jan["equity_movement"] == {"opening": "0.00", "profit": "-2483.12", "share_capital": "0.00", "other": "0.00",
                                      "closing": "-2483.12"}
    assert jan["checks"] == {"balances": True, "ties": True, "equity": True, "unplaced": []}

    feb = s["2026-02"]
    assert feb["profit_and_loss"] == {
        "revenue": _section("2000.00", {"Services": "2000.00"}),
        "cost_of_sales": _section("0.00"),
        "gross_profit": "2000.00",
        "operating_expenses": {"total": "99.99", "groups": {"6300": "99.99"}, "functions": {"Other": "99.99"}},  # reversed
        "ebitda": "2099.99",
        "depreciation_and_amortisation": _section("-40.00", {"Depreciation & amortisation": "-40.00"}),  # 6620, same group
        "operating_profit": "2059.99",
        "other_income": _section("1533.33", {"Gains & losses on disposal": "33.33", "Grants & subsidies": "1500.00"}),
        "finance_costs_and_other_expenses": _section("-50.00", {"Finance costs": "-50.00"}),
        "profit_before_tax": "3543.32",  # 2,059.99 + 1,533.33 - 50.00
        "income_tax": "-100.00",
        "net_profit": "3443.32",
        "percent_of_revenue": {  # of 2,000.00
            "revenue": "100.0", "cost_of_sales": "0.0", "gross_profit": "100.0",
            "operating_expenses": "5.0",  # 4.9995
            "ebitda": "105.0",  # 104.9995
            "depreciation_and_amortisation": "-2.0",
            "operating_profit": "103.0",  # 102.9995
            "other_income": "76.7",  # 76.6665
            "finance_costs_and_other_expenses": "-2.5",
            "profit_before_tax": "177.2",  # 177.166
            "income_tax": "-5.0",
            "net_profit": "172.2",  # 172.166
        },
    }
    # Bank 5,430.21 - 500.00 - 1,200.00 + 600.00 + 1,500.00 + 2,950.00 - 1,050.00 - 300.00 + 99.99 = 7,530.20, and
    # 2,180.00 in 1020; the laptop gone and 40.00 of amortisation left on 1590; GST 162.00 debit - 180.00 = 18.00 owed;
    # the loan 10,800.00 - 1,000.00 - 2,000.00; retained earnings -2,483.12 + 3,443.32 - 300.00 of dividend.
    assert feb["balance_sheet"] == {
        "current_assets": _section("11218.20", {"Cash and bank": "9710.20", "Prepayments": "1200.00",
                                                "Trade receivables": "308.00"}),
        "non_current_assets": _section("-40.00", {"Property, plant & equipment": "-40.00"}),
        "total_assets": "11178.20",
        "current_liabilities": _section("718.00", {"Deferred revenue": "600.00", "GST": "18.00",
                                                   "Income tax payable": "100.00"}),  # trade payables back at nothing
        "borrowings": _section("7800.00", {"Loan from director": "7800.00"}),
        "total_liabilities": "8518.00",
        "equity": _section("2660.20", {"Capital contribution": "2000.00", "Retained earnings": "660.20"}),
        "total_liabilities_and_equity": "11178.20",
    }
    # The sale (+2,950.00) is investing with its 33.33 gain taken out of operating; the repayment with interest
    # (-1,050.00) and the dividend (-300.00) are financing, the interest put back; the conversion moved no money.
    # Operating: 3,443.32 + 40.00 + 16.67 - 1,200.00 prepaid - 500.00 paid to the supplier + 280.00 tax (180.00 GST,
    # 100.00 income tax) + 600.00 paid ahead = 2,679.99 = -500.00 - 1,200.00 + 2,180.00 + 600.00 + 1,500.00 + 99.99.
    assert feb["cash_flow"] == {
        "operating": {"net_profit": "3443.32", "depreciation_and_amortisation": "40.00",
                      "investing_and_financing_results": "16.67",
                      "working_capital": {"Other current assets": "-1200.00", "Payables & accruals": "-500.00",
                                          "Tax liabilities": "280.00", "Deferred revenue": "600.00"},
                      "total": "2679.99"},
        "investing": "2950.00",
        "financing": "-1350.00",
        "net_change": "4279.99",
        "opening_cash": "5430.21",
        "closing_cash": "9710.20",
        "non_cash": [{"document": "JNL-0018", "kind": "post", "posting_date": "2026-02-26", "section": "financing"}],
    }
    # The capital contribution 2,000.00 less the dividend 300.00.
    assert feb["equity_movement"] == {"opening": "-2483.12", "profit": "3443.32", "share_capital": "0.00",
                                      "other": "1700.00", "closing": "2660.20"}
    assert feb["checks"] == {"balances": True, "ties": True, "equity": True, "unplaced": []}

    mar = s["2026-03"]
    assert mar["profit_and_loss"] == _NO_PROFIT
    assert mar["balance_sheet"] == feb["balance_sheet"]
    assert mar["cash_flow"] == {"operating": _NO_CASH_FLOW, "investing": "0.00", "financing": "0.00",
                                "net_change": "0.00", "opening_cash": "9710.20", "closing_cash": "9710.20", "non_cash": []}
    assert mar["equity_movement"] == {"opening": "2660.20", "profit": "0.00", "share_capital": "0.00", "other": "0.00",
                                      "closing": "2660.20"}
    assert mar["checks"] == {"balances": True, "ties": True, "equity": True, "unplaced": []}

    # 50.00 paid to 1990, which has no cash-flow line: it sits in no section, so the balance sheet is 50.00 short
    # and the cash flow misses the 50.00 that left the bank. Both checks say so, and name the account.
    apr = s["2026-04"]
    assert apr["profit_and_loss"] == _NO_PROFIT
    assert apr["balance_sheet"]["current_assets"] == _section(
        "11168.20", {"Cash and bank": "9660.20", "Prepayments": "1200.00", "Trade receivables": "308.00"})
    assert (apr["balance_sheet"]["total_assets"], apr["balance_sheet"]["total_liabilities_and_equity"]) == (
        "11128.20", "11178.20")
    assert apr["cash_flow"] == {"operating": _NO_CASH_FLOW, "investing": "0.00", "financing": "0.00",
                                "net_change": "0.00", "opening_cash": "9710.20", "closing_cash": "9660.20", "non_cash": []}
    assert apr["checks"] == {"balances": False, "ties": False, "equity": True, "unplaced": ["1990"]}


# req: FR-KPI-02, FR-KPI-03, RND-KPI, QA-ENG-02
def test_pending_kpis():
    k = _run("statements")["kpis"]
    assert list(k) == ["2026-01", "2026-02", "2026-03", "2026-04"]
    # January: 4010 is the subscription stream; 10 paying customers, all new; 3 people.
    assert k["2026-01"] == {
        "booked_mrr": "1200.00", "arr": "14400.00", "contracted_mrr": None,
        "revenue_growth": None,  # nothing posted the month before
        "gross_margin": "83.4",  # 1,000.20 ÷ 1,200.00 = 83.35%
        "spend_by_function": {"R&D": "2000.00", "S&M": "500.00", "G&A": "883.33"},  # rent 800.00 + depreciation 83.33
        "net_burn": "4569.79",  # -(-1,299.79 operating - 3,270.00 investing)
        "runway": "1.2",  # 5,430.21 ÷ 4,569.79 (January alone) = 1.188
        "arpa": "120.00",  # 1,200.00 ÷ 10
        "churn": None,  # 10 - 10 + 0 = no customers at the start of the month
        "cac": "50.00",  # 500.00 ÷ 10
        "ltv": None, "ltv_to_cac": None,  # no churn
        "cac_payback": "0.5",  # 50.00 ÷ (120.00 × 0.8335) = 0.4999
        "burn_multiple": "0.3",  # 4,569.79 ÷ 14,400.00 of new ARR
        "current_ratio": "17.0", "quick_ratio": "17.0",  # 5,738.21 ÷ 338.00; cash and receivables are all of it
        "working_capital": "5400.21",
        "debt_to_equity": None,  # equity -2,483.12
        "dso": "7.7",  # 308.00 ÷ (1,200.00 ÷ 30)
        "dpo": "4.2",  # 500.00 ÷ ((199.80 + 3,399.99) ÷ 30) = 4.167
        "ebitda_margin": "-200.0", "net_margin": "-206.9",
        "headcount": "3", "revenue_per_employee": "4800.00",  # 1,200.00 × 12 ÷ 3
    }
    # February: services only, so booked MRR is 0.00 (the stream exists); the month made cash.
    assert k["2026-02"] == {
        "booked_mrr": "0.00", "arr": "0.00", "contracted_mrr": None,
        "revenue_growth": "66.7",  # (2,000.00 - 1,200.00) ÷ 1,200.00
        "gross_margin": "100.0",
        "spend_by_function": {"R&D": "0.00", "S&M": "0.00", "G&A": "0.00"},  # 6300 and 6620 have no function
        "net_burn": "-5629.99",  # -(2,679.99 + 2,950.00)
        "runway": "not-burning",  # (4,569.79 - 5,629.99) ÷ 2 ≤ 0
        "arpa": "0.00",
        "churn": "20.0",  # 2 ÷ (12 - 4 + 2)
        "cac": "0.00",  # no S&M spend
        "ltv": "0.00",  # 0.00 × 100% ÷ 20%
        "ltv_to_cac": None,  # a CAC of 0
        "cac_payback": None,  # ARPA × margin is 0
        "burn_multiple": None,  # not burning
        "current_ratio": "15.6",  # 11,218.20 ÷ 718.00 = 15.624
        "quick_ratio": "14.0",  # (9,710.20 + 308.00) ÷ 718.00 = 13.953
        "working_capital": "10500.20",
        "debt_to_equity": "3.2",  # 8,518.00 ÷ 2,660.20
        "dso": "5.8",  # 308.00 ÷ ((1,200.00 + 2,000.00) ÷ 2 ÷ 30) = 5.775
        "dpo": "0.0",  # nothing owed to suppliers
        "ebitda_margin": "105.0", "net_margin": "172.2",
        "headcount": "4", "revenue_per_employee": "6000.00",
    }
    # March: no entry and no customer counts; headcount entered.
    mar = k["2026-03"]
    assert (mar["booked_mrr"], mar["revenue_growth"], mar["gross_margin"], mar["net_margin"]) == ("0.00", "-100.0", None, None)
    assert (mar["net_burn"], mar["runway"]) == ("0.00", "not-burning")
    assert [mar[key] for key in ("arpa", "churn", "cac", "ltv", "ltv_to_cac", "cac_payback")] == [None] * 6  # not entered
    assert mar["dso"] == "8.7"  # 308.00 ÷ (3,200.00 ÷ 3 ÷ 30) = 8.6625
    assert (mar["headcount"], mar["revenue_per_employee"]) == ("4", "0.00")
    # April: counts without headcount; the 50.00 to an unplaced account is in neither the cash flow nor its burn.
    apr = k["2026-04"]
    assert (apr["revenue_growth"], apr["net_burn"], apr["runway"]) == (None, "0.00", "not-burning")
    assert (apr["arpa"], apr["churn"], apr["cac"], apr["ltv"]) == ("0.00", "0.0", None, None)  # no new customers
    assert (apr["current_ratio"], apr["quick_ratio"], apr["working_capital"]) == ("15.6", "13.9", "10450.20")
    assert apr["dso"] == "13.9"  # February to April: 308.00 ÷ (2,000.00 ÷ 3 ÷ 30) = 13.86
    assert (apr["headcount"], apr["revenue_per_employee"]) == (None, None)
