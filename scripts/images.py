#!/usr/bin/env python3
"""Checks the images `make images` built (PRD-001 §12.9), and scans them.

    python3 scripts/images.py check --version 0.1.0 --flavours "release local" [--platform linux/arm64]
    python3 scripts/images.py platforms                # every pinned image has linux/amd64 and linux/arm64 (make images)
    python3 scripts/images.py scan --version 0.1.0     # SBOM and vulnerability scan (make image-scan)
    python3 scripts/images.py sign --version 0.1.0     # push by digest, sign, attest the SBOM, verify (make image-sign)
    python3 scripts/images.py self-test                # the checks on planted images (part of make check)

check (part of `make images`; Docker only):
  - the compressed size is within deploy/docker/budgets.json (NFR-IMG-04);
  - the image runs as its non-root user, with the binary as its entrypoint (NFR-IMG-02, SEC-27);
  - a Go image holds no shell, package manager or toolchain (NFR-IMG-01); the web image is the
    nginx-unprivileged Alpine image §12.9 names, used locally and in previews only;
  - a release binary contains no local adapter, and a local one does, which proves the check
    (DEV-07, CTL-CHG-09, QA-IMG-01);
  - the image, and the ELF binary it runs, are for the platform asked for: --platform, else the Docker
    engine's own, so a build on an Apple-silicon laptop is linux/arm64 and runs natively (NFR-IMG-08).
platforms (part of `make images`; needs the registries): every image pinned by digest in the Dockerfiles,
the local stack, the dev container, the database tests and this script is a multi-platform index with
linux/amd64 and linux/arm64, so neither platform needs emulation (NFR-IMG-08).
scan (CI): an SPDX SBOM per release image (syft) and a grype scan; a Critical finding fails unless
compliance/scan-waivers.toml waives it (tool "grype-image"; SEC-21). Reports go to .data/images/.
sign (CI, after scan): pushes each release image to a registry, signs it by digest with cosign, attaches
its SBOM as an SPDX attestation and verifies both, then shows that verification refuses a wrong key and an
unsigned image (NFR-IMG-05, SEC-22, CTL-CHG-04). Without --registry and --key it starts a throwaway
registry and makes a key pair for the run, so a pull request proves the mechanism with no cloud account,
no OIDC token and nothing published. The release workflow signs keyless with GitHub OIDC and pushes to
Artifact Registry (p0-ci-images-release design; with p0-infra-opentofu). The private key stays in a
temporary directory; the evidence (signatures.json, cosign.pub) goes to .data/images/.
"""

from __future__ import annotations

import argparse
import base64
import datetime as dt
import io
import json
import os
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile
import time
import urllib.request
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
# The throwaway registry `sign` pushes to without --registry: a stand-in for Artifact Registry, never a
# registry of record. Dependabot does not read a pin in a script, so update this one by hand.
REGISTRY_IMAGE = "registry:3.0.0@sha256:6c5666b861f3505b116bb9aa9b25175e71210414bd010d92035ff64018f9457e"
SPDX = "https://spdx.dev/Document"   # the predicate type of `cosign attest --type spdxjson`
# The platforms every image builds for (NFR-IMG-08), and the ELF e_machine of each one's binaries.
PLATFORMS = ("linux/amd64", "linux/arm64")
ELF_MACHINES = {62: "amd64", 183: "arm64"}   # EM_X86_64, EM_AARCH64
# Where images are pinned by digest; each must be a multi-platform index with PLATFORMS.
PINNED_IN = ("deploy/docker/Dockerfile.go-service", "deploy/docker/Dockerfile.web", "deploy/local/compose.yaml",
             ".devcontainer/Dockerfile", "internal/adapters/postgres/pgtest/pgtest.go", "scripts/images.py")
PIN = re.compile(r"([a-z0-9][a-z0-9._/-]*(?::[A-Za-z0-9._-]+)?)@(sha256:[0-9a-f]{64})")


def sh(*cmd: str, capture: bool = True) -> str:
    if shutil.which(cmd[0]) is None:
        raise SystemExit(f"✖ {cmd[0]} is not installed: install the toolchain with `make setup` (mise.toml pins it)")
    p = subprocess.run(cmd, cwd=ROOT, capture_output=capture, text=True)
    if p.returncode != 0:
        raise SystemExit(f"✖ {' '.join(cmd[:4])}: {p.stderr.strip()[-2000:]}")
    return p.stdout


def run(*cmd: str, env: dict[str, str] | None = None) -> subprocess.CompletedProcess[str]:
    """A command whose failure the caller judges (sh stops at the first failure)."""
    if shutil.which(cmd[0]) is None:
        raise SystemExit(f"✖ {cmd[0]} is not installed: install the toolchain with `make setup` (mise.toml pins it)")
    return subprocess.run(cmd, cwd=ROOT, capture_output=True, text=True, env=env)


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


def filesystem(image: str, binary: str) -> tuple[list[str], bytes]:
    """The image's files, and the named binary (empty when it has none)."""
    cid = sh("docker", "create", image).strip()
    try:
        raw = subprocess.run(["docker", "export", cid], capture_output=True, check=True).stdout
    finally:
        sh("docker", "rm", "-f", cid)
    names, app = [], b""
    with tarfile.open(fileobj=io.BytesIO(raw)) as tar:
        for m in tar:
            names.append(m.name.lstrip("./"))
            if m.name.lstrip("./") == binary and m.isfile():
                f = tar.extractfile(m)
                app = f.read() if f else b""
    return names, app


def tags(version: str, flavours: list[str]) -> list[tuple[str, str, str]]:
    out = [(svc, f, f"fyniq-{svc}:{'local' if f == 'local' else version}") for svc in GO_IMAGES for f in flavours]
    return out + [("web", "local", "fyniq-web:local")]


def elf_arch(binary: bytes) -> str:
    """The architecture a 64-bit little-endian ELF binary runs on, from its header."""
    if binary[:4] != b"\x7fELF" or binary[4:6] != b"\x02\x01":
        return "not a 64-bit little-endian ELF binary"
    machine = int.from_bytes(binary[18:20], "little")
    return ELF_MACHINES.get(machine, f"ELF machine {machine}")


def judge(svc: str, flavour: str, image: str, size: int, cfg: dict, names: list[str], app: bytes,
          platform: str = "linux/amd64", built: str = "linux/amd64") -> list[str]:
    """What is wrong with one built image (§12.9). app is its binary: /app, or nginx in the web image."""
    problems = []
    if size > BUDGETS[svc] * 1_000_000:
        problems.append(f"{image} is {size / 1e6:.1f} MB compressed, over its {BUDGETS[svc]} MB budget (NFR-IMG-04)")
    if built != platform:
        problems.append(f"{image} is built for {built}, not {platform} (NFR-IMG-08)")
    if app and (arch := elf_arch(app)) != platform.split("/")[1]:
        problems.append(f"{image}: its binary is {arch}, not {platform.split('/')[1]}, so it would not run natively (NFR-IMG-08)")
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


def engine_platform() -> str:
    """The Docker engine's own platform: what a build without --platform makes."""
    return sh("docker", "version", "--format", "{{.Server.Os}}/{{.Server.Arch}}").strip()


def check(version: str, flavours: list[str], platform: str) -> int:
    platform = platform or engine_platform()
    if platform not in PLATFORMS:
        raise SystemExit(f"✖ images: {platform} is not one of {', '.join(PLATFORMS)} (NFR-IMG-08)")
    problems, rows = [], []
    for svc, flavour, image in tags(version, flavours):
        size = compressed_size(image)
        info = json.loads(sh("docker", "image", "inspect", image))[0]
        names, app = filesystem(image, "usr/sbin/nginx" if svc == "web" else "app")
        rows.append(f"  {image:<28} {size / 1e6:6.1f} MB of {BUDGETS[svc]} MB  {info['Os']}/{info['Architecture']}")
        problems += judge(svc, flavour, image, size, info["Config"], names, app, platform, f"{info['Os']}/{info['Architecture']}")
    print("\n".join(rows))
    if problems:
        print("✖ images:\n  " + "\n  ".join(problems))
        return 1
    print(f"✔ {len(rows)} images for {platform}, within budget, non-root, without a shell; release images without local adapters")
    return 0


def missing_platforms(manifest: dict) -> list[str]:
    """The PLATFORMS a registry manifest lacks: all of them unless it is a multi-platform index."""
    have = {f"{m['platform']['os']}/{m['platform']['architecture']}" for m in manifest.get("manifests", []) if "platform" in m}
    return [p for p in PLATFORMS if p not in have]


def pins() -> dict[str, str]:
    """Every image pinned by digest where images are pinned: name@digest → where it is pinned."""
    found: dict[str, str] = {}
    for f in PINNED_IN:
        for m in PIN.finditer((ROOT / f).read_text()):
            name = m.group(1)
            if ":" in name.rsplit("/", 1)[-1]:   # the tag goes: a moved tag must not hide the pinned digest
                name = name.rsplit(":", 1)[0]
            found.setdefault(f"{name}@{m.group(2)}", f"{f}: {m.group(1)}")
    return found


# req: NFR-IMG-08
def platforms() -> int:
    problems, refs = [], pins()
    for ref, where in sorted(refs.items(), key=lambda kv: kv[1]):
        raw = run("docker", "buildx", "imagetools", "inspect", "--raw", ref)
        if raw.returncode:
            raise SystemExit(f"✖ {where}: cannot read its manifest ({raw.stderr.strip()[-300:]}); `make images` needs the registries and docker buildx")
        if missing := missing_platforms(json.loads(raw.stdout)):
            problems.append(f"{where} has no {' or '.join(missing)} image: pin a multi-platform index, or an image built for both (NFR-IMG-08)")
    if problems:
        print("✖ pinned images:\n  " + "\n  ".join(problems))
        return 1
    print(f"✔ {len(refs)} pinned images are multi-platform, each with {' and '.join(PLATFORMS)}")
    return 0


# req: NFR-IMG-04
# req: NFR-IMG-01
# req: NFR-IMG-02
# req: NFR-IMG-08
# req: DEV-07
# req: CTL-CHG-09
# req: CTL-CHG-08
def self_test() -> None:
    good = {"User": "65532:65532", "Entrypoint": ["/app"]}
    elf = lambda machine: b"\x7fELF\x02\x01" + bytes(12) + machine.to_bytes(2, "little")  # noqa: E731  (an ELF header)
    if judge("api", "release", "fyniq-api:0", 5_000_000, good, ["app", "etc/passwd"], elf(62) + b"main"):
        raise SystemExit("✖ images self-test: a good image was refused")
    planted = judge("api", "release", "fyniq-api:0", 31_000_000, {"User": "0:0", "Entrypoint": ["/bin/sh"]},
                    ["app", "bin/sh"], elf(62) + LOCAL_MARK)
    if len(planted) != 4:
        raise SystemExit(f"✖ images self-test: want 4 problems (budget, user, shell, local adapter), got {planted}")
    if not judge("web", "local", "fyniq-web:local", 26_000_000, {"User": "root"}, [], b""):
        raise SystemExit("✖ images self-test: an over-budget web image running as root was accepted")
    # Platforms (NFR-IMG-08): an amd64 binary in an arm64 image, an image for the wrong platform, a single-platform pin.
    if judge("api", "release", "fyniq-api:0", 5_000_000, good, ["app"], elf(183), "linux/arm64", "linux/arm64"):
        raise SystemExit("✖ images self-test: a good arm64 image was refused")
    if len(judge("api", "release", "fyniq-api:0", 5_000_000, good, ["app"], elf(62), "linux/arm64", "linux/amd64")) != 2 \
            or not judge("web", "local", "fyniq-web:local", 5_000_000, {"User": "101"}, [], elf(62), "linux/arm64", "linux/arm64"):
        raise SystemExit("✖ images self-test: an image or a binary for the wrong platform was accepted")
    index = {"manifests": [{"platform": {"os": "linux", "architecture": a}} for a in ("amd64", "arm64", "s390x")]}
    if missing_platforms(index) or missing_platforms({"mediaType": "application/vnd.oci.image.manifest.v1+json"}) != list(PLATFORMS) \
            or missing_platforms({"manifests": index["manifests"][:1]}) != ["linux/arm64"]:
        raise SystemExit("✖ images self-test: a single-platform pin, or an index without arm64, was accepted")
    # Signing: a verified answer counts only for the digest it names, an attestation only with the SPDX type.
    answer = json.dumps([{"critical": {"image": {"docker-manifest-digest": "sha256:aa"}}}])
    statement = {"predicateType": SPDX, "subject": [{"digest": {"sha256": "bb"}}]}
    line = lambda st: json.dumps({"payload": base64.b64encode(json.dumps(st).encode()).decode()})  # noqa: E731
    if signed_digests(answer) != {"sha256:aa"} or attested(line(statement), SPDX) != {"sha256:bb"} \
            or attested(line({**statement, "predicateType": "https://slsa.dev/provenance/v1"}), SPDX):
        raise SystemExit("✖ images self-test: a signature or an attestation counted for the wrong digest or type")
    print("✔ image checks: an over-budget image, a root user, a shell, a local adapter in release, the wrong platform"
          " and a single-platform pin are each refused; a signature counts only for its digest")


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


def signed_digests(answer: str) -> set[str]:
    """The manifest digests a `cosign verify` answer vouches for."""
    return {sig["critical"]["image"]["docker-manifest-digest"] for sig in json.loads(answer)}


def attested(answer: str, predicate: str) -> set[str]:
    """The digests a `cosign verify-attestation` answer attests with the predicate type."""
    out = set()
    for line in answer.splitlines():
        if line.strip():
            statement = json.loads(base64.b64decode(json.loads(line)["payload"]))
            if statement.get("predicateType") == predicate:
                out |= {"sha256:" + s["digest"]["sha256"] for s in statement.get("subject", [])}
    return out


def push(image: str, target: str, tagged: list[str]) -> str:
    """Pushes the image to target and returns it by digest (registry/name@sha256:…)."""
    sh("docker", "tag", image, target)
    tagged.append(target)
    sh("docker", "push", "-q", target)
    name = target.rsplit(":", 1)[0] + "@"
    found = [d for d in json.loads(sh("docker", "image", "inspect", target))[0].get("RepoDigests", []) if d.startswith(name)]
    if not found:
        raise SystemExit(f"✖ {target}: pushed, but Docker reports no digest for it")
    return found[0]


def start_registry() -> tuple[str, str]:
    """A throwaway registry on a free loopback port: (container, host:port)."""
    container = sh("docker", "run", "-d", "--rm", "-p", "127.0.0.1:0:5000", REGISTRY_IMAGE).strip()
    registry = "localhost:" + sh("docker", "port", container, "5000/tcp").splitlines()[0].rsplit(":", 1)[1]
    for _ in range(100):
        try:
            with urllib.request.urlopen(f"http://{registry}/v2/", timeout=1):  # noqa: S310  (a loopback URL we built)
                return container, registry
        except OSError:
            time.sleep(0.1)
    raise SystemExit(f"✖ the throwaway registry on {registry} did not start")


# The CI proof that every release image is signed by digest and carries its SBOM, and that verification
# refuses a wrong key and an unsigned image.
# req: NFR-IMG-05
# req: SEC-22
# req: CTL-CHG-04
def sign_images(version: str, registry: str, key: str) -> int:
    for svc in GO_IMAGES:
        if not (REPORTS / f"{svc}.spdx.json").is_file():
            raise SystemExit(f"✖ fyniq-{svc}: no SBOM in .data/images/: run `make image-scan` first, which writes them")
    tmp = Path(tempfile.mkdtemp(prefix="fyniq-sign-"))
    env = {**os.environ, "COSIGN_PASSWORD": os.environ.get("COSIGN_PASSWORD", "")}   # the run's own key pair has none
    container, throwaway, tagged = "", not registry, []
    try:
        if throwaway:
            container, registry = start_registry()
        ephemeral = not key
        # The private key stays in tmp: .data/images/ is uploaded as evidence. "other" never signs anything.
        for name in (["cosign", "other"] if ephemeral else ["other"]):
            if (p := run("cosign", "generate-key-pair", "--output-key-prefix", str(tmp / name), env=env)).returncode:
                raise SystemExit(f"✖ cosign generate-key-pair: {p.stderr.strip()[-500:]}")
        key = key or str(tmp / "cosign.key")
        public = str(tmp / "signer.pub")   # from a key file or a KMS URI alike
        if (p := run("cosign", "public-key", "--key", key, "--outfile", public, env=env)).returncode:
            raise SystemExit(f"✖ cosign public-key --key {key}: {p.stderr.strip()[-500:]}")
        shutil.copy(public, REPORTS / "cosign.pub")
        rows, problems = [], []
        for svc in GO_IMAGES:
            ref = push(f"fyniq-{svc}:{version}", f"{registry}/fyniq-{svc}:{version}", tagged)
            digest = ref.split("@", 1)[1]
            for step in (["sign"], ["attest", "--type", "spdxjson", "--predicate", str(REPORTS / f"{svc}.spdx.json")]):
                if (p := run("cosign", step[0], "--yes", "--key", key, "--tlog-upload=false", *step[1:], ref, env=env)).returncode:
                    raise SystemExit(f"✖ cosign {step[0]} {ref}: {p.stderr.strip()[-1000:]}")
            v = run("cosign", "verify", "--key", public, "--insecure-ignore-tlog=true", ref, env=env)
            a = run("cosign", "verify-attestation", "--key", public, "--insecure-ignore-tlog=true", "--type", "spdxjson", ref, env=env)
            signed = v.returncode == 0 and digest in signed_digests(v.stdout)
            sbom = a.returncode == 0 and digest in attested(a.stdout, SPDX)
            if not signed:
                problems.append(f"{ref}: the signature does not verify for this digest: {v.stderr.strip()[-300:]}")
            if not sbom:
                problems.append(f"{ref}: the SBOM attestation does not verify for this digest: {a.stderr.strip()[-300:]}")
            rows.append({"image": f"fyniq-{svc}:{version}", "reference": ref, "digest": digest, "signature_verified": signed,
                         "sbom_attestation_verified": sbom})

        # The planted failures: verification must refuse a key that did not sign, and an image nobody signed.
        refused = {}
        w = run("cosign", "verify", "--key", str(tmp / "other.pub"), "--insecure-ignore-tlog=true", rows[0]["reference"], env=env)
        refused["wrong_key"] = w.returncode != 0
        if w.returncode == 0:
            problems.append(f"{rows[0]['reference']} verified with a key that never signed it")
        if throwaway:   # the local flavour goes only to the throwaway registry, never to a real one (DEV-07)
            unsigned = push("fyniq-api:local", f"{registry}/fyniq-api:local", tagged)
            u = run("cosign", "verify", "--key", public, "--insecure-ignore-tlog=true", unsigned, env=env)
            refused["unsigned_image"] = u.returncode != 0
            if u.returncode == 0:
                problems.append(f"{unsigned}: an image nobody signed verified")

        cosign_version = json.loads(run("cosign", "version", "--json").stdout or "{}").get("gitVersion", "unknown")
        (REPORTS / "signatures.json").write_text(json.dumps({
            "version": version,
            "git_sha": run("git", "rev-parse", "HEAD").stdout.strip(),
            "signed_at": dt.datetime.now(dt.timezone.utc).isoformat(timespec="seconds"),
            "cosign": cosign_version,
            "registry": f"throwaway {REGISTRY_IMAGE.split('@')[0]} on {registry}" if throwaway else registry,
            "key": "a key pair made for this run; its public half is cosign.pub" if ephemeral else key,
            "key_custody": "made for this run and deleted after it; the release workflow signs keyless with GitHub OIDC" if ephemeral else "as named",
            "transparency_log": "not used: nothing from a pull request is published; verified against the key",
            "images": rows,
            "refused": refused,
            "problems": problems,
        }, indent=1))
    finally:
        shutil.rmtree(tmp, ignore_errors=True)
        if tagged:      # the registry's tags only; the images keep their own
            subprocess.run(["docker", "rmi", *tagged], capture_output=True)
        if container:
            subprocess.run(["docker", "rm", "-f", container], capture_output=True)
    if problems:
        print("✖ image signing (NFR-IMG-05, SEC-22):\n  " + "\n  ".join(problems))
        return 1
    print(f"✔ {len(rows)} release images pushed by digest, signed and their SBOMs attested; both verify, and a wrong key"
          f"{' and an unsigned image are' if throwaway else ' is'} refused; evidence in .data/images/signatures.json")
    return 0


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("command", choices=["check", "platforms", "scan", "sign", "self-test"])
    ap.add_argument("--version", default="0.0.0")
    ap.add_argument("--flavours", default="release local")
    ap.add_argument("--platform", default="", help="check: the platform the images were built for; default the Docker engine's")
    ap.add_argument("--registry", default="", help="sign: push here (host[:port][/path]); default a throwaway local registry")
    ap.add_argument("--key", default="", help="sign: a cosign key reference (a key file or a KMS URI); default a key pair made for the run")
    a = ap.parse_args()
    if a.command == "self-test":
        self_test()
        return 0
    if a.command == "check":
        return check(a.version, a.flavours.split(), a.platform)
    if a.command == "platforms":
        return platforms()
    if a.command == "sign":
        return sign_images(a.version, a.registry, a.key)
    return scan_images(a.version)


if __name__ == "__main__":
    sys.exit(main())
