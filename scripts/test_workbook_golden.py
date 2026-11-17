"""The workbook's golden sets reader (scripts/workbook_golden.py; p1-launch-readiness task 3.1, design D4).

Run with `make script-tests` (part of `make check`)."""
import datetime as dt
import pathlib
import sys
import tempfile
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))
import workbook_golden  # noqa: E402

EDGE = '''
import datetime as dt
D = dt.date
BANK = "1020 - Bank - Operating account"
LEAK = __import__("os").getcwd()


def row(status, **fields):
    return {"_status": status, **fields}


def _transactions():
    base = dict(bank=BANK)
    return [
        row("✔ OK", date=D(2026, 3, 2), desc="Sale", **{"in": 1090}, **base),
        row("✖ Enter the date", desc="No date", out=50, **base),
    ]


def _payroll():
    print("ran")
    return []
'''


class TheReader(unittest.TestCase):
    """It reads the workbook's data files as data: literals, dates and rows, and the assignments of a function before
    it returns them. Nothing in them runs (design D4)."""

    def module(self, text: str) -> workbook_golden.Module:
        with tempfile.TemporaryDirectory() as d:
            path = pathlib.Path(d) / "edge.py"
            path.write_text(text, encoding="utf-8")
            return workbook_golden.Module(path)

    def test_rows_dates_and_unpacked_fields_are_read(self):
        m = self.module(EDGE)
        self.assertEqual(m.function("_transactions"), [
            {"_status": "✔ OK", "date": dt.date(2026, 3, 2), "desc": "Sale", "in": 1090, "bank": "1020 - Bank - Operating account"},
            {"_status": "✖ Enter the date", "desc": "No date", "out": 50, "bank": "1020 - Bank - Operating account"},
        ])

    def test_code_is_refused_not_run(self):
        m = self.module(EDGE)
        self.assertNotIn("LEAK", m.env)  # the import call was refused, so os never ran
        with self.assertRaises(workbook_golden.Refused):
            m.function("_payroll")  # a statement that is not an assignment or the return

    def test_amounts_and_codes(self):
        self.assertEqual(workbook_golden.money(1089.4), "1089.40")
        self.assertEqual(workbook_golden.money(-1000), "-1000.00")
        self.assertEqual(workbook_golden.code("6210 - Software & SaaS subscriptions"), "6210")
        self.assertEqual(workbook_golden.code(1100), "1100")

    def test_a_documents_total(self):
        invoice = {"lines": [{"amount": "1000.00", "gst_code": "SR"}, {"quantity": "1", "unit_price": "200.00", "discount": "25", "gst_code": "SR"}]}
        self.assertEqual(str(workbook_golden.document_total(invoice)), "1253.50")  # 1,150 + 9 % GST
        bill = {"lines": [{"amount": "3000.00", "gst_code": "TX", "gst": "270.00"}]}
        self.assertEqual(str(workbook_golden.document_total(bill)), "3270.00")


if __name__ == "__main__":
    unittest.main()
