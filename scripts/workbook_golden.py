"""Write test/golden/workbook/ from the workbook's golden data sets (p1-launch-readiness task 3.1, design D4).

The workbook (zh-sheet-accounting) tests its engine on data sets of its own: the posting edge cases
(tests/edge_posting.py with tests/test_edge_posting.py) and the others PRD-001 Appendix G.1 names. This script reads
them, at the commit to pin, as data: it parses each file and evaluates only literals, dates, the rows' own calls and the
small lambdas that build them, so nothing from that repository runs (design D4; the data is synthetic, DEV-08). For each
set it writes the scenario (fyniq-scenario@1, the format test/differential/README.md describes) and mapping.json, where
every workbook row went: the document it became, or why Fyniq does not post it. sources.json records the commit, the
SHA-256 of each source file read and of each fixture. The expected results, expected.json, are kept by hand from the
workbook's tests, each naming the test and the values it pins; this script never writes them.

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
import operator
import re
import subprocess
import sys
from decimal import Decimal
from pathlib import Path

REPOSITORY = "cognizhi/zh-sheet-accounting"


class Refused(Exception):
    """A construct the reader does not evaluate."""


class Closure:
    """A lambda of the file, called only by the reader's own evaluation of its body."""

    def __init__(self, node: ast.Lambda, env: dict):
        self.node, self.env = node, env


COMPARE = {ast.Gt: operator.gt, ast.Lt: operator.lt, ast.GtE: operator.ge, ast.LtE: operator.le, ast.Eq: operator.eq,
           ast.NotEq: operator.ne}


class Module:
    """A Python file's data, evaluated from its syntax tree: constants, names, lists, tuples, dicts, dict(...), D(...)
    for a date, row(...) for a table row, coa.label(code) for an account's label, small lambdas, conditional
    expressions and comparisons, and the assignments of a function before it returns them. Anything else is refused,
    so no code of the file runs."""

    def __init__(self, path: Path, labels: dict[int, str] | None = None):
        self.tree = ast.parse(path.read_text(encoding="utf-8"), filename=str(path))
        self.labels = labels or {}
        self.env: dict = {}
        for node in self.tree.body:
            if isinstance(node, ast.Assign) and len(node.targets) == 1:
                try:
                    self.assign(node.targets[0], self.value(node.value, self.env), self.env)
                except Refused:
                    pass  # an import alias or a computed value: not data

    def function(self, name: str):
        """What the module's function `name` returns, evaluated from its statements: assignments, a dict's update,
        and the return. A test fixture's `return _build_and_calc(data, ...)` returns its data."""
        fn = next(n for n in self.tree.body if isinstance(n, ast.FunctionDef) and n.name == name)
        env = dict(self.env)
        for node in fn.body:
            if isinstance(node, ast.Expr) and isinstance(node.value, ast.Constant):
                continue  # the docstring
            if isinstance(node, ast.Assign) and len(node.targets) == 1:
                target = node.targets[0]
                if isinstance(target, ast.Subscript):
                    self.value(target.value, env)[self.value(target.slice, env)] = self.value(node.value, env)
                else:
                    self.assign(target, self.value(node.value, env), env)
            elif isinstance(node, ast.Expr) and isinstance(node.value, ast.Call) and isinstance(node.value.func, ast.Attribute) \
                    and node.value.func.attr == "update" and not node.value.args:
                self.value(node.value.func.value, env).update({kw.arg: self.value(kw.value, env) for kw in node.value.keywords})
            elif isinstance(node, ast.Return) and node.value is not None:
                v = node.value
                if isinstance(v, ast.Call) and isinstance(v.func, ast.Name) and v.func.id == "_build_and_calc":
                    return self.value(v.args[0], env)
                return self.value(v, env)
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
        if isinstance(node, ast.Subscript):
            return self.value(node.value, env)[self.value(node.slice, env)]
        if isinstance(node, ast.IfExp):
            return self.value(node.body if self.value(node.test, env) else node.orelse, env)
        if isinstance(node, ast.Compare) and len(node.ops) == 1 and type(node.ops[0]) in COMPARE:
            return COMPARE[type(node.ops[0])](self.value(node.left, env), self.value(node.comparators[0], env))
        if isinstance(node, ast.Lambda):
            return Closure(node, env)
        if isinstance(node, ast.Call):
            args = [self.value(a, env) for a in node.args]
            kwargs: dict = {}
            for kw in node.keywords:
                if kw.arg is None:
                    kwargs.update(self.value(kw.value, env))
                else:
                    kwargs[kw.arg] = self.value(kw.value, env)
            f = node.func
            if isinstance(f, ast.Attribute) and isinstance(f.value, ast.Name) and f.value.id == "coa" and f.attr == "label":
                return self.labels[args[0]]
            if isinstance(f, ast.Name):
                if f.id == "D":
                    return dt.date(*args)
                if f.id == "row":
                    return {"_status": args[0], **kwargs}
                if f.id == "dict":
                    return dict(kwargs)
                if f.id == "blank_data":
                    return {"settings": {}, "opening": {}}
                fn = env.get(f.id)
                if isinstance(fn, Closure) and not kwargs:
                    names = [a.arg for a in fn.node.args.args]
                    return self.value(fn.node.body, {**fn.env, **dict(zip(names, args, strict=True))})
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
        self.labels = {r[0]: f"{r[0]} - {r[1]}" for r in coa.env["COA"]}  # coa.label(code)

    def gst(self, account: str, default: str) -> str:
        """The account's default GST code, or default for an account the chart does not have."""
        return self.rows[account][7] if account in self.rows else default

    def type(self, account: str) -> str:
        return TYPES[self.rows[account][2]]

    def pl(self, account: str) -> bool:
        """Whether the account is an income or expense account, the only kind a spread takes (FR-GL-03)."""
        return account in self.rows and self.type(account) in ("revenue", "expense")

    def accounts(self, extra: list[dict], revalue: tuple[str, ...] = ()) -> list[dict]:
        out = []
        for c, r in self.rows.items():
            a = {"code": c, "type": TYPES[r[2]], "group": False, "active": True}
            if r[3] == "Share capital":
                a["role"] = "share-capital"  # INV-13: only the register and the opening balances post there
            if c in revalue:
                a["revalue"] = True
            out.append(a)
        return out + extra

    def money_accounts(self) -> dict[str, str]:
        return {c: "SGD" for c, r in self.rows.items() if r[6] == "Yes"}


ID = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$")
SPELLED = {"*": "-star", "?": "-q", "~": "tilde-", "<": "lt-", ">": "-gt", " ": "-"}


def doc_id(number) -> str:
    """A workbook document number as a scenario ID: the number itself, or, when it holds a character the format's IDs
    do not take (* ? ~ < > or a space), the number with each spelled out."""
    n = str(number).strip()
    if ID.match(n):
        return n
    out = re.sub(r"-+", "-", "".join(SPELLED.get(ch, ch) for ch in n)).strip("-")
    if not ID.match(out):
        raise ValueError(f"no scenario ID for {number!r}")
    return out


def unrepresentable(row: dict) -> str | None:
    """Why a ✖ row cannot be a document of the format, or None: a missing or unreadable date, amount or account is
    refused by the API's input checks (ENG-23, ENG-44) before anything posts."""
    s = row.get("_status", "")
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


def document_total(doc: dict, registered: bool) -> Decimal:
    """What a document owes in its currency: its nets and, for an SGD document coded SR while registered, 9% GST on
    them, or the GST its lines state."""
    total = Decimal("0")
    for ln in doc["lines"]:
        net = Decimal(ln["amount"]) if "amount" in ln else (Decimal(ln["quantity"]) * Decimal(ln["unit_price"]) * (1 - Decimal(ln.get("discount", "0")) / 100))
        total += net.quantize(Decimal("0.01"))
        if "gst" in ln:
            total += Decimal(ln["gst"])
        elif ln["gst_code"] == "SR" and registered:
            total += (net * Decimal("0.09")).quantize(Decimal("0.01"))
    return total


def ledger(name: str, data: dict, chart: Chart, *, as_of: dt.date | None = None, revalue: bool = False) -> tuple[dict, list[dict]]:
    """A workbook data set of the posting tables (Transactions, Invoices, Bills, Doc Lines, Journal, Assets) as one
    scenario, and where each row went. A row's `_status`, when it has one, is the status the workbook shows."""
    settings = data["settings"]
    books_start = settings["S_BooksStart"]
    registered = settings.get("S_GSTReg") == "Yes"
    for k in ("transactions", "invoices", "bills", "doc_lines", "journal", "assets", "payroll", "quotes"):
        data.setdefault(k, [])
    docs: list[dict] = []
    mapping: list[dict] = []
    rates: dict[tuple[str, str], list[Decimal]] = {}  # (currency, month) -> the rates of the month's table rows
    for r in data.get("fx", []):
        rates.setdefault((r["ccy"], r["month"].strftime("%Y-%m")), []).append(Decimal(str(r["rate"])))

    def note(sheet: str, i: int, row: dict, *, document: str | None = None, reason: str | None = None, read: str | None = None) -> None:
        m = {"sheet": sheet, "row": i + 1, "status": row.get("_status", "")}
        if row.get("_fixture"):
            m["fixture"] = row["_fixture"]  # the workbook test's fixture the row comes from
        for k in ("no", "entry", "name", "emp", "desc"):
            if row.get(k) not in (None, ""):
                m["workbook"] = str(row[k])
                break
        m["document" if document else "not_ported"] = document or reason
        if read:
            m["read"] = read  # how Fyniq reads the row where it differs from what was typed
        mapping.append(m)

    def before_books(row: dict) -> bool:
        return isinstance(row.get("date"), dt.date) and row["date"] < books_start and row.get("opening") != "Yes"

    def spelled(number) -> str | None:
        n = doc_id(number)
        if n != str(number).strip():
            return f"its number {str(number)!r} is the scenario's {n}: the format's IDs take no such character; the bank matcher's test names the number itself"
        return None

    # The opening position, each balance on its side; an opening item posts its own Dr 1100 against 3900 Opening balance
    # clearing (rev 0.257), so when there is one 3900 takes the receivable's part of the opening balances.
    opening_items = any(d.get("opening") == "Yes" for d in data["invoices"])
    if data["opening"]:
        ob = []
        for a, v in data["opening"].items():
            a = str(a)
            if a == "1100" and opening_items:
                a = "3900"
            debit = a == "3900" or chart.type(a) in ("asset", "expense")
            ob.append({"account": a, "currency": "SGD", "amount": money(v if debit else -v)})
        docs.append({"type": "opening_balances", "id": "OB", "version": 1, "date": books_start.isoformat(),
                     "memo": "The data set's opening position", "lines": ob})

    # Bank lines, transfers, and the payments of documents.
    numbers = {str(d.get("no")).strip() for k in ("invoices", "bills") for d in data[k] if not d.get("_status", "").startswith("✖")}
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
        if cat in ("1100", "2010"):
            sales = cat == "1100"
            ref = str(t.get("ref") or "").strip()
            if ref in numbers:
                read = "matched with the spaces around its reference trimmed, as Fyniq's matcher does" if str(t["ref"]) != ref else spelled(ref)
                doc = {"type": "customer_receipt" if sales else "bill_payment", "id": f"{'RC' if sales else 'PY'}-{doc_id(ref)}",
                       "version": 1, "date": t["date"].isoformat(), "account": bank, "pays": ref}
            elif not ref and t.get("contact"):
                read = "recorded on account for its contact (FR-AR-18): a payment with no invoice yet"
                doc = {"type": "customer_receipt" if sales else "bill_payment", "id": f"{'RC' if sales else 'PY'}-ACCT-{i + 1:02d}",
                       "version": 1, "date": t["date"].isoformat(), "account": bank, "currency": "SGD", "parts": [],
                       "unapplied": money(abs(gross))}
            else:
                note("Transactions", i, t, reason="its reference names no document: the line waits for its match (FR-BNK-07)")
                continue
            docs.append(doc)
            note("Transactions", i, t, document=doc["id"], read=read)
            continue
        if cat in chart.money_accounts() and cat != bank:
            src, dst = (bank, cat) if gross < 0 else (cat, bank)
            doc = {"type": "bank_transfer", "id": f"TR-{i + 1:02d}", "version": 1, "date": t["date"].isoformat(),
                   "from": {"account": src, "currency": "SGD", "amount": money(abs(gross))},
                   "to": {"account": dst, "currency": "SGD", "amount": money(abs(gross))}}
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

    def lines_of(no, account: str, currency: str, rate: Decimal) -> list[dict]:
        """A document's Doc Lines: each at its item's price (converted at the document's rate when the document is in
        another currency), or its own; a line of quantity 0 is left out, since Fyniq refuses a line of nothing."""
        out = []
        for dl in data["doc_lines"]:
            if dl["doc"] != no:
                continue
            qty = dl.get("qty", 1)
            if qty == 0:
                continue
            price = Decimal(str(dl["price"])) if "price" in dl else Decimal(str(chart.items[code(dl["item"])][5]))
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
            doc = {"type": "invoice" if kind == "sales" else "bill", "id": doc_id(d["no"]), "version": 1, "date": d["date"].isoformat(),
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
                if kind == "purchases" and code_ in ("TX", "IM") and registered:
                    line["gst"] = money(Decimal(str(amount)) * Decimal("0.09"))
                lines = [line]
            reads = [spelled(d["no"])]
            if d.get("months") and chart.pl(account):
                lines[0]["spread_months"] = d["months"]
                if d.get("svcstart"):
                    lines[0]["spread_from"] = d["svcstart"].isoformat()
            elif d.get("months"):
                reads.append("not spread: Fyniq offers a spread only on an income or expense account (FR-GL-03)")
            if amount is not None and any(dl["doc"] == d["no"] for dl in data["doc_lines"]):
                reads.append("its lines, not the amount typed: a Fyniq document's total is its lines' (FR-AR-01)")
            if any(dl["doc"] == d["no"] and dl.get("qty", 1) == 0 for dl in data["doc_lines"]):
                reads.append("without its line of quantity 0, which the API refuses (a line of nothing); the total is the same")
            if d.get("opening") == "Yes":
                doc["opening_item"] = True
            doc["lines"] = lines
            docs.append(doc)
            note(sheet, i, d, document=doc["id"], read="; ".join(r for r in reads if r) or None)

    # The payments found above, now that their documents are known: each pays its document. A document in another
    # currency paid from an SGD account gives what the account paid, its base; an SGD one paid a little short within the
    # S$1.00 tolerance is settled in full (FR-BNK-08).
    by_id = {d["id"]: d for d in docs}
    for p in docs:
        ref = p.pop("pays", None)
        if ref is None:
            continue
        target = by_id[doc_id(ref)]
        t = next(t for t in data["transactions"] if str(t.get("ref") or "").strip() == ref and t["date"].isoformat() == p["date"])
        paid = Decimal(str(t.get("in", 0) or t.get("out", 0)))
        owed = document_total(target, registered)
        p["currency"] = target["currency"]
        if target["currency"] != "SGD":
            p["base_amount"] = money(paid)
            p["parts"] = [{"document": target["id"], "amount": money(owed)}]
        elif paid > owed:
            p["parts"] = [{"document": target["id"], "amount": money(owed)}]
            p["unapplied"] = money(paid - owed)
        else:
            p["parts"] = [{"document": target["id"], "amount": money(paid)}]
            if paid < owed and owed - paid <= Decimal("1.00"):
                p["parts"][0]["in_full"] = True

    # Journal entries: an entry with a line the API refuses is refused whole, as the workbook's is.
    entries: dict[str, list] = {}
    for i, j in enumerate(data["journal"]):
        if not j.get("entry"):
            note("Journal", i, j, reason=unrepresentable(j) or "no entry")
            continue
        entries.setdefault(j["entry"], []).append((i, j))
    for entry, rows in list(entries.items()):
        why = next((unrepresentable(j) for _, j in rows if unrepresentable(j) and "Another line" not in j["_status"]), None)
        if why is None and any(not isinstance(j.get("date"), dt.date) for _, j in rows):
            why = "an input the API refuses before posting (ENG-23, ENG-44): a journal has one date"
        if why is None and len({j["date"] for _, j in rows}) > 1:
            why = "a Fyniq journal has one date, so its lines cannot have two (FR-GL-01)"
        if why or any(before_books(j) for _, j in rows):
            for i, j in rows:
                note("Journal", i, j, reason=why or "Fyniq has no period before the books start: what came before is in the opening balances (FR-GL-06)")
            del entries[entry]
    for entry, rows in entries.items():
        lines = [{"account": code(j["acct"]), "currency": "SGD", "amount": money(Decimal(str(j.get("dr", 0))) - Decimal(str(j.get("cr", 0))))}
                 for _, j in rows]
        docs.append({"type": "manual_journal", "id": doc_id(entry), "version": 1, "date": rows[0][1]["date"].isoformat(),
                     "memo": rows[0][1].get("desc", ""), "lines": lines})
        for i, j in rows:
            note("Journal", i, j, document=doc_id(entry), read=spelled(entry))

    for i, ts in enumerate(data.get("timesheets", [])):
        note("Timesheets", i, ts, reason="projects and timesheets are not in the launch: these cases come with them")
    for i, b in enumerate(data.get("bank_import", [])):
        note("Bank Import", i, b, reason="a statement import, not a posting: Fyniq's import tests see a line already recorded (FR-BNK-04)")
    for i, b in enumerate(data.get("budget", [])):
        note("Budget", i, b, reason="a budget, not a posting: the planning tests hold budgets (FR-PLN-01)")
    for i, p in enumerate(data["payroll"]):
        note("Payroll", i, p, reason="pay runs (FR-PAY-01 to FR-PAY-03) are not in the launch: these cases come with them")
    for i, q in enumerate(data["quotes"]):
        status, q = q if isinstance(q, list) else (q.get("_status", ""), q)
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
    first = [d for d in docs if d["type"] == "opening_balances"]
    rest = [d for d in docs if d["type"] != "opening_balances"]
    order = {id(d): n for n, d in enumerate(rest)}
    rest.sort(key=lambda d: (d["date"], d["type"] in ("customer_receipt", "bill_payment", "asset_disposal"), order[id(d)]))
    as_of = as_of or max(dt.date.fromisoformat(d["date"]) for d in docs)
    extra = [{"code": "3900", "type": "equity", "group": False, "active": True}]
    accounts = {"receivable": "1100", "payable": "2010", "gst": "2200", "prepayments": "1210", "deferred_revenue": "2400",
                "fx": "7030", "bank_charges": "6460", "depreciation": "6610", "accumulated_depreciation": "1590",
                "disposal": "7040", "opening_clearing": "3900"}
    rate = settings.get("S_GSTRate", 0.09)
    gst: dict = {"rates": [{"from": "2024-01-01", "percent": str(int(Decimal(str(rate)) * 100))}]}
    if registered:
        # The workbook's registration is a Yes with no date (S_GSTStart starts the return's period): registered
        # throughout, an opening item's date included.
        gst["registered_from"] = "2025-01-01"
    scenario = {
        "schema": "fyniq-scenario@1",
        "name": name,
        "base_currency": "SGD",
        "currencies": {"SGD": 2, "USD": 2},
        "accounts": chart.accounts(extra),
        "settings": {
            "books_start": books_start.isoformat(),
            "first_year_end": "2026-12-31",
            "accounts": accounts,
            "money_accounts": chart.money_accounts(),
            "gst": gst,
        },
        "as_of": as_of.isoformat(),
        "documents": first + rest,
        "reversals": [],
    }
    if revalue:
        # Each month's end revalues the receivable at the table's latest rate on or before it, as the workbook's memo
        # takes the latest monthly rate; the unrealised FX account is Fyniq's (the workbook's 7040 is its disposal account).
        scenario["accounts"] = chart.accounts(extra + [{"code": "7045", "type": "revenue", "group": False, "active": True}], revalue=("1100", "2010"))
        accounts["unrealised_fx"] = "7045"
        months = sorted({m for _, m in rates})
        ends = []
        y, mo = map(int, months[0].split("-"))
        while dt.date(y, mo, 1) <= as_of:
            end = dt.date(y + mo // 12, mo % 12 + 1, 1) - dt.timedelta(days=1)
            if end <= as_of:
                latest = max(m for c, m in rates if m <= end.strftime("%Y-%m"))
                found = rates[("USD", latest)]
                ends.append({"date": end.isoformat(), "currency": "USD", "fx_rate": str(sum(found) / len(found)),
                             "fx_rate_source": f"the workbook's latest monthly rate, {latest}"})
            y, mo = y + mo // 12, mo % 12 + 1
        scenario["settings"]["period_end_rates"] = ends
    return scenario, mapping


def edge_set(edge: Module) -> dict:
    """The posting edge cases' data (tests/edge_posting.py): edge_data() without its employees."""
    fn = next(n for n in edge.tree.body if isinstance(n, ast.FunctionDef) and n.name == "edge_data")
    ret = next(n.value for n in ast.walk(fn) if isinstance(n, ast.Return))
    wb = {edge.value(k, edge.env): v for k, v in zip(ret.keys, ret.values)}
    data = {k: edge.value(wb[k], edge.env) for k in ("settings", "opening", "fx")}
    for k in ("transactions", "invoices", "bills", "doc_lines", "journal", "payroll", "assets"):
        data[k] = edge.function(f"_{k}")
    data["quotes"] = edge.env["QUOTES"]
    return data


STATUS = ("✖", "⚠", "✔", "⏰", "✎")


def statuses(edge: Path, tests: Path) -> list[dict]:
    """The non-posting set's cases, read from its syntax trees without running them: each status text a top-level
    block of tests/edge_nonposting.py holds (a text built with {…} by its fixed start), and each of
    tests/test_edge_nonposting.py's test_bug_ tests, the cases the workbook once got wrong."""
    out: list[dict] = []
    tree = ast.parse(edge.read_text(encoding="utf-8"))
    for node in tree.body:
        if isinstance(node, ast.Assign) and isinstance(node.targets[0], ast.Name):
            block = node.targets[0].id
        elif isinstance(node, ast.FunctionDef):
            block = node.name
        else:
            continue
        for sub in ast.walk(node):
            text = None
            if isinstance(sub, ast.Constant) and isinstance(sub.value, str):
                text = sub.value
            elif isinstance(sub, ast.JoinedStr) and sub.values and isinstance(sub.values[0], ast.Constant):
                text = str(sub.values[0].value).rstrip()
            text = (text or "").strip()
            if text.startswith(STATUS) and any(ch.isalpha() for ch in text):
                case = {"block": block, "status": text}
                if case not in out:
                    out.append(case)
    for node in ast.parse(tests.read_text(encoding="utf-8")).body:
        if isinstance(node, ast.FunctionDef) and node.name.startswith("test_bug_"):
            out.append({"block": "tests/test_edge_nonposting.py", "status": node.name})
    return out


def sha256(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("repo", type=Path, help="a checkout of zh-sheet-accounting at the commit to pin")
    ap.add_argument("--out", type=Path, default=Path("test/golden/workbook"))
    a = ap.parse_args()
    commit = subprocess.run(["git", "-C", str(a.repo), "rev-parse", "HEAD"], capture_output=True, text=True, check=True).stdout.strip()
    chart = Chart(Module(a.repo / "src/cfh/coa.py"))
    labels = chart.labels

    def tests(name: str) -> Module:
        return Module(a.repo / "tests" / name, labels)

    def regressions() -> dict:
        """tests/test_regressions.py's two fixtures as one set: neither touches the other's accounts."""
        m = tests("test_regressions.py")
        refs, dates = m.function("spaced_refs"), m.function("two_dates")
        data = {"settings": dict(refs["settings"]), "opening": dates["opening"]}
        for k in ("transactions", "invoices", "bills", "journal"):
            data[k] = [{**r, "_fixture": "spaced_refs"} for r in refs.get(k, [])] + [{**r, "_fixture": "two_dates"} for r in dates.get(k, [])]
        data["settings"]["S_AsOf"] = dates["settings"]["S_AsOf"]
        return data

    sets = {
        "posting": (["tests/edge_posting.py", "tests/test_edge_posting.py"],
                    lambda: ledger("workbook-posting-edge-cases", edge_set(tests("edge_posting.py")), chart)),
        "document-numbers": (["tests/test_edge_docnumbers.py"],
                             lambda: ledger("workbook-document-numbers", tests("test_edge_docnumbers.py").function("docno_data"), chart)),
        "regressions": (["tests/test_regressions.py"], lambda: ledger("workbook-regressions", regressions(), chart)),
    }
    # The non-posting set posts nothing: its cases are listed (statuses.json), and mapping.json, kept by hand, gives
    # each the Fyniq test that holds the same rule, or why it waits for after the launch or has no counterpart.
    def nonposting() -> dict:
        return {"statuses.json": statuses(a.repo / "tests/edge_nonposting.py", a.repo / "tests/test_edge_nonposting.py")}

    sets["non-posting"] = (["tests/edge_nonposting.py", "tests/test_edge_nonposting.py"], nonposting)

    def misc() -> dict:
        """The miscellaneous set: its scenario and mapping, and its tests by name (functions.json), each of which
        cases.json, kept by hand, says is ported as figures or held by a Fyniq test."""
        scenario, mapping = ledger("workbook-miscellaneous", tests("test_edge_misc.py").function("misc_data"), chart,
                                   as_of=dt.date(2026, 10, 1), revalue=True)
        tree = ast.parse((a.repo / "tests/test_edge_misc.py").read_text(encoding="utf-8"))
        functions = [{"block": "tests/test_edge_misc.py", "status": n.name} for n in tree.body
                     if isinstance(n, ast.FunctionDef) and n.name.startswith("test_")]
        return {"scenario.json": scenario, "mapping.json": mapping, "functions.json": functions}

    # As of 1 October, a day past the workbook's, so September's revaluation is seen reversed on day 1 (FR-GL-04).
    sets["miscellaneous"] = (["tests/test_edge_misc.py"], misc)
    record = {"repository": REPOSITORY, "commit": commit, "sets": {}}
    for name, (files, build) in sets.items():
        built = build()
        out = a.out / name
        out.mkdir(parents=True, exist_ok=True)
        if isinstance(built, dict):
            files_out = built
        else:
            scenario, mapping = built
            files_out = {"scenario.json": scenario, "mapping.json": mapping}
        for fname, content in files_out.items():
            (out / fname).write_text(json.dumps(content, indent=1, ensure_ascii=False) + "\n", encoding="utf-8")
        fixtures = sorted(p for p in out.iterdir() if p.suffix == ".json")
        record["sets"][name] = {
            "sources": {f: sha256(a.repo / f) for f in files + ["src/cfh/coa.py"]},
            "fixtures": {f"{name}/{p.name}": sha256(p) for p in fixtures},
        }
    (a.out / "sources.json").write_text(json.dumps(record, indent=1) + "\n", encoding="utf-8")
    print(f"wrote {a.out} from {REPOSITORY}@{commit[:12]}", file=sys.stderr)


if __name__ == "__main__":
    main()
