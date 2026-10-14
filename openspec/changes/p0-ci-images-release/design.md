# Design

## Context

ADR-T5 (accepted) covers lean images, Conventional Commits and release-please, which already run. Two
release pull requests (#1 and #3, both "release 0.1.0") are open; release-please should own one.

## Goals / Non-Goals

**Goals:**
- Every merge proves the gates; every image that reaches staging is the signed, scanned digest CI built.

**Non-Goals:**
- Binary Authorization and SLSA level 3 provenance at deploy time (SEC-22's P1 part).

## Decisions

- **Two workflows.** `ci.yml` runs on pull requests and `main` (check, test, images, scans, QA-LOCAL-01);
  `release.yml` runs on tags (build, sign, push to Artifact Registry by digest, deploy to staging).
  Alternative: one workflow with conditions, rejected because the token permissions differ.
- **Scanners.** gosec and Semgrep (SAST), grype (images and dependencies), gitleaks (secrets), Checkov
  (OpenTofu) and go-licenses with a licence allow-list. Every tool is pinned in `mise.toml`.
- **Budgets as data.** The budget table lives in `deploy/docker/budgets.json`, copied from §12.9; CI
  compares each image's compressed size with it, and a change to the file needs review (NFR-IMG-04).
- **Signing.** cosign keyless with GitHub OIDC; staging deploys pass the digest, never a tag.
- **Audit events / ports.** None in the product; the CI run logs are the evidence (compliance-as-code).

## Risks / Trade-offs

- [Scanner noise blocks merges] → findings can be waived only in a reviewed file with an expiry date.
- [Two open release PRs] → close the stale one after confirming which release-please updates; the
  founder decides.
