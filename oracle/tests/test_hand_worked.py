"""Hand-worked journals: every expected figure is worked out by hand in the comment beside it.

None of them comes from running the oracle (or the engine): the test fails if the oracle's figure
differs from the hand-worked one.
"""

from builders import journal, line, post

SGD_JPY_KWD = {"SGD": 2, "USD": 2, "JPY": 0, "KWD": 3}


def _lines(result: dict, index: int = 0) -> list[tuple[str, str, str, str]]:
    lines = result["entries"][index]["lines"]
    return [(ln["account"], ln["currency"], ln["amount"], ln["base_amount"]) for ln in lines]


# req: QA-ENG-01
def test_a_foreign_line_converts_at_its_rate():
    # USD 100.00 × 1.3472 = 134.72 exactly; the SGD line balances it.
    result, _ = post(
        journal(
            "JNL-0001",
            "2026-01-05",
            line("1010", "USD", "100.00", "1.3472"),
            line("4010", "SGD", "-134.72"),
        )
    )
    assert result["entries"][0]["kind"] == "post"
    assert result["entries"][0]["posting_date"] == "2026-01-05"
    assert _lines(result) == [("1010", "USD", "100.00", "134.72"), ("4010", "SGD", "-134.72", "-134.72")]
    assert result["rejected"] == []
    assert result["trial_balance"] == {"2026-01": {"1010": "134.72", "4010": "-134.72"}}


# req: QA-ENG-01
def test_rnd_fx_rounds_a_positive_half_cent_up():
    # USD 0.10 × 1.25 = 0.125: exactly half a cent, rounded away from zero to 0.13.
    result, _ = post(
        journal("JNL-0001", "2026-01-05", line("1010", "USD", "0.10", "1.25"), line("4010", "SGD", "-0.13"))
    )
    assert _lines(result)[0] == ("1010", "USD", "0.10", "0.13")


# req: QA-ENG-01
def test_rnd_fx_rounds_a_negative_half_cent_away_from_zero():
    # USD -0.10 × 1.25 = -0.125: half away from zero gives -0.13. Banker's rounding (half to even)
    # would give -0.12, and so would rounding half toward positive infinity.
    result, _ = post(
        journal("JNL-0001", "2026-01-05", line("6010", "SGD", "0.13"), line("2010", "USD", "-0.10", "1.25"))
    )
    assert _lines(result) == [("6010", "SGD", "0.13", "0.13"), ("2010", "USD", "-0.10", "-0.13")]
    assert result["trial_balance"] == {"2026-01": {"2010": "-0.13", "6010": "0.13"}}


# req: QA-ENG-01
def test_rnd_fx_rounds_below_half_toward_zero():
    # USD -0.10 × 1.24 = -0.124 -> -0.12.
    result, _ = post(
        journal("JNL-0001", "2026-01-05", line("6010", "SGD", "0.12"), line("2010", "USD", "-0.10", "1.24"))
    )
    assert _lines(result)[1] == ("2010", "USD", "-0.10", "-0.12")


# req: QA-ENG-01
def test_a_jpy_line_has_no_decimals_and_converts_to_cents():
    # JPY 12345 × 0.0093 = 114.8085 -> SGD 114.81 (the third decimal, 8, rounds up).
    result, _ = post(
        journal("JNL-0001", "2026-03-31", line("1010", "JPY", "12345", "0.0093"), line("4010", "SGD", "-114.81")),
        currencies=SGD_JPY_KWD,
    )
    assert _lines(result) == [("1010", "JPY", "12345", "114.81"), ("4010", "SGD", "-114.81", "-114.81")]
    assert result["trial_balance"] == {"2026-03": {"1010": "114.81", "4010": "-114.81"}}


# req: QA-ENG-01
def test_a_jpy_base_rounds_to_whole_yen():
    # Base JPY: USD -0.50 × 149 = -74.5, half away from zero -> JPY -75. RND-FX rounds to the
    # *base* currency's minor units (0), not the line currency's (2).
    result, _ = post(
        journal("JNL-0001", "2026-01-05", line("6010", "JPY", "75"), line("2010", "USD", "-0.50", "149")),
        base="JPY",
        currencies=SGD_JPY_KWD,
    )
    assert _lines(result) == [("6010", "JPY", "75", "75"), ("2010", "USD", "-0.50", "-75")]
    assert result["trial_balance"] == {"2026-01": {"2010": "-75", "6010": "75"}}


# req: QA-ENG-01
def test_a_kwd_line_keeps_three_decimals():
    # KWD 1.234 × 4.4321 = 5.4692114 -> SGD 5.47.
    result, _ = post(
        journal("JNL-0001", "2026-01-05", line("1010", "KWD", "1.234", "4.4321"), line("4010", "SGD", "-5.47")),
        currencies=SGD_JPY_KWD,
    )
    assert _lines(result) == [("1010", "KWD", "1.234", "5.47"), ("4010", "SGD", "-5.47", "-5.47")]


# req: QA-ENG-01
def test_a_kwd_base_rounds_to_three_decimals():
    # Base KWD: SGD 10.00 × 0.2263 = 2.263 exactly; USD 3.33 × 0.30715 = 1.0228095 -> 1.023
    # (the fourth decimal, 8, rounds up); the KWD line is -(2.263 + 1.023) = -3.286.
    result, _ = post(
        journal(
            "JNL-0001",
            "2026-01-05",
            line("1010", "SGD", "10.00", "0.2263"),
            line("1020", "USD", "3.33", "0.30715"),
            line("4010", "KWD", "-3.286"),
        ),
        base="KWD",
        currencies=SGD_JPY_KWD,
    )
    assert _lines(result) == [
        ("1010", "SGD", "10.00", "2.263"),
        ("1020", "USD", "3.33", "1.023"),
        ("4010", "KWD", "-3.286", "-3.286"),
    ]
    assert result["trial_balance"] == {"2026-01": {"1010": "2.263", "1020": "1.023", "4010": "-3.286"}}


# req: QA-ENG-01
def test_amounts_with_fewer_decimals_are_written_at_the_minor_units():
    # USD "100" is 100.00; × 1.5 = 150.00. SGD "-150" is -150.00.
    result, _ = post(
        journal("JNL-0001", "2026-01-05", line("1010", "USD", "100", "1.5"), line("4010", "SGD", "-150"))
    )
    assert _lines(result) == [("1010", "USD", "100.00", "150.00"), ("4010", "SGD", "-150.00", "-150.00")]


# req: QA-ENG-01
def test_a_multi_currency_journal_needs_to_balance_only_in_base():
    # USD 10.00 × 1.35 = 13.50; EUR -5.00 × 1.5 = -7.50; SGD -6.00: 13.50 - 7.50 - 6.00 = 0.
    # Neither USD nor EUR balances on its own, and need not: the lines do not share one currency.
    result, _ = post(
        journal(
            "JNL-0001",
            "2026-01-05",
            line("1010", "USD", "10.00", "1.35"),
            line("4010", "EUR", "-5.00", "1.5"),
            line("4010", "SGD", "-6.00"),
        )
    )
    assert _lines(result) == [
        ("1010", "USD", "10.00", "13.50"),
        ("4010", "EUR", "-5.00", "-7.50"),
        ("4010", "SGD", "-6.00", "-6.00"),
    ]
    # Two lines on 4010 in one month: -7.50 - 6.00 = -13.50.
    assert result["trial_balance"] == {"2026-01": {"1010": "13.50", "4010": "-13.50"}}


# req: QA-ENG-01
def test_a_reversal_negates_the_stored_lines_in_its_own_month():
    # Posted 31 Jan: USD 0.10 × 1.25 = 0.125 -> 0.13. Reversed 1 Feb: every line negated as stored,
    # so February holds -0.13 on 1010 and +0.13 on 4010, and each month nets to zero.
    result, _ = post(
        journal("JNL-0001", "2026-01-31", line("1010", "USD", "0.10", "1.25"), line("4010", "SGD", "-0.13")),
        reversals=(("JNL-0001", "2026-02-01"),),
    )
    assert [(e["document"], e["kind"], e["posting_date"]) for e in result["entries"]] == [
        ("JNL-0001", "post", "2026-01-31"),
        ("JNL-0001", "reversal", "2026-02-01"),
    ]
    assert _lines(result, 1) == [("1010", "USD", "-0.10", "-0.13"), ("4010", "SGD", "0.13", "0.13")]
    assert result["rejected"] == []
    assert result["trial_balance"] == {
        "2026-01": {"1010": "0.13", "4010": "-0.13"},
        "2026-02": {"1010": "-0.13", "4010": "0.13"},
    }


# req: QA-ENG-01
def test_a_reversal_in_the_same_month_leaves_the_month_with_nothing_but_zeros():
    # +250.00 and -250.00 on each account in March: every total is zero, so no account is listed,
    # but March is still a posting month.
    result, _ = post(
        journal("JNL-0001", "2026-03-02", line("6010", "SGD", "250.00"), line("2010", "SGD", "-250.00")),
        reversals=(("JNL-0001", "2026-03-20"),),
    )
    assert len(result["entries"]) == 2
    assert result["trial_balance"] == {"2026-03": {}}


# req: QA-ENG-01
def test_the_trial_balance_adds_up_each_month_separately():
    #   JNL-0001  January  1010 +100.00  4010 -100.00
    #   JNL-0002  January  1010  +20.00  6010  -20.00
    #   JNL-0003  March    6010  +45.50  1010  -45.50
    # January: 1010 = 100.00 + 20.00 = 120.00; 4010 = -100.00; 6010 = -20.00.
    # March: 1010 = -45.50; 6010 = 45.50. February has no posting, so it is not listed.
    result, _ = post(
        journal("JNL-0001", "2026-01-05", line("1010", "SGD", "100.00"), line("4010", "SGD", "-100.00")),
        journal("JNL-0002", "2026-01-31", line("1010", "SGD", "20.00"), line("6010", "SGD", "-20.00")),
        journal("JNL-0003", "2026-03-01", line("6010", "SGD", "45.50"), line("1010", "SGD", "-45.50")),
    )
    assert result["trial_balance"] == {
        "2026-01": {"1010": "120.00", "4010": "-100.00", "6010": "-20.00"},
        "2026-03": {"1010": "-45.50", "6010": "45.50"},
    }


# req: QA-ENG-01
def test_amounts_of_sixteen_integer_digits_post():
    # The largest SGD amount NUMERIC(20,4) holds at 2 decimals: 9 999 999 999 999 999.99.
    big = "9999999999999999.99"
    result, _ = post(journal("JNL-0001", "2026-01-05", line("1010", "SGD", big), line("2010", "SGD", "-" + big)))
    assert _lines(result) == [("1010", "SGD", big, big), ("2010", "SGD", "-" + big, "-" + big)]
    assert result["trial_balance"] == {"2026-01": {"1010": big, "2010": "-" + big}}


# req: QA-ENG-01
def test_a_long_product_is_exact_before_its_one_rounding():
    # KWD 1 234 567 890 123 456.789 × 0.0000000123: 1234567890123456.789 × 123 =
    # 151 851 850 485 185 185.047, shifted ten places: 15 185 185.0485185185047 -> SGD 15 185 185.05.
    # The product has 22 significant digits; nothing may round it before RND-FX does.
    result, _ = post(
        journal(
            "JNL-0001",
            "2026-01-05",
            line("1010", "KWD", "1234567890123456.789", "0.0000000123"),
            line("4010", "SGD", "-15185185.05"),
        ),
        currencies=SGD_JPY_KWD,
    )
    assert _lines(result)[0] == ("1010", "KWD", "1234567890123456.789", "15185185.05")


# req: INV-05
def test_a_non_zero_line_whose_base_rounds_to_zero_posts():
    # Decided in PRD-001 rev 0.24 (test/differential/README.md): JPY 1 × 0.001 = 0.001 -> SGD 0.00.
    # The line is not zero in its own currency, and JPY 1 - 1 = 0 balances, so the journal posts.
    result, _ = post(
        journal("JNL-0001", "2026-01-05", line("1010", "JPY", "1", "0.001"), line("4010", "JPY", "-1", "0.001")),
        currencies=SGD_JPY_KWD,
    )
    assert _lines(result) == [("1010", "JPY", "1", "0.00"), ("4010", "JPY", "-1", "0.00")]
    assert result["trial_balance"] == {"2026-01": {}}
