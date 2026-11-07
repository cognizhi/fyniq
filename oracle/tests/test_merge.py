"""Account merges (``account_merge@1``; FR-COA-04, Appendix B.8, PRD-001 rev 0.174; test/differential/README.md,
"Account merges"): what an account holds and how it moves, the financial year an income or expense account reads,
a merge of an account holding nothing, each rule that rejects a merge, and the voids, edits and documents that a
merged account refuses afterwards.

Every figure is worked by hand beside it (QA-ENG-01), and so are those of the golden case
``test/differential/golden/account-merge.json`` (the last test).
"""

from datetime import date
from pathlib import Path

import pytest
from builders import P1_ACCOUNTS, P1_SETTINGS, asset, bank_line, disposal, journal, line, p1_scenario, part

from oracle.ledger import Ledger
from oracle.scenario import ScenarioError, load_scenario, scenario_from_json
from oracle.sources.merge import year_start

GOLDEN = Path(__file__).resolve().parents[2] / "test" / "differential" / "golden"

EXTRA_ACCOUNTS = [
    {"code": "1600", "type": "asset", "group": False, "active": True},  # deposits held
    {"code": "1610", "type": "asset", "group": False, "active": True},  # other deposits
    {"code": "1620", "type": "asset", "group": False, "active": True},  # never posted to
    {"code": "1690", "type": "asset", "group": False, "active": False},  # deactivated
    {"code": "1700", "type": "asset", "group": True, "active": True},  # a group
    {"code": "6120", "type": "expense", "group": False, "active": True},
    {"code": "6130", "type": "expense", "group": False, "active": True},
]
DIMENSIONS = [{"name": "Department", "active": True,
               "values": [{"code": "ENG", "active": True}, {"code": "OPS", "active": True}]}]
# The first financial year is short: 1 January to 30 June 2026; the second runs to 30 June 2027 (FR-ORG-02).
SETTINGS = {**P1_SETTINGS, "books_start": "2026-01-01", "first_year_end": "2026-06-30"}


def merge(doc_id: str, day: str, account: str, into: str) -> dict:
    return {"type": "account_merge", "id": doc_id, "version": 1, "date": day, "account": account, "into": into}


def tagged(account: str, currency: str, amount: str, rate: str | None = None, department: str | None = None) -> dict:
    out = line(account, currency, amount, rate)
    if department is not None:
        out["dimensions"] = {"Department": department}
    return out


def data(*documents: dict, reversals: list | None = None, settings: dict | None = None, as_of: str = "2026-12-31",
         accounts: list | None = None) -> dict:
    out = p1_scenario(*documents, as_of=as_of, settings=SETTINGS if settings is None else settings,
                      accounts=P1_ACCOUNTS + EXTRA_ACCOUNTS if accounts is None else accounts)
    out["dimensions"] = DIMENSIONS
    out["reversals"] = reversals or []
    return out


def run(*documents: dict, **options) -> tuple[dict, Ledger]:
    ledger = Ledger(scenario_from_json(data(*documents, **options)))
    return ledger.result(), ledger


def entry(result: dict, document: str, kind: str = "post") -> list[tuple]:
    """The lines of the document's entry of ``kind``: (account, currency, amount, base, dimension set)."""
    for e in result["entries"]:
        if e["document"] == document and e["kind"] == kind:
            return [(ln["account"], ln["currency"], ln["amount"], ln["base_amount"],
                     "|".join(f"{k}={v}" for k, v in sorted(ln.get("dimensions", {}).items())))
                    for ln in e["lines"]]
    raise AssertionError(f"no {kind} entry for {document}")


def posted(result: dict) -> list[tuple[str, str, str]]:
    return [(e["document"], e["kind"], e["posting_date"]) for e in result["entries"]]


# ------------------------------------------------------------------------------------------------ what moves
# req: FR-COA-04
def test_a_balance_sheet_account_moves_each_currency_and_dimension_set_apart():
    r, _ = run(
        journal("JNL-1", "2026-02-10", line("1600", "SGD", "250.00"), line("1010", "SGD", "-250.00")),
        # Engineering: USD 100.00 at 1.35 (135.00) and USD 50.00 at 1.31 (65.50): A = 150.00, B = 200.50.
        journal("JNL-2", "2026-03-05", tagged("1600", "USD", "100.00", "1.35", "ENG"), line("1010", "SGD", "-135.00")),
        journal("JNL-3", "2026-04-07", tagged("1600", "USD", "50.00", "1.31", "ENG"), line("1010", "SGD", "-65.50")),
        # Operations: USD 300.00 in at 1.35 (405.00), USD 290.00 out at 1.40 (-406.00): A = 10.00, B = -1.00.
        journal("JNL-4", "2026-05-04", tagged("1600", "USD", "300.00", "1.35", "OPS"), line("1010", "SGD", "-405.00")),
        journal("JNL-5", "2026-06-08", tagged("1600", "USD", "-290.00", "1.40", "OPS"), line("1010", "SGD", "406.00")),
        merge("AM-1", "2026-09-30", "1600", "1610"),
    )
    assert r["rejected"] == []
    # SGD: 250.00 moves as it is. Engineering: r = RND-RATE(200.50 / 150.00 = 1.33666...) = 1.3366666667, and
    # RND-FX(150.00 x 1.3366666667 = 200.500000005) = 200.50 = B, so no base is left. Operations: A and B differ in
    # sign, so r = 1: USD 10.00 at base 10.00, and the base left d = -1.00 - 10.00 = -11.00 moves in SGD (-d on 1600).
    assert entry(r, "AM-1") == [
        ("1600", "SGD", "-250.00", "-250.00", ""),
        ("1600", "SGD", "11.00", "11.00", "Department=OPS"),
        ("1600", "USD", "-150.00", "-200.50", "Department=ENG"),
        ("1600", "USD", "-10.00", "-10.00", "Department=OPS"),
        ("1610", "SGD", "250.00", "250.00", ""),
        ("1610", "SGD", "-11.00", "-11.00", "Department=OPS"),
        ("1610", "USD", "150.00", "200.50", "Department=ENG"),
        ("1610", "USD", "10.00", "10.00", "Department=OPS"),
    ]
    # 1600 held 250.00 + 200.50 - 1.00 = 449.50 in base; afterwards it holds nothing, set by set.
    assert r["trial_balance"]["2026-09"] == {"1600": "-449.50", "1610": "449.50"}
    assert r["dimension_balances"]["2026-09"]["1600"] == {"": "-250.00", "Department=ENG": "-200.50",
                                                          "Department=OPS": "1.00"}


# req: FR-COA-04, RND-RATE
def test_the_rate_rounded_to_10_decimals_leaves_its_cent_in_the_base_currency():
    r, _ = run(
        # JPY 200,000,000 at 0.009 (1,800,000.00) and JPY 100,000,000 at 0.0091 (910,000.00).
        journal("JNL-1", "2026-07-06", line("1600", "JPY", "200000000", "0.009"), line("1010", "SGD", "-1800000.00")),
        journal("JNL-2", "2026-08-03", line("1600", "JPY", "100000000", "0.0091"), line("1010", "SGD", "-910000.00")),
        merge("AM-1", "2026-10-31", "1600", "1610"),
    )
    # r = RND-RATE(2,710,000.00 / 300,000,000 = 0.00903333...) = 0.0090333333; 300,000,000 x 0.0090333333 =
    # 2,709,999.99, so d = 0.01 moves in SGD.
    assert entry(r, "AM-1") == [
        ("1600", "JPY", "-300000000", "-2709999.99", ""),
        ("1600", "SGD", "-0.01", "-0.01", ""),
        ("1610", "JPY", "300000000", "2709999.99", ""),
        ("1610", "SGD", "0.01", "0.01", ""),
    ]


# req: FR-COA-04
def test_a_foreign_amount_of_nothing_moves_its_base_in_the_base_currency_and_a_holding_of_nothing_moves_nothing():
    r, _ = run(
        # USD 100.00 in at 1.35 (135.00) and out at 1.30 (-130.00): A = 0.00, B = 5.00, all of it d.
        journal("JNL-1", "2026-03-02", line("1600", "USD", "100.00", "1.35"), line("1010", "SGD", "-135.00")),
        journal("JNL-2", "2026-03-20", line("1600", "USD", "-100.00", "1.30"), line("1010", "SGD", "130.00")),
        # USD 40.00 in and out at 1.35, in operations: A = 0, B = 0, nothing to move.
        journal("JNL-3", "2026-04-01", tagged("1600", "USD", "40.00", "1.35", "OPS"), line("1010", "SGD", "-54.00")),
        journal("JNL-4", "2026-04-02", tagged("1600", "USD", "-40.00", "1.35", "OPS"), line("1010", "SGD", "54.00")),
        merge("AM-1", "2026-04-30", "1600", "1610"),
    )
    assert entry(r, "AM-1") == [("1600", "SGD", "-5.00", "-5.00", ""), ("1610", "SGD", "5.00", "5.00", "")]


# req: FR-COA-04, RND-RATE
def test_a_rate_no_stored_rate_can_hold_is_1():
    r, _ = run(
        # JPY 1 at 9,999,999,999 twice, and JPY -1 at 0.0001 (-0.0001, so 0.00) against 7030: A = 1,
        # B = 19,999,999,998.00, and B / A is 10^10 or more: r = 1, JPY 1 at base 1.00, d = 19,999,999,997.00.
        journal("JNL-1", "2026-03-02", line("1600", "JPY", "1", "9999999999"), line("1010", "SGD", "-9999999999.00")),
        journal("JNL-2", "2026-03-03", line("1600", "JPY", "1", "9999999999"), line("1010", "SGD", "-9999999999.00")),
        journal("JNL-3", "2026-03-04", line("1600", "JPY", "-1", "0.0001"), line("7030", "JPY", "1", "0.0001")),
        # USD 1,000,000,000.00 at 0.0000000001 (0.10) and USD -1.00 at 0.06 (-0.06): A = 999,999,999.00, B = 0.04;
        # B / A = 0.00000000004 is 0 once rounded to 10 decimals: r = 1, d = 0.04 - 999,999,999.00.
        journal("JNL-4", "2026-03-05", line("1610", "USD", "1000000000.00", "0.0000000001"), line("1010", "SGD", "-0.10")),
        journal("JNL-5", "2026-03-06", line("1610", "USD", "-1.00", "0.06"), line("1010", "SGD", "0.06")),
        merge("AM-1", "2026-03-31", "1600", "1620"),
        merge("AM-2", "2026-03-31", "1610", "1620"),
    )
    assert r["rejected"] == []
    assert entry(r, "AM-1") == [
        ("1600", "JPY", "-1", "-1.00", ""),
        ("1600", "SGD", "-19999999997.00", "-19999999997.00", ""),
        ("1620", "JPY", "1", "1.00", ""),
        ("1620", "SGD", "19999999997.00", "19999999997.00", ""),
    ]
    assert entry(r, "AM-2") == [
        ("1610", "SGD", "999999998.96", "999999998.96", ""),
        ("1610", "USD", "-999999999.00", "-999999999.00", ""),
        ("1620", "SGD", "-999999998.96", "-999999998.96", ""),
        ("1620", "USD", "999999999.00", "999999999.00", ""),
    ]


# req: FR-COA-04, FR-ORG-02
def test_an_expense_account_moves_its_movement_in_the_financial_year_that_holds_the_date():
    r, _ = run(
        # The first year ends on 30 June: these two are in retained earnings and stay where they are.
        journal("JNL-1", "2026-05-15", line("6120", "SGD", "300.00"), line("1010", "SGD", "-300.00")),
        journal("JNL-2", "2026-06-30", line("6120", "SGD", "25.00"), line("1010", "SGD", "-25.00")),
        # The second year's first day.
        journal("JNL-3", "2026-07-01", tagged("6120", "SGD", "40.00", department="ENG"), line("1010", "SGD", "-40.00")),
        # 300.00 spread over three months: 100.00 released on 10 July, 1 August and 1 September.
        bank_line("BT-1", "2026-07-10", "1010", "SGD", "-300.00", part("6120", "-300.00", "OP", spread_months=3)),
        # An accrual and its automatic reversal on 1 October, both before the merge: they net to nothing.
        {**journal("JNL-4", "2026-09-30", line("6120", "SGD", "200.00"), line("2999", "SGD", "-200.00")),
         "reverse_on": "2026-10-01"},
        # A revenue account's credit moves too.
        journal("JNL-5", "2026-08-01", line("1010", "SGD", "500.00"), line("4020", "SGD", "-500.00")),
        merge("AM-1", "2026-10-31", "6120", "6110"),
        merge("AM-2", "2026-10-31", "4020", "4010"),
    )
    assert r["rejected"] == []
    # 6120 in the year from 1 July: 300.00 released + 200.00 - 200.00 = 300.00 with no value, 40.00 for engineering.
    assert entry(r, "AM-1") == [
        ("6110", "SGD", "300.00", "300.00", ""),
        ("6110", "SGD", "40.00", "40.00", "Department=ENG"),
        ("6120", "SGD", "-300.00", "-300.00", ""),
        ("6120", "SGD", "-40.00", "-40.00", "Department=ENG"),
    ]
    assert entry(r, "AM-2") == [("4010", "SGD", "-500.00", "-500.00", ""), ("4020", "SGD", "500.00", "500.00", "")]
    # 6120 keeps the first year's 325.00; its second year nets to nothing.
    assert r["trial_balance"]["2026-05"]["6120"] == "300.00"
    assert r["trial_balance"]["2026-06"]["6120"] == "25.00"
    assert r["trial_balance"]["2026-10"]["6120"] == "-540.00"  # the merge's -340.00 and the reversal's -200.00


# req: FR-COA-04, FR-ORG-02
def test_releases_dated_before_a_later_years_merge_move_from_that_years_start():
    settings = {**P1_SETTINGS, "books_start": "2026-01-01", "first_year_end": "2026-12-31"}
    r, _ = run(
        # 600.00 over six months from September: 100.00 on 15 Sep, 1 Oct, 1 Nov, 1 Dec, 1 Jan and 1 Feb.
        bank_line("BT-1", "2026-09-15", "1010", "SGD", "-600.00", part("6120", "-600.00", "OP", spread_months=6)),
        merge("AM-1", "2027-02-01", "6120", "6110"),
        settings=settings, as_of="2027-03-31",
    )
    # The year from 1 January 2027 holds January's and February's releases: 200.00.
    assert entry(r, "AM-1") == [("6110", "SGD", "200.00", "200.00", ""), ("6120", "SGD", "-200.00", "-200.00", "")]


# req: FR-ORG-02
@pytest.mark.parametrize(
    ("books_start", "first_year_end", "day", "start"),
    [
        ("2026-01-01", "2026-06-30", "2026-01-01", "2026-01-01"),  # a short first year
        ("2026-01-01", "2026-06-30", "2026-06-30", "2026-01-01"),
        ("2026-01-01", "2026-06-30", "2026-07-01", "2026-07-01"),
        ("2026-01-01", "2026-06-30", "2027-06-30", "2026-07-01"),
        ("2026-01-01", "2026-06-30", "2027-07-01", "2027-07-01"),
        ("2026-03-15", "2027-06-30", "2027-05-01", "2026-03-15"),  # a long first year
        ("2026-03-15", "2027-06-30", "2028-01-01", "2027-07-01"),
        # Year ends on the last day of February: 1 Mar 2027 to 29 Feb 2028 (one year later, 1 Mar 2028, less a day).
        ("2026-03-01", "2027-02-28", "2028-02-29", "2027-03-01"),
        ("2026-03-01", "2027-02-28", "2029-02-28", "2028-03-01"),
        ("2026-03-01", "2027-02-28", "2029-03-01", "2029-03-01"),
        # A year starting on 29 Feb 2028: one year later is read as 1 Mar 2029, so it ends on 28 Feb 2029.
        ("2027-03-01", "2028-02-28", "2029-02-28", "2028-02-29"),
        ("2027-03-01", "2028-02-28", "2029-03-01", "2029-03-01"),
    ],
)
def test_the_financial_year_that_holds_a_date(books_start, first_year_end, day, start):
    assert year_start(date.fromisoformat(books_start), date.fromisoformat(first_year_end),
                      date.fromisoformat(day)) == date.fromisoformat(start)


# ------------------------------------------------------------------------------------------------ holding nothing
# req: FR-COA-04
def test_an_account_holding_nothing_posts_no_entry_and_is_inactive_from_then_on():
    r, ledger = run(
        journal("JNL-1", "2026-03-03", line("6130", "SGD", "80.00"), line("1010", "SGD", "-80.00")),  # the first year
        merge("AM-1", "2026-10-31", "6130", "6110"),  # holds nothing in the year from 1 July
        merge("AM-2", "2026-10-31", "1620", "1610"),  # never posted to
        journal("JNL-2", "2026-11-05", line("6130", "SGD", "10.00"), line("1010", "SGD", "-10.00")),
        bank_line("BT-1", "2026-11-10", "1010", "SGD", "-50.00", part("6130", "-50.00", "OP")),
        journal("JNL-3", "2026-11-11", line("1620", "SGD", "10.00"), line("1010", "SGD", "-10.00")),
        merge("AM-3", "2026-11-30", "6130", "6110"),  # merged before: inactive
        journal("JNL-4", "2026-11-12", line("6110", "SGD", "10.00"), line("1010", "SGD", "-10.00")),  # into: active
    )
    assert posted(r) == [("JNL-1", "post", "2026-03-03"), ("JNL-4", "post", "2026-11-12")]
    assert ledger.reasons == [("JNL-2", "inactive-account"), ("BT-1", "inactive-account"),
                              ("JNL-3", "inactive-account"), ("AM-3", "inactive-account")]


# ------------------------------------------------------------------------------------------------ voids and edits
# req: FR-COA-04, FR-GL-12
def test_a_void_or_an_edit_of_a_journal_with_a_line_on_a_merged_account_is_refused_whatever_its_date():
    r, ledger = run(
        journal("JNL-1", "2026-02-10", line("1600", "SGD", "1000.00"), line("1010", "SGD", "-1000.00")),
        journal("JNL-2", "2026-03-01", line("1610", "SGD", "200.00"), line("1010", "SGD", "-200.00")),
        journal("JNL-3", "2026-03-02", line("6110", "SGD", "50.00"), line("1010", "SGD", "-50.00")),
        merge("AM-1", "2026-09-30", "1600", "1610"),
        reversals=[
            {"document": "JNL-1", "date": "2026-03-01"},  # dated before the merge, made after it
            {"document": "JNL-2", "date": "2026-10-05"},  # on the account merged into: posts
            {"document": "JNL-1", "edit": {"version": 2, "date": "2026-02-10", "memo": "",
                                           "lines": [line("6110", "SGD", "1000.00"), line("1010", "SGD", "-1000.00")]}},
            {"document": "JNL-3", "edit": {"version": 2, "date": "2026-03-02", "memo": "",
                                           "lines": [line("1600", "SGD", "50.00"), line("1010", "SGD", "-50.00")]}},
        ],
    )
    # Reversals and edits come after every document, so the merge moves JNL-1's 1,000.00 and JNL-2's is untouched.
    assert entry(r, "AM-1") == [("1600", "SGD", "-1000.00", "-1000.00", ""), ("1610", "SGD", "1000.00", "1000.00", "")]
    assert entry(r, "JNL-2", "reversal") == [("1610", "SGD", "-200.00", "-200.00", ""),
                                             ("1010", "SGD", "200.00", "200.00", "")]
    assert ledger.reasons == [("JNL-1/reversal", "line-on-merged-account"),  # its entry has a line on 1600
                              ("JNL-1/edit", "line-on-merged-account"),  # its reversal would post on 1600
                              ("JNL-3/edit", "inactive-account")]  # its new version has a line on 1600


# req: FR-COA-04, FR-GL-06
def test_an_edit_of_the_opening_balances_with_a_line_on_a_merged_account_is_refused():
    opening = {"type": "opening_balances", "id": "OB", "version": 1, "date": "2026-01-01", "memo": "",
               "lines": [line("1600", "SGD", "1000.00"), line("3010", "SGD", "-1000.00")]}
    r, ledger = run(
        opening,
        merge("AM-1", "2026-01-31", "1600", "1610"),
        reversals=[{"document": "OB", "edit": {"version": 2, "date": "2026-01-01", "memo": "",
                                               "lines": [line("1610", "SGD", "1000.00"), line("3010", "SGD", "-1000.00")]}}],
    )
    assert entry(r, "AM-1") == [("1600", "SGD", "-1000.00", "-1000.00", ""), ("1610", "SGD", "1000.00", "1000.00", "")]
    assert ledger.reasons == [("OB/edit", "line-on-merged-account")]


# ------------------------------------------------------------------------------------------------ rejections
# req: FR-COA-04
@pytest.mark.parametrize(
    ("account", "into", "reason"),
    [
        ("1600", "1600", "same-account"),
        ("1601", "1610", "unknown-account"),
        ("1600", "1601", "unknown-account"),
        ("1700", "1610", "group-account"),
        ("1600", "1700", "group-account"),
        ("1690", "1610", "inactive-account"),
        ("1600", "1690", "inactive-account"),
        ("3010", "3300", "account-with-role"),  # share capital
        ("2999", "2500", "account-with-role"),  # the director's loan
        ("1210", "1610", "system-account"),  # prepayments
        ("1600", "1100", "system-account"),  # the receivable
        ("1011", "1610", "money-account"),
        ("1600", "1011", "money-account"),
        ("1600", "2999", "types-differ"),  # rule 2: an asset into a liability
        ("6120", "4010", "types-differ"),  # an expense into a revenue account
    ],
)
def test_a_merge_of_accounts_it_cannot_join_is_refused(account, into, reason):
    r, ledger = run(journal("JNL-1", "2026-02-10", line("1600", "SGD", "100.00"), line("1010", "SGD", "-100.00")),
                    merge("AM-1", "2026-10-31", account, into))
    assert r["rejected"] == ["AM-1"]
    assert ledger.reasons == [("AM-1", reason)]


# req: FR-COA-04, FR-GL-04
@pytest.mark.parametrize(("account", "into"), [("1640", "1610"), ("1610", "1640"), ("1640", "1650")])
def test_a_merge_of_a_revalued_account_is_refused(account, into):
    # The format leaves merges of revalued accounts out for now, into another revalued one included (README).
    revalued = [{"code": code, "type": "asset", "group": False, "active": True, "revalue": True} for code in ("1640", "1650")]
    settings = {**SETTINGS, "accounts": {**SETTINGS["accounts"], "unrealised_fx": "7040"}}
    _, ledger = run(merge("AM-1", "2026-10-31", account, into), settings=settings,
                    accounts=P1_ACCOUNTS + EXTRA_ACCOUNTS + revalued
                    + [{"code": "7040", "type": "expense", "group": False, "active": True}])
    assert ledger.reasons == [("AM-1", "revalued-account")]


# req: FR-COA-04
def test_an_account_merged_before_is_inactive_on_either_side():
    _, ledger = run(merge("AM-1", "2026-10-31", "1620", "1610"),
                    merge("AM-2", "2026-10-31", "1620", "1600"),
                    merge("AM-3", "2026-10-31", "1600", "1620"))
    assert ledger.reasons == [("AM-2", "inactive-account"), ("AM-3", "inactive-account")]


# req: FR-COA-04
def test_a_merge_dated_before_the_books_start_is_refused_and_after_as_of_is_not_a_scenario():
    _, ledger = run(merge("AM-1", "2025-12-31", "1600", "1610"))
    assert ledger.reasons == [("AM-1", "before-books-start")]
    with pytest.raises(ScenarioError, match="as_of"):  # every date of a scenario is on or before as_of
        scenario_from_json(data(merge("AM-1", "2027-01-01", "1600", "1610")))


# req: FR-COA-04
def test_a_merge_dated_before_a_posted_line_on_the_account_is_refused():
    r, ledger = run(journal("JNL-1", "2026-10-15", line("1600", "SGD", "500.00"), line("1010", "SGD", "-500.00")),
                    merge("AM-1", "2026-09-30", "1600", "1610"),
                    merge("AM-2", "2026-10-15", "1600", "1610"))  # on the day of its last line: posts
    assert ledger.reasons == [("AM-1", "posted-after-merge-date")]
    assert entry(r, "AM-2") == [("1600", "SGD", "-500.00", "-500.00", ""), ("1610", "SGD", "500.00", "500.00", "")]


# req: FR-COA-04
def test_a_merge_waits_while_a_spread_still_releases_to_the_account():
    # 600.00 over six months from 15 September: 1 November's release, and those after as_of, are still to come.
    _, ledger = run(bank_line("BT-1", "2026-09-15", "1010", "SGD", "-600.00",
                              part("6120", "-600.00", "OP", spread_months=6)),
                    merge("AM-1", "2026-10-31", "6120", "6110"))
    assert ledger.reasons == [("AM-1", "scheduled-after-merge-date")]


# req: FR-COA-04, FR-GL-02
@pytest.mark.parametrize(("day", "reverse_on", "as_of"), [("2026-10-31", "2026-11-01", "2026-12-31"),
                                                           ("2026-12-31", "2027-01-01", "2026-12-31")])  # after as_of
def test_a_merge_waits_while_a_journal_still_reverses_a_line_on_the_account(day, reverse_on, as_of):
    accrual = {**journal("JNL-1", day, line("6120", "SGD", "120.00"), line("2999", "SGD", "-120.00")),
               "reverse_on": reverse_on}
    _, ledger = run(accrual, merge("AM-1", day, "6120", "6110"), as_of=as_of)
    assert ledger.reasons == [("AM-1", "auto-reversal-after-merge-date")]


# req: FR-COA-04, FR-FA-03
def test_a_merge_waits_while_a_fixed_asset_has_its_cost_on_the_account():
    cost = journal("JNL-1", "2026-01-15", line("1600", "SGD", "1200.00"), line("1010", "SGD", "-1200.00"))
    laptop = asset("FA-1", "2026-01-15", "1200.00", 12, account="1600")
    # Not disposed of: refused. Disposed of after the merge's date: refused for the disposal's line (rule 4), and for
    # a disposal later in the scenario: refused for the asset.
    _, ledger = run(cost, laptop, merge("AM-1", "2026-06-30", "1600", "1610"))
    assert ledger.reasons == [("AM-1", "asset-not-disposed")]
    _, ledger = run(cost, laptop, disposal("AD-1", "2026-07-10", "FA-1"), merge("AM-1", "2026-06-30", "1600", "1610"))
    assert ledger.reasons == [("AM-1", "posted-after-merge-date")]
    _, ledger = run(cost, laptop, merge("AM-1", "2026-06-30", "1600", "1610"), disposal("AD-1", "2026-05-20", "FA-1"))
    assert ledger.reasons == [("AM-1", "asset-not-disposed")]
    # Disposed of on 20 May: 1600 took 1,200.00 and gave it back, so it holds nothing and posts no entry.
    r, ledger = run(cost, laptop, disposal("AD-1", "2026-05-20", "FA-1"), merge("AM-1", "2026-06-30", "1600", "1610"))
    assert ledger.reasons == []
    assert "AM-1" not in [e["document"] for e in r["entries"]]


# req: FR-COA-04, FR-ORG-02
def test_a_merge_of_an_income_or_expense_account_needs_the_financial_years():
    for missing in ("books_start", "first_year_end"):
        settings = {k: v for k, v in SETTINGS.items() if k != missing}
        for account, into in (("6120", "6110"), ("4020", "4010")):
            with pytest.raises(ScenarioError, match="first_year_end"):
                scenario_from_json(data(merge("AM-1", "2026-10-31", account, into), settings=settings))
    # A balance-sheet account's merge reads its whole balance, so it needs neither.
    r, _ = run(journal("JNL-1", "2026-02-10", line("1600", "SGD", "100.00"), line("1010", "SGD", "-100.00")),
               merge("AM-1", "2026-10-31", "1600", "1610"), settings=P1_SETTINGS)
    assert entry(r, "AM-1") == [("1600", "SGD", "-100.00", "-100.00", ""), ("1610", "SGD", "100.00", "100.00", "")]


# req: FR-COA-04
def test_first_year_end_must_be_a_calendar_date():
    with pytest.raises(ScenarioError, match="calendar date"):
        scenario_from_json(data(settings={**SETTINGS, "first_year_end": "2026-02-30"}))


# ------------------------------------------------------------------------------------------------ the golden case
def _golden() -> tuple[dict, Ledger]:
    ledger = Ledger(load_scenario(GOLDEN / "account-merge.json"))
    return ledger.result(), ledger


# req: QA-ENG-02, FR-COA-04
def test_golden_account_merge():
    r, ledger = _golden()
    assert posted(r) == [
        ("JNL-0001", "post", "2026-02-10"), ("JNL-0002", "post", "2026-03-05"), ("JNL-0003", "post", "2026-04-07"),
        ("JNL-0004", "post", "2026-05-04"), ("JNL-0005", "post", "2026-06-08"), ("JNL-0006", "post", "2026-07-06"),
        ("JNL-0007", "post", "2026-08-03"), ("JNL-0008", "post", "2026-03-03"), ("JNL-0009", "post", "2026-05-15"),
        ("BT-0001", "post", "2026-07-10"), ("JNL-0010", "post", "2026-08-20"), ("JNL-0011", "post", "2026-09-30"),
        ("JNL-0012", "post", "2026-10-15"), ("BT-0002", "post", "2026-09-15"), ("JNL-0013", "post", "2026-10-31"),
        # FA-0001 posts nothing; AM-0003 moves 6130, which holds nothing in the year from 1 July.
        ("AM-0001", "post", "2026-10-31"), ("AM-0002", "post", "2026-10-31"),
        ("JNL-0015", "post", "2026-11-12"),
        ("JNL-0012", "reversal", "2026-11-16"),  # 1620 was never merged: its void posts
        ("BT-0001", "release", "2026-07-10"), ("BT-0001", "release", "2026-08-01"),
        ("BT-0001", "release", "2026-09-01"), ("BT-0002", "release", "2026-09-15"),
        ("JNL-0011", "auto_reversal", "2026-10-01"), ("BT-0002", "release", "2026-10-01"),
        ("BT-0002", "release", "2026-11-01"), ("JNL-0013", "auto_reversal", "2026-11-01"),
        ("BT-0002", "release", "2026-12-01"),  # January's and February's are after as_of
    ]
    # 1600 holds, on 31 October: SGD 1,000.00; USD 150.00 at 200.50 for engineering (135.00 + 65.50); USD 10.00 at
    # -1.00 for operations (405.00 - 406.00); JPY 300,000,000 at 2,710,000.00 (1,800,000.00 + 910,000.00).
    # Engineering: r = RND-RATE(200.50 / 150.00) = 1.3366666667, RND-FX(150.00 x r = 200.500000005) = 200.50, none
    # left. Operations: A and B differ in sign, r = 1: USD 10.00 at 10.00, d = -1.00 - 10.00 = -11.00 (+11.00 on
    # 1600 in SGD). JPY: r = RND-RATE(2,710,000.00 / 300,000,000) = 0.0090333333, RND-FX(300,000,000 x r) =
    # 2,709,999.99, d = 0.01, which joins the SGD 1,000.00 with no value in canonical form: -1,000.01.
    assert entry(r, "AM-0001") == [
        ("1600", "JPY", "-300000000", "-2709999.99", ""),
        ("1600", "SGD", "-1000.01", "-1000.01", ""),
        ("1600", "SGD", "11.00", "11.00", "Department=OPS"),
        ("1600", "USD", "-150.00", "-200.50", "Department=ENG"),
        ("1600", "USD", "-10.00", "-10.00", "Department=OPS"),
        ("1610", "JPY", "300000000", "2709999.99", ""),
        ("1610", "SGD", "1000.01", "1000.01", ""),
        ("1610", "SGD", "-11.00", "-11.00", "Department=OPS"),
        ("1610", "USD", "150.00", "200.50", "Department=ENG"),
        ("1610", "USD", "10.00", "10.00", "Department=OPS"),
    ]
    # 6120 in the year from 1 July 2026 (the first ends on 30 June): 100.00 released on 10 July, 1 August and
    # 1 September, JNL-0011's 200.00 and its automatic reversal on 1 October, -200.00: 300.00 with no value; JNL-0010's
    # 450.00 for engineering. JNL-0009's 300.00 of 15 May is in the first year and stays.
    assert entry(r, "AM-0002") == [
        ("6100", "SGD", "300.00", "300.00", ""),
        ("6100", "SGD", "450.00", "450.00", "Department=ENG"),
        ("6120", "SGD", "-300.00", "-300.00", ""),
        ("6120", "SGD", "-450.00", "-450.00", "Department=ENG"),
    ]
    assert entry(r, "JNL-0012", "reversal") == [("1620", "SGD", "-500.00", "-500.00", ""),
                                                ("1010", "SGD", "500.00", "500.00", "")]
    assert r["rejected"] == ["AM-0004", "AM-0005", "AM-0006", "AM-0007", "AM-0008", "AM-0009", "AM-0010", "AM-0011",
                             "AM-0012", "AM-0013", "AM-0014", "JNL-0014", "BT-0003", "JNL-0001/reversal", "JNL-0008/edit",
                             "JNL-0015/edit"]
    assert ledger.reasons == [
        ("AM-0004", "same-account"),
        ("AM-0005", "inactive-account"),  # 1600, merged by AM-0001
        ("AM-0006", "money-account"),
        ("AM-0007", "system-account"),  # prepayments
        ("AM-0008", "types-differ"),  # an asset into a liability
        ("AM-0009", "before-books-start"),
        ("AM-0010", "posted-after-merge-date"),  # JNL-0012's 500.00 on 1620 on 15 October
        ("AM-0011", "scheduled-after-merge-date"),  # BT-0002 releases to 6140 on 1 November
        ("AM-0012", "auto-reversal-after-merge-date"),  # JNL-0013 reverses its 6150 line on 1 November
        ("AM-0013", "asset-not-disposed"),  # FA-0001's cost is on 1500
        ("AM-0014", "revalued-account"),  # 1630 is marked revalue
        ("JNL-0014", "inactive-account"),  # 1600
        ("BT-0003", "inactive-account"),  # 6130, merged holding nothing
        ("JNL-0001/reversal", "line-on-merged-account"),
        ("JNL-0008/edit", "line-on-merged-account"),  # its entry is on 6130
        ("JNL-0015/edit", "inactive-account"),  # its new version is on 6120
    ]
    assert r["trial_balance"] == {
        "2026-02": {"1010": "-1000.00", "1600": "1000.00"},
        "2026-03": {"1010": "-215.00", "1600": "135.00", "6130": "80.00"},  # -135.00 - 80.00
        "2026-04": {"1010": "-65.50", "1600": "65.50"},
        "2026-05": {"1010": "-705.00", "1600": "405.00", "6120": "300.00"},  # -405.00 - 300.00
        "2026-06": {"1010": "406.00", "1600": "-406.00"},
        # BT-0001: 300.00 to 1210, 100.00 of it released on 10 July.
        "2026-07": {"1010": "-300.00", "1011": "-1800000.00", "1210": "200.00", "1600": "1800000.00", "6120": "100.00"},
        "2026-08": {"1010": "-450.00", "1011": "-910000.00", "1210": "-100.00", "1600": "910000.00",
                    "6120": "550.00"},  # 450.00 + the release of 1 August
        # BT-0002: 600.00 to 1210, 100.00 released to 6140 on 15 September; BT-0001's last 100.00 to 6120.
        "2026-09": {"1010": "-600.00", "1210": "400.00", "2999": "-200.00", "6120": "300.00", "6140": "100.00"},
        # 1600: -(1,000.01 + 2,709,999.99 - 11.00 + 200.50 + 10.00) = -2,711,199.50, all it ever held; 6120:
        # -750.00 merged and -200.00 reversed; 2999: -120.00 + 200.00.
        "2026-10": {"1010": "-500.00", "1210": "-100.00", "1600": "-2711199.50", "1610": "2711199.50",
                    "1620": "500.00", "2999": "80.00", "6100": "750.00", "6120": "-950.00", "6140": "100.00",
                    "6150": "120.00"},
        "2026-11": {"1010": "460.00", "1210": "-100.00", "1620": "-500.00", "2999": "120.00", "6100": "40.00",
                    "6140": "100.00", "6150": "-120.00"},  # 1010: -40.00 + 500.00
        "2026-12": {"1210": "-100.00", "6140": "100.00"},
    }
    # Each set of values moves apart: with no value 1,000.00 + 2,710,000.00 (the JPY and its cent); engineering
    # 200.50; operations -1.00.
    october = r["dimension_balances"]["2026-10"]
    assert october["1600"] == {"": "-2711000.00", "Department=ENG": "-200.50", "Department=OPS": "1.00"}
    assert october["1610"] == {"": "2711000.00", "Department=ENG": "200.50", "Department=OPS": "-1.00"}
    assert october["6100"] == {"": "300.00", "Department=ENG": "450.00"}
    assert october["6120"] == {"": "-500.00", "Department=ENG": "-450.00"}

