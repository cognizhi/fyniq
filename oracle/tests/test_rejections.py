"""Each rule a journal or a reversal must meet, broken on its own (ENG-21 ✖: nothing posts).

Every test checks three things: the document is listed in ``rejected``, it posted no line (no
entry, no trial-balance figure), and it was refused for the intended rule, so a test cannot pass
because some other rule happened to refuse it. The reason never appears in the result.
"""

import pytest
from builders import journal, line, post

SGD_JPY_KWD = {"SGD": 2, "USD": 2, "JPY": 0, "KWD": 3}


def _refused(doc, reason, **options):
    result, ledger = post(doc, **options)
    assert result["rejected"] == [doc["id"]]
    assert result["entries"] == []
    assert result["trial_balance"] == {}
    assert ledger.reasons == [(doc["id"], reason)]
    assert reason not in str(result)


# req: QA-ENG-01
def test_a_journal_with_one_line_is_rejected():
    _refused(journal("JNL-0001", "2026-01-05", line("1010", "SGD", "10.00")), "fewer-than-two-lines")


# req: QA-ENG-01
def test_a_journal_with_no_lines_is_rejected():
    _refused(journal("JNL-0001", "2026-01-05"), "fewer-than-two-lines")


# req: QA-ENG-01
@pytest.mark.parametrize("zero", ["0.00", "0", "-0", "-0.00"])
def test_a_zero_line_is_rejected_even_when_the_journal_balances(zero):
    _refused(
        journal(
            "JNL-0001",
            "2026-01-05",
            line("1010", "SGD", "10.00"),
            line("6010", "SGD", zero),
            line("4010", "SGD", "-10.00"),
        ),
        "zero-line",
    )


# req: QA-ENG-01
@pytest.mark.parametrize(
    ("account", "reason"),
    [("1000", "group-account"), ("1090", "inactive-account"), ("9999", "unknown-account")],
)
def test_a_line_on_an_account_that_cannot_take_postings_is_rejected(account, reason):
    _refused(
        journal("JNL-0001", "2026-01-05", line(account, "SGD", "10.00"), line("4010", "SGD", "-10.00")),
        reason,
    )


# req: QA-ENG-01
def test_a_foreign_line_without_a_rate_is_rejected():
    _refused(
        journal("JNL-0001", "2026-01-05", line("1010", "USD", "100.00"), line("4010", "SGD", "-134.72")),
        "missing-rate",
    )


# req: QA-ENG-01
def test_a_foreign_rate_without_its_source_is_rejected():
    _refused(
        journal(
            "JNL-0001",
            "2026-01-05",
            line("1010", "USD", "100.00", "1.3472", source=None),
            line("4010", "SGD", "-134.72"),
        ),
        "missing-rate-source",
    )


# req: QA-ENG-01
def test_a_rate_on_a_base_currency_line_is_rejected():
    _refused(
        journal("JNL-0001", "2026-01-05", line("1010", "SGD", "10.00", "1"), line("4010", "SGD", "-10.00")),
        "rate-on-base-line",
    )


# req: QA-ENG-01
def test_a_journal_that_does_not_balance_in_base_is_rejected():
    # 10.00 - 9.99 = 0.01
    _refused(
        journal("JNL-0001", "2026-01-05", line("1010", "SGD", "10.00"), line("4010", "SGD", "-9.99")),
        "unbalanced-base",
    )


# req: QA-ENG-01
def test_a_single_currency_journal_balanced_in_its_currency_but_not_in_base_is_rejected():
    # USD 0.01 + 0.01 - 0.02 = 0, but in base each line rounds on its own (INV-05):
    # 0.01 × 1.5 = 0.015 -> 0.02, twice; -0.02 × 1.5 = -0.03. 0.02 + 0.02 - 0.03 = 0.01.
    # A manual journal carries no rounding line, so it cannot post.
    _refused(
        journal(
            "JNL-0001",
            "2026-01-05",
            line("1010", "USD", "0.01", "1.5"),
            line("1020", "USD", "0.01", "1.5"),
            line("4010", "USD", "-0.02", "1.5"),
        ),
        "unbalanced-base",
    )


# req: QA-ENG-01
def test_a_single_currency_journal_balanced_in_base_but_not_in_its_currency_is_rejected():
    # JPY 100 × 0.0001 = 0.0100 -> 0.01; JPY -101 × 0.0001 = -0.0101 -> -0.01. Base: 0.01 - 0.01 = 0.
    # JPY: 100 - 101 = -1, and every line is in JPY, so it must balance in JPY too (INV-01).
    _refused(
        journal("JNL-0001", "2026-01-05", line("1010", "JPY", "100", "0.0001"), line("4010", "JPY", "-101", "0.0001")),
        "unbalanced-currency",
        currencies=SGD_JPY_KWD,
    )


# req: QA-ENG-01
def test_a_line_in_a_currency_the_scenario_does_not_know_is_rejected():
    _refused(
        journal("JNL-0001", "2026-01-05", line("1010", "XTS", "10.00", "1.2"), line("4010", "SGD", "-12.00")),
        "unknown-currency",
    )


# req: QA-ENG-01
@pytest.mark.parametrize(
    ("currency", "amount", "rate", "balance"),
    [
        ("USD", "10.001", "1.5", "-15.00"),  # three decimals for a two-decimal currency
        ("JPY", "12345.0", "0.0093", "-114.81"),  # a zero is still a decimal JPY does not have
        ("SGD", "10.000", None, "-10.00"),
    ],
)
def test_an_amount_with_more_decimals_than_its_currency_is_rejected_not_rounded(currency, amount, rate, balance):
    _refused(
        journal("JNL-0001", "2026-01-05", line("1010", currency, amount, rate), line("4010", "SGD", balance)),
        "amount-too-many-decimals",
        currencies=SGD_JPY_KWD,
    )


# req: QA-ENG-01
@pytest.mark.parametrize(
    "amount",
    [
        "1e3", "1E+2", "+5.00", " 5.00", "5.00 ", "1,000.00", "1_000", "NaN", "Infinity", "-Infinity", "0x1A",
        "", ".5", "5.", "--5",
        "\u22125.00",  # a minus sign, not a hyphen-minus
        "\u0661\u0662\u0663",  # Arabic-Indic digits: Decimal() reads them as 123
    ],
)
def test_an_amount_that_is_not_a_plain_decimal_is_rejected(amount):
    _refused(
        journal("JNL-0001", "2026-01-05", line("1010", "SGD", amount), line("4010", "SGD", "-5.00")),
        "amount-not-plain-decimal",
    )


# req: QA-ENG-01
def test_an_amount_of_seventeen_integer_digits_is_rejected():
    big = "10000000000000000.00"
    _refused(
        journal("JNL-0001", "2026-01-05", line("1010", "SGD", big), line("4010", "SGD", "-" + big)),
        "amount-out-of-range",
    )


# req: QA-ENG-01
@pytest.mark.parametrize(
    ("rate", "reason"),
    [
        ("1.00000000001", "rate-too-many-decimals"),  # eleven decimals
        ("1e-3", "rate-not-plain-decimal"),
        ("-1.25", "rate-not-plain-decimal"),
        ("", "rate-not-plain-decimal"),
        ("0", "rate-not-positive"),
        ("0.0000000000", "rate-not-positive"),
        ("10000000000", "rate-out-of-range"),  # eleven integer digits; NUMERIC(20,10) holds ten
    ],
)
def test_a_rate_that_breaks_the_format_is_rejected(rate, reason):
    _refused(
        journal("JNL-0001", "2026-01-05", line("1010", "USD", "1.00", rate), line("4010", "SGD", "-1.00")),
        reason,
    )


# req: QA-ENG-01
def test_a_base_amount_beyond_sixteen_integer_digits_is_rejected():
    # KWD 9 999 999 999 999 999.999 × 2 = SGD 19 999 999 999 999 999.998: seventeen integer digits.
    _refused(
        journal(
            "JNL-0001",
            "2026-01-05",
            line("1010", "KWD", "9999999999999999.999", "2"),
            line("4010", "KWD", "-9999999999999999.999", "2"),
        ),
        "base-out-of-range",
        currencies=SGD_JPY_KWD,
    )


GOOD = journal("JNL-0001", "2026-01-05", line("1010", "SGD", "10.00"), line("4010", "SGD", "-10.00"))
BAD = journal("JNL-0002", "2026-01-06", line("1010", "SGD", "10.00"), line("4010", "SGD", "-9.00"))


# req: QA-ENG-01
def test_a_reversal_of_an_unknown_document_is_rejected():
    result, ledger = post(GOOD, reversals=(("JNL-0404", "2026-01-07"),))
    assert result["rejected"] == ["JNL-0404/reversal"]
    assert ledger.reasons == [("JNL-0404/reversal", "reversal-of-unposted-document")]
    assert [e["kind"] for e in result["entries"]] == ["post"]


# req: QA-ENG-01
def test_a_reversal_of_a_rejected_document_is_rejected():
    result, ledger = post(GOOD, BAD, reversals=(("JNL-0002", "2026-01-07"),))
    assert result["rejected"] == ["JNL-0002", "JNL-0002/reversal"]
    assert ledger.reasons == [("JNL-0002", "unbalanced-base"), ("JNL-0002/reversal", "reversal-of-unposted-document")]
    assert result["trial_balance"] == {"2026-01": {"1010": "10.00", "4010": "-10.00"}}


# req: QA-ENG-01
def test_a_document_is_reversed_at_most_once():
    # The first reversal posts on 7 Jan; the second and third are refused, in order.
    result, ledger = post(
        GOOD,
        reversals=(("JNL-0001", "2026-01-07"), ("JNL-0001", "2026-02-07"), ("JNL-0001", "2026-03-07")),
    )
    assert [(e["kind"], e["posting_date"]) for e in result["entries"]] == [
        ("post", "2026-01-05"),
        ("reversal", "2026-01-07"),
    ]
    assert result["rejected"] == ["JNL-0001/reversal", "JNL-0001/reversal"]
    assert [reason for _, reason in ledger.reasons] == ["already-reversed", "already-reversed"]
    assert result["trial_balance"] == {"2026-01": {}}


# req: QA-ENG-01
def test_rejections_are_listed_in_order_and_do_not_stop_later_documents():
    result, _ = post(
        BAD,
        GOOD,
        journal("JNL-0003", "2026-01-08", line("1010", "SGD", "5.00")),
        reversals=(("JNL-0003", "2026-01-09"), ("JNL-0001", "2026-02-01")),
    )
    assert result["rejected"] == ["JNL-0002", "JNL-0003", "JNL-0003/reversal"]
    assert [(e["document"], e["kind"]) for e in result["entries"]] == [("JNL-0001", "post"), ("JNL-0001", "reversal")]
    assert result["trial_balance"] == {
        "2026-01": {"1010": "10.00", "4010": "-10.00"},
        "2026-02": {"1010": "-10.00", "4010": "10.00"},
    }
