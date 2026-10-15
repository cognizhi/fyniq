# Tasks

## 1. CI and supply chain

- [x] 1.1 Extend `ci.yml` to run `make check` and `make test` on pull requests and `make ci` on `main`, with actions pinned by SHA and least-privilege tokens; test `// req: SEC-28` is a workflow lint that fails on an unpinned action or a write token (one CI job per `make ci` step, on pull requests, `main` and weekly; archtest checks both)
- [x] 1.2 Add SAST, dependency, container, secret, IaC and licence scanning with critical findings blocking the merge and waivers only in a reviewed file with an expiry; verify `// req: SEC-21` with a planted secret and a planted vulnerable dependency on a throwaway branch (draft #26, 15 Oct 2026: both failed the scan; container images are scanned with the image tasks)

## 2. Images

- [x] 2.1 Write multi-stage Dockerfiles in `deploy/docker/` onto `distroless/static:nonroot` with bases pinned by digest, non-root and a read-only root filesystem; test `// req: NFR-IMG-01`, `// req: NFR-IMG-02`, `// req: NFR-IMG-03` and `// req: SEC-27` inspects each image's config
- [x] 2.2 Enforce the §12.9 size budgets from `deploy/docker/budgets.json`; test `// req: NFR-IMG-04` fails on an image over budget
- [ ] 2.3 Produce an SBOM (syft), a scan (grype) and a cosign signature per image, and push by digest; verify `// req: NFR-IMG-05` and `// req: SEC-22` with `cosign verify` in CI (15 Oct 2026: the SBOM and the scan run on every pull request, `make image-scan`; signing and pushing by digest go to Artifact Registry once p0-infra-opentofu exists, the founder's choice over an interim GHCR) (15 Oct 2026: `make image-sign` runs on every pull request: each release image is pushed by digest to a throwaway local registry, signed with cosign and its SBOM attached as an SPDX attestation, and `cosign verify` and `verify-attestation` check both, while a wrong key and an unsigned image are refused; the key is made for the run, so a pull request needs no OIDC token and publishes nothing. Keyless signing with GitHub OIDC and pushing by digest to Artifact Registry in the release workflow wait for p0-infra-opentofu and staging, so the task stays open)
- [x] 2.4 Build the local flavour from the same commit and add the QA-LOCAL-01 job (no cloud credentials: `make up`, seed, smoke, golden and parity suites); verify `// req: QA-LOCAL-01` in CI (15 Oct 2026: the job builds both flavours and runs `make up` and the smoke checks with no cloud credentials; loading the sample company into the database waits for the first-run setup (p0-identity-oauth-first-run), and the parity suites arrive in P1) (15 Oct 2026: the job now signs in, sets up and loads the sample company into the database through the engine with `make seed-load`, which checks the trial balance against the seed's own totals; the parity suites are P1)

## 3. Release and staging

- [ ] 3.1 After the founder creates the GitHub App, switch release-please to its token so release pull requests run CI; verify `// req: REL-06` on the next release PR
- [ ] 3.2 Deploy each release to staging by digest (needs p0-infra-opentofu) and record the digests in the release record (§17.3 items 4 and 5); then `make check` and `make test`
