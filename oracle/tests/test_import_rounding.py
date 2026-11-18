"""RND-IMPORT (PRD-001 §10.4, rev 0.255), worked by hand.

The workbook import reads the figures the workbook calculated from their saved decimal text and rounds each once to the
currency's minor units, half away from zero (p1-workbook-migration design D2). Every expected figure is worked out in
the comment beside it, never by running the oracle or the engine (QA-ENG-01).
"""

from decimal import Decimal as D

import pytest

from oracle import money


# req: RND-IMPORT
@pytest.mark.parametrize(
    "saved, minor, want",
    [
        ("13080.000006", 2, D("13080.00")),  # a binary artefact above the cent: the cent it stands for
        ("0.009999", 2, D("0.01")),  # just below a cent: up to it
        ("648.012461778111", 2, D("648.01")),  # a timesheet's cost: 648.0124… -> 648.01
        ("-180.00499999", 2, D("-180.00")),  # below the half: toward zero
        ("-180.005", 2, D("-180.01")),  # the half, negative: away from zero
        ("2.675", 2, D("2.68")),  # the half, positive: away from zero (in binary it would read 2.67499…)
        ("-0.0049", 2, D("0.00")),  # rounds to zero: shown as 0, never -0
        ("3000", 2, D("3000.00")),
        ("1234.5", 0, D("1235")),  # JPY to the yen
        ("1.0049999999999999", 2, D("1.01")),  # 15 significant digits first: 1.00499999999999|99 -> 1.005, then 1.01
        ("98.09999999999999", 2, D("98.10")),  # 98.0999999999999|9 -> 98.1, then 98.10
    ],
)
def test_a_calculated_figure_reads_as_its_cents(saved, minor, want):
    got = money.rnd_import(saved, minor)
    assert got == want and str(got) == str(want)


# req: RND-IMPORT
def test_a_saved_value_is_decimal_text():
    with pytest.raises(money.FormatError):
        money.rnd_import("1.3E+4", 2)


# req: RND-IMPORT
@pytest.mark.parametrize(
    "saved, want",
    [
        ("98.09999999999999", "98.1"),  # 98.0999999999999|9 rounds up at the 15th digit: the 98.1 a person sees
        ("0.30000000000000004", "0.3"),  # 0.300000000000000|04: the 16th and 17th digits go
        ("1234.5678", "1234.5678"),  # within 15 digits: as typed, never rounded to cents
        ("300", "300"),
        ("-98.09999999999999", "-98.1"),  # the same, negative: half away from zero
        ("1.23456789012345678", "1.23456789012346"),  # 1.23456789012345|678: the 15th digit 5 rounds up to 6
    ],
)
def test_a_saved_number_reads_to_15_significant_digits(saved, want):
    assert str(money.saved_number(saved)) == want
