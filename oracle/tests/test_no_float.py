"""ENG-10: no binary floating point anywhere in the oracle, its tests included."""

import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
WORD = "fl" + "oat"  # spelled apart so this file does not match itself


# req: QA-ENG-01
def test_no_source_file_names_the_float_type():
    pattern = re.compile(rf"\b{WORD}\b")
    files = sorted((ROOT / "src").rglob("*.py")) + sorted((ROOT / "tests").rglob("*.py"))
    assert len(files) > 5
    offenders = [
        f"{path.relative_to(ROOT)}:{n}"
        for path in files
        for n, text in enumerate(path.read_text().splitlines(), 1)
        if pattern.search(text)
    ]
    assert offenders == []
