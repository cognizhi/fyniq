"""The visual snapshots' baselines (QA-UI-07, QA-RPT-04; p1-launch-readiness design D5): apps/web/visual/baselines.json
maps each picture, page/frame/theme, to its SHA-256. A run of `make test-visual` leaves every hash it saw in
apps/web/test-results/visual/hashes-*.json (CI uploads them); approving a change takes them into the baselines, in a pull
request labelled visual-baseline. Standard library only.

    python3 scripts/visual.py approve [DIR]   # take DIR's hashes (default: apps/web/test-results/visual) into the baselines
    python3 scripts/visual.py check           # the baselines are well formed (part of make check)
"""
import json
import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
BASELINES = ROOT / "apps/web/visual/baselines.json"
RUN = ROOT / "apps/web/test-results/visual"
KEY = re.compile(r"^[a-z0-9-]+/(desktop|tablet|foldable-open|phone|foldable-folded)/(light|dark)$")
HASH = re.compile(r"^[0-9a-f]{64}$")


def problems(baselines: object) -> list[str]:
    """What is wrong with a baselines document: not an object, or a key or hash of the wrong shape."""
    if not isinstance(baselines, dict):
        return ["the baselines are not a JSON object of picture → SHA-256"]
    out = []
    for k, v in baselines.items():
        if not KEY.match(k):
            out.append(f"{k}: not page/frame/theme (frames: desktop, tablet, foldable-open, phone, foldable-folded; themes: light, dark)")
        if not isinstance(v, str) or not HASH.match(v):
            out.append(f"{k}: {v!r} is not a SHA-256 in hex")
    return out


def approve(run: pathlib.Path, baselines: dict) -> dict:
    """The baselines with every hash a run saw taken in, sorted by picture."""
    merged = dict(baselines)
    for f in sorted(run.glob("hashes-*.json")):
        merged.update(json.loads(f.read_text()))
    return dict(sorted(merged.items()))


def main(argv: list[str]) -> int:
    if len(argv) < 2 or argv[1] not in ("approve", "check"):
        print(__doc__)
        return 2
    current = json.loads(BASELINES.read_text())
    if argv[1] == "check":
        bad = problems(current)
        for p in bad:
            print(f"✖ {BASELINES.relative_to(ROOT)}: {p}")
        if not bad:
            print(f"✔ visual baselines: {len(current)} pictures")
        return 1 if bad else 0
    run = pathlib.Path(argv[2]) if len(argv) > 2 else RUN
    merged = approve(run, current)
    bad = problems(merged)
    if bad:
        for p in bad:
            print(f"✖ {p}")
        return 1
    BASELINES.write_text(json.dumps(merged, indent=2) + "\n")
    print(f"✔ {len(merged) - len(current)} new and {sum(1 for k in current if merged[k] != current[k])} changed pictures taken into "
          f"{BASELINES.relative_to(ROOT)}: open the pull request with the label visual-baseline")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
