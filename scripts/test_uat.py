"""The UAT scripts in docs/uat/ (p1-launch-readiness task 7.1): every launch journey of QA-E2E-01 and every role of
PRD-001 §7.1's table "What each role sees" has its script; each step names a screen of the inventory, what to see and
the evidence to keep; and each script ends with the founders' and the accountant's sign-off. Run with
`make script-tests` (part of `make check`). Standard library only."""
import json
import pathlib
import re
import sys
import tempfile
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))
import prdspec  # noqa: E402

ROOT = prdspec.ROOT
UAT = ROOT / "docs/uat"
PRD = ROOT / "docs/prd/prd-001-fyniq-platform.md"
INVENTORY = ROOT / "docs/design/screens/inventory.json"

# QA-E2E-01's journeys, as its PRD row names them, and each one's script; None is after go-live by the owner's
# decisions (rev 0.25): payroll with Nemura. The GST journey's launch part is the monitor and the calendar; its F5 comes
# at registration.
JOURNEYS = {
    "set-up": "setup",
    "record": "record",
    "sell": "sell",
    "buy": "buy",
    "pay": "pay",
    "payroll": None,
    "reconcile": "reconcile",
    "close": "close",
    "GST": "gst-monitor-calendar",
    "AI ask": "ai-ask",
    "AI propose → approve → execute": "ai-propose-approve-execute",
}
STEPS = ["#", "Who", "Screen", "Do", "What to see", "Evidence to keep"]
SIGNERS = ["Founder", "Founder", "Accountant"]


def e2e_journeys(prd_text):
    """The journeys QA-E2E-01's row lists."""
    m = re.search(r"^\| QA-E2E-01 \| [^|]*?key journeys: ([^|]+?) \|", prd_text, re.M)
    if not m:
        raise AssertionError("PRD-001 has no QA-E2E-01 row listing its key journeys")
    return [j.strip() for j in re.split(r", | and ", m.group(1)) if j.strip()]


def roles(prd_text):
    """The roles of §7.1's table "What each role sees", as it names them."""
    lines = prd_text.split("\n")
    start = next((i for i, l in enumerate(lines) if l.startswith("| Role | Workspace · lands on |")), None)
    if start is None:
        raise AssertionError("PRD-001 §7.1 has no table of what each role sees")
    out = []
    for line in lines[start + 2:]:
        if not line.startswith("|"):
            break
        out.append(prdspec.split_cells(line)[0])
    return out


def table(text, heading):
    """The rows of the first table under a `## heading`, as lists of cells, its header first."""
    m = re.search(rf"^## {re.escape(heading)}\n(.*?)(?=^## |\Z)", text, re.M | re.S)
    if not m:
        return []
    return [prdspec.split_cells(l) for l in m.group(1).split("\n") if l.startswith("|") and not re.match(r"^\|[-| :]+\|$", l)]


def problems(name, text, screens):
    """What is wrong with one script: its steps, their screens, what to see, the evidence and the sign-off."""
    out = []
    rows = table(text, "Steps")
    if not rows or rows[0] != STEPS:
        return [f"{name}: no steps table with the columns {' | '.join(STEPS)}"]
    if len(rows) < 4:
        out.append(f"{name}: fewer than three steps")
    for n, r in enumerate(rows[1:], 1):
        if len(r) != len(STEPS):
            out.append(f"{name} step {n}: {len(r)} cells, not {len(STEPS)}")
            continue
        if r[0] != str(n):
            out.append(f"{name} step {n}: numbered {r[0]!r}")
        named = re.findall(r"`([a-z0-9-]+)`", r[2])
        if not named:
            out.append(f"{name} step {n}: names no screen")
        out += [f"{name} step {n}: {s!r} is not a screen of the inventory" for s in named if s not in screens]
        for col in (1, 3, 4, 5):
            if not r[col]:
                out.append(f"{name} step {n}: no {STEPS[col].lower()}")
    signed = table(text, "Sign-off")
    if not signed or [r[0] for r in signed[1:]] != SIGNERS:
        out.append(f"{name}: no sign-off by the two founders and the accountant")
    return out


def check(uat=UAT, prd_text=None, screens=None):
    """Every problem of the UAT scripts, as lines; empty when they are complete."""
    prd_text = PRD.read_text(encoding="utf-8") if prd_text is None else prd_text
    if screens is None:
        screens = {s["id"] for s in json.loads(INVENTORY.read_text(encoding="utf-8"))["screens"]}
    out = []
    listed = e2e_journeys(prd_text)
    if sorted(listed) != sorted(JOURNEYS):
        out.append(f"QA-E2E-01's journeys {listed} are not the ones mapped here {sorted(JOURNEYS)}: map each to its script")
    readme = (uat / "README.md").read_text(encoding="utf-8") if (uat / "README.md").exists() else ""
    if not re.search(r"payroll's journey and the GST F5's.{0,40}after go-live.{0,40}rev 0\.25", readme, re.S):
        out.append("README.md: does not record payroll's journey and the GST F5's as after go-live (rev 0.25)")
    expected = {}
    for journey, slug in JOURNEYS.items():
        if slug:
            expected[f"journeys/{slug}.md"] = ("Journey", None)
    for label in roles(prd_text):
        slug = re.sub(r"[^a-z0-9]+", "-", re.split(r" \(|:", label)[0].lower()).strip("-")
        expected[f"roles/{slug}.md"] = ("Role", label)
    for rel, (kind, label) in sorted(expected.items()):
        path = uat / rel
        if not path.exists():
            out.append(f"{rel}: missing, the script of {label or rel}")
            continue
        if f"]({rel})" not in readme:
            out.append(f"README.md: does not link {rel}")
        text = path.read_text(encoding="utf-8")
        head = re.search(rf"^- \*\*{kind}:\*\* (.+)$", text, re.M)
        if not head:
            out.append(f"{rel}: no '- **{kind}:**' line")
        elif label and head.group(1) != label:
            out.append(f"{rel}: its role is {head.group(1)!r}, not §7.1's {label!r}")
        out += problems(rel, text, screens)
    for path in sorted(uat.glob("*/*.md")):
        rel = path.relative_to(uat).as_posix()
        if rel not in expected:
            out.append(f"{rel}: neither a launch journey of QA-E2E-01 nor a role of §7.1")
    return out


SCRIPT = """# UAT: planted

- **Journey:** sell (QA-E2E-01)

## Steps

| # | Who | Screen | Do | What to see | Evidence to keep |
|---|---|---|---|---|---|
| 1 | Bea | `contacts-list` | Add a customer. | It is listed. | Screenshot |
| 2 | Bea | `invoice-editor` | Prepare an invoice. | A draft. | Screenshot |
| 3 | Cal | `invoice-record` | Issue it. | INV-0001. | Screenshot |

## Sign-off

| Who | Name | Date | Result |
|---|---|---|---|
| Founder | | | |
| Founder | | | |
| Accountant | | | |
"""


class TheUATScripts(unittest.TestCase):

    def test_every_launch_journey_and_role_has_its_complete_script(self):
        self.assertEqual(check(), [])

    def test_the_prds_journeys_and_roles_are_read(self):
        text = PRD.read_text(encoding="utf-8")
        self.assertIn("AI propose → approve → execute", e2e_journeys(text))
        got = roles(text)
        self.assertEqual((got[0], got[-1], len(got)), ("Employee", "HR officer (Nemura only)", 18))

    def test_a_planted_script_fails_on_each_gap(self):
        screens = {"contacts-list", "invoice-editor", "invoice-record"}
        self.assertEqual(problems("p.md", SCRIPT, screens), [])
        cases = {
            "an unknown screen": (SCRIPT.replace("`invoice-record`", "`invoice-page`"), "'invoice-page' is not a screen"),
            "a step with no screen": (SCRIPT.replace("`invoice-record`", "the invoice"), "step 3: names no screen"),
            "nothing to see": (SCRIPT.replace("| It is listed. |", "|  |"), "step 1: no what to see"),
            "no evidence": (SCRIPT.replace("| A draft. | Screenshot |", "| A draft. |  |"), "step 2: no evidence to keep"),
            "no accountant's sign-off": (SCRIPT.replace("| Accountant | | | |\n", ""), "no sign-off"),
            "a step out of order": (SCRIPT.replace("| 3 | Cal |", "| 4 | Cal |"), "step 3: numbered '4'"),
        }
        for what, (text, want) in cases.items():
            with self.subTest(what):
                self.assertTrue(any(want in p for p in problems("p.md", text, screens)), problems("p.md", text, screens))

    def test_a_missing_role_or_journey_fails(self):
        d = pathlib.Path(tempfile.mkdtemp())
        (d / "journeys").mkdir()
        (d / "README.md").write_text("payroll's journey and the GST F5's. Both are after go-live by the owner's decisions "
                                     "(rev 0.25)\n[sell](journeys/sell.md)\n", encoding="utf-8")
        (d / "journeys/sell.md").write_text(SCRIPT, encoding="utf-8")
        (d / "journeys/payroll.md").write_text(SCRIPT, encoding="utf-8")
        got = check(d, screens={"contacts-list", "invoice-editor", "invoice-record"})
        self.assertIn("journeys/close.md: missing, the script of journeys/close.md", got)
        self.assertIn("roles/employee.md: missing, the script of Employee", got)
        self.assertIn("journeys/payroll.md: neither a launch journey of QA-E2E-01 nor a role of §7.1", got)
        self.assertFalse(any(p.startswith("journeys/sell.md") for p in got), got)


if __name__ == "__main__":
    unittest.main()
