"""Fixed asset depreciation and disposals, worked by hand (asset_depreciation@1, asset_disposal@1).

Every expected figure is worked out in the comment beside it from PRD-001 Appendix B.4, §10.2,
FR-FA-02, FR-FA-03 and rev 0.149/0.150, never by running the oracle or the engine (QA-ENG-01).
"""

import pytest
from builders import P1_SETTINGS, asset, disposal, lines_of, p1_post


def _months(result: dict, document: str = "FA-1") -> list[tuple[str, str]]:
    """(posting date, 6610 amount) of each depreciation entry of ``document``."""
    return [(e["posting_date"], next(ln["amount"] for ln in e["lines"] if ln["account"] == "6610"))
            for e in result["entries"] if e["document"] == document and e["kind"] == "depreciation"]


# req: FR-FA-02
def test_monthly_depreciation_posts_on_each_months_last_day_up_to_as_of():
    # 3,600.00 over 36 months = 100.00 a month (ALLOC-EVEN), from the purchase month, January. As of 31
    # March: January, February and March, on 31 Jan, 28 Feb and 31 Mar. The register posts nothing itself.
    result, _ = p1_post(asset("FA-1", "2026-01-15", "3600.00", 36), as_of="2026-03-31")
    assert [e["kind"] for e in result["entries"]] == ["depreciation"] * 3
    assert _months(result) == [("2026-01-31", "100.00"), ("2026-02-28", "100.00"), ("2026-03-31", "100.00")]
    assert lines_of(result, "FA-1", "depreciation") == [("1590", "SGD", "-100.00", "-100.00"),
                                                        ("6610", "SGD", "100.00", "100.00")]


# req: ALLOC-EVEN
def test_an_uneven_cost_and_a_residual():
    # 1,000.00 over 3: 333.33, 333.34, 333.33 (cumulative 333.333 -> 333.33, 666.667 -> 666.67, 1,000.00).
    # 1,200.00 less a residual of 200.00 over 10 months: 100.00 a month.
    result, _ = p1_post(asset("FA-1", "2026-01-15", "1000.00", 3),
                        asset("FA-2", "2026-01-15", "1200.00", 10, residual="200.00"), as_of="2026-12-31")
    assert _months(result) == [("2026-01-31", "333.33"), ("2026-02-28", "333.34"), ("2026-03-31", "333.33")]
    assert len(_months(result, "FA-2")) == 10 and {a for _, a in _months(result, "FA-2")} == {"100.00"}


# req: FR-FA-02
def test_depreciation_from_a_later_start_and_from_the_books_start():
    # Depreciating from 1 March: the first month is March. Bought November 2025 with books from 1 March
    # 2026: November to February are in the opening balances, so March (the fifth month) is the first posted.
    settings = {**P1_SETTINGS, "books_start": "2026-03-01"}
    result, _ = p1_post(
        asset("FA-1", "2026-01-15", "1200.00", 12, depreciate_from="2026-03-01"),
        asset("FA-2", "2025-11-10", "1200.00", 12),
        as_of="2026-04-30", settings=settings,
    )
    assert _months(result) == [("2026-03-31", "100.00"), ("2026-04-30", "100.00")]
    assert _months(result, "FA-2") == [("2026-03-31", "100.00"), ("2026-04-30", "100.00")]


# req: FR-FA-02
def test_a_month_whose_last_day_is_after_as_of_has_not_come():
    result, _ = p1_post(asset("FA-1", "2026-01-15", "3600.00", 36), as_of="2026-03-30")
    assert [d for d, _ in _months(result)] == ["2026-01-31", "2026-02-28"]


# req: FR-FA-03
def test_a_sale_with_gst_on_the_proceeds_received():
    # Accumulated by April: January to March, 300.00 (no depreciation in the month of disposal). Sold for
    # 3,270.00 into the bank on SR: GST 3,270 × 9 ÷ 109 = 270.00. Carrying amount 3,300.00 against 3,000.00
    # net: a loss of 300.00 to 7050. Nothing depreciates from April, though as_of is 30 June.
    result, _ = p1_post(asset("FA-1", "2026-01-15", "3600.00", 36),
                        disposal("AD-1", "2026-04-10", "FA-1", "1010", "3270.00", "SR"), as_of="2026-06-30")
    assert lines_of(result, "AD-1") == [
        ("1010", "SGD", "3270.00", "3270.00"),
        ("1500", "SGD", "-3600.00", "-3600.00"),
        ("1590", "SGD", "300.00", "300.00"),
        ("2200", "SGD", "-270.00", "-270.00"),
        ("7050", "SGD", "300.00", "300.00"),
    ]
    assert [d for d, _ in _months(result)] == ["2026-01-31", "2026-02-28", "2026-03-31"]


# req: FR-FA-03
def test_a_sale_invoiced_through_the_disposal_clearing_account():
    # The invoice to the buyer credited 1160 with 3,500.00; the disposal debits it. 3,500.00 against a
    # carrying amount of 3,300.00: a gain of 200.00.
    result, _ = p1_post(asset("FA-1", "2026-01-15", "3600.00", 36), disposal("AD-1", "2026-04-10", "FA-1", "1160", "3500.00"))
    assert lines_of(result, "AD-1") == [
        ("1160", "SGD", "3500.00", "3500.00"),
        ("1500", "SGD", "-3600.00", "-3600.00"),
        ("1590", "SGD", "300.00", "300.00"),
        ("7050", "SGD", "-200.00", "-200.00"),
    ]


# req: FR-FA-03
def test_a_write_off_has_no_proceeds_and_after_its_life_nothing_is_left():
    # FA-1 written off in April: a loss of its carrying amount, 3,300.00. FA-2, 1,000.00 over 3 months
    # from January, written off in June: fully depreciated (1,000.00), so nothing is lost.
    result, _ = p1_post(
        asset("FA-1", "2026-01-15", "3600.00", 36),
        asset("FA-2", "2026-01-15", "1000.00", 3),
        disposal("AD-1", "2026-04-10", "FA-1"),
        disposal("AD-2", "2026-06-10", "FA-2"),
    )
    assert lines_of(result, "AD-1") == [("1500", "SGD", "-3600.00", "-3600.00"), ("1590", "SGD", "300.00", "300.00"),
                                        ("7050", "SGD", "3300.00", "3300.00")]
    assert lines_of(result, "AD-2") == [("1500", "SGD", "-1000.00", "-1000.00"), ("1590", "SGD", "1000.00", "1000.00")]


# req: FR-FA-03
def test_a_disposal_in_the_purchase_month_clears_no_depreciation():
    result, _ = p1_post(asset("FA-1", "2026-01-15", "3600.00", 36), disposal("AD-1", "2026-01-20", "FA-1", "1010", "3600.00"))
    assert lines_of(result, "AD-1") == [("1010", "SGD", "3600.00", "3600.00"), ("1500", "SGD", "-3600.00", "-3600.00")]
    assert _months(result) == []


def _refused(documents, doc_id, reason):
    result, ledger = p1_post(*documents)
    assert doc_id in result["rejected"]
    assert dict(ledger.reasons)[doc_id] == reason


FA = asset("FA-1", "2026-01-15", "3600.00", 36)


# req: QA-ENG-01
@pytest.mark.parametrize(
    ("document", "reason"),
    [
        (asset("FA-9", "2026-01-15", "100.00", 12, account="6010"), "not-an-asset-account"),
        (asset("FA-9", "2026-01-15", "100.00", 12, residual="100.00"), "residual-not-below-cost"),
        (asset("FA-9", "2026-01-15", "100.00", 0), "life-months"),
        (asset("FA-9", "2026-01-15", "-100.00", 12), "wrong-sign"),
        (disposal("AD-9", "2026-04-10", "FA-7"), "not-a-registered-asset"),
        (disposal("AD-9", "2026-01-14", "FA-1"), "disposal-before-asset"),
        (disposal("AD-9", "2026-04-10", "FA-1", "1160", "100.00", "SR"), "gst-on-invoiced-proceeds"),
        (disposal("AD-9", "2026-04-10", "FA-1", "1021", "100.00"), "money-account-currency"),
        (disposal("AD-9", "2026-04-10", "FA-1", "1500", "100.00"), "not-a-money-account"),
    ],
)
def test_an_asset_document_that_breaks_a_rule_is_rejected(document, reason):
    _refused([FA, document], document["id"], reason)


# req: FR-FA-03
def test_an_asset_is_disposed_of_once():
    _refused([FA, disposal("AD-1", "2026-04-10", "FA-1"), disposal("AD-2", "2026-05-10", "FA-1")], "AD-2", "already-disposed")
