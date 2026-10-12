#!/usr/bin/env python3
"""Validate the contrast of the design tokens in every theme (PRD-001 QA-UI-03).

Reads tokens.css, resolves the custom properties for each theme the way the
browser's cascade does (light, dark, light high-contrast, dark high-contrast),
converts OKLCH and hex colours to sRGB and checks every required
foreground/background pair against its WCAG 2.2 target:

  text pairs            4.5:1  (7:1 in the high-contrast themes, WCAG AAA 1.4.6)
  non-text UI pairs     3:1    (control boundaries, focus rings, WCAG 1.4.11)

Standard library only.

  python3 scripts/check_contrast.py                 # report, exit 1 on any failure
  python3 scripts/check_contrast.py --hex           # also print resolved hex per token
  python3 scripts/check_contrast.py --tokens PATH   # another tokens file
"""
from __future__ import annotations

import argparse
import math
import re
import sys
from pathlib import Path

DEFAULT_TOKENS = Path(__file__).resolve().parent.parent / "tokens.css"

THEMES = {
    "light": {"dark": False, "hc": False},
    "dark": {"dark": True, "hc": False},
    "light-hc": {"dark": False, "hc": True},
    "dark-hc": {"dark": True, "hc": True},
}

TEXT, UI = "text", "ui"

# (foreground, background, kind)
PAIRS = [
    *[(fg, bg, TEXT)
      for fg in ("text-primary", "text-secondary", "text-muted")
      for bg in ("background", "surface-1", "surface-2", "surface-3")],
    ("primary-foreground", "primary", TEXT),
    ("primary-foreground", "primary-hover", TEXT),
    ("primary-subtle-foreground", "primary-subtle", TEXT),
    ("link", "background", TEXT),
    ("link", "surface-1", TEXT),
    ("ai-foreground", "ai", TEXT),
    ("ai-subtle-foreground", "ai-subtle", TEXT),
    ("text-primary", "ai-subtle", TEXT),
    ("text-primary", "primary-subtle", TEXT),
    *[(f"status-{s}", f"status-{s}-subtle", TEXT)
      for s in ("good", "warning", "serious", "critical", "neutral")],
    *[(f"status-{s}", "surface-1", TEXT)
      for s in ("good", "warning", "serious", "critical", "neutral")],
    ("destructive-foreground", "destructive", TEXT),
    ("amount-positive", "surface-1", TEXT),
    ("amount-negative", "surface-1", TEXT),
    ("border-strong", "surface-1", UI),
    ("border-strong", "background", UI),
    ("ring", "background", UI),
    ("ring", "surface-1", UI),
    ("primary", "background", UI),
    ("ai-border", "surface-1", UI),
]

TARGET = {(TEXT, False): 4.5, (TEXT, True): 7.0, (UI, False): 3.0, (UI, True): 3.0}


# ---------------------------------------------------------------- CSS parsing

def parse_blocks(css: str) -> list[tuple[str, dict[str, str]]]:
    """Top-level rule blocks in source order: [(selector, {prop: value})].

    Blocks nested in at-rules (@media, @theme, @supports) are skipped: the
    themes are selected by class and attribute, not by media queries.
    """
    css = re.sub(r"/\*.*?\*/", "", css, flags=re.S)
    blocks: list[tuple[str, dict[str, str]]] = []
    stack: list[str] = []
    buf = ""
    body = ""
    for ch in css:
        if ch == "{":
            stack.append(buf.strip())
            buf, body = "", ""
        elif ch == "}":
            if stack:
                sel = stack.pop()
                if not stack and not sel.startswith("@"):
                    blocks.append((sel, parse_decls(body + buf)))
            buf, body = "", ""
        elif ch == ";":
            body += buf + ";"
            buf = ""
        else:
            buf += ch
    return blocks


def parse_decls(body: str) -> dict[str, str]:
    decls = {}
    for part in body.split(";"):
        if ":" in part:
            name, value = part.split(":", 1)
            name = name.strip()
            if name.startswith("--"):
                decls[name[2:]] = value.strip()
    return decls


def matches(selector: str, dark: bool, hc: bool) -> tuple[bool, int]:
    """Does a theme's root element match this selector? Returns (match, specificity)."""
    sel = selector.replace(" ", "")
    if sel == ":root":
        return True, 1
    parts = re.findall(r"\.[\w-]+|\[[^\]]+\]", sel)
    if not parts or "".join(parts) != sel:
        return False, 0
    for p in parts:
        if p == ".dark" and not dark:
            return False, 0
        if p.startswith("[data-contrast") and not hc:
            return False, 0
        if p.startswith("[data-density"):
            return False, 0
        if p not in (".dark",) and not p.startswith(("[data-contrast", "[data-density")):
            return False, 0
    return True, len(parts)


def resolve(blocks, dark: bool, hc: bool) -> dict[str, str]:
    applicable = []
    for order, (sel, decls) in enumerate(blocks):
        for single in sel.split(","):
            ok, spec = matches(single, dark, hc)
            if ok:
                applicable.append((spec, order, decls))
    tokens: dict[str, str] = {}
    for _, _, decls in sorted(applicable, key=lambda t: (t[0], t[1])):
        tokens.update(decls)
    return tokens


# ---------------------------------------------------------------- colour maths

def parse_color(value: str) -> tuple[tuple[float, float, float], bool] | None:
    """Return (linear sRGB, in_gamut) or None when the value is not a plain colour."""
    value = value.strip()
    m = re.fullmatch(r"#([0-9a-fA-F]{6})", value)
    if m:
        h = m.group(1)
        rgb = [int(h[i:i + 2], 16) / 255 for i in (0, 2, 4)]
        return tuple(to_linear(c) for c in rgb), True
    m = re.fullmatch(r"oklch\(\s*([\d.]+)(%?)\s+([\d.]+)\s+([\d.]+)\s*\)", value)
    if m:
        L = float(m.group(1)) / (100 if m.group(2) else 1)
        C, H = float(m.group(3)), float(m.group(4))
        return oklch_to_linear_srgb(L, C, H)
    return None


def to_linear(c: float) -> float:
    return c / 12.92 if c <= 0.04045 else ((c + 0.055) / 1.055) ** 2.4


def to_gamma(c: float) -> float:
    c = min(max(c, 0.0), 1.0)
    return 12.92 * c if c <= 0.0031308 else 1.055 * c ** (1 / 2.4) - 0.055


def oklch_to_linear_srgb(L: float, C: float, H: float):
    a, b = C * math.cos(math.radians(H)), C * math.sin(math.radians(H))
    l_ = L + 0.3963377774 * a + 0.2158037573 * b
    m_ = L - 0.1055613458 * a - 0.0638541728 * b
    s_ = L - 0.0894841775 * a - 1.2914855480 * b
    l, m, s = l_ ** 3, m_ ** 3, s_ ** 3
    rgb = (
        +4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s,
        -1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s,
        -0.0041960863 * l - 0.7034186147 * m + 1.7076147010 * s,
    )
    in_gamut = all(-0.002 <= c <= 1.002 for c in rgb)
    return tuple(min(max(c, 0.0), 1.0) for c in rgb), in_gamut


def luminance(lin) -> float:
    r, g, b = lin
    return 0.2126 * r + 0.7152 * g + 0.0722 * b


def contrast(a, b) -> float:
    la, lb = luminance(a), luminance(b)
    hi, lo = max(la, lb), min(la, lb)
    return (hi + 0.05) / (lo + 0.05)


def to_hex(lin) -> str:
    return "#" + "".join(f"{round(to_gamma(c) * 255):02x}" for c in lin)


# ---------------------------------------------------------------- main

def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--tokens", type=Path, default=DEFAULT_TOKENS)
    ap.add_argument("--hex", action="store_true", help="print resolved hex values per theme")
    args = ap.parse_args()

    blocks = parse_blocks(args.tokens.read_text(encoding="utf-8"))
    failures = 0
    warnings = 0
    for theme, flags in THEMES.items():
        tokens = resolve(blocks, **flags)
        colours = {}
        for name, value in tokens.items():
            parsed = parse_color(value)
            if parsed:
                colours[name] = parsed
        print(f"\n== {theme}")
        if args.hex:
            for name, (lin, _) in sorted(colours.items()):
                print(f"   {name:28} {to_hex(lin)}")
        for name, (_, in_gamut) in sorted(colours.items()):
            if not in_gamut:
                warnings += 1
                print(f"   WARN  {name} is outside sRGB; browsers gamut-map it (value clipped here)")
        for fg, bg, kind in PAIRS:
            if fg not in colours or bg not in colours:
                failures += 1
                print(f"   FAIL  {fg} on {bg}: token missing")
                continue
            ratio = contrast(colours[fg][0], colours[bg][0])
            target = TARGET[(kind, flags["hc"])]
            ok = ratio >= target
            failures += not ok
            mark = "ok  " if ok else "FAIL"
            print(f"   {mark}  {ratio:5.2f} ≥ {target:<3}  {fg} on {bg}")
    print(f"\n{failures} failure(s), {warnings} gamut warning(s)")
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
