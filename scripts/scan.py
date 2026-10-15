#!/usr/bin/env python3
"""The security scans of CI (SEC-21, CTL-CHG-03, CTL-CHG-11): SAST, dependency, secret, workflow,
container-definition and licence scanning. Each tool writes its JSON report to .data/scan/ (the
evidence); this script applies the waivers in compliance/scan-waivers.toml and fails on any blocking
finding that no waiver covers, on an expired waiver, and on a waiver that no longer matches anything.

    python3 scripts/scan.py              # every tool (make scan)
    python3 scripts/scan.py --only gosec,licences
    python3 scripts/scan.py --self-test  # the waiver rules on planted findings (part of make check)

What blocks (P0): gosec HIGH, Semgrep ERROR, any secret, grype Critical (NFR-IMG-05 adds High in P1),
any failed Checkov check, actionlint errors, zizmor High, and a licence outside the allow-list. Every
other finding is reported, not blocking. The tools are pinned in mise.toml; `make setup` installs them.
"""

from __future__ import annotations

import argparse
import csv
import datetime as dt
import fnmatch
import io
import json
import os
import re
import shutil
import subprocess
import sys
import tomllib
from dataclasses import dataclass, field
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
WAIVERS = ROOT / "compliance" / "scan-waivers.toml"
REPORTS = ROOT / ".data" / "scan"
EXCLUDE = ["node_modules", ".data", "dist", "test-results", ".git"]

# Licences a distributed dependency may have (the Go binaries and the web app's production bundle).
ALLOWED = {"MIT", "Apache-2.0", "BSD-2-Clause", "BSD-3-Clause", "ISC", "0BSD", "MPL-2.0", "Unlicense", "CC0-1.0"}
# Licences no dependency may have, development tools included: strong copyleft and source-available.
DENIED = re.compile(r"\b(A?GPL|LGPL|SSPL|EUPL|OSL|CPAL|BUSL|Commons-Clause|CC-BY-NC)", re.I)


@dataclass
class Finding:
    tool: str
    rule: str
    path: str
    detail: str
    blocking: bool
    waived_by: str = ""

    def key(self) -> str:
        return f"{self.tool} {self.rule} {self.path}"


@dataclass
class Waiver:
    tool: str
    rule: str
    path: str
    reason: str
    expires: dt.date
    used: bool = field(default=False)

    def covers(self, f: Finding) -> bool:
        return self.tool == f.tool and fnmatch.fnmatchcase(f.rule, self.rule) and fnmatch.fnmatchcase(f.path, self.path)

    def name(self) -> str:
        return f"{self.tool} {self.rule} {self.path}"


def load_waivers(path: Path = WAIVERS) -> list[Waiver]:
    data = tomllib.loads(path.read_text()) if path.exists() else {}
    out = []
    for i, w in enumerate(data.get("waiver", []), 1):
        missing = [k for k in ("tool", "rule", "path", "reason", "expires") if not w.get(k)]
        if missing:
            raise SystemExit(f"✖ {path.name}: waiver {i} lacks {', '.join(missing)}")
        if not isinstance(w["expires"], dt.date):
            raise SystemExit(f"✖ {path.name}: waiver {i}: expires must be a date (2027-04-15)")
        if len(w["reason"]) < 20:
            raise SystemExit(f"✖ {path.name}: waiver {i}: say why the finding is accepted")
        out.append(Waiver(w["tool"], w["rule"], w["path"], w["reason"], w["expires"]))
    return out


def judge(findings: list[Finding], waivers: list[Waiver], ran: set[str], today: dt.date) -> list[str]:
    """Applies the waivers; returns the reasons the scan fails."""
    problems = []
    for w in waivers:
        if w.expires < today:
            problems.append(f"waiver expired on {w.expires}: {w.name()} — fix the finding, or renew the waiver in a reviewed pull request")
    for f in findings:
        for w in waivers:
            if w.covers(f) and w.expires >= today:
                f.waived_by, w.used = w.name(), True
                break
        if f.blocking and not f.waived_by:
            problems.append(f"{f.tool}: {f.rule} in {f.path}: {f.detail}")
    for w in waivers:
        if w.tool in ran and not w.used and w.expires >= today:
            problems.append(f"waiver matches nothing: {w.name()} — remove it")
    return problems


# ------------------------------------------------------------------ the tools


def run(cmd: list[str], report: str | None = None, ok_codes=(0,), env: dict[str, str] | None = None) -> str:
    if shutil.which(cmd[0]) is None:
        raise SystemExit(f"✖ {cmd[0]} is not installed: install the toolchain with `make setup` (mise.toml pins it)")
    p = subprocess.run(cmd, cwd=ROOT, capture_output=True, text=True, env={**os.environ, **(env or {})})
    if p.returncode not in ok_codes:
        raise SystemExit(f"✖ {' '.join(cmd[:3])} failed ({p.returncode}):\n{p.stderr[-3000:]}")
    if report:
        (REPORTS / report).write_text(p.stdout)
    return p.stdout


def rel(path: str) -> str:
    path = path.replace(str(ROOT) + "/", "")
    return path.lstrip("/")


def gosec() -> list[Finding]:
    out, seen = [], set()
    for tags in ("", "local"):
        cmd = ["gosec", "-quiet", "-fmt", "json", "-no-fail", "-exclude-generated"]
        if tags:
            cmd += ["-tags", tags]
        data = json.loads(run(cmd + ["./..."], f"gosec{'-' + tags if tags else ''}.json") or "{}")
        for i in data.get("Issues") or []:
            k = (i["rule_id"], i["file"], i["line"])
            if k not in seen:
                seen.add(k)
                out.append(Finding("gosec", i["rule_id"], rel(i["file"]), f"line {i['line']}: {i['details']} ({i['severity']})",
                                   i["severity"] == "HIGH"))
    return out


def semgrep() -> list[Finding]:
    cmd = ["semgrep", "scan", "--metrics=off", "--quiet", "--json"]
    for c in ("p/golang", "p/typescript", "p/react", "p/dockerfile"):
        cmd += ["--config", c]
    for e in EXCLUDE:
        cmd += ["--exclude", e]
    data = json.loads(run(cmd + ["."], "semgrep.json"))
    if data.get("errors"):
        raise SystemExit(f"✖ semgrep reported errors: {data['errors'][:3]}")
    return [Finding("semgrep", r["check_id"].split(".")[-1], r["path"], f"line {r['start']['line']}: {r['extra']['message'][:160]}",
                    r["extra"]["severity"] == "ERROR") for r in data["results"]]


def gitleaks() -> list[Finding]:
    REPORTS.mkdir(parents=True, exist_ok=True)
    report = REPORTS / "gitleaks.json"
    # The history of the commit under test only: CI fetches every branch, and other branches are
    # scanned in their own pull requests.
    run(["gitleaks", "git", "--no-banner", "--redact", "--exit-code", "0", "--log-opts=HEAD", "--report-format", "json",
         "--report-path", str(report), "."])
    return [Finding("gitleaks", x["RuleID"], x["File"], f"commit {x['Commit'][:12]} line {x['StartLine']}", True)
            for x in json.loads(report.read_text() or "[]")]


def grype() -> list[Finding]:
    cmd = ["grype", "dir:.", "-o", "json", "-q"]
    for e in EXCLUDE:
        cmd += ["--exclude", f"./**/{e}/**"]
    data = json.loads(run(cmd, "grype.json"))
    out = []
    for m in data.get("matches", []):
        v, a = m["vulnerability"], m["artifact"]
        fix = ", ".join(v.get("fix", {}).get("versions") or []) or "no fix yet"
        out.append(Finding("grype", v["id"], a["name"], f"{a['name']} {a['version']}: {v['severity']} (fixed in {fix})",
                           v["severity"] == "Critical"))
    return out


def checkov() -> list[Finding]:
    cmd = ["checkov", "-d", ".", "--framework", "dockerfile", "github_actions", "-o", "json", "--quiet", "--compact"]
    for e in EXCLUDE:
        cmd += ["--skip-path", e]
    raw = run(cmd, "checkov.json", ok_codes=(0, 1))
    data = json.loads(raw) if raw.strip() else []
    out = []
    for report in data if isinstance(data, list) else [data]:
        for c in report.get("results", {}).get("failed_checks", []):
            out.append(Finding("checkov", c["check_id"], rel(c["file_path"]), c["check_name"], True))
    return out


def workflows() -> list[Finding]:
    out = []
    for line in run(["actionlint", "-oneline"], "actionlint.txt", ok_codes=(0, 1)).splitlines():
        m = re.match(r"(.+?):\d+:\d+: (.*) \[(.+)\]$", line)
        if m:
            out.append(Finding("actionlint", m[3], m[1], m[2], True))
    for f in json.loads(run(["zizmor", "--offline", "--format", "json", ".github/workflows"], "zizmor.json",
                            ok_codes=tuple(range(0, 20))) or "[]"):
        loc = f["locations"][0]["symbolic"]["key"]["Local"]
        path = loc.get("verbatim_path") or loc.get("given_path", "")
        sev = f["determinations"]["severity"]
        out.append(Finding("zizmor", f["ident"], path, f"{sev}: {f['desc'] if 'desc' in f else f['ident']}", sev == "High"))
    return out


def licences() -> list[Finding]:
    out = []
    for tags in ("", "local"):
        report = run(["go-licenses", "report", "./..."], f"licences-go{'-' + tags if tags else ''}.csv",
                     env={"GOFLAGS": f"-tags={tags}"} if tags else None)
        for row in csv.reader(io.StringIO(report)):
            if len(row) == 3 and not row[0].startswith("github.com/cognizhi/fyniq"):
                out.append(Finding("licences", row[2], row[0], f"Go module licensed {row[2]}", row[2] not in ALLOWED))
    for prod in (True, False):
        cmd = ["pnpm", "licenses", "list", "--json"] + (["--prod"] if prod else [])
        for lic, pkgs in json.loads(run(cmd, f"licences-npm-{'prod' if prod else 'all'}.json")).items():
            for p in pkgs:
                blocking = DENIED.search(lic) is not None or (prod and lic not in ALLOWED)
                out.append(Finding("licences", lic, p["name"], f"npm package licensed {lic}{'' if prod else ' (development only)'}", blocking))
    return out


TOOLS = {"workflows": workflows, "gosec": gosec, "semgrep": semgrep, "gitleaks": gitleaks, "grype": grype,
         "checkov": checkov, "licences": licences}
# A waiver's tool, mapped to the scan that produces its findings.
SCAN_OF = {"actionlint": "workflows", "zizmor": "workflows"}


# req: SEC-21
# req: CTL-CHG-03
def self_test() -> None:
    today = dt.date(2026, 10, 15)
    w = [Waiver("gosec", "G115", "internal/domain/money/*.go", "bounded by the ISO 4217 table", dt.date(2027, 1, 1)),
         Waiver("gitleaks", "*", "old.txt", "a removed test fixture, long gone", dt.date(2026, 1, 1)),
         Waiver("grype", "CVE-1", "left-pad", "no longer used anywhere at all", dt.date(2027, 1, 1))]
    f = [Finding("gosec", "G115", "internal/domain/money/money.go", "x", True),
         Finding("gosec", "G115", "internal/app/app.go", "x", True),
         Finding("gosec", "G304", "internal/seed/seed.go", "x", False),
         Finding("gitleaks", "generic-api-key", "old.txt", "x", True)]
    problems = judge(f, w, {"gosec", "gitleaks", "grype"}, today)
    want = ["waiver expired", "gosec: G115 in internal/app/app.go", "gitleaks: generic-api-key in old.txt", "waiver matches nothing: grype"]
    for i, p in enumerate(want):
        if not any(x.startswith(p) for x in problems):
            raise SystemExit(f"✖ scan self-test: want a problem starting {p!r}, got {problems}")
    if len(problems) != 4 or f[0].waived_by == "" or f[2].waived_by != "":
        raise SystemExit(f"✖ scan self-test: {problems}, {f}")
    load_waivers()  # the real file parses and every waiver has its fields
    print("✔ scan waivers: the rules hold on planted findings, and compliance/scan-waivers.toml is well formed")


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--only", help="comma-separated: " + ",".join(TOOLS))
    ap.add_argument("--self-test", action="store_true")
    args = ap.parse_args()
    if args.self_test:
        self_test()
        return 0
    names = args.only.split(",") if args.only else list(TOOLS)
    unknown = [n for n in names if n not in TOOLS]
    if unknown:
        raise SystemExit(f"✖ unknown scans: {unknown}")
    REPORTS.mkdir(parents=True, exist_ok=True)
    os.environ.setdefault("SEMGREP_SEND_METRICS", "off")
    findings = []
    for n in names:
        found = TOOLS[n]()
        print(f"  {n}: {len(found)} finding(s), {sum(f.blocking for f in found)} blocking before waivers")
        findings += found
    waivers = load_waivers()
    ran = set(names) | {t for t, s in SCAN_OF.items() if s in names}
    problems = judge(findings, waivers, ran, dt.date.today())
    summary = [{"tool": f.tool, "rule": f.rule, "path": f.path, "detail": f.detail, "blocking": f.blocking, "waived_by": f.waived_by}
               for f in findings]
    (REPORTS / "summary.json").write_text(json.dumps({"date": dt.date.today().isoformat(), "scans": names,
                                                     "problems": problems, "findings": summary}, indent=1))
    waived = sum(1 for f in findings if f.waived_by)
    if problems:
        print("✖ security scan failed (SEC-21):\n  " + "\n  ".join(problems))
        print(f"  reports in {REPORTS.relative_to(ROOT)}/")
        return 1
    print(f"✔ security scans clean: {len(findings)} finding(s), none blocking unwaived ({waived} waived); reports in .data/scan/")
    return 0


if __name__ == "__main__":
    sys.exit(main())
