#!/usr/bin/env python3
"""DAST (CTL-CHG-07, PRD-001 rev 0.246; p1-security-hardening design D9): ZAP's baseline scan of a running Fyniq.

    python3 scripts/dast.py                              # the local stack on :8080, signed in through the local sign-in page
    python3 scripts/dast.py --origin URL --anonymous     # another origin, such as staging, without signing in
    python3 scripts/dast.py asvs                         # the ASVS coverage of the security tests (SEC-20)
    python3 scripts/dast.py self-test

`make dast` runs the first against the stack `KEEP=1 scripts/local-smoke.sh` left running, as CI does after the smoke
test. ZAP's baseline spiders the origin, with its AJAX spider too (a browser, which runs the single-page app and so
reaches its pages and the API calls they make), and reports what its passive rules see, with the session cookie on
every request so the app's pages are reached. compliance/zap-rules.tsv sets each rule's level, as ZAP reads it: FAIL,
WARN or IGNORE. A High alert, or one of a FAIL rule, fails the scan unless compliance/scan-waivers.toml waives it
(tool "zap", the rule's ID, the alert's path) with a reason and an expiry. The reports land in .data/dast/: ZAP's
HTML and JSON, and dast.md, the evidence. ZAP runs from its image, pinned by digest (both platforms).
"""
from __future__ import annotations

import argparse
import datetime as dt
import http.cookiejar
import json
import os
import platform
import re
import shutil
import subprocess
import sys
import urllib.parse
import urllib.request
from collections import Counter
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import scan  # noqa: E402  (the waivers and their judgement, shared with make scan)

ROOT = Path(__file__).resolve().parent.parent
REPORTS = ROOT / ".data" / "dast"
RULES = ROOT / "compliance" / "zap-rules.tsv"
# ZAP 2.17.0 (15 Dec 2025), its multi-platform index.
ZAP_IMAGE = "ghcr.io/zaproxy/zaproxy:2.17.0@sha256:9624669db72f54d86c4b46a7064518bd9bf79d8c61a4c31befa9923489f328af"
LEVELS = {"FAIL", "WARN", "IGNORE"}
HIGH = "3"   # ZAP's riskcode for High


def rules(path: Path = RULES) -> dict[str, str]:
    """compliance/zap-rules.tsv: a rule's ID, its level and its name, tab-separated; # starts a comment."""
    out = {}
    for n, line in enumerate(path.read_text().splitlines(), 1):
        if not line.strip() or line.startswith("#"):
            continue
        parts = line.split("\t")
        if len(parts) < 3 or not parts[0].isdigit() or parts[1] not in LEVELS:
            raise SystemExit(f"✖ {path.name}:{n}: want <rule ID>\\t<FAIL|WARN|IGNORE>\\t<name>, got {line!r}")
        if parts[0] in out:
            raise SystemExit(f"✖ {path.name}:{n}: rule {parts[0]} is listed twice")
        out[parts[0]] = parts[1]
    return out


def findings(report: dict, levels: dict[str, str]) -> list[scan.Finding]:
    """ZAP's JSON report as findings: one per alert and path, blocking when High or of a FAIL rule; IGNORE rules
    dropped."""
    out, seen = [], set()
    for site in report.get("site", []):
        for a in site.get("alerts", []):
            rule = str(a.get("pluginid", ""))
            level = levels.get(rule, "WARN")
            if level == "IGNORE":
                continue
            for inst in a.get("instances", []) or [{"uri": site.get("@name", "")}]:
                path = urllib.parse.urlsplit(inst.get("uri", "")).path or "/"
                if (rule, path) in seen:
                    continue
                seen.add((rule, path))
                out.append(scan.Finding("zap", rule, path, f"{a.get('name', '')} ({a.get('riskdesc', '')})",
                                        blocking=a.get("riskcode") == HIGH or level == "FAIL"))
    return out


def sign_in(origin: str) -> str:
    """Signs in through the local sign-in page, as scripts/local-smoke.sh does, and returns the session cookie."""
    jar = http.cookiejar.CookieJar()
    opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))
    with opener.open(f"{origin}/api/auth/signin?provider=local&return_to=/", timeout=30) as r:
        page = r.geturl()
    if "/api/auth/dev/authorize?" not in page:
        raise SystemExit(f"✖ the sign-in flow did not reach the local sign-in page: {page}")
    form = urllib.parse.parse_qs(urllib.parse.urlsplit(page).query)
    form.update({"subject": ["smoke-founder"], "name": ["Smoke Founder"], "decision": ["sign-in"], "second_factor": ["yes"]})
    with opener.open(f"{origin}/api/auth/dev/authorize", data=urllib.parse.urlencode(form, doseq=True).encode(), timeout=30):
        pass
    for c in jar:
        if c.name == "fyniq_session":
            return c.value
    raise SystemExit("✖ signing in set no session cookie")


def zap(origin: str, session: str | None) -> dict:
    """Runs ZAP's baseline against origin and returns its JSON report."""
    if shutil.which("docker") is None:
        raise SystemExit("✖ docker is not installed: ZAP runs from its image")
    REPORTS.mkdir(parents=True, exist_ok=True)
    for f in ("zap.json", "zap.html"):
        (REPORTS / f).unlink(missing_ok=True)
    shutil.copy(RULES, REPORTS / "zap-rules.tsv")
    target = origin
    if platform.system() != "Linux":   # Docker Desktop and Colima: the host is not the container's localhost
        target = re.sub(r"//(localhost|127\.0\.0\.1)\b", "//host.docker.internal", origin)
    options = []
    if session:   # the session on every request ZAP sends, so the spider reaches the app's pages
        options = ["replacer.full_list(0).description=session", "replacer.full_list(0).enabled=true",
                   "replacer.full_list(0).matchtype=REQ_HEADER", "replacer.full_list(0).matchstr=Cookie",
                   "replacer.full_list(0).regex=false", f"replacer.full_list(0).replacement=fyniq_session={session}"]
    network = ["--network", "host"] if platform.system() == "Linux" else ["--add-host", "host.docker.internal:host-gateway"]
    cmd = ["docker", "run", "--rm", *network, "-v", f"{REPORTS}:/zap/wrk:rw", ZAP_IMAGE, "zap-baseline.py", "-t", target,
           "-c", "zap-rules.tsv", "-J", "zap.json", "-r", "zap.html", "-m", "3", "-j", "-I"]
    if options:
        cmd += ["-z", " ".join(f"-config {o}" for o in options)]
    os.chmod(REPORTS, 0o777)   # ZAP writes as its own user
    p = subprocess.run(cmd, capture_output=True, text=True)
    (REPORTS / "zap.log").write_text(p.stdout + p.stderr)
    if not (REPORTS / "zap.json").exists():
        raise SystemExit(f"✖ ZAP wrote no report ({p.returncode}); see .data/dast/zap.log:\n{p.stderr[-2000:]}")
    return json.loads((REPORTS / "zap.json").read_text())


def evidence(origin: str, signed_in: bool, found: list[scan.Finding], problems: list[str]) -> str:
    lines = [f"# DAST baseline (CTL-CHG-07): {origin}", "",
             f"{dt.datetime.now(dt.timezone.utc):%Y-%m-%d %H:%M} UTC, ZAP {ZAP_IMAGE.split('@')[0].rsplit(':', 1)[1]}, "
             f"{'signed in through the local sign-in page' if signed_in else 'not signed in'}.", "",
             "| Rule | Path | Alert | Blocks | Waived by |", "|---|---|---|---|---|"]
    for f in sorted(found, key=lambda f: (not f.blocking, f.rule, f.path)):
        lines.append(f"| {f.rule} | `{f.path}` | {f.detail} | {'yes' if f.blocking else 'no'} | {f.waived_by or ''} |")
    lines += ["", "**Fails:**" if problems else "**Passes:** no unwaived High alert and no alert of a FAIL rule."]
    lines += [f"- {p}" for p in problems]
    return "\n".join(lines) + "\n"


def asvs() -> int:
    """The ASVS 5.0 coverage of the security tests (SEC-20): how many tests carry an `asvs:` tag in each chapter."""
    tag = re.compile(r"^\s*(?://|#)\s*asvs:\s*(.+)$", re.M)   # a comment line, not a fixture inside a string
    chapters: Counter[str] = Counter()
    ids: set[str] = set()
    for f in [*ROOT.glob("internal/**/*_test.go"), *ROOT.glob("cmd/**/*_test.go"), *ROOT.glob("oracle/**/test_*.py")]:
        for m in tag.finditer(f.read_text(errors="replace")):
            for i in re.findall(r"V\d+(?:\.\d+)*", m.group(1)):
                ids.add(i)
                chapters[i.split(".")[0]] += 1
    REPORTS.mkdir(parents=True, exist_ok=True)
    out = ["# ASVS 5.0 coverage of the security tests (SEC-20)", "", f"{len(ids)} requirements tagged.", "",
           "| Chapter | Tags |", "|---|---|"]
    out += [f"| {c} | {n} |" for c, n in sorted(chapters.items(), key=lambda kv: int(kv[0][1:]))]
    (REPORTS / "asvs-coverage.md").write_text("\n".join(out) + "\n")
    print(f"✔ ASVS coverage: {len(ids)} requirements in {len(chapters)} chapters → .data/dast/asvs-coverage.md")
    return 0


def self_test() -> int:
    levels = {"10020": "FAIL", "10096": "IGNORE"}
    report = {"site": [{"@name": "http://localhost:8080", "alerts": [
        {"pluginid": "10020", "name": "Missing Anti-clickjacking Header", "riskcode": "2", "riskdesc": "Medium (Medium)",
         "instances": [{"uri": "http://localhost:8080/"}, {"uri": "http://localhost:8080/?a=1"}]},
        {"pluginid": "10096", "name": "Timestamp Disclosure", "riskcode": "0", "instances": [{"uri": "http://localhost:8080/x"}]},
        {"pluginid": "40012", "name": "Cross Site Scripting (Reflected)", "riskcode": "3", "instances": [{"uri": "http://localhost:8080/s"}]},
        {"pluginid": "10049", "name": "Storable and Cacheable Content", "riskcode": "0", "instances": [{"uri": "http://localhost:8080/"}]},
    ]}]}
    got = findings(report, levels)
    assert [(f.rule, f.path, f.blocking) for f in got] == [("10020", "/", True), ("40012", "/s", True), ("10049", "/", False)], got
    today = dt.date(2099, 1, 1)
    waiver = scan.Waiver("zap", "40012", "/s", "a reflected value the scan cannot tell apart", dt.date(2099, 2, 1))
    problems = scan.judge(got, [waiver], {"zap"}, today)
    assert problems == ["zap: 10020 in /: Missing Anti-clickjacking Header (Medium (Medium))"], problems
    assert rules()   # the repository's rules file reads
    print("✔ dast self-test")
    return 0


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("what", nargs="?", default="scan", choices=["scan", "asvs", "self-test"])
    ap.add_argument("--origin", default=f"http://localhost:{os.environ.get('FYNIQ_HTTP_PORT', '8080')}")
    ap.add_argument("--anonymous", action="store_true", help="scan without signing in (an origin with no local sign-in)")
    a = ap.parse_args()
    if a.what == "asvs":
        return asvs()
    if a.what == "self-test":
        return self_test()
    levels = rules()
    session = None if a.anonymous else sign_in(a.origin)
    found = findings(zap(a.origin, session), levels)
    problems = scan.judge(found, [w for w in scan.load_waivers() if w.tool == "zap"], {"zap"}, dt.date.today())
    (REPORTS / "dast.md").write_text(evidence(a.origin, session is not None, found, problems))
    if problems:
        print("✖ DAST baseline (CTL-CHG-07):\n  " + "\n  ".join(problems) + "\n  see .data/dast/zap.html")
        return 1
    blocking = sum(f.blocking for f in found)
    print(f"✔ DAST baseline: {len(found)} alert(s), none blocking unwaived ({blocking} waived) → .data/dast/")
    return 0


if __name__ == "__main__":
    sys.exit(main())
