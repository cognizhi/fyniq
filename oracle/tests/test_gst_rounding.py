"""RND-GST-EXCL and RND-GST-INCL (PRD-001 §10.4), worked by hand.

Every expected figure is worked out in the comment beside it, never by running the oracle or the
engine (QA-ENG-01). p1-statutory-packs task 2.3: per subtotal or per line, as the organisation's
setting says, and the extraction from a GST-inclusive amount.
"""

from decimal import Decimal as D

import pytest
from hypothesis import given
from hypothesis import strategies as st

from oracle import money


# req: RND-GST-EXCL
def test_per_subtotal_rounds_each_codes_subtotal_once():
    # 3 × 10.05 = 30.15; 30.15 × 9% = 2.7135 -> 2.71.
    lines = [("SR", D("10.05"))] * 3
    assert money.rnd_gst_excl(lines, D("9"), 2) == {"SR": D("2.71")}


# req: RND-GST-EXCL
def test_per_line_rounds_each_line_and_sums():
    # 10.05 × 9% = 0.9045 -> 0.90 per line; three lines 2.70, a cent below the subtotal method.
    lines = [("SR", D("10.05"))] * 3
    assert money.rnd_gst_excl(lines, D("9"), 2, per_line=True) == {"SR": D("2.70")}


# req: RND-GST-EXCL
def test_each_gst_code_is_its_own_subtotal():
    # TX 0.05 × 9% = 0.0045 -> 0.00 and IM likewise; one subtotal of 0.10 would give 0.009 -> 0.01.
    lines = [("TX", D("0.05")), ("IM", D("0.05"))]
    assert money.rnd_gst_excl(lines, D("9"), 2) == {"TX": D("0.00"), "IM": D("0.00")}


# req: RND-GST-EXCL
@pytest.mark.parametrize(("net", "gst"), [("0.50", "0.05"), ("-0.50", "-0.05"), ("0.49", "0.04"), ("-0.49", "-0.04")])
def test_half_a_cent_rounds_away_from_zero(net, gst):
    # 0.50 × 9% = 0.045 -> 0.05 (half away from zero), the credit note's -0.045 -> -0.05;
    # 0.49 × 9% = 0.0441 -> 0.04. To the nearest cent, never to five cents (cash rounding only).
    assert money.rnd_gst_excl([("SR", D(net))], D("9"), 2) == {"SR": D(gst)}


# req: RND-GST-EXCL
def test_the_rate_is_the_one_given_and_the_minor_units_the_currencys():
    # 1,234.56 × 8% = 98.7648 -> 98.76 (an 8% invoice credited at 8%); in a 3-decimal base,
    # 1.235 × 9% = 0.11115 -> 0.111.
    assert money.rnd_gst_excl([("SR", D("1234.56"))], D("8"), 2) == {"SR": D("98.76")}
    assert money.rnd_gst_excl([("SR", D("1.235"))], D("9"), 3) == {"SR": D("0.111")}


# req: RND-GST-EXCL
def test_codes_keep_the_order_they_first_appear_in():
    lines = [("IM", D("1.00")), ("TX", D("2.00")), ("IM", D("3.00"))]
    assert list(money.rnd_gst_excl(lines, D("9"), 2)) == ["IM", "TX"]
    assert money.rnd_gst_excl(lines, D("9"), 2) == {"IM": D("0.36"), "TX": D("0.18")}  # 4.00 and 2.00 × 9%


# req: RND-GST-INCL
@pytest.mark.parametrize(
    ("gross", "percent", "gst"),
    [
        ("109.00", "9", "9.00"),  # 109 × 9 ÷ 109 = 9 exactly
        ("100.00", "9", "8.26"),  # 900 ÷ 109 = 8.2568... -> 8.26
        ("-100.00", "9", "-8.26"),  # money out mirrors money in
        ("54.50", "9", "4.50"),  # G4-026's receipt: 490.5 ÷ 109 = 4.5 exactly
        ("0.13", "4", "0.01"),  # 0.52 ÷ 104 = 0.005, half a cent -> 0.01
        ("-0.13", "4", "-0.01"),  # and away from zero when negative
        ("1.00", "9", "0.08"),  # 9 ÷ 109 = 0.0825... -> 0.08
        ("0", "9", "0.00"),
    ],
)
def test_gst_extracted_from_a_gst_inclusive_amount(gross, percent, gst):
    assert money.rnd_gst_incl(D(gross), D(percent), 2) == D(gst)


# req: RND-GST-INCL
def test_extraction_rounds_to_the_base_currencys_minor_units():
    # JPY base: 10,000 × 9 ÷ 109 = 825.68... -> 826; KWD base: 10.000 × 9 ÷ 109 = 0.82568... -> 0.826.
    assert money.rnd_gst_incl(D("10000"), D("9"), 0) == D("826")
    assert money.rnd_gst_incl(D("10.000"), D("9"), 3) == D("0.826")


# req: RND-GST-INCL
def test_round_quotient_is_exact_where_a_float_would_not_be():
    # 2.675 is not representable in binary; 2.675 ÷ 1 to 2 places is half a cent -> 2.68.
    assert money.round_quotient(D("2.675"), D("1"), 2) == D("2.68")
    assert money.round_quotient(D("-2.675"), D("1"), 2) == D("-2.68")
    assert money.round_quotient(D("1"), D("3"), 4) == D("0.3333")
    assert money.round_quotient(D("2"), D("3"), 0) == D("1")
    assert money.round_quotient(D("1"), D("-8"), 2) == D("-0.13")  # -0.125 -> -0.13


_CENTS = st.integers(-10**12, 10**12).map(lambda n: D(n).scaleb(-2))
_PERCENT = st.sampled_from([D("3"), D("4"), D("5"), D("7"), D("8"), D("9"), D("8.5")])


# req: RND-GST-INCL
@given(_CENTS, _PERCENT)
def test_extraction_is_within_half_a_cent_and_mirrors_for_money_out(gross, percent):
    gst = money.rnd_gst_incl(gross, percent, 2)
    exact = gross * percent / (100 + percent)  # the default context, for the check only
    assert abs(gst - exact) <= D("0.005")
    assert money.rnd_gst_incl(-gross, percent, 2) == -gst
    assert gst.as_tuple().exponent == -2


# req: RND-GST-EXCL
@given(st.lists(_CENTS, min_size=1, max_size=12), _PERCENT)
def test_per_line_and_per_subtotal_differ_by_less_than_half_a_cent_a_line(nets, percent):
    lines = [("SR", n) for n in nets]
    subtotal = money.rnd_gst_excl(lines, percent, 2)["SR"]
    per_line = money.rnd_gst_excl(lines, percent, 2, per_line=True)["SR"]
    assert abs(subtotal - per_line) <= D("0.005") * (len(nets) + 1)
    assert money.rnd_gst_excl([("SR", -n) for n in nets], percent, 2)["SR"] == -subtotal
