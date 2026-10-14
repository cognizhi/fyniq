# Tasks

## 1. CI and supply chain

- [ ] 1.1 Extend `ci.yml` to run `make check` and `make test` on pull requests and `make ci` on `main`, with actions pinned by SHA and least-privilege tokens; test `// req: SEC-28` is a workflow lint that fails on an unpinned action or a write token
- [ ] 1.2 Add SAST, dependency, container, secret, IaC and licence scanning with critical findings blocking the merge and waivers only in a reviewed file with an expiry; verify `// req: SEC-21` with a planted secret and a planted vulnerable dependency on a throwaway branch

## 2. Images

- [ ] 2.1 Write multi-stage Dockerfiles in `deploy/docker/` onto `distroless/static:nonroot` with bases pinned by digest, non-root and a read-only root filesystem; test `// req: NFR-IMG-01`, `// req: NFR-IMG-02`, `// req: NFR-IMG-03` and `// req: SEC-27` inspects each image's config
- [ ] 2.2 Enforce the §12.9 size budgets from `deploy/docker/budgets.json`; test `// req: NFR-IMG-04` fails on an image over budget
- [ ] 2.3 Produce an SBOM (syft), a scan (grype) and a cosign signature per image, and push by digest; verify `// req: NFR-IMG-05` and `// req: SEC-22` with `cosign verify` in CI
- [ ] 2.4 Build the local flavour from the same commit and add the QA-LOCAL-01 job (no cloud credentials: `make up`, seed, smoke, golden and parity suites); verify `// req: QA-LOCAL-01` in CI

## 3. Release and staging

- [ ] 3.1 After the founder creates the GitHub App, switch release-please to its token so release pull requests run CI; verify `// req: REL-06` on the next release PR
- [ ] 3.2 Deploy each release to staging by digest (needs p0-infra-opentofu) and record the digests in the release record (§17.3 items 4 and 5); then `make check` and `make test`
