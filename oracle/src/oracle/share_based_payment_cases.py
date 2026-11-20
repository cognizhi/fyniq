"""Writes test/differential/share-services/generated.json: generated grants of shares for services and the reference's answer to each (FR-EQ-13)."""

import json
import sys
from pathlib import Path


def main() -> int:
    sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "tests"))
    from test_share_based_payment import _generated

    target = Path(__file__).resolve().parents[3] / "test" / "differential" / "share-services" / "generated.json"
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(json.dumps(_generated(), indent=1, sort_keys=True) + "\n")
    print(f"wrote {target}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
