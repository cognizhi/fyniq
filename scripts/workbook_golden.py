"""Write test/golden/workbook/ from the workbook's golden data sets (p1-launch-readiness task 3.1, design D4).

The workbook (zh-sheet-accounting) tests its engine on data sets of its own: the posting edge cases
(tests/edge_posting.py with tests/test_edge_posting.py) and the others PRD-001 Appendix G.1 names. This script reads
them, at the commit to pin, as data: it parses each file and evaluates only literals, dates and the rows' own calls,
so nothing from that repository runs (design D4; the data is synthetic, DEV-08). For each set it writes the scenario
(fyniq-scenario@1, the format test/differential/README.md describes) and mapping.json, where every workbook row went:
the document it became, or why Fyniq does not post it. sources.json records the commit, the SHA-256 of each source
file read and of each fixture. The expected results, expected.json, are kept by hand from the workbook's tests, each
naming the test and the values it pins; this script never writes them.

Run it from this repository with a checkout of zh-sheet-accounting at the commit to pin:

    python3 -I scripts/workbook_golden.py <path to zh-sheet-accounting> [--out test/golden/workbook]

It is run by hand, when the workbook's sets change. The tests never run it: TestTheFixturesAreTheRecordedOnes checks
each fixture against its recorded hash, so an expected value is never edited quietly.
"""

from __future__ import annotations

import argparse
import ast
import datetime as dt
import hashlib
import json
import subprocess
import sys
from decimal import Decimal
from pathlib import Path

REPOSITORY = "cognizhi/zh-sheet-accounting"


class Refused(Exception):
    """A construct the reader does not evaluate."""


class Module:
    """A Python file's data, evaluated from its syntax tree: constants, names, lists, tuples, dicts, dict(...), D(...)
    for a date and row(...) for a table row, and the assignments of a function before it returns them. Anything else
    is refused, so no code of the file runs."""

    def __init__(self, path: Path):
        self.tree = ast.parse(path.read_text(encoding="utf-8"), filename=str(path))
        self.env: dict = {}
        for node in self.tree.body:
            if isinstance(node, ast.Assign) and len(node.targets) == 1:
                try:
                    self.assign(node.targets[0], self.value(node.value, self.env), self.env)
                except Refused:
                    pass  # an import alias or a computed value: not data

    def function(self, name: str):
        """What the module's function `name` returns, evaluated from its assignments and its return."""
        fn = next(n for n in self.tree.body if isinstance(n, ast.FunctionDef) and n.name == name)
        env = dict(self.env)
        for node in fn.body:
            if isinstance(node, ast.Expr) and isinstance(node.value, ast.Constant):
                continue  # the docstring
            if isinstance(node, ast.Assign) and len(node.targets) == 1:
                self.assign(node.targets[0], self.value(node.value, env), env)
            elif isinstance(node, ast.Return) and node.value is not None:
                return self.value(node.value, env)
            else:
                raise Refused(f"{name}: {ast.dump(node)[:80]}")
        raise Refused(f"{name} returns nothing")

    def assign(self, target, value, env: dict) -> None:
        if isinstance(target, ast.Name):
            env[target.id] = value
        elif isinstance(target, ast.Tuple) and all(isinstance(t, ast.Name) for t in target.elts):
            for t, v in zip(target.elts, value, strict=True):
                env[t.id] = v
        else:
            raise Refused("assignment")

    def value(self, node, env: dict):
        if isinstance(node, ast.Constant):
            return node.value
        if isinstance(node, ast.Name):
            if node.id in env:
                return env[node.id]
            raise Refused(node.id)
        if isinstance(node, ast.UnaryOp) and isinstance(node.op, ast.USub):
            return -self.value(node.operand, env)
        if isinstance(node, (ast.List, ast.Tuple)):
            return [self.value(e, env) for e in node.elts]
        if isinstance(node, ast.Dict):
            out = {}
            for k, v in zip(node.keys, node.values):
                if k is None:
                    out.update(self.value(v, env))
                else:
                    out[self.value(k, env)] = self.value(v, env)
            return out
        if isinstance(node, ast.Call) and isinstance(node.func, ast.Name):
            args = [self.value(a, env) for a in node.args]
            kwargs: dict = {}
            for kw in node.keywords:
                if kw.arg is None:
                    kwargs.update(self.value(kw.value, env))
                else:
                    kwargs[kw.arg] = self.value(kw.value, env)
            if node.func.id == "D":
                return dt.date(*args)
            if node.func.id == "row":
                return {"_status": args[0], **kwargs}
            if node.func.id == "dict":
                return dict(kwargs)
        raise Refused(ast.dump(node)[:80])


def money(v) -> str:
    """A workbook amount as a plain decimal string at two places."""
    return str(Decimal(str(v)).quantize(Decimal("0.01")))


def code(account: str | int) -> str:
    """The code of a workbook account cell ("1020 - Bank - Operating account") or number."""
    return str(account).split(" - ")[0].strip()


TYPES = {"Asset": "asset", "Liability": "liability", "Equity": "equity", "Revenue": "revenue", "Other Income": "revenue",
         "Cost of Sales": "expense", "Expense": "expense", "Other Expense": "expense", "Tax": "expense"}


class Chart:
    """The workbook's chart (src/cfh/coa.py): each account's type, default GST code and whether it holds money."""

    def __init__(self, coa: Module):
        self.rows = {str(r[0]): r for r in coa.env["COA"]}
        self.items = {r[0]: r for r in coa.env["ITEMS"]}  # code, name, description, type, unit, price, account

    def gst(self, account: str, default: str) -> str:
        """The account's default GST code, or default for an account the chart does not have."""
        return self.rows[account][7] if account in self.rows else default

    def pl(self, account: str) -> bool:
        """Whether the account is an income or expense account, the only kind a spread takes (FR-GL-03)."""
        return account in self.rows and TYPES[self.rows[account][2]] in ("revenue", "expense")

    def accounts(self, extra: list[dict]) -> list[dict]:
        out = []
        for c, r in self.rows.items():
            a = {"code": c, "type": TYPES[r[2]], "group": False, "active": True}
            if r[3] == "Share capital":
                a["role"] = "share-capital"  # INV-13: only the register and the opening balances post there
            out.append(a)
        return out + extra

    def money_accounts(self) -> dict[str, str]:
        return {c: "SGD" for c, r in self.rows.items() if r[6] == "Yes"}


def posting(edge: Module, chart: Chart) -> tuple[dict, list[dict]]:
    """The posting edge cases (tests/edge_posting.py) as one scenario, and where each row went."""
    data = {k: edge.function(f"_{k}") for k in ("transactions", "invoices", "bills", "doc_lines", "journal", "payroll", "assets")}
    settings_fn = next(n for n in edge.tree.body if isinstance(n, ast.FunctionDef) and n.name == "edge_data")
    edge_dict = next(n.value for n in ast.walk(settings_fn) if isinstance(n, ast.Return))
    wb = {edge.value(k, edge.env): v for k, v in zip(edge_dict.keys, edge_dict.values)}
    settings = edge.value(wb["settings"], edge.env)
    opening = edge.value(wb["opening"], edge.env)
    books_start = settings["S_BooksStart"]

    docs: list[dict] = []
    mapping: list[dict] = []
    rates = {}  # (currency, month) -> the rates of the month's table rows
    for r in edge.value(wb["fx"], edge.env):
        rates.setdefault((r["ccy"], r["month"].strftime("%Y-%m")), []).append(Decimal(str(r["rate"])))

    def note(sheet: str, i: int, row: dict, *, document: str | None = None, reason: str | None = None, read: str | None = None) -> None:
        m = {"sheet": sheet, "row": i + 1, "status": row["_status"]}
        for k in ("no", "entry", "name", "emp", "desc"):
            if row.get(k):
                m["workbook"] = str(row[k])
                break
        m["document" if document else "not_ported"] = document or reason
        if read:
            m["read"] = read  # how Fyniq reads the row where it differs from what was typed
        mapping.append(m)

    def unrepresentable(row: dict) -> str | None:
        """Why a ✖ row cannot be a document of the format, or None: a missing or unreadable date, amount or account
        is refused by the API's input checks (ENG-23, ENG-44) before anything posts."""
        s = row["_status"]
        if not s.startswith("✖"):
            return None
        if "Duplicate number" in s:
            return "Fyniq numbers each document from its sequence (FR-ORG-04), so two cannot share a number"
        if "No exchange rate" in s:
            return "a currency Fyniq has no rate for is refused at entry by the rate cascade (ENG-40)"
        for words in ("Enter", "Date not recognised", "Choose", "Use only one", "Use debit OR credit", "Another line",
                      "Lines of this entry have different dates", "Month not recognised"):
            if words in s:
                return "an input the API refuses before posting (ENG-23, ENG-44): " + s.removeprefix("✖ ")
        return None

    def before_books(row: dict) -> bool:
        return isinstance(row.get("date"), dt.date) and row["date"] < books_start and not row.get("opening")

    # The opening position (edge_data()["opening"]): the opening invoice INV-E24 is an opening item, which posts its own
    # Dr 1100 against 3900 (rev 0.257), so 3900 takes its part of the opening balances in place of 1100.
    ob = [{"account": str(a), "currency": "SGD", "amount": money(v if a != 3010 else -v)} for a, v in opening.items() if a != 1100]
    ob.append({"account": "3900", "currency": "SGD", "amount": money(opening[1100])})
    docs.append({"type": "opening_balances", "id": "OB", "version": 1, "date": books_start.isoformat(),
                 "memo": "The edge data set's opening position", "lines": ob})

    # Bank lines, transfers, and the payments of documents.
    numbers = {d.get("no") for k in ("invoices", "bills") for d in data[k] if not d["_status"].startswith("✖")}
    for i, t in enumerate(data["transactions"]):
        why = unrepresentable(t)
        if why:
            note("Transactions", i, t, reason=why)
            continue
        if before_books(t):
            note("Transactions", i, t, reason="Fyniq has no period before the books start: what came before is in the opening balances (FR-GL-06)")
            continue
        bank, cat = code(t["bank"]), code(t["cat"])
        gross = Decimal(str(t.get("in", 0))) - Decimal(str(t.get("out", 0)))
        if cat in ("1100", "2010") and t.get("ref") not in numbers:
            note("Transactions", i, t, reason="a bank line on the receivable or payable account waits for the document it pays (FR-BNK-07): it posts when matched")
            continue
        if cat in ("1100", "2010"):
            ref = t["ref"]
            sales = cat == "1100"
            doc = {"type": "customer_receipt" if sales else "bill_payment", "id": f"{'RC' if sales else 'PY'}-{ref}",
                   "version": 1, "date": t["date"].isoformat(), "account": bank}
            docs.append(doc)
            note("Transactions", i, t, document=doc["id"])
            continue
        if cat in chart.money_accounts() and cat != bank:
            doc = {"type": "bank_transfer", "id": f"TR-{i + 1:02d}", "version": 1, "date": t["date"].isoformat(),
                   "from": {"account": bank, "currency": "SGD", "amount": money(-gross)},
                   "to": {"account": cat, "currency": "SGD", "amount": money(-gross)}}
            docs.append(doc)
            note("Transactions", i, t, document=doc["id"])
            continue
        part = {"account": cat, "amount": money(gross), "gst_code": t.get("gst") or chart.gst(cat, "NA")}
        read = None
        if t.get("months"):
            if chart.pl(cat):
                part["spread_months"] = t["months"]
            else:
                read = "not spread: Fyniq offers a spread only on an income or expense account (FR-GL-03)"
        doc = {"type": "bank_transaction", "id": f"BT-{i + 1:02d}", "version": 1, "date": t["date"].isoformat(),
               "account": bank, "currency": "SGD", "amount": money(gross), "parts": [part]}
        docs.append(doc)
        note("Transactions", i, t, document=doc["id"], read=read)

    items = chart.items

    def data_lines(no: str | None) -> bool:
        return any(dl["doc"] == no for dl in data["doc_lines"])

    def lines_of(no: str, account: str, currency: str, rate: Decimal) -> list[dict]:
        """A document's Doc Lines: each at its item's price (converted at the document's rate when the document is in
        another currency), or its own; a line of quantity 0 is left out, since Fyniq refuses a line of nothing."""
        out = []
        for dl in data["doc_lines"]:
            if dl["doc"] != no:
                continue
            qty = dl.get("qty", 1)
            if qty == 0:
                continue
            price = Decimal(str(dl["price"])) if "price" in dl else Decimal(str(items[code(dl["item"])][5]))
            if currency != "SGD":
                price = price / rate
            line = {"account": account, "quantity": str(qty), "unit_price": money(price), "gst_code": "SR" if currency == "SGD" else "ZR"}
            if dl.get("disc"):
                line["discount"] = str(Decimal(str(dl["disc"])) * 100).rstrip("0").rstrip(".")
            out.append(line)
        return out

    def rate_of(row: dict) -> tuple[str, str]:
        if "fx" in row:
            return str(row["fx"]), "document"
        found = rates[(row["ccy"], row["date"].strftime("%Y-%m"))]
        return str(sum(found) / len(found)), "table" if len(found) == 1 else "table average"

    for sheet, key, kind in (("Invoices", "invoices", "sales"), ("Bills", "bills", "purchases")):
        for i, d in enumerate(data[key]):
            why = unrepresentable(d)
            if why:
                note(sheet, i, d, reason=why)
                continue
            if d.get("draft") == "Yes":
                note(sheet, i, d, reason="a draft posts nothing, in Fyniq as in the workbook")
                continue
            if before_books(d):
                note(sheet, i, d, reason="dated before the books start and not an opening item: Fyniq takes such a document only as an opening item (FR-GL-06, rev 0.257)")
                continue
            if not d.get("cust" if kind == "sales" else "supp"):
                note(sheet, i, d, reason="Fyniq names the customer or supplier before a document posts (FR-AR-01, FR-AP-01)")
                continue
            account = code(d["rev"] if kind == "sales" else d["exp"])
            currency = d.get("ccy", "SGD")
            doc = {"type": "invoice" if kind == "sales" else "bill", "id": d["no"], "version": 1, "date": d["date"].isoformat(),
                   "currency": currency}
            rate = Decimal("1")
            if currency != "SGD":
                rs, src = rate_of(d)
                rate = Decimal(rs)
                doc["fx_rate"], doc["fx_rate_source"] = rs, src
            amount = d.get("amt")
            if amount is not None and amount < 0:
                doc["type"] = "credit_note" if kind == "sales" else "vendor_credit"
                amount = -amount
            code_ = d.get("gst") or ("SR" if kind == "sales" else chart.gst(account, "TX"))
            lines = lines_of(d["no"], account, currency, rate) if kind == "sales" else []
            if not lines:
                line = {"account": account, "amount": money(amount), "gst_code": code_}
                if kind == "purchases" and code_ in ("TX", "IM"):
                    line["gst"] = money(Decimal(str(amount)) * Decimal("0.09"))
                lines = [line]
            read = None
            if d.get("months") and chart.pl(account):
                lines[0]["spread_months"] = d["months"]
                if d.get("svcstart"):
                    lines[0]["spread_from"] = d["svcstart"].isoformat()
            elif d.get("months"):
                read = "not spread: Fyniq offers a spread only on an income or expense account (FR-GL-03)"
            if amount is not None and data_lines(d["no"]):
                read = "its lines, not the amount typed: a Fyniq document's total is its lines' (FR-AR-01)"
            if any(dl["doc"] == d["no"] and dl.get("qty", 1) == 0 for dl in data["doc_lines"]):
                read = "without its line of quantity 0, which the API refuses (a line of nothing); the total is the same"
            if d.get("opening") == "Yes":
                doc["opening_item"] = True
            doc["lines"] = lines
            docs.append(doc)
            note(sheet, i, d, document=doc["id"], read=read)

    # The payments found above, now that their documents are known: each pays its document, the bank's money at the
    # document's own figures (a document in another currency paid from an SGD account gives what it paid, its base).
    by_id = {d["id"]: d for d in docs}
    for t in data["transactions"]:
        ref = t.get("ref", "")
        pid = ("RC-" if code(t.get("cat", "")) == "1100" else "PY-") + ref
        if pid not in by_id or "parts" in by_id[pid]:
            continue
        p, target = by_id[pid], by_id[ref]
        paid = Decimal(str(t.get("in", 0) or t.get("out", 0)))
        p["currency"] = target["currency"]
        owed = document_total(target)
        if target["currency"] != "SGD":
            p["base_amount"] = money(paid)
            p["parts"] = [{"document": ref, "amount": money(owed)}]
        elif paid > owed:
            p["parts"] = [{"document": ref, "amount": money(owed)}]
            p["unapplied"] = money(paid - owed)
        elif t["desc"].endswith("part payment"):
            p["parts"] = [{"document": ref, "amount": money(paid)}]
        else:
            p["parts"] = [{"document": ref, "amount": money(paid)}]
            if paid < owed:
                p["parts"][0]["in_full"] = True  # settled within the S$1.00 tolerance (FR-BNK-08)

    # Journal entries, line by line: an entry with a line the API refuses is refused whole, as the workbook's is.
    entries: dict[str, list] = {}
    for i, j in enumerate(data["journal"]):
        if not j.get("entry"):
            note("Journal", i, j, reason=unrepresentable(j) or "no entry")
            continue
        entries.setdefault(j["entry"], []).append((i, j))
    for entry, rows in list(entries.items()):
        why = next((unrepresentable(j) for _, j in rows if unrepresentable(j) and "Another line" not in j["_status"]), None)
        if why or any(before_books(j) for _, j in rows):
            for i, j in rows:
                note("Journal", i, j, reason=why or "Fyniq has no period before the books start: what came before is in the opening balances (FR-GL-06)")
            del entries[entry]
    for entry, rows in entries.items():
        lines = [{"account": code(j["acct"]), "currency": "SGD", "amount": money(Decimal(str(j.get("dr", 0))) - Decimal(str(j.get("cr", 0))))}
                 for _, j in rows]
        docs.append({"type": "manual_journal", "id": entry, "version": 1, "date": rows[0][1]["date"].isoformat(),
                     "memo": rows[0][1].get("desc", ""), "lines": lines})
        for i, j in rows:
            note("Journal", i, j, document=entry)

    for i, p in enumerate(data["payroll"]):
        note("Payroll", i, p, reason="pay runs (FR-PAY-01 to FR-PAY-03) are not in the launch: these cases come with them")
    for i, (status, q) in enumerate(edge.env["QUOTES"]):
        note("Quotes & Orders", i, {"_status": status, **q}, reason="a quote or an order posts nothing; its statuses are the non-posting set's")

    for i, a in enumerate(data["assets"]):
        why = unrepresentable(a)
        if why:
            note("Assets", i, a, reason=why)
            continue
        fid = f"FA-{i + 1:02d}"
        doc = {"type": "fixed_asset", "id": fid, "version": 1, "date": a["date"].isoformat(), "account": code(a["acct"]),
               "cost": money(a["cost"]), "residual": money(a.get("resid", 0)), "life_months": a["life"]}
        if a.get("depstart"):
            doc["depreciate_from"] = a["depstart"].isoformat()
        docs.append(doc)
        if a.get("disp"):
            docs.append({"type": "asset_disposal", "id": f"AD-{i + 1:02d}", "version": 1, "date": a["disp"].isoformat(), "asset": fid})
            note("Assets", i, a, document=fid, read=f"its disposal posts too (AD-{i + 1:02d}, a write-off with no proceeds): the workbook asks for a journal")
        else:
            note("Assets", i, a, document=fid)

    # Documents post in order: the opening balances, then by date (each payment after what it pays), as entered.
    first, rest = docs[:1], docs[1:]
    order = {id(d): n for n, d in enumerate(rest)}
    rest.sort(key=lambda d: (d["date"] if d["type"] != "opening_balances" else "", d["type"] in ("customer_receipt", "bill_payment", "asset_disposal"), order[id(d)]))
    as_of = max(d["date"] for d in docs)
    scenario = {
        "schema": "fyniq-scenario@1",
        "name": "workbook-posting-edge-cases",
        "base_currency": "SGD",
        "currencies": {"SGD": 2, "USD": 2},
        "accounts": chart.accounts([{"code": "3900", "type": "equity", "group": False, "active": True}]),
        "settings": {
            "books_start": books_start.isoformat(),
            "first_year_end": "2026-12-31",
            "accounts": {"receivable": "1100", "payable": "2010", "gst": "2200", "prepayments": "1210", "deferred_revenue": "2400",
                         "fx": "7030", "bank_charges": "6460", "depreciation": "6610", "accumulated_depreciation": "1590",
                         "disposal": "7040", "opening_clearing": "3900"},
            "money_accounts": chart.money_accounts(),
            # The workbook's registration is a Yes with no date (S_GSTStart starts the return's period), and the opening
            # invoice INV-E24 bears GST: registered throughout.
            "gst": {"registered_from": "2025-01-01", "rates": [{"from": "2024-01-01", "percent": str(int(settings["S_GSTRate"] * 100))}]},
        },
        "as_of": as_of,
        "documents": first + rest,
        "reversals": [],
    }
    return scenario, mapping


def document_total(doc: dict) -> Decimal:
    """What a document owes in its currency: its nets and, for an SGD document coded SR or TX, 9% GST on them."""
    total = Decimal("0")
    for ln in doc["lines"]:
        net = Decimal(ln["amount"]) if "amount" in ln else (Decimal(ln["quantity"]) * Decimal(ln["unit_price"]) * (1 - Decimal(ln.get("discount", "0")) / 100))
        total += net.quantize(Decimal("0.01"))
        if "gst" in ln:
            total += Decimal(ln["gst"])
        elif ln["gst_code"] == "SR":
            total += (net * Decimal("0.09")).quantize(Decimal("0.01"))
    return total


def sha256(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("repo", type=Path, help="a checkout of zh-sheet-accounting at the commit to pin")
    ap.add_argument("--out", type=Path, default=Path("test/golden/workbook"))
    a = ap.parse_args()
    commit = subprocess.run(["git", "-C", str(a.repo), "rev-parse", "HEAD"], capture_output=True, text=True, check=True).stdout.strip()
    sources = {"posting": ["tests/edge_posting.py", "tests/test_edge_posting.py", "src/cfh/coa.py"]}
    chart = Chart(Module(a.repo / "src/cfh/coa.py"))
    scenario, mapping = posting(Module(a.repo / "tests/edge_posting.py"), chart)
    out = a.out / "posting"
    out.mkdir(parents=True, exist_ok=True)
    (out / "scenario.json").write_text(json.dumps(scenario, indent=1, ensure_ascii=False) + "\n", encoding="utf-8")
    (out / "mapping.json").write_text(json.dumps(mapping, indent=1, ensure_ascii=False) + "\n", encoding="utf-8")
    record = {"repository": REPOSITORY, "commit": commit, "sets": {}}
    for name, files in sources.items():
        fixtures = sorted(p for p in (a.out / name).iterdir() if p.suffix == ".json")
        record["sets"][name] = {
            "sources": {f: sha256(a.repo / f) for f in files},
            "fixtures": {f"{name}/{p.name}": sha256(p) for p in fixtures},
        }
    (a.out / "sources.json").write_text(json.dumps(record, indent=1) + "\n", encoding="utf-8")
    print(f"wrote {a.out} from {REPOSITORY}@{commit[:12]}", file=sys.stderr)


if __name__ == "__main__":
    main()
