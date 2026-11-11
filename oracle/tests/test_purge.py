"""The oracle's purge (AUD-10, rev 0.238; p1-audit-explorer design D7b): figures worked by hand, refusals, properties."""

from __future__ import annotations

import copy
import json
from decimal import Decimal
from pathlib import Path

import pytest
from hypothesis import HealthCheck, given, settings

from oracle.purge import PurgeError, purge
from oracle.strategies_purge import generate_purges, purges

GOLDEN = Path(__file__).resolve().parents[2] / "test" / "differential" / "purges"


def ledger(name: str) -> dict:
    return json.loads((GOLDEN / f"{name}.json").read_text(encoding="utf-8"))


def shown(result: dict) -> list[str]:
    """Each carry-forward entry as text: its source, then each line as account@set currency amount rate base."""
    return [f"{e['source']['type']} {e['source']['id']}:" + "".join(
        f" {x['account']}@{x['dimension_set']} {x['currency']} {x['amount']} {x['rate']} {x['base_amount']}" for x in e["lines"])
        for e in result["entries"]]


def test_a_year_carried_forward_by_hand() -> None:  # req: AUD-10, INV-01, INV-07
    # FY2018 (SGD books, January to December): INV-1 owes 1,090.00 (1,000.00 of "sales" revenue, 90.00 of GST) and
    # PAY-1 pays it; INV-2 owes 545.00 ("eng" revenue 500.00, GST 45.00); BILL-1 is 300.00 of expense owed; INV-3 owes
    # USD 100.00 at 1.35; SHR-1 puts USD 50.00 (66.67) in the USD bank for shares; LOAN-1 is a director's loan of
    # 200.00. SHR-2, a share issue the register names, is kept; so is J-9's expense of 50.00, as its reversal of
    # 15 January 2019 is after the boundary. The other seven entries go.
    r = purge(ledger("hand-worked-year"))
    assert (r["kept"], r["purged"], r["unchanged"]) == (["E8", "E9"], 7, True)
    # Boundary: bank 1,090.00 + 200.00; the USD bank at 66.67 ÷ 50.00 = 1.3334; GST −90.00 − 45.00; share capital
    # −66.67; retained earnings 300.00 − 135.00 = 165.00, "eng" −500.00, "sales" −1,000.00; suspense takes back the
    # documents' 300.00 + 1,090.00 − 1,090.00 − 545.00 − 135.00 + 200.00 = −180.00, so 180.00.
    assert shown(r) == [
        "ledger_boundary 2019-01-01: 1010@ SGD 1290.00 1 1290.00 1020@ USD 50.00 1.3334 66.67 2200@ SGD -135.00 1 -135.00"
        " 3010@ SGD -66.67 1 -66.67 3100@ SGD 165.00 1 165.00 3100@dept=eng SGD -500.00 1 -500.00"
        " 3100@dept=sales SGD -1000.00 1 -1000.00 2999@ SGD 180.00 1 180.00",
        "bill BILL-1: 2010@ SGD -300.00 1 -300.00 2999@ SGD 300.00 1 300.00",
        "customer_receipt PAY-1: 1100@ SGD -1090.00 1 -1090.00 2999@ SGD 1090.00 1 1090.00",
        "invoice INV-1: 1100@ SGD 1090.00 1 1090.00 2999@ SGD -1090.00 1 -1090.00",
        "invoice INV-2: 1100@ SGD 545.00 1 545.00 2999@ SGD -545.00 1 -545.00",
        "invoice INV-3: 1100@ USD 100.00 1.35 135.00 2999@ SGD -135.00 1 -135.00",
        "journal LOAN-1: 2500@ SGD -200.00 1 -200.00 2999@ SGD 200.00 1 200.00",
    ]
    # On 1 February 2019: the bank 1,290.00 + 1,000.00 − 50.00 + 50.00; retained earnings 165.00 + J-9's 50.00 of last
    # year's expense; this year's −50.00 of expense from its reversal; each open item on receivables by its document.
    last = {(b["account"], b["dimension_set"], b["currency"], b["source"] and b["source"]["id"]): (b["amount"], b["base_amount"])
            for b in r["positions"][-1]["balances"]}
    assert [p["date"] for p in r["positions"]] == ["2019-01-01", "2019-01-15", "2019-02-01"]
    assert last[("1010", "", "SGD", None)] == ("2290.00", "2290.00")
    assert last[("3100", "", "SGD", None)] == ("215.00", "215.00")
    assert last[("6010", "", "SGD", None)] == ("-50.00", "-50.00")
    assert last[("1100", "", "USD", "INV-3")] == ("100.00", "135.00")
    assert last[("1100", "", "SGD", "INV-4")] == ("218.00", "218.00")
    assert last[("2200", "", "SGD", None)] == ("-153.00", "-153.00")
    assert ("2999", "", "SGD", None) not in last  # suspense nets to zero across the carry-forward


def test_foreign_residuals_by_hand() -> None:  # req: AUD-10, INV-05
    # USD 999,999,999.99 worth 1,234,567,895.12 (J2's revaluation of 5.00 included): 1.2345678951 gives
    # 1,234,567,895.09, and 0.03 is base-only. USD 0.01 worth 100,000,000.00 would need the rate 10,000,000,000, of 11
    # integer digits, so it carries at 1 with 99,999,999.99 base-only; USD 5.00 worth −1.00 at 1 with −6.00 base-only.
    # JPY 5,000 worth 43.21 is an open item of J5, at 0.008642 exactly.
    r = purge(ledger("foreign-residuals"))
    assert (r["kept"], r["purged"], r["unchanged"]) == ([], 5, True)
    assert shown(r) == [
        "ledger_boundary 2021-04-01: 1020@ USD 999999999.99 1.2345678951 1234567895.09 1020@ USD 0.00 1 0.03"
        " 1020@dept=eng USD 0.01 1 0.01 1020@dept=eng USD 0.00 1 99999999.99"
        " 1020@dept=sales USD 5.00 1 5.00 1020@dept=sales USD 0.00 1 -6.00"
        " 3010@ SGD -1234567895.12 1 -1234567895.12 3100@ SGD -99999999.00 1 -99999999.00"
        " 3100@dept=eng SGD -43.21 1 -43.21 2999@ SGD 43.21 1 43.21",
        "journal J5: 1100@dept=eng JPY 5000 0.008642 43.21 2999@ SGD -43.21 1 -43.21",
    ]


def test_reversals_keep_each_other() -> None:  # req: AUD-10
    # E9 is reversed by E10 after the boundary, so both stay; reverse E8 (named) by a new E12 before the boundary, and
    # reverse E12 by E13 before it too: E12 stays because it reverses a kept entry, and E13 because it reverses E12.
    doc = ledger("hand-worked-year")
    lines = [{"account": "1010", "dimension_set": "", "currency": "SGD", "amount": "-1000.00", "base_amount": "-1000.00"},
             {"account": "3010", "dimension_set": "", "currency": "SGD", "amount": "1000.00", "base_amount": "1000.00"}]
    back = [{**x, "amount": x["amount"].lstrip("-") if x["amount"].startswith("-") else "-" + x["amount"],
             "base_amount": x["base_amount"].lstrip("-") if x["base_amount"].startswith("-") else "-" + x["base_amount"]} for x in lines]
    doc["entries"] += [{"id": "E12", "source": {"type": "share_issue_cash", "id": "SHR-2"}, "posting_date": "2018-11-01", "reverses": "E8", "lines": lines},
                       {"id": "E13", "source": {"type": "share_issue_cash", "id": "SHR-2"}, "posting_date": "2018-12-01", "reverses": "E12", "lines": back}]
    r = purge(doc)
    assert (r["kept"], r["unchanged"]) == (["E12", "E13", "E8", "E9"], True)


@pytest.mark.parametrize("edit, reason", [
    (lambda d: d["accounts"].append(dict(d["accounts"][0])), "two accounts"),
    (lambda d: d["entries"].append(dict(d["entries"][0])), "two entries"),
    (lambda d: d["entries"][0]["lines"][0].update(account="9999"), "no such account"),
    (lambda d: d["entries"][4]["lines"][0].update(currency="EUR"), "not in currencies"),
    (lambda d: d["entries"][0]["lines"][0].update(amount="1090.001", base_amount="1090.001"), "amount-too-many-decimals"),
    (lambda d: d["entries"][0]["lines"][0].update(amount="1000.00"), "is not the base amount"),
    (lambda d: d["entries"][0]["lines"][0].update(amount="1000.00", base_amount="1000.00"), "does not balance"),
    (lambda d: d["entries"][9].update(reverses="E99"), "not an entry"),
    (lambda d: d.update(boundary="2019-02-01"), "not the first day"),
    (lambda d: d.update(base_currency="EUR"), "base currency"),
    (lambda d: d.update(suspense="1100"), "control account"),
    (lambda d: d.update(retained_earnings="4010"), "revenue or expense"),
    (lambda d: d.update(retained_earnings="9999"), "not an account"),
    (lambda d: d["entries"][0].update(posting_date="2018-02-30"), "not a calendar date"),
    (lambda d: d.update(extra=1), "Additional properties"),
])
def test_what_the_oracle_refuses(edit, reason: str) -> None:  # req: AUD-10
    doc = copy.deepcopy(ledger("hand-worked-year"))
    edit(doc)
    with pytest.raises(PurgeError, match=reason):
        purge(doc)


@settings(max_examples=150, deadline=None, suppress_health_check=list(HealthCheck))
@given(purges())
def test_a_purge_changes_no_position_from_the_boundary_on(doc: dict) -> None:  # req: AUD-10, INV-01, INV-07
    r = purge(doc)
    assert r["unchanged"]
    for entry in r["entries"]:  # each entry balances
        assert sum((Decimal(x["base_amount"]) for x in entry["lines"]), Decimal(0)) == 0
    # Suspense nets to zero across the carry-forward: it carries exactly the purged lines' own suspense balance.
    purged = [e for e in doc["entries"] if e["posting_date"] < doc["boundary"] and e["id"] not in r["kept"]]
    held = sum((Decimal(x["base_amount"]) for e in purged for x in e["lines"] if x["account"] == doc["suspense"]), Decimal(0))
    carried = sum((Decimal(x["base_amount"]) for e in r["entries"] for x in e["lines"] if x["account"] == doc["suspense"]), Decimal(0))
    assert carried == held
    assert r["purged"] == len(purged)


def test_generated_ledgers_are_the_same_for_the_same_seed() -> None:  # req: QA-ENG-02
    a, b = generate_purges(7, 5), generate_purges(7, 5)
    assert a == b and len({json.dumps(x, sort_keys=True) for x in a}) == 5
    assert all(purge(doc)["unchanged"] for doc in a)


def test_each_person_and_instrument_keeps_their_balance() -> None:  # req: AUD-10, INV-13
    # LOAN-2 lends 300.00 from P-1 and 200.00 from P-2 on the director's loan (a control account); SAFE-1 puts USD 100.00
    # (135.00) into S-1 and 50.00 into S-2. Each keeps its own line, and P-1's repayment after the boundary clears P-1.
    r = purge(ledger("persons-and-instruments"))
    assert r["unchanged"]
    tagged = [(e["source"]["id"], x["account"], x["party"], x["instrument"], x["currency"], x["amount"]) for e in r["entries"] for x in e["lines"]]
    assert tagged == [
        ("2022-07-01", "1010", None, None, "SGD", "550.00"), ("2022-07-01", "1020", None, None, "USD", "100.00"),
        ("2022-07-01", "2700", None, "S-2", "SGD", "-50.00"), ("2022-07-01", "2700", None, "S-1", "USD", "-100.00"),
        ("2022-07-01", "2999", None, None, "SGD", "-500.00"),
        ("LOAN-2", "2500", "P-1", None, "SGD", "-300.00"), ("LOAN-2", "2500", "P-2", None, "SGD", "-200.00"),
        ("LOAN-2", "2999", None, None, "SGD", "500.00"),
    ]
    last = {(b["account"], b["party"], b["source"] and b["source"]["id"]): b["amount"] for b in r["positions"][-1]["balances"]}
    assert last[("2500", "P-1", "LOAN-2")] == "-300.00" and last[("2500", "P-1", "REPAY-1")] == "300.00"
    assert last[("2500", "P-2", "LOAN-2")] == "-200.00"
