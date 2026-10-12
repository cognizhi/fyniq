---
name: fh-local-dev
description: Run, test and debug Cognizhi Finance Hub Cloud entirely on a laptop or in CI with no cloud credentials ("local mode", ADR-T4), and build the lean, hardened container images (§12.9). Use this whenever you start, stop, seed or reset the local stack, touch deploy/local/ or deploy/docker/, add a cloud dependency (it needs a local substitute), change a Dockerfile or an image budget, set up a new machine, connect Claude Code or Claude Desktop to the local /mcp, override the business date for a test, or when someone asks "how do I run it", "make dev fails", "the image is too big", "it works locally but not in staging", "add a service to compose" or "which emulator do we use for X".
---

# Local mode and lean images

The platform must **run and pass every merge gate on a laptop or in CI with no cloud account,
no cloud credentials and no production data** (principle 12; ADR-T4 in PRD §12.8). Every cloud
dependency sits behind a Go interface with two adapters: production and local. Configuration
(`FH_ENV=local|ci|dev|staging|production`) picks the adapter. Local adapters are compiled only
into builds tagged `local` (DEV-07).

## What exists today vs what arrives in P0

| Today (rev 0.2 bootstrap) | Arrives in P0 |
|---|---|
| `deploy/local/compose.yaml` with three dependencies, bound to `127.0.0.1` only: `postgres` (PostgreSQL 18 Alpine; database and user `fh`, password `fh-local-only`; host port **55432** by default, `FH_PG_PORT` to change), `mailpit` (UI on 8025, SMTP on 1025; `FH_MAIL_UI_PORT`, `FH_SMTP_PORT`) and `gcs` (fake-gcs-server on 4443, `-scheme http`, in-memory; `FH_GCS_PORT`) | The full stack and profiles (`templates/compose.full.example.yaml`) |
| `make deps-up`, `make deps-down`, `make deps-logs` | `make setup`, `dev`, `up`, `down`, `seed`, `reset`, `test`, `test-e2e`, `evals`, `images`, `ci` (the command contract below) |
| `make help`, `check`, `check-tokens`, `check-config`, `hooks`, `prototype` | Dockerfiles in `deploy/docker/` from `templates/` |

`fh-local-only` is a **local-only** password for a throw-away container. Never reuse it, and
never put a real secret in a committed file (SEC-12).

## The substitute table (§12.8)

| Production | Local substitute | Fidelity notes |
|---|---|---|
| Cloud SQL PostgreSQL 18 | `postgres:18` container, same extensions, password auth instead of IAM | Testcontainers uses the same image |
| Identity Platform (sign-in, TOTP) | Firebase Authentication emulator [verify TOTP MFA support at P0; fallback: a dev OIDC issuer in the `local` build] | Passkeys work on `localhost` (secure context) |
| Ory Hydra (ADR-T3) | The same Hydra image | Identical |
| Cloud KMS | Tink keysets in `.data/keys/` (git-ignored), created by `make setup` | Same algorithms; key custody differs by design |
| Cloud Storage | `fake-gcs-server` | Production client library unchanged. **Bucket Lock cannot be emulated**: CTL-AUD-03 checks it in staging and production |
| Secret Manager | `.env` from `make setup` (git-ignored); `.env.example` committed | Same `Secrets` interface |
| Claude on Agent Platform | **Replay** (default): recorded cassettes keyed by prompt, tools and model version. **Live** (`FH_AI_LIVE=1`): the Claude API with a developer key, or a developer's own GCP project, on **synthetic data only** | CI runs the eval gates live before any model or prompt change |
| Model Armor | Stub that flags the red-team corpus | Real service tested in staging |
| Cloud Tasks, Cloud Scheduler → Cloud Run jobs (rev 0.7, ADR-T7) | **River** as the local adapter of the queue port: the same outbox, an in-process consumer, and periodic jobs that call the same tick and handlers | One schedule definition, the same handlers |
| Pub/Sub (P2) | Pub/Sub emulator | Same client library |
| Load balancer, Cloud Armor, CDN | Caddy, one origin (`templates/Caddyfile.example`) | Same cookies and CSP; WAF tested in staging |
| Email provider | Mailpit (`http://localhost:8025`) | Templates and links end to end |
| Slack, Zoho Cliq (rev 0.6, P2) | A message sink at `/dev/messages` in the local `api`, with the same payloads | Routing and content level end to end |
| Sign-in providers (rev 0.6) | The Firebase Authentication emulator shows a mock sign-in page for any provider ID (Google, Zoho, `oidc.*`); real provider sign-in is tested in staging | Provider linking, MFA after sign-in (the emulator's TOTP support is unconfirmed) |
| Malware scanning | EICAR-rejecting stub; `scan` profile runs real ClamAV | — |
| Peppol AP, bank APIs, Stripe | Prism mocks from `contracts/partners/`; `stripe-mock` | Contract-level; partner sandboxes in staging |
| Observability | `obs` profile: OTel Collector and Jaeger | Same instrumentation |

**Adding a cloud dependency?** You must add (1) the interface, (2) the production adapter,
(3) the local adapter or emulator, (4) a row in this table and in PRD §12.8, and (5) a staging
check for anything the substitute cannot prove (risk R17).

## Compose profiles (P0)

| Profile | Services |
|---|---|
| *(default)* `core` | postgres, auth emulator, hydra, gcs, mailpit, api, ai, mcp, worker, executor, web (Vite in `make dev`, the image in `make up`), caddy |
| `obs` | otel-collector, jaeger |
| `scan` | clamav |
| `partners` | prism (Peppol AP, bank), stripe-mock |
| `pubsub` (P2) | Pub/Sub emulator |

Our own services always run with `read_only: true`, `user: "65532:65532"`, `cap_drop: [ALL]`
and a `tmpfs` for `/tmp`, so local runs catch anything that would break under the production
hardening (NFR-IMG-02).

## The command contract (arrives in P0)

The Makefile is the single entry point, and CI calls the same targets (`make ci`).

| Command | Must do | Budget |
|---|---|---|
| `make setup` | Install the pinned toolchain from `mise.toml`; write `.env` from `.env.example`; create Tink keysets in `.data/keys/`; register Hydra clients for the first-party apps; install git hooks (`make hooks`) | ≤ 10 min first run |
| `make dev` | Start `core` with hot reload (`air` for Go, Vite HMR); run migrations; seed the **sample company**; print the URLs and sample sign-ins | ≤ 60 s on later runs (DEV-01) |
| `make up` / `make down` | Run the stack from the **production images** (`make images` output), not dev builds / stop it | — |
| `make seed` / `make reset` | Reload the sample company / wipe `.data/` and the database | — |
| `make test` | Every merge gate offline: unit, property, DB invariant attacks (Testcontainers), oracle differential, contract, authz matrix, front-end unit and accessibility | ≤ 10 min (NFR-DEVX-02) |
| `make test-e2e` | Playwright journeys against `make up` (QA-E2E-01) | — |
| `make evals` | AI golden, proposal and red-team suites; replay by default, `FH_AI_LIVE=1` for live | — |
| `make images` | Build every image, enforce `image-budgets.yaml`, scan, produce SBOMs | — |
| `make ci` | Exactly what CI runs, in the same order | — |

Today, `make deps-up` starts only the three dependencies above and prints their URLs
(`postgres://fh:fh-local-only@localhost:55432/fh`, `http://localhost:8025`,
`http://localhost:4443/storage/v1/b`). Use them for spikes, for example trying a migration
against PostgreSQL 18 or sending a template to Mailpit. `make deps-down` keeps the database
volume; remove it with `docker volume rm finance-hub_pgdata` for a clean start.

## Requirements to keep green (§12.8)

| ID | Rule | How you notice a break |
|---|---|---|
| DEV-01 | `make setup && make dev` brings up the full stack on a clean machine | QA-LOCAL-01 in CI |
| DEV-02 | No cloud credentials for `dev`, `test`, `test-e2e`, `evals` (replay) | CI runs with no GCP identity at all |
| DEV-03 | Tool versions pinned in `mise.toml`; the dev container matches | `mise doctor`; CI uses `mise install` |
| DEV-04 | One sample-company generator for seed data, sandbox, fixtures and the AI golden set | Fixture drift fails the golden tests |
| DEV-05 | Injectable clock everywhere; non-production "business date" override | A `time.Now()` lint rule outside `internal/clock` |
| DEV-06 | Local `/mcp` reachable by Claude Code or Claude Desktop | Manual check in the P1 release checklist |
| DEV-07 | Production binaries refuse local adapters; no local symbols in production images | QA-IMG-01 symbol check |
| DEV-08 | Synthetic data only, never production data, on laptops and in non-production projects | CTL-CHG-05 DLP scan |

## Business date (clock override)

Period close, CPF age bands, statutory deadlines and FX revaluation depend on the date.

- Every component reads time through `internal/clock` (P0). Tests use a frozen clock.
- In `local`, `ci`, `dev` and `staging` builds, an Owner can set a **business date** for an
  organisation (Settings → Developer, or `FH_BUSINESS_DATE=2026-12-31` for the whole stack).
  The UI shows a persistent banner whenever it is set.
- The production build does not contain the override. A start-up check refuses the setting.

## Connecting Claude Code to the local MCP server (DEV-06, P1)

1. `make dev`, then sign in once in the browser as a sample user, and enable MCP for the sample
   organisation and for your user (AI-35; locally the dual control is pre-approved by the seed).
2. Add the server to Claude Code with `claude mcp add --transport http fh-local http://localhost:8080/mcp`
   (Caddy's origin; the port is printed by `make dev`).
3. Claude Code discovers the RFC 9728 metadata and opens the Hydra consent screen. Grant
   `fh.read.reports` first, then add `fh.propose.*` scopes when testing proposals.
4. Proposals appear in the local Inbox. Approving them needs the web app and a passkey (AI-42).

Never point a local MCP client at staging or production with test credentials.

## Lean images (§12.9, ADR-T5)

| Image | Build | Runtime base | Budget (compressed) |
|---|---|---|---|
| `api`, `ai`, `mcp`, `worker`, `executor`, `jobs`, `migrate` | Static Go binary (`CGO_ENABLED=0`, `-trimpath`, `-ldflags "-s -w -X …"`), embedded migrations, templates and `time/tzdata` | `gcr.io/distroless/static-debian12:nonroot` | ≤ 30 MB each |
| `render` (Typst PDFs) | Go binary + pinned Typst + self-hosted fonts | `gcr.io/distroless/cc-debian12:nonroot` | ≤ 90 MB |
| `web` (local and preview only) | `pnpm build` | `nginxinc/nginx-unprivileged` (Alpine), strict CSP | ≤ 25 MB |
| `oauth` | Upstream Ory Hydra, pinned by digest | Upstream | Upstream |

| ID | Rule |
|---|---|
| NFR-IMG-01 | Multi-stage; runtime has only the binary and static assets: no compiler, shell, package manager or source |
| NFR-IMG-02 | UID 65532, read-only root filesystem, explicit `tmpfs`, no capabilities |
| NFR-IMG-03 | Bases pinned by digest; automated update PRs; weekly rebuilds |
| NFR-IMG-04 | Budgets enforced by `make images` from `image-budgets.yaml`; changed only by a reviewed PR |
| NFR-IMG-05 | SBOM (syft), scan with no unwaived critical/high (grype or Trivy), cosign signature, SLSA provenance; deploy by digest |
| NFR-IMG-06 | `/healthz`, `/readyz`, `/version` (SemVer, git SHA, build time) used by Cloud Run probes |
| NFR-IMG-07 | Reproducible: same commit → same digest (`SOURCE_DATE_EPOCH`, no timestamps in layers) |

Each service is a **separate binary and image** from its own `cmd/<service>`. The AI and MCP
images must not contain domain write code (§12.3). Check with `go list -deps ./cmd/ai` that no
write-side package is imported.

Templates for P0 are in `templates/`: `Dockerfile.go-service`, `Dockerfile.web`,
`compose.full.example.yaml`, `Caddyfile.example` and `image-budgets.yaml`. They are references,
not wired into any build yet.

### When an image is over budget

1. `docker history --no-trunc <image>`: find the layer that grew.
2. Usual causes: CGO turned on (pulls libc, needs `cc` base), debug symbols (`-s -w` missing),
   a new embedded asset (fonts, sample data in a service binary), a heavy dependency (check
   `go version -m <binary>`).
3. Fix the cause. Raise a budget only with a PR that explains why, reviewed like code.

## Troubleshooting

| Symptom | Look at |
|---|---|
| Port 55432, 8025, 1025 or 4443 already in use | A previous stack (`make deps-down`, `docker ps`), or set `FH_PG_PORT`, `FH_MAIL_UI_PORT`, `FH_SMTP_PORT`, `FH_GCS_PORT` |
| `make deps-up` hangs on postgres | `make deps-logs`; a volume from an older major version needs `docker volume rm` (data is disposable) |
| Works locally, fails in staging | A substitute gap (Bucket Lock, Model Armor, WAF, IAM DB auth): see the fidelity column; add a staging check (R17) |
| Service exits with "local adapter in production mode" | `FH_ENV` is `staging`/`production` with a local adapter configured: correct, by design (DEV-07) |
| Passkey prompt fails | Use `http://localhost`, not a LAN IP; WebAuthn needs a secure context |
| AI tests fail with "cassette not found" | The prompt, tools or model changed: re-record with `FH_AI_LIVE=1 make evals` on synthetic data, review the diff, commit the cassettes |
| Container cannot write a file | It runs read-only: write under the declared `tmpfs` or object storage, never the root filesystem |

## Definition of done (changes to local mode or images)

1. `make dev` and `make test` still pass with no cloud credentials (arrives in P0; today:
   `make deps-up` and `make check`).
2. A new cloud dependency has its interface, both adapters, a substitute-table row (here and in
   PRD §12.8) and a staging check for any fidelity gap.
3. `make images` passes the budgets; the production image has no local adapter symbols.
4. Compose services keep `read_only`, `user` and `cap_drop: [ALL]`.
5. Commit with the `deploy` or `infra` scope (see **fh-release**).
