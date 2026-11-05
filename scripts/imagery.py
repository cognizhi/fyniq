#!/usr/bin/env python3
"""Generated imagery for design work: Meta's Muse Image model, art-directed by Muse Spark, on the Meta Model API
(fyniq-design-system skill).

The key is META_MODEL_API_KEY: the environment's, else the one in the repository's .env, else the main checkout's .env
(so it works from a git worktree). It is never printed, logged or written anywhere. Images and their provenance go to
.data/design/ (git-ignored): they are concepts and references, which the design work redraws as SVG with the tokens
(the API's images are always opaque, so they cannot theme). Standard library only.

    python3 scripts/imagery.py check                         # exit 0 when the key works and muse-image-1.0 is there (and says if muse-spark-1.3 is)
    python3 scripts/imagery.py generate "<prompt>" [--n 4] [--size 1024x1024] [--format png] [--out DIR]
    python3 scripts/imagery.py generate "<prompt>" --from ref.png [--from other.png]   # edit or compose (images/edits)
    python3 scripts/imagery.py review a.png b.png [--brief "<what it is for>"]   # Muse Spark ranks them, says what to fix
    python3 scripts/imagery.py iterate "<prompt>" [--rounds 2] [--n 4]   # generate, review, regenerate from its prompt

Muse Spark judges against the brand brief below (MASTER.md and tokens.css) and writes the next round's prompt. Only the
standard model is used: the "-contributor" variants let Meta train on what is sent. Each generated image costs
US$0.01 and Muse Spark US$1.25 per million input tokens (about 1,400 per image), per dev.meta.ai in Nov 2026 [verify];
`check` costs nothing.
"""
import argparse
import base64
import datetime
import json
import mimetypes
import os
import pathlib
import re
import subprocess
import sys
import urllib.error
import urllib.request

ROOT = pathlib.Path(__file__).resolve().parent.parent
BASE = "https://api.meta.ai/v1"
MODEL = "muse-image-1.0"
SPARK = "muse-spark-1.3"  # never a "-contributor" variant: those let Meta train on the request
KEY = "META_MODEL_API_KEY"
OUT = ROOT / ".data" / "design"

# BRAND is the brief Muse Spark judges against: docs/design-system/fyniq/MASTER.md §1 and §2 and tokens.css, in short.
BRAND = """Fyniq: web accounting, compliance, reporting and planning for Singapore start-ups, for founders who are not
accountants and for accountants. Its line: "Intelligence behind every number". Direction: calm confidence with a spark;
minimal Swiss; serious about numbers, quiet in chrome; personality from colour and craft, never decoration. Brand colour
deep teal #00736e on light (#41c1ba on dark); ink #141b24; paper #fbfaf7. Violet is reserved for AI content and never
appears in brand assets. The UI font is Inter. Every asset works in light and dark themes and in one colour; a logo mark
stays legible at 16 px; no gradients behind text, no text inside illustrations, no emoji, clip-art, stock-photo people,
coins, dollar signs or rising-arrow cliches."""


def env_files() -> list[pathlib.Path]:
    files = [ROOT / ".env"]
    try:
        common = subprocess.run(["git", "-C", str(ROOT), "rev-parse", "--path-format=absolute", "--git-common-dir"],
                                capture_output=True, text=True, check=True).stdout.strip()
        files.append(pathlib.Path(common).parent / ".env")  # the main checkout's, from a worktree
    except (OSError, subprocess.CalledProcessError):
        pass
    return files


def key() -> str | None:
    if os.environ.get(KEY):
        return os.environ[KEY].strip()
    for f in env_files():
        if not f.is_file():
            continue
        for line in f.read_text().splitlines():
            m = re.match(rf"^\s*(?:export\s+)?{KEY}\s*=\s*(.*)$", line)
            if m:
                value = m.group(1).strip().strip("'\"")
                if value:
                    return value
    return None


def call(method: str, path: str, body: dict | None = None, timeout: int = 300) -> tuple[int, dict]:
    k = key()
    if not k:
        raise SystemExit(f"✖ no {KEY} in the environment or in .env: add it there (never commit it), or draw the asset by hand")
    req = urllib.request.Request(BASE + path, method=method, data=json.dumps(body).encode() if body is not None else None,
                                 headers={"Authorization": f"Bearer {k}", "Content-Type": "application/json"})
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            return r.status, json.loads(r.read() or b"{}")
    except urllib.error.HTTPError as e:
        try:
            return e.code, json.loads(e.read() or b"{}")
        except ValueError:
            return e.code, {}
    except urllib.error.URLError as e:
        raise SystemExit(f"✖ the Meta Model API did not answer: {e.reason}")


def problem(status: int, data: dict) -> str:
    err = data.get("error") if isinstance(data.get("error"), dict) else data
    return f"HTTP {status}: {err.get('code') or err.get('type') or ''} {err.get('message') or ''}".strip()


def check() -> int:
    status, data = call("GET", "/models", timeout=30)
    if status != 200:
        print(f"✖ {KEY} does not work ({problem(status, data)}): draw the asset by hand, and ask for a new key")
        return 1
    models = [m.get("id") for m in data.get("data", [])]
    if MODEL not in models:
        print(f"✖ {KEY} works, but {MODEL} is not among its models ({', '.join(models)})")
        return 1
    if SPARK not in models:
        print(f"⚠ {KEY} works and {MODEL} is available, but {SPARK} is not: generate, and review the images yourself")
        return 0
    print(f"✔ {KEY} works: {MODEL} draws and {SPARK} reviews")
    return 0


def slug(text: str) -> str:
    return re.sub(r"[^a-z0-9]+", "-", text.lower()).strip("-")[:48] or "image"


def data_url(path: pathlib.Path) -> str:
    kind = mimetypes.guess_type(path.name)[0] or "image/png"
    return f"data:{kind};base64,{base64.b64encode(path.read_bytes()).decode()}"


def review(files: list[pathlib.Path], brief: str, ask: str = "") -> dict:
    """Muse Spark ranks the images against BRAND and the brief, says what to keep and fix, and writes the next prompt."""
    content: list[dict] = [{"type": "text", "text": (
        f"Brief: {brief}\n\n{ask}\n\nJudge each image below as the art director, against the brand. Answer with JSON "
        'only: {"ranking": [{"file": "<name>", "score": <0-10>, "keep": "<what works>", "fix": "<what is wrong: '
        'garbled or stray text, detail lost at small size, off-brand colour, cliche>"}], "best": "<name>", '
        '"next_prompt": "<one prompt for the image generator that keeps the best and fixes the rest>"}').strip()}]
    for f in files:
        content.append({"type": "text", "text": f"Image: {f.name}"})
        content.append({"type": "image_url", "image_url": {"url": data_url(f)}})
    status, data = call("POST", "/chat/completions", {"model": SPARK, "messages": [
        {"role": "system", "content": "You are the art director of a product's brand. " + BRAND},
        {"role": "user", "content": content}]})
    if status != 200:
        raise SystemExit(f"✖ no review ({problem(status, data)})")
    text = (data.get("choices") or [{}])[0].get("message", {}).get("content") or ""
    m = re.search(r"\{.*\}", text, re.S)
    try:
        return json.loads(m.group(0)) if m else {"text": text}
    except ValueError:
        return {"text": text}


def generate(a: argparse.Namespace) -> list[pathlib.Path]:
    body = {"model": MODEL, "prompt": a.prompt, "n": a.n, "output_format": a.format}
    if a.size:
        body["size"] = a.size
    path = "/images/generations"
    if a.source:
        path = "/images/edits"
        body["images"] = [{"image_url": data_url(pathlib.Path(p))} for p in a.source]
    status, data = call("POST", path, body)
    if status != 200:
        print(f"✖ no image ({problem(status, data)})")
        return []
    stamp = datetime.datetime.now(datetime.timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    out = pathlib.Path(a.out) if a.out else OUT / f"{stamp}-{slug(a.name or a.prompt)}"
    out.mkdir(parents=True, exist_ok=True)
    saved = []
    for i, item in enumerate(data.get("data", []), 1):
        if item.get("b64_json"):
            blob = base64.b64decode(item["b64_json"])
        elif item.get("url"):
            with urllib.request.urlopen(item["url"], timeout=120) as r:
                blob = r.read()
        else:
            continue
        f = out / f"{slug(a.name or 'image')}-{i}.{a.format}"
        f.write_bytes(blob)
        saved.append(f)
    # Provenance: how each image was made, for the asset that is redrawn from it. Never the key.
    (out / "provenance.json").write_text(json.dumps({
        "model": MODEL, "endpoint": BASE + path, "prompt": a.prompt, "size": a.size or "auto", "n": a.n,
        "sources": [str(p) for p in a.source or []], "created_at": stamp, "files": [p.name for p in saved],
        "usage": data.get("usage"),
    }, indent=2) + "\n")
    for f in saved:
        print(f)
    return saved


def iterate(a: argparse.Namespace) -> int:
    """Rounds of generate then review, each round from the review's next prompt; the last review names the best."""
    stamp = datetime.datetime.now(datetime.timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    root = pathlib.Path(a.out) if a.out else OUT / f"{stamp}-{slug(a.name or a.prompt)}"
    prompt, verdict = a.prompt, {}
    for r in range(1, a.rounds + 1):
        a.prompt, a.out = prompt, str(root / f"round-{r}")
        files = generate(a)
        if not files:
            return 1
        verdict = review(files, a.brief or a.prompt)
        (root / f"round-{r}" / "review.json").write_text(json.dumps(verdict, indent=2) + "\n")
        print(f"round {r}: best {verdict.get('best', '?')}")
        prompt = verdict.get("next_prompt") or prompt
    print(json.dumps(verdict, indent=2))
    return 0


def main() -> int:
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = p.add_subparsers(dest="cmd", required=True)
    sub.add_parser("check", help="does the key work, and is the model there (free)")
    rv = sub.add_parser("review", help="Muse Spark ranks images against the brand and writes the next prompt")
    rv.add_argument("files", nargs="+")
    rv.add_argument("--brief", default="", help="what the images are for, such as the logo mark of Fyniq")
    rv.add_argument("--ask", default="", help="anything else to judge them on")
    g = sub.add_parser("generate", help="generate images from a prompt, or edit and compose with --from")
    it = sub.add_parser("iterate", help="generate, review and regenerate from the review's prompt, for some rounds")
    it.add_argument("--rounds", type=int, default=2, choices=range(1, 6), metavar="1-5")
    it.add_argument("--brief", default="", help="what the images are for; default the prompt")
    for g in (g, it):
        add_generation(g)
    a = p.parse_args()
    if a.cmd == "check":
        return check()
    if a.cmd == "review":
        verdict = review([pathlib.Path(f) for f in a.files], a.brief or "an asset of Fyniq's brand", a.ask)
        print(json.dumps(verdict, indent=2))
        return 0
    if a.cmd == "iterate":
        return iterate(a)
    return 0 if generate(a) else 1


def add_generation(g: argparse.ArgumentParser) -> None:
    g.add_argument("prompt")
    g.add_argument("--n", type=int, default=1, choices=range(1, 11), metavar="1-10")
    g.add_argument("--size", default="", help="WxH, which sets the aspect ratio only, such as 1024x1024; default auto")
    g.add_argument("--format", default="png", choices=["png", "webp", "jpeg"])
    g.add_argument("--from", dest="source", action="append", help="an input image to edit or compose (up to 10)")
    g.add_argument("--name", default="", help="a short name for the files, such as logo-mark")
    g.add_argument("--out", default="", help="the folder to write to; default .data/design/<time>-<name>")


if __name__ == "__main__":
    sys.exit(main())
