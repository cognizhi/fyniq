#!/usr/bin/env python3
"""Checks the images `make images` built (PRD-001 §12.9), and scans them.

    python3 scripts/images.py check --version 0.1.0 --flavours "release local"
    python3 scripts/images.py scan --version 0.1.0     # SBOM and vulnerability scan (make image-scan)
    python3 scripts/images.py self-test                # the checks on planted images (part of make check)

check (part of `make images`; Docker only):
  - the compressed size is within deploy/docker/budgets.json (NFR-IMG-04);
  - the image runs as its non-root user, with the binary as its entrypoint (NFR-IMG-02, SEC-27);
  - a Go image holds no shell, package manager or toolchain (NFR-IMG-01); the web image is the
    nginx-unprivileged Alpine image §12.9 names, used locally and in previews only;
  - a release binary contains no local adapter, and a local one does, which proves the check
    (DEV-07, CTL-CHG-09, QA-IMG-01).
scan (CI): an SPDX SBOM per release image (syft) and a grype scan; a Critical finding fails unless
compliance/scan-waivers.toml waives it (tool "grype-image"; SEC-21). Reports go to .data/images/.
"""

from __future__ import annotations

import argparse
import datetime as dt
import io
import json
import subprocess
import sys
import tarfile
import zlib
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import scan  # noqa: E402  (the waivers and their rules)

ROOT = Path(__file__).resolve().parent.parent
REPORTS = ROOT / ".data" / "images"
BUDGETS = json.loads((ROOT / "deploy" / "docker" / "budgets.json").read_text())["images"]
GO_IMAGES = [i for i in BUDGETS if i != "web"]
FORBIDDEN = ("bin/sh", "bin/bash", "bin/ash", "bin/busybox", "sbin/apk", "usr/bin/apt", "usr/bin/apt-get", "usr/bin/dpkg",
             "usr/local/go/bin/go", "usr/bin/gcc")
LOCAL_MARK = b"github.com/cognizhi/fyniq/internal/adapters/local"


def sh(*cmd: str, capture: bool = True) -> str:
    p = subprocess.run(cmd, cwd=ROOT, capture_output=capture, text=True)
    if p.returncode != 0:
        raise SystemExit(f"✖ {' '.join(cmd[:4])}: {p.stderr.strip()[-2000:]}")
    return p.stdout


def compressed_size(image: str) -> int:
    """The size of `docker save image | gzip -6`."""
    p = subprocess.Popen(["docker", "save", image], stdout=subprocess.PIPE)
    z, n = zlib.compressobj(6, zlib.DEFLATED, 31), 0
    assert p.stdout is not None
    while chunk := p.stdout.read(1 << 20):
        n += len(z.compress(chunk))
    n += len(z.flush())
    if p.wait() != 0:
        raise SystemExit(f"✖ docker save {image} failed")
    return n


def filesystem(image: str) -> tuple[list[str], bytes]:
    """The image's files, and its /app binary (empty when it has none)."""
    cid = sh("docker", "create", image).strip()
    try:
        raw = subprocess.run(["docker", "export", cid], capture_output=True, check=True).stdout
    finally:
        sh("docker", "rm", "-f", cid)
    names, app = [], b""
    with tarfile.open(fileobj=io.BytesIO(raw)) as tar:
        for m in tar:
            names.append(m.name.lstrip("./"))
            if m.name.lstrip("./") == "app" and m.isfile():
                f = tar.extractfile(m)
                app = f.read() if f else b""
    return names, app


def tags(version: str, flavours: list[str]) -> list[tuple[str, str, str]]:
    out = [(svc, f, f"fyniq-{svc}:{'local' if f == 'local' else version}") for svc in GO_IMAGES for f in flavours]
    return out + [("web", "local", "fyniq-web:local")]


def judge(svc: str, flavour: str, image: str, size: int, cfg: dict, names: list[str], app: bytes) -> list[str]:
    """What is wrong with one built image (§12.9)."""
    problems = []
    if size > BUDGETS[svc] * 1_000_000:
        problems.append(f"{image} is {size / 1e6:.1f} MB compressed, over its {BUDGETS[svc]} MB budget (NFR-IMG-04)")
    if svc == "web":
        if cfg.get("User") != "101":
            problems.append(f"{image} runs as {cfg.get('User')!r}, not nginx (101)")
        return problems
    if cfg.get("User") != "65532:65532" or cfg.get("Entrypoint") != ["/app"]:
        problems.append(f"{image} runs as {cfg.get('User')!r} with entrypoint {cfg.get('Entrypoint')}, not 65532:65532 and /app (NFR-IMG-02)")
    if found := [n for n in names if n in FORBIDDEN]:
        problems.append(f"{image} contains {', '.join(found)}: no shell, package manager or toolchain at runtime (NFR-IMG-01)")
    if not app:
        problems.append(f"{image} has no /app binary")
    elif flavour == "release" and LOCAL_MARK in app:
        problems.append(f"{image} is a release image with a local adapter compiled in (DEV-07, CTL-CHG-09)")
    elif flavour == "local" and LOCAL_MARK not in app and svc in ("api", "worker", "jobs", "executor"):
        problems.append(f"{image}: the local adapters are missing from a local image, so the release check proves nothing")
    return problems


def check(version: str, flavours: list[str]) -> int:
    problems, rows = [], []
    for svc, flavour, image in tags(version, flavours):
        size = compressed_size(image)
        cfg = json.loads(sh("docker", "image", "inspect", image))[0]["Config"]
        names, app = filesystem(image)
        rows.append(f"  {image:<28} {size / 1e6:6.1f} MB of {BUDGETS[svc]} MB")
        problems += judge(svc, flavour, image, size, cfg, names, app)
    print("\n".join(rows))
    if problems:
        print("✖ images:\n  " + "\n  ".join(problems))
        return 1
    print(f"✔ {len(rows)} images within budget, non-root, without a shell; release images without local adapters")
    return 0


# req: NFR-IMG-04
# req: NFR-IMG-01
# req: NFR-IMG-02
# req: DEV-07
def self_test() -> None:
    good = {"User": "65532:65532", "Entrypoint": ["/app"]}
    if judge("api", "release", "fyniq-api:0", 5_000_000, good, ["app", "etc/passwd"], b"main"):
        raise SystemExit("✖ images self-test: a good image was refused")
    planted = judge("api", "release", "fyniq-api:0", 31_000_000, {"User": "0:0", "Entrypoint": ["/bin/sh"]},
                    ["app", "bin/sh"], b"x" + LOCAL_MARK)
    if len(planted) != 4:
        raise SystemExit(f"✖ images self-test: want 4 problems (budget, user, shell, local adapter), got {planted}")
    if not judge("web", "local", "fyniq-web:local", 26_000_000, {"User": "root"}, [], b""):
        raise SystemExit("✖ images self-test: an over-budget web image running as root was accepted")
    print("✔ image checks: an over-budget image, a root user, a shell and a local adapter in release are each refused")


def scan_images(version: str) -> int:
    REPORTS.mkdir(parents=True, exist_ok=True)
    findings = []
    for svc in GO_IMAGES:
        image = f"fyniq-{svc}:{version}"
        sh("syft", "scan", f"docker:{image}", "-q", "-o", f"spdx-json={REPORTS / (svc + '.spdx.json')}")
        out = sh("grype", f"docker:{image}", "-q", "-o", "json")
        (REPORTS / f"{svc}.grype.json").write_text(out)
        for m in json.loads(out).get("matches", []):
            v, a = m["vulnerability"], m["artifact"]
            findings.append(scan.Finding("grype-image", v["id"], image.split(":")[0],
                                         f"{a['name']} {a['version']}: {v['severity']}", v["severity"] == "Critical"))
    problems = scan.judge(findings, scan.load_waivers(), {"grype-image"}, dt.date.today())
    (REPORTS / "summary.json").write_text(json.dumps({"problems": problems, "findings": [f.__dict__ for f in findings]}, indent=1))
    if problems:
        print("✖ image scan (SEC-21):\n  " + "\n  ".join(problems))
        return 1
    print(f"✔ release images: SBOMs written, {len(findings)} vulnerability finding(s), none critical unwaived; reports in .data/images/")
    return 0


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("command", choices=["check", "scan", "self-test"])
    ap.add_argument("--version", default="0.0.0")
    ap.add_argument("--flavours", default="release local")
    a = ap.parse_args()
    if a.command == "self-test":
        self_test()
        return 0
    if a.command == "check":
        return check(a.version, a.flavours.split())
    return scan_images(a.version)


if __name__ == "__main__":
    sys.exit(main())
