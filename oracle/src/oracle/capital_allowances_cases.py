"""Writes test/differential/capital-allowances/cases.json: generated capital allowance schedules and the reference's answer to each (FR-FA-04)."""

import json
import sys
from pathlib import Path

from oracle.capital_allowances import schedule  # noqa: F401  (the cases are made by the test module's generator)


def main() -> int:
    sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "tests"))
    from test_capital_allowances import _generated

    target = Path(__file__).resolve().parents[3] / "test" / "differential" / "capital-allowances" / "cases.json"
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(json.dumps(_generated(), indent=1, sort_keys=True) + "\n")
    print(f"wrote {target}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
