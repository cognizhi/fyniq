#!/usr/bin/env python3
"""Validate the categorical chart palette in every theme (PRD-001 FR-VIZ-07; p1-reports task 5.1, design D7).

Reads --chart-1 … --chart-8 from tokens.css as each theme resolves them (light, dark, light high-contrast, dark
high-contrast) and runs the dataviz method's computable checks (MASTER.md §10), the thresholds and the colour-vision
simulation exactly as the method's validator defines them:

  lightness band        OKLCH L within 0.43–0.77 in a light theme, 0.48–0.67 in a dark one        FAIL outside
  chroma floor          OKLCH C ≥ 0.10, or a hue reads as grey                                     FAIL below
  CVD separation        adjacent slots under protanopia and deuteranopia (Machado, Oliveira and
                        Fernandes 2009, severity 1.0), Euclidean OKLab ΔE ×100: ≥ 8 target,
                        6–8 legal only with secondary encoding                                     FAIL below 6, WARN 6–8
  normal-vision floor   the same pairs unsimulated, ΔE ≥ 15                                        FAIL below
  contrast              each slot against the theme's chart surfaces (surface-1, background),
                        3:1; below it the values must be readable another way, which every chart
                        gives by its table view and direct labels (FR-VIZ-06)                      WARN below 3:1

Adjacent pairs, because the slots are assigned in fixed order and only neighbours touch in a stack, bar or line
(FR-VIZ-03); a scatter or map with more than three series folds into "Other" instead. Standard library only.

  python3 scripts/check_palette.py                 # report, exit 1 on any FAIL
  python3 scripts/check_palette.py --tokens PATH   # another tokens file
  python3 scripts/check_palette.py --self-test     # planted palettes are refused
"""
from __future__ import annotations

import argparse
import math
import re
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from check_contrast import DEFAULT_TOKENS, THEMES, contrast, parse_blocks, parse_color, resolve, to_hex  # noqa: E402

SLOTS = [f"chart-{i}" for i in range(1, 9)]
SURFACES = ["surface-1", "background"]
BAND = {False: (0.43, 0.77), True: (0.48, 0.67)}  # by dark
CHROMA_FLOOR = 0.10
CVD_TARGET, CVD_FLOOR = 8.0, 6.0
NORMAL_FLOOR = 15.0
CONTRAST_MIN = 3.0

# Machado, Oliveira and Fernandes (2009) at severity 1.0, on linear sRGB.
MACHADO = {
    "protan": ((0.152286, 1.052583, -0.204868), (0.114503, 0.786281, 0.099216), (-0.003882, -0.048116, 1.051998)),
    "deutan": ((0.367322, 0.860646, -0.227968), (0.280085, 0.672501, 0.047413), (-0.011820, 0.042940, 0.968881)),
}


def cbrt(x: float) -> float:
    return math.copysign(abs(x) ** (1 / 3), x)


def oklab(lin):
    r, g, b = lin
    l = cbrt(0.4122214708 * r + 0.5363325363 * g + 0.0514459929 * b)
    m = cbrt(0.2119034982 * r + 0.6806995451 * g + 0.1073969566 * b)
    s = cbrt(0.0883024619 * r + 0.2817188376 * g + 0.6299787005 * b)
    return (0.2104542553 * l + 0.7936177850 * m - 0.0040720468 * s,
            1.9779984951 * l - 2.4285922050 * m + 0.4505937099 * s,
            0.0259040371 * l + 0.7827717662 * m - 0.8086757660 * s)


def simulate(lin, kind):
    M = MACHADO[kind]
    return tuple(max(0.0, min(1.0, sum(M[i][j] * lin[j] for j in range(3)))) for i in range(3))


def delta_e(a, b, kind=None):
    if kind:
        a, b = simulate(a, kind), simulate(b, kind)
    x, y = oklab(a), oklab(b)
    return 100 * math.dist(x, y)


def check(tokens_path: Path) -> int:
    blocks = parse_blocks(tokens_path.read_text())
    failed = False
    for theme, t in THEMES.items():
        tokens = resolve(blocks, t["dark"], t["hc"])
        if not all(s in tokens for s in SLOTS):
            print(f"✖ {theme}: the tokens file does not define {', '.join(s for s in SLOTS if s not in tokens)}")
            failed = True
            continue

        def colour(name, seen=()):
            v = tokens[name].strip()
            if v.startswith("var(--") and v.endswith(")") and v[6:-1] not in seen:
                return colour(v[6:-1], (*seen, name))
            parsed = parse_color(v)
            if parsed is None:
                raise SystemExit(f"✖ {theme}: --{name} is not a colour ({v})")
            return parsed[0]

        slots = [colour(s) for s in SLOTS]
        surfaces = {s: colour(s) for s in SURFACES if s in tokens}
        lines = []
        lo, hi = BAND[t["dark"]]
        for name, lin in zip(SLOTS, slots):
            L, a, b = oklab(lin)
            C = math.hypot(a, b)
            if not lo <= L <= hi:
                lines.append(("FAIL", f"--{name} {to_hex(lin)}: lightness {L:.3f} outside {lo}–{hi}"))
            if C < CHROMA_FLOOR:
                lines.append(("FAIL", f"--{name} {to_hex(lin)}: chroma {C:.3f} below {CHROMA_FLOOR}, reads as grey"))
        worst = min(((delta_e(slots[i], slots[i + 1], k), k, i) for k in MACHADO for i in range(len(slots) - 1)))
        d, kind, i = worst
        pair = f"--{SLOTS[i]}↔--{SLOTS[i + 1]}"
        if d < CVD_FLOOR:
            lines.append(("FAIL", f"CVD: {pair} ΔE {d:.1f} ({kind}) below the floor {CVD_FLOOR}"))
        elif d < CVD_TARGET:
            lines.append(("WARN", f"CVD: {pair} ΔE {d:.1f} ({kind}) in the floor band: only with direct labels, gaps or texture"))
        normal = min((delta_e(slots[i], slots[i + 1]), i) for i in range(len(slots) - 1))
        if normal[0] < NORMAL_FLOOR:
            lines.append(("FAIL", f"normal vision: --{SLOTS[normal[1]]}↔--{SLOTS[normal[1] + 1]} ΔE {normal[0]:.1f} below {NORMAL_FLOOR}"))
        low = [f"--{n} {contrast(lin, s):.2f}:1 on {sn}" for n, lin in zip(SLOTS, slots) for sn, s in surfaces.items() if contrast(lin, s) < CONTRAST_MIN]
        if low:
            lines.append(("WARN", f"contrast below {CONTRAST_MIN}:1, read through the table view and labels (FR-VIZ-06): {'; '.join(low)}"))
        fails = [x for x in lines if x[0] == "FAIL"]
        failed = failed or bool(fails)
        mark = "✖" if fails else "✔"
        print(f"{mark} {theme}: 8 slots, worst adjacent CVD ΔE {d:.1f} ({kind}), normal ΔE {normal[0]:.1f}")
        for level, text in lines:
            print(f"    {'✖' if level == 'FAIL' else '⚠'} {text}")
    return 1 if failed else 0


# Planted light palettes the check must refuse, each for its reason (FR-VIZ-07: a palette change that fails validation
# fails the build): a slot that reads as grey, a red beside a green that a protanope cannot tell apart, two blues a
# reader with full colour vision can hardly tell apart, and a yellow too light for the band.
PLANTED = {
    "a grey slot": ("#2a78d6,#eb6834,#1baf7a,#eda100,#e87ba4,#7a7a7a,#6250d6,#e34948", "--chart-6 #7a7a7a: chroma"),
    "red beside green": ("#2a78d6,#eb6834,#1baf7a,#eda100,#e87ba4,#d0453b,#5f8f2a,#6250d6", "CVD: --chart-6↔--chart-7"),
    "two close blues": ("#2a78d6,#3a74d0,#1baf7a,#eda100,#e87ba4,#008300,#6250d6,#e34948", "normal vision: --chart-1↔--chart-2"),
    "a pale yellow": ("#2a78d6,#eb6834,#1baf7a,#ffe066,#e87ba4,#008300,#6250d6,#e34948", "--chart-4 #ffe066: lightness"),
}


def self_test() -> int:
    import contextlib
    import io
    import tempfile
    base = DEFAULT_TOKENS.read_text()
    bad = 0
    for name, (hexes, why) in PLANTED.items():
        css = base
        for i, h in enumerate(hexes.split(","), 1):  # the first declaration of each slot is the light theme's
            css = re.sub(rf"(?<![\w-])--chart-{i}:\s*#[0-9a-fA-F]{{6}}", f"--chart-{i}: {h}", css, count=1)
        with tempfile.NamedTemporaryFile("w", suffix=".css", delete=False) as f:
            f.write(css)
        out = io.StringIO()
        with contextlib.redirect_stdout(out):
            refused = check(Path(f.name)) == 1
        Path(f.name).unlink()
        if not refused or f"✖ {why}" not in out.getvalue() or "✖ dark:" in out.getvalue():  # the dark theme keeps its own
            print(f"✖ the palette check did not refuse a planted palette, {name}, for its reason ({why}):\n{out.getvalue()}")
            bad += 1
    if not bad:
        print(f"✔ palette check: refuses {len(PLANTED)} planted palettes")
    return 1 if bad else 0


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--tokens", type=Path, default=DEFAULT_TOKENS)
    ap.add_argument("--self-test", action="store_true", help="check that planted palettes are refused")
    args = ap.parse_args()
    return self_test() if args.self_test else check(args.tokens)


if __name__ == "__main__":
    sys.exit(main())
