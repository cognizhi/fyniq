"""The design and image checks that block a merge, run as tests (p1-launch-readiness task 2.1, design D3): the tokens'
contrast in every theme (QA-UI-03), the chart palette and its planted refusals (FR-VIZ-07), the sequential and
diverging scales read from the tokens (FR-VIZ-04), and the image rules on planted images (QA-IMG-01). The scripts are
the ones `make check-tokens` and `make scan-waivers` run; these tests prove each refuses what it must.

Run with `make script-tests` (part of `make check`)."""
import math
import pathlib
import re
import subprocess
import sys
import tempfile
import unittest

ROOT = pathlib.Path(__file__).resolve().parent.parent
DESIGN = ROOT / "docs/design-system/fyniq/scripts"
FYNIQ = ROOT / "docs/design-system/fyniq/tokens.css"
NEMURA = ROOT / "docs/design-system/nemura/tokens.css"

sys.path.insert(0, str(DESIGN))
from check_contrast import THEMES, parse_blocks, parse_color, resolve  # noqa: E402
from check_palette import oklab  # noqa: E402


def run(script, *args):
    return subprocess.run([sys.executable, str(script), *map(str, args)], capture_output=True, text=True, cwd=ROOT)


def planted(css, token, value):
    """The tokens with the light theme's first declaration of token set to value, in a temporary file."""
    out, n = re.subn(rf"(?<![\w-])--{token}:[^;]*;", f"--{token}: {value};", css, count=1)
    assert n == 1, token
    f = tempfile.NamedTemporaryFile("w", suffix=".css", delete=False)
    f.write(out)
    f.close()
    return pathlib.Path(f.name)


def lch(resolved, token):
    """A token's OKLCH lightness, chroma and hue in degrees, as a theme resolves it."""
    L, a, b = oklab(parse_color(resolved[token])[0])
    return L, math.hypot(a, b), math.degrees(math.atan2(b, a)) % 360


def hue_gap(a, b):
    d = abs(a - b) % 360
    return min(d, 360 - d)


class TheContrastCheck(unittest.TestCase):
    """Every required pair of tokens meets its WCAG target in the four themes, in Fyniq's and Nemura's tokens, and a
    muted text too light for its surface fails the check, naming the pair.
    # req: QA-UI-03
    """

    def test_the_tokens_pass(self):
        for tokens in (FYNIQ, NEMURA):
            r = run(DESIGN / "check_contrast.py", "--tokens", tokens)
            self.assertEqual(r.returncode, 0, f"{tokens.name}:\n{r.stdout}{r.stderr}")

    def test_a_planted_failure_fails(self):
        bad = planted(FYNIQ.read_text(), "text-muted", "oklch(0.9 0.01 255)")
        try:
            r = run(DESIGN / "check_contrast.py", "--tokens", bad)
        finally:
            bad.unlink()
        self.assertEqual(r.returncode, 1, r.stdout)
        self.assertIn("text-muted", r.stdout)


class ThePaletteCheck(unittest.TestCase):
    """The categorical palette passes the dataviz checks in every theme, and each planted palette is refused for its
    reason (FR-VIZ-07: a palette change that fails validation fails the build).
    # req: FR-VIZ-07
    # req: QA-UI-03
    """

    def test_the_palette_passes(self):
        r = run(DESIGN / "check_palette.py")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)

    def test_planted_palettes_are_refused(self):
        r = run(DESIGN / "check_palette.py", "--self-test")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("refuses", r.stdout)


class TheScales(unittest.TestCase):
    """FR-VIZ-04, in every theme: the sequential scale is one hue (within 15°) from light to dark, each step darker
    than the last; the diverging scale has two poles of distinct hue (at least 90° apart) with colour, and a neutral
    grey midpoint (chroma under 0.02) on the background's side of both, lighter in a light theme, darker in a dark one.
    # req: FR-VIZ-04
    """

    def setUp(self):
        self.blocks = parse_blocks(FYNIQ.read_text())

    def test_the_sequential_scale_is_one_hue_light_to_dark(self):
        for theme, f in THEMES.items():
            r = resolve(self.blocks, f["dark"], f["hc"])
            steps = [lch(r, f"seq-{n}") for n in (100, 250, 400, 550, 700)]
            lightness = [s[0] for s in steps]
            self.assertEqual(lightness, sorted(lightness, reverse=True), f"{theme}: {lightness}")
            self.assertEqual(len(set(lightness)), len(lightness), f"{theme}: two steps of one lightness")
            hues = [s[2] for s in steps]
            self.assertLessEqual(max(hue_gap(a, b) for a in hues for b in hues), 15, f"{theme}: hues {hues}")

    def test_the_diverging_scale_has_two_poles_and_a_grey_midpoint(self):
        for theme, f in THEMES.items():
            r = resolve(self.blocks, f["dark"], f["hc"])
            neg, mid, pos = (lch(r, f"div-{n}") for n in ("negative", "midpoint", "positive"))
            self.assertGreaterEqual(hue_gap(neg[2], pos[2]), 90, f"{theme}: poles {neg} and {pos}")
            self.assertGreaterEqual(min(neg[1], pos[1]), 0.08, f"{theme}: a pole without colour")
            self.assertLess(mid[1], 0.02, f"{theme}: the midpoint {mid} is not grey")
            if f["dark"]:
                self.assertLess(mid[0], min(neg[0], pos[0]), f"{theme}: the midpoint is not darker than the poles")
            else:
                self.assertGreater(mid[0], max(neg[0], pos[0]), f"{theme}: the midpoint is not lighter than the poles")

    def test_a_scale_of_two_hues_is_caught(self):
        green = re.sub(r"--seq-550:[^;]*;", "--seq-550: #1cab5c;", FYNIQ.read_text(), count=1)
        r = resolve(parse_blocks(green), False, False)
        hues = [lch(r, f"seq-{n}")[2] for n in (100, 250, 400, 550, 700)]
        self.assertGreater(max(hue_gap(a, b) for a in hues for b in hues), 15)


class TheImageRules(unittest.TestCase):
    """The rules `make images` applies refuse each planted image: over its budget, run as root, with a shell, with a
    local adapter in a release image, built for the wrong platform, or pinned to a single-platform digest. The budgets
    and the digest pins in the Dockerfiles are archtests (TestImageBudgetsAreThePRDs, TestImagesArePinnedByDigest).
    # req: QA-IMG-01
    """

    def test_planted_images_are_refused(self):
        r = run(ROOT / "scripts/images.py", "self-test")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)


if __name__ == "__main__":
    unittest.main()
