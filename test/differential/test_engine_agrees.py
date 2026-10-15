"""The full differential run (QA-ENG-02, `make differential`, part of `make ci`).

Hypothesis searches the oracle's scenario strategies for one on which the Go engine
(`cmd/scenario-run`, built by the Makefile into $FYNIQ_SCENARIO_RUN) and the oracle disagree. When it
finds one, it shrinks it to the smallest disagreeing scenario, which is saved as a new golden case in
test/differential/golden/ and fails the run (task 3.1). Harness code: it calls only the oracle's
public entry points and the engine's command line.
"""

import hashlib
import json
import os
import subprocess
import tempfile
from decimal import Decimal
from pathlib import Path

from hypothesis import HealthCheck, find, settings
from hypothesis.errors import NoSuchExample

from oracle.ledger import Ledger
from oracle.scenario import ScenarioError, scenario_from_json
from oracle.strategies import scenarios

GOLDEN = Path(__file__).parent / "golden"
EXAMPLES = int(os.environ.get("FYNIQ_DIFFERENTIAL_EXAMPLES", "1000"))


def engine(scenario: dict) -> dict:
    with tempfile.NamedTemporaryFile("w", suffix=".json", delete=False) as f:
        json.dump(scenario, f)
    try:
        run = subprocess.run([os.environ["FYNIQ_SCENARIO_RUN"], f.name], capture_output=True, text=True, check=False)
    finally:
        os.unlink(f.name)
    if run.returncode == 2:
        return {"invalid": run.stderr.strip()}
    if run.returncode != 0:
        raise RuntimeError(f"scenario-run failed: {run.stderr}")
    return json.loads(run.stdout)


def oracle(scenario: dict) -> dict:
    try:
        return Ledger(scenario_from_json(scenario)).result()
    except ScenarioError as exc:
        return {"invalid": str(exc)}


def same(a: str | None, b: str | None) -> bool:
    return Decimal(a or "0") == Decimal(b or "0")


def difference(e: dict, o: dict) -> str | None:
    """The first difference to the cent, read as test/differential/README.md says."""
    if ("invalid" in e) != ("invalid" in o):
        return f"one side found the scenario invalid: engine {e.get('invalid')!r}, oracle {o.get('invalid')!r}"
    if "invalid" in e:
        return None
    if e["rejected"] != o["rejected"]:
        return f"rejected: engine {e['rejected']}, oracle {o['rejected']}"
    if len(e["entries"]) != len(o["entries"]):
        return f"entries: engine {len(e['entries'])}, oracle {len(o['entries'])}"
    for i, (x, y) in enumerate(zip(e["entries"], o["entries"]), 1):
        if (x["document"], x["kind"], x["posting_date"]) != (y["document"], y["kind"], y["posting_date"]):
            return f"entry {i}: engine {x['kind']} {x['document']} on {x['posting_date']}, oracle {y['kind']} {y['document']} on {y['posting_date']}"
        if len(x["lines"]) != len(y["lines"]):
            return f"entry {i}: engine {len(x['lines'])} lines, oracle {len(y['lines'])}"
        for k, (a, b) in enumerate(zip(x["lines"], y["lines"]), 1):
            if (a["account"], a["currency"]) != (b["account"], b["currency"]) or not same(a["amount"], b["amount"]) \
                    or not same(a["base_amount"], b["base_amount"]):
                return f"entry {i} line {k}: engine {a}, oracle {b}"
    for month in sorted(set(e["trial_balance"]) | set(o["trial_balance"])):
        ea, oa = e["trial_balance"].get(month, {}), o["trial_balance"].get(month, {})
        for account in sorted(set(ea) | set(oa)):
            if not same(ea.get(account), oa.get(account)):
                return f"trial balance {month} account {account}: engine {ea.get(account, '0')}, oracle {oa.get(account, '0')}"
    return None


def test_the_engine_and_the_oracle_agree() -> None:  # req: QA-ENG-02
    try:
        bad = find(
            scenarios("differential"),
            lambda s: difference(engine(s), oracle(s)) is not None,
            settings=settings(max_examples=EXAMPLES, deadline=None, database=None,
                              suppress_health_check=list(HealthCheck)),
        )
    except NoSuchExample:
        return
    digest = hashlib.sha256(json.dumps(bad, sort_keys=True).encode()).hexdigest()[:10]
    bad["name"] = f"shrunk-{digest}"
    path = GOLDEN / f"{bad['name']}.json"
    path.write_text(json.dumps(bad, indent=1, ensure_ascii=False) + "\n", encoding="utf-8")
    raise AssertionError(f"the engine and the oracle disagree: {difference(engine(bad), oracle(bad))}\n"
                         f"the smallest such scenario is saved as {path}; commit it as a golden case")
