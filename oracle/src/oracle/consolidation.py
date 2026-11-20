"""Consolidation of a group, worked out from the rules and not from the engine (QA-ENG-01; PRD-001 FR-ENT-06, RND-TRANSLATE).

The rules. Each entity gives, per account of the parent's chart (and the counterparty entity when the account is an intercompany one), its net movement in each month through the run's
last month, in its own currency, a debit positive. It is translated into the group currency, every translated amount rounded once, to the group currency's minor units, half away from
zero (RND-TRANSLATE), by the account's type:

* an asset or a liability: the account's cumulative balance through the last month, at the closing rate of the last month, translated once (section ``balance``);
* an equity account: each month's movement at that month's closing rate, the rounded months added (section ``equity``);
* a revenue or an expense: each month's movement at that month's average rate, the rounded months added: the months from the run's first month on are the section ``result``, the
  months before it the section ``brought_forward``.

An entity whose currency is the group currency is not translated. A line whose translated amount is nothing is not listed. The translation reserve of an entity is the negative of
the sum of all its translated lines, so its translated trial balance is nothing. A pair names two accounts that mirror each other, ``a`` normally a debit and ``b`` a credit. For two
entities X and Y, x is X's amount on ``a`` with Y as counterparty and y is Y's on ``b`` with X as counterparty, each in the account's own section; when x is above nothing and y below
nothing the matched amount is the smaller of x and -y, which is taken off both; otherwise nothing is matched. When x + y is not nothing the difference is listed: nothing is plugged and
what is not matched stays where it is. The consolidated figures add the entities' lines and the eliminations by account and section.
"""

from decimal import Decimal

from oracle.money import EXACT, fmt, rnd_fx

_SECTION = {"asset": "balance", "liability": "balance", "equity": "equity", "revenue": "result", "expense": "result"}


class ConsolidationRefused(ValueError):
    """An input the run cannot work from."""


def _month_ok(text: str) -> bool:
    return len(text) == 7 and text[4] == "-" and text[:4].isdigit() and text[5:].isdigit() and 1 <= int(text[5:]) <= 12


def _entity_lines(case: dict, entity: dict, types: dict[str, str]) -> tuple[list[tuple[str, str, str, Decimal]], Decimal]:
    group_units = case["group_minor_units"]
    rates = {(r["currency"], r["month"]): (Decimal(r["closing"]), Decimal(r["average"])) for r in case["rates"]}
    ids = {e["id"] for e in case["entities"]}
    foreign = entity["currency"] != case["group_currency"]

    def translate(amount: Decimal, month: str, closing: bool) -> Decimal:
        if not foreign:
            return amount
        if (entity["currency"], month) not in rates:
            raise ConsolidationRefused(f"no rate for {entity['currency']} in {month}")
        return rnd_fx(amount, rates[(entity["currency"], month)][0 if closing else 1], group_units)

    months: dict[tuple[str, str], dict[str, Decimal]] = {}
    for line in entity["lines"]:
        kind = line["type"]
        if kind not in _SECTION:
            raise ConsolidationRefused(f"type {kind}")
        if types.setdefault(line["account"], kind) != kind:
            raise ConsolidationRefused(f"account {line['account']} has two types")
        if not _month_ok(line["month"]) or line["month"] > case["to"]:
            raise ConsolidationRefused(f"month {line['month']}")
        cp = line["counterparty"]
        if cp and (cp not in ids or cp == entity["id"]):
            raise ConsolidationRefused(f"counterparty {cp}")
        per = months.setdefault((line["account"], cp), {})
        per[line["month"]] = EXACT.add(per.get(line["month"], Decimal(0)), Decimal(line["amount"]))

    out: list[tuple[str, str, str, Decimal]] = []
    for (account, cp), per in months.items():
        kind = types[account]
        if kind in ("asset", "liability"):
            cumulative = Decimal(0)
            for m in per:
                cumulative = EXACT.add(cumulative, per[m])
            out.append((account, cp, "balance", translate(cumulative, case["to"], True)))
        elif kind == "equity":
            out.append((account, cp, "equity", _sum(translate(per[m], m, True) for m in per)))
        else:
            out.append((account, cp, "result", _sum(translate(per[m], m, False) for m in per if m >= case["from"])))
            out.append((account, cp, "brought_forward", _sum(translate(per[m], m, False) for m in per if m < case["from"])))
    out = [row for row in out if row[3] != 0]
    reserve = -_sum(row[3] for row in out)
    return out, reserve


def _sum(values) -> Decimal:
    total = Decimal(0)
    for v in values:
        total = EXACT.add(total, v)
    return total


def run(case: dict) -> dict[str, object]:
    """The run's figures as the text a case writes them in (the flat forms are in consolidation.schema.json)."""
    if not (_month_ok(case["from"]) and _month_ok(case["to"]) and case["from"] <= case["to"]):
        raise ConsolidationRefused("the run's months")
    units = case["group_minor_units"]
    types: dict[str, str] = {}
    entities = {}
    for entity in case["entities"]:
        if entity["id"] in entities:
            raise ConsolidationRefused("an entity twice")
        entities[entity["id"]] = _entity_lines(case, entity, types)

    def amount_of(entity_id: str, account: str, cp: str) -> tuple[Decimal, str, str]:
        for acc, c, section, value in entities[entity_id][0]:
            if acc == account and c == cp and section == _SECTION[types[account]]:
                return value, section, types[account]
        return Decimal(0), "", ""

    eliminations: list[tuple[str, str, str, str, Decimal]] = []
    differences: list[str] = []
    for pair in case["pairs"]:
        for x_id in entities:
            for y_id in entities:
                if x_id == y_id:
                    continue
                x, x_section, _ = amount_of(x_id, pair["a"], y_id)
                y, y_section, _ = amount_of(y_id, pair["b"], x_id)
                matched = Decimal(0)
                if x > 0 and y < 0:
                    matched = min(x, -y)
                if matched != 0:
                    eliminations.append((x_id, pair["a"], y_id, x_section, -matched))
                    eliminations.append((y_id, pair["b"], x_id, y_section, matched))
                gap = EXACT.add(x, y)
                if gap != 0:
                    differences.append("|".join([x_id, y_id, pair["a"], pair["b"], fmt(x, units), fmt(y, units), fmt(matched, units), fmt(gap, units)]))

    by_account: dict[tuple[str, str], Decimal] = {}
    for rows, _ in entities.values():
        for acc, _, section, value in rows:
            by_account[(acc, section)] = EXACT.add(by_account.get((acc, section), Decimal(0)), value)
    for _, acc, _, section, value in eliminations:
        by_account[(acc, section)] = EXACT.add(by_account.get((acc, section), Decimal(0)), value)

    return {
        "entities": [
            {
                "id": eid,
                "lines": sorted("|".join([acc, cp, section, fmt(value, units)]) for acc, cp, section, value in rows),
                "reserve": fmt(reserve, units),
            }
            for eid, (rows, reserve) in entities.items()
        ],
        "eliminations": sorted("|".join([eid, acc, cp, section, fmt(value, units)]) for eid, acc, cp, section, value in eliminations),
        "differences": sorted(differences),
        "consolidated": sorted("|".join([acc, section, fmt(value, units)]) for (acc, section), value in by_account.items() if value != 0),
        "reserve": fmt(_sum(reserve for _, reserve in entities.values()), units),
    }
