#!/usr/bin/env python3
"""Checks the OpenTofu code in infra/ without a cloud account (NFR-PORT-05: `tofu validate` and policy checks
per module).

    python3 scripts/infra.py check       # make infra-check
    python3 scripts/infra.py self-test   # the rules below catch planted violations
    python3 scripts/infra.py plan staging [--drift] [--apply]   # CI only (.github/workflows/infra.yml)

check: `tofu fmt -check`, then in every root and module `tofu init -backend=false -lockfile=readonly` (the
committed lock files pin the providers), `tofu validate` and, where a module has tests/, `tofu test` against
mock providers. Then the rules no plan can show:
  - no service-account key or HMAC key resource anywhere (SEC-04, CTL-IAM-04);
  - no authoritative IAM resource (`*_iam_policy`, `*_iam_binding`), which would silently remove grants
    made elsewhere: grants are `*_iam_member`;
  - no person or group granted a role in code (`user:`, `group:` in a grant): people get roles only
    through just-in-time elevation (CTL-IAM-03);
  - no region outside Singapore written into the code (CTL-DP-07).
`tofu init` downloads the providers, so this needs the network; it is not part of `make test`, which runs
offline. Providers are cached in .data/tofu-plugins/. The lock files carry every platform's hashes; to move
to a new provider version, change the version constraints, delete the lock files and run `tofu init`.

plan (CI, with Google credentials from Workload Identity Federation): plans one environment's root against
its state (TOFU_STATE_BUCKET) with its values (TOFU_TFVARS, JSON, written to a git-ignored file). --drift
fails on any difference from the code (CTL-CHG-02); --apply applies the plan it just made. The log shows
only which resources would change and how, never their values: this repository's CI logs are public.
"""

from __future__ import annotations

import json
import os
import re
import shutil
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
INFRA = ROOT / "infra"

FORBIDDEN_RESOURCES = re.compile(r'resource\s+"(google_service_account_key|google_storage_hmac_key|google_\w+_iam_(?:policy|binding))"')
HUMAN_GRANT = re.compile(r'(?:member|members|principals)\s*=\s*\[?\s*"(?:user|group):')
REGION = re.compile(r'"(?:[a-z]+-)?(us|europe|asia|australia|northamerica|southamerica|me|africa)-[a-z]+[0-9]"')
# The one create right on secrets: the api's account manages the organisations' connector secrets in their own project
# and nowhere else (SEC-12; p1-bank-import-reconcile design D12, the owner's decision of 25 Oct 2026).
SECRETS_ADMIN = re.compile(r'"roles/secretmanager\.admin"')
SECRETS_PROJECT = re.compile(r'^\s*project\s*=\s*var\.secrets_project\s*$', re.M)
RESOURCE = re.compile(r'^resource\s+"[^"]+"\s+"[^"]+"\s*\{', re.M)


def blocks(text: str) -> list[tuple[int, str]]:
    """Each resource block of a file, with the line it starts on, by counting braces."""
    out = []
    for m in RESOURCE.finditer(text):
        depth, i = 0, m.end() - 1
        while i < len(text):
            depth += {"{": 1, "}": -1}.get(text[i], 0)
            if depth == 0:
                break
            i += 1
        out.append((text.count("\n", 0, m.start()) + 1, text[m.start():i + 1]))
    return out


def tofu(*args: str, cwd: Path) -> subprocess.CompletedProcess[str]:
    if shutil.which("tofu") is None:
        raise SystemExit("✖ tofu is not installed: install the toolchain with `make setup` (mise.toml pins OpenTofu)")
    env = {**os.environ, "TF_IN_AUTOMATION": "1"}
    env.setdefault("TF_PLUGIN_CACHE_DIR", str(ROOT / ".data" / "tofu-plugins"))
    Path(env["TF_PLUGIN_CACHE_DIR"]).mkdir(parents=True, exist_ok=True)
    return subprocess.run(["tofu", *args, "-no-color"], cwd=cwd, capture_output=True, text=True, env=env)


def directories() -> list[Path]:
    """Every root and module: each directory under infra/ with a .tf file of its own."""
    return sorted({p.parent for p in INFRA.rglob("*.tf") if ".terraform" not in p.parts})


def rules(name: str, text: str) -> list[str]:
    """What is wrong with one .tf file that no plan shows."""
    problems = []
    for m in FORBIDDEN_RESOURCES.finditer(text):
        problems.append(f"{name}: {m[1]}: no keys (SEC-04, CTL-IAM-04) and no authoritative IAM; grant with *_iam_member")
    for line_no, line in enumerate(text.splitlines(), 1):
        code = line.split("#", 1)[0]
        if HUMAN_GRANT.search(code):
            problems.append(f"{name}:{line_no}: a role granted to a person or group in code: people elevate just in time (CTL-IAM-03)")
        for m in REGION.finditer(code):
            if m[0] != '"asia-southeast1"':
                problems.append(f"{name}:{line_no}: {m[0]} is outside Singapore (CTL-DP-07)")
    for line_no, block in blocks(text):
        code = "\n".join(line.split("#", 1)[0] for line in block.splitlines())
        if SECRETS_ADMIN.search(code) and not SECRETS_PROJECT.search(code):
            problems.append(f"{name}:{line_no}: roles/secretmanager.admin outside the organisations' secrets project: secrets are created "
                            "only there (SEC-12, design D12)")
    return problems


# req: NFR-PORT-05
# req: CTL-IAM-04
# req: CTL-IAM-03
# req: CTL-DP-07
def check() -> int:
    problems = []
    fmt = tofu("fmt", "-check", "-recursive", ".", cwd=INFRA)
    if fmt.returncode:
        problems.append("tofu fmt: run `tofu fmt -recursive infra` for " + ", ".join(fmt.stdout.split()))
    tested = 0
    for d in directories():
        rel = d.relative_to(ROOT)
        steps = [("init", "-backend=false", "-input=false", "-lockfile=readonly"), ("validate",)]
        if (d / "tests").is_dir():
            steps.append(("test",))
            tested += 1
        for step in steps:
            p = tofu(*step, cwd=d)
            if p.returncode:
                problems.append(f"{rel}: tofu {step[0]} failed:\n{(p.stdout + p.stderr).strip()[-3000:]}")
                break
        for f in sorted(d.glob("*.tf")):
            problems += rules(str(f.relative_to(ROOT)), f.read_text())
    if problems:
        print("✖ infra:\n  " + "\n  ".join(problems))
        return 1
    print(f"✔ infra: {len(directories())} roots and modules formatted, initialised from their lock files and valid; "
          f"{tested} tested against mock providers; no keys, no authoritative IAM, no standing human grant, nothing outside Singapore, "
          "no secrets create right outside the secrets project")
    return 0


def self_test() -> None:
    planted = '''
resource "google_service_account_key" "k" {}
resource "google_project_iam_binding" "b" {}
resource "google_project_iam_member" "m" { member = "user:someone@example.com" }
resource "google_storage_bucket" "b" { location = "us-central1" }
resource "google_project_iam_member" "s" {
  project = var.project
  role    = "roles/secretmanager.admin"
}
'''
    if len(rules("planted.tf", planted)) != 5:
        raise SystemExit(f"✖ infra self-test: want 5 problems, got {rules('planted.tf', planted)}")
    clean = '''
resource "google_project_iam_member" "m" { member = "serviceAccount:x@p.iam.gserviceaccount.com" }  # not user: in code
resource "google_storage_bucket" "b" { location = "asia-southeast1" }
resource "google_project_iam_member" "s" {
  project = var.secrets_project
  role    = "roles/secretmanager.admin"
}
'''
    if rules("clean.tf", clean):
        raise SystemExit(f"✖ infra self-test: a clean file was refused: {rules('clean.tf', clean)}")
    print("✔ infra rules: a key, authoritative IAM, a person's grant, a region outside Singapore and a secrets create right outside "
          "the secrets project are each refused")


# req: CTL-CHG-02
def plan(environment: str, drift: bool, apply: bool) -> int:
    d = INFRA / "gcp" / "envs" / environment
    bucket, tfvars = os.environ.get("TOFU_STATE_BUCKET", ""), os.environ.get("TOFU_TFVARS", "")
    if not d.is_dir() or not bucket or not tfvars:
        raise SystemExit(f"✖ plan {environment}: needs infra/gcp/envs/{environment}, TOFU_STATE_BUCKET and TOFU_TFVARS")
    (d / "ci.auto.tfvars.json").write_text(json.dumps(json.loads(tfvars)))
    try:
        p = tofu("init", "-input=false", "-lockfile=readonly", f"-backend-config=bucket={bucket}", cwd=d)
        if p.returncode:
            print(f"✖ tofu init ({environment}):\n{p.stderr.strip()[-2000:]}")
            return 1
        p = tofu("plan", "-input=false", "-lock-timeout=5m", "-detailed-exitcode", "-out=tfplan", cwd=d)
        if p.returncode == 1:
            print(f"✖ tofu plan ({environment}) failed:\n{p.stderr.strip()[-2000:]}")
            return 1
        shown = json.loads(tofu("show", "-json", "tfplan", cwd=d).stdout or "{}")
        changes = [f"{'/'.join(c['change']['actions'])} {c['address']}" for c in shown.get("resource_changes", [])
                   if c["change"]["actions"] != ["no-op"]]
        print(f"{environment}: {len(changes)} change(s)" + "".join(f"\n  {c}" for c in changes))
        if p.returncode == 2 and drift:
            print(f"✖ drift in {environment}: the cloud differs from the code (CTL-CHG-02); find who changed it, then apply the code or change it in a reviewed pull request")
            return 1
        if p.returncode == 2 and apply:
            a = tofu("apply", "-input=false", "-lock-timeout=5m", "tfplan", cwd=d)
            if a.returncode:
                print(f"✖ tofu apply ({environment}) failed:\n{a.stderr.strip()[-2000:]}")
                return 1
            print(f"✔ {environment}: applied {len(changes)} change(s)")
        return 0
    finally:
        (d / "ci.auto.tfvars.json").unlink(missing_ok=True)
        (d / "tfplan").unlink(missing_ok=True)


def main() -> int:
    cmd = sys.argv[1] if len(sys.argv) > 1 else "check"
    if cmd == "self-test":
        self_test()
        return 0
    if cmd == "check":
        return check()
    if cmd == "plan" and len(sys.argv) > 2:
        return plan(sys.argv[2], "--drift" in sys.argv, "--apply" in sys.argv)
    raise SystemExit(f"usage: {sys.argv[0]} check|self-test|plan <environment> [--drift] [--apply]")


if __name__ == "__main__":
    sys.exit(main())
