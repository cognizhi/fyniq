"""Run the oracle on many scenario files in one process, for the Go differential gate.

Harness code, not the oracle: it only calls the oracle's public entry points. Each line of output is
JSON: {"file": <path>, "result": <fyniq-scenario-result@1>} or {"file": <path>, "invalid": <why>}. A fyniq-forecast@1
document gets its fyniq-forecast-result@1 instead.

    uv run --project oracle --locked python test/differential/oracle_batch.py <scenario.json>...
"""

import json
import sys

from pathlib import Path

from oracle.ledger import Ledger
from oracle.planning import ForecastError, forecast
from oracle.scenario import ScenarioError, load_scenario, parse_json


def main(paths: list[str]) -> int:
    for path in paths:
        try:
            doc = parse_json(Path(path).read_text(encoding="utf-8"))
            if isinstance(doc, dict) and doc.get("schema") == "fyniq-forecast@1":
                line = {"file": path, "result": forecast(doc)}
            else:
                line = {"file": path, "result": Ledger(load_scenario(path)).result()}
        except (ScenarioError, ForecastError) as exc:
            line = {"file": path, "invalid": str(exc)}
        sys.stdout.write(json.dumps(line, ensure_ascii=False) + "\n")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
