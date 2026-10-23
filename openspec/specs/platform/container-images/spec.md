# platform/container-images Specification

## Purpose
How the container images of Fyniq are built: one small, hardened, reproducible image per service, for `linux/amd64` and `linux/arm64`, from multi-stage Dockerfiles, on distroless bases within size budgets, non-root, with pinned and patched bases, an SBOM, a scan gate, a signature and provenance, deployment by digest, and health and version endpoints. Derived from PRD-001 §12.9 (ADR-T5, part 1).

## Requirements

### Requirement: Go service images
The `api`, `ai`, `mcp`, `worker`, `executor`, `jobs` and `migrate` images SHALL each be a static Go binary on `gcr.io/distroless/static-debian12:nonroot`, built by a multi-stage Dockerfile in `deploy/docker/`, within ≤ 30 MB compressed each.

Source: PRD-001 §12.9

#### Scenario: Static Go build
- **WHEN** a Go service image is built
- **THEN** the binary is built with `CGO_ENABLED=0`, `-trimpath`, `-ldflags "-s -w -X version=… -X commit=…"` and `-buildvcs=false`
- **AND** the version and git SHA are stamped with `-X`, because the `.git` directory is not in the build context

#### Scenario: Embedded assets
- **WHEN** a Go service image is built
- **THEN** the migrations, templates and time-zone data (`time/tzdata`) are embedded in the binary

#### Scenario: Distroless static runtime base
- **WHEN** a Go service image runs
- **THEN** its runtime base is `gcr.io/distroless/static-debian12:nonroot` (≈ 2 MB: CA certificates, `/etc/passwd`, no shell, no package manager)

#### Scenario: Size budget
- **WHEN** a Go service image is built
- **THEN** it is ≤ 30 MB compressed

### Requirement: Render image
The `render` image (Typst PDF service, rev 0.96; formerly a job) SHALL contain the Go binary plus the pinned Typst binary and self-hosted fonts on `gcr.io/distroless/static-debian12:nonroot` (rev 0.96; formerly cc), within ≤ 90 MB compressed.

Source: PRD-001 §12.9

#### Scenario: Render image contents and base
- **WHEN** the `render` image is built
- **THEN** it holds the Go binary, the pinned Typst binary and the self-hosted fonts
- **AND** its runtime base is `gcr.io/distroless/static-debian12:nonroot` (rev 0.96; formerly `gcr.io/distroless/cc-debian12:nonroot`)

#### Scenario: Render size budget
- **WHEN** the `render` image is built
- **THEN** it is ≤ 90 MB compressed

#### Scenario: The render service as built (rev 0.96)
- **WHEN** the `render` image is built
- **THEN** it holds Typst 0.15.1's static build checked against its release's SHA-256 and a binary with the fonts embedded, on `gcr.io/distroless/static-debian12:nonroot`, which supersedes the cc base: neither needs a C library, and the cc base's glibc and OpenSSL failed the image scan
- **AND** the binary carries no database adapter, driver or domain command

### Requirement: Web image for local and preview
The `web` image SHALL be used in local and preview only, built with `pnpm build` on `nginxinc/nginx-unprivileged` (Alpine) with a strict CSP, within ≤ 25 MB compressed.

Source: PRD-001 §12.9

#### Scenario: Web image in local and preview
- **WHEN** the web app runs in local or preview
- **THEN** it is served by the `web` image, built with `pnpm build` on `nginxinc/nginx-unprivileged` (Alpine) with a strict CSP
- **AND** the image is ≤ 25 MB compressed

#### Scenario: Production serves static files
- **WHEN** the web app is served in production
- **THEN** it is served as static files from Cloud Storage and CDN, not from the `web` image

### Requirement: OAuth image
The `oauth` image SHALL be the upstream Ory Hydra image pinned by digest, with its version and edition recorded in ADR-T3.

Source: PRD-001 §12.9

#### Scenario: Upstream Hydra pinned by digest
- **WHEN** the `oauth` service is deployed
- **THEN** it runs the upstream Ory Hydra image (upstream distroless, upstream size) pinned by digest

#### Scenario: Version changes need the identity suite
- **WHEN** the Hydra version or edition changes
- **THEN** the change is recorded in ADR-T3
- **AND** it is made only with the identity test suite green

### Requirement: NFR-IMG-01 Multi-stage builds with a minimal runtime stage
Images SHALL be built in multiple stages, and the runtime stage SHALL contain only the binary and its static assets: no compiler, shell, package manager or source.

Source: PRD-001 §12.9 · Priority M · Phase P0

#### Scenario: Runtime stage contents
- **WHEN** an image is built
- **THEN** its runtime stage contains only the binary and its static assets
- **AND** it contains no compiler, shell, package manager or source

### Requirement: NFR-IMG-02 Non-root, read-only and no capabilities
Images SHALL run as non-root (UID 65532) with a read-only root filesystem where the platform supports it, writable paths only under an explicit `tmpfs`, and no Linux capabilities.

Source: PRD-001 §12.9 · Priority M · Phase P0

#### Scenario: Read-only root where supported
- **WHEN** an image runs on Compose, ECS or Kubernetes
- **THEN** it runs as UID 65532 with a read-only root filesystem, writable paths only under an explicit `tmpfs`, and no Linux capabilities

#### Scenario: Cloud Run filesystem
- **GIVEN** Cloud Run gives every container a writable in-memory filesystem with no read-only option
- **WHEN** an image runs on Cloud Run
- **THEN** the image files are root-owned and not writable by UID 65532
- **AND** `/tmp` is an in-memory volume with a size limit

#### Scenario: Start-up self-test on Cloud Run
- **WHEN** a service starts on Cloud Run
- **THEN** a self-test tries to write to a root-owned image path
- **AND** if that write does not fail, the service does not become ready

#### Scenario: No added capabilities on Cloud Run
- **WHEN** a service runs on Cloud Run
- **THEN** it has no added capabilities and privilege escalation is disabled

#### Scenario: G4-083 Writes outside /tmp and beyond its limit
- **WHEN** a deployed `api` revision tries to write to a root-owned image path and to `/tmp` beyond its size limit
- **THEN** the write outside `/tmp` fails (the start-up self-test passes)
- **AND** the oversized `/tmp` write fails with a clear error, not an instance crash loop

### Requirement: NFR-IMG-03 Pinned and patched base images
Base images SHALL be pinned by digest, updated by automated pull requests, and rebuilt at least weekly for base-image patches.

Source: PRD-001 §12.9 · Priority M · Phase P0

#### Scenario: Base pinned by digest
- **WHEN** a Dockerfile names a base image
- **THEN** the base image is pinned by digest

#### Scenario: Automated update pull requests
- **WHEN** a base image has a newer digest
- **THEN** an automated pull request updates the pin

#### Scenario: Weekly rebuild
- **WHEN** a week passes
- **THEN** every image has been rebuilt at least once in it for base-image patches

### Requirement: NFR-IMG-04 Size budgets enforced in CI
The size budgets SHALL be enforced in CI, and the budget table SHALL be the source of truth, changed only by a reviewed pull request.

Source: PRD-001 §12.9 · Priority M · Phase P0

#### Scenario: Over budget fails
- **WHEN** `make images` builds an image over its budget
- **THEN** `make images` fails

#### Scenario: Budgets change by reviewed pull request
- **WHEN** a budget is changed
- **THEN** the change is made in the budget table by a reviewed pull request

### Requirement: NFR-IMG-05 SBOM, scan, signature and provenance
Every image SHALL have an SBOM (syft), a vulnerability scan with no unwaived critical or high findings (grype or Trivy), a cosign signature and SLSA provenance (SEC-22, SEC-26), and SHALL be deployed by digest.

Source: PRD-001 §12.9 · Priority M · Phase P0 (budget, hardening, cosign signature, deployment by digest) / P1 (SBOM, scan gate, SLSA provenance)

#### Scenario: SBOM and provenance
- **WHEN** an image is built
- **THEN** an SBOM is generated with syft
- **AND** SLSA provenance is produced

#### Scenario: Scan gate
- **WHEN** an image is scanned with grype or Trivy
- **THEN** it passes only with no unwaived critical or high findings

#### Scenario: Signed with cosign
- **WHEN** an image is published
- **THEN** it carries a cosign signature

#### Scenario: Deployment by digest
- **WHEN** an image is deployed
- **THEN** the deployment references it by digest

### Requirement: NFR-IMG-06 Health and version endpoints
Images SHALL expose `/healthz` (liveness), `/readyz` (readiness: database and dependencies) and `/version` (SemVer, git SHA, build time), and Cloud Run startup and liveness probes SHALL use them.

Source: PRD-001 §12.9 · Priority M · Phase P0

#### Scenario: Liveness and readiness
- **WHEN** `/healthz` and `/readyz` are called
- **THEN** `/healthz` reports liveness
- **AND** `/readyz` reports readiness of the database and dependencies

#### Scenario: Version
- **WHEN** `/version` is called
- **THEN** it returns the SemVer, the git SHA and the build time

#### Scenario: Cloud Run probes
- **WHEN** a service is deployed on Cloud Run
- **THEN** its startup and liveness probes use these endpoints

### Requirement: NFR-IMG-07 Reproducible builds
Builds SHALL be reproducible: the same commit SHALL produce the same image digest.

Source: PRD-001 §12.9 · Priority S · Phase P1

#### Scenario: Same commit, same digest
- **WHEN** the same commit is built twice
- **THEN** both builds produce the same image digest

#### Scenario: No timestamps or random data
- **WHEN** an image is built
- **THEN** it uses `SOURCE_DATE_EPOCH`
- **AND** its layers contain no timestamps or random data

### Requirement: NFR-IMG-08 Images for linux/amd64 and linux/arm64
Every image SHALL build for `linux/amd64` and `linux/arm64` from the same Dockerfile, natively for the build machine unless a platform is named, and nothing in the build or the local stack SHALL need emulation.

Source: PRD-001 §12.9 · Priority M · Phase P0

#### Scenario: Native by default
- **WHEN** the images are built without a named platform
- **THEN** each image and its binary are for the build machine's platform
- **AND** on an Apple-silicon laptop they are `linux/arm64`

#### Scenario: Either platform from one Dockerfile
- **WHEN** `linux/amd64` or `linux/arm64` is named
- **THEN** the build stages run on the build machine and cross-compile for the named platform
- **AND** `make images` refuses an image whose platform, or whose binary's architecture, is not the one named

#### Scenario: Multi-platform pins
- **WHEN** a base image or a third-party image of the local stack is pinned by digest
- **THEN** the digest is a multi-platform index that includes `linux/amd64` and `linux/arm64`

#### Scenario: Both platforms in CI
- **WHEN** CI runs
- **THEN** it builds the images and runs the local stack on an amd64 runner and on an arm64 runner

#### Scenario: The cloud's platform
- **WHEN** an image is deployed to Cloud Run
- **THEN** the deployed reference includes `linux/amd64`, the only platform Cloud Run runs
