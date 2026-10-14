# Proposal

## Why

P0 is done only when images are within budget, hardened, signed and deployed to staging by digest, and
the first release is cut by release-please (§17.3 exit items 1, 4 and 5). CI must also enforce every gate
from the first service onwards (golden rule 10, §12.9, §12.10, §13.3).

## What Changes

- CI workflows running `make check`, `make test` and, on `main`, `make ci`; the existing Conventional
  Commit checks stay (REL-01).
- Supply-chain rules: third-party actions pinned by SHA, least-privilege workflow tokens, no secrets for
  pull requests from forks, release and deploy workflows only on `main` and tags (SEC-28, CTL-CHG-11).
- Security gates: SAST, dependency and container scanning, secret scanning, infrastructure-as-code
  policy checks and licence checks, with critical findings blocking the merge (SEC-21, CTL-CHG-03).
- Images: multi-stage builds onto `distroless/static:nonroot`, non-root, read-only root filesystem, no
  capabilities, bases pinned by digest (NFR-IMG-01…03, SEC-27); the size budgets of the §12.9 table
  enforced in CI (NFR-IMG-04); an SBOM, a scan, a cosign signature and deployment by digest
  (NFR-IMG-05, SEC-22 signing part).
- The local flavour of each image built from the same commit, and the QA-LOCAL-01 job: `make up` from
  the built images with no cloud credentials, seed, then the smoke, golden and parity suites.
- release-please switched to a GitHub App token (REL-06, open question 13), so its pull requests run CI.
- Deploy to staging by digest after a release (§17.3 item 4), once p0-infra-opentofu exists.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None (`skip_specs: true`). It builds `platform/container-images` (NFR-IMG-01…05),
`security/application-security` (SEC-20 scaffold, SEC-21, SEC-22 signing, SEC-27, SEC-28),
`platform/release-and-versioning` (REL-06) and `quality/test-gates` (QA-LOCAL-01).

## Impact

- `.github/workflows/`, `deploy/docker/`, the `images` and `ci` make targets, the image budget table.
- **Needs the founder first:** the organisation-owned GitHub App (open question 13) for REL-06.
- Branch protection on `main` (CTL-CHG-01) is the founder's decision when the review pull requests open.
