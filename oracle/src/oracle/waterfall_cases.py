"""Writes test/differential/waterfalls/cases.json: generated exit-waterfall structures and the reference's answer to each (FR-EQ-29)."""

import json
import sys
from pathlib import Path

from oracle.waterfall import waterfall  # noqa: F401  (the cases are made by the test module's generator)


def main() -> int:
    sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "tests"))
    from test_waterfall import _generated

    target = Path(__file__).resolve().parents[3] / "test" / "differential" / "waterfalls" / "cases.json"
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(json.dumps(_generated(), indent=1, sort_keys=True) + "\n")
    print(f"wrote {target}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
