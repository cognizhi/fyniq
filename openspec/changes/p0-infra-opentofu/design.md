# Design

## Context

§12.3 is the reference architecture; §12.6 the cost model. The rev 0.19 sweep confirmed that a zonal
Cloud SQL instance keeps its logs in Cloud Storage and restores manually; HA comes later, with the
budget revisited (NFR-COST-04).

## Goals / Non-Goals

**Goals:**
- `tofu apply` from CI creates staging from nothing, and drift is visible the next morning.

**Non-Goals:**
- AWS and Azure modules (built when a deployment needs them), and production go-live hardening beyond P0.

## Decisions

- **Variables shared across clouds.** `infra/modules/<capability>` exposes cloud-neutral variables
  (service names, sizes, schedules), and `infra/gcp/` implements them (NFR-PORT-05).
- **One service account per Cloud Run service or job**, granted only what it needs. The `api` account
  alone can create executor tasks (§12.11).
- **No keys anywhere.** CI uses Workload Identity Federation scoped to this repository and to `main` or
  tags; humans use IAM with MFA and just-in-time elevation (SEC-04).
- **Budgets alert, never cap.** A cap would stop the service mid-month; the alerts go to both founders.
- **Audit events / ports.** None in the product; Cloud Audit Logs record the infrastructure changes.

Decided while building (16 Oct 2026; `infra/README.md`):

- **The spec is the cloud-neutral layer.** `infra/modules/platform-spec` holds the services, jobs,
  worker classes, sizes, tick, budget and anchor retention as data. `infra/gcp/` implements it.
- **`dev` is a sandbox with a budget, without the platform.** The release build refuses
  `FYNIQ_ENV=dev` (RUNTIME-FLAVOUR-01), and local adapters stay off the cloud (DEV-07).
- **Deterministic Cloud Run URLs.** Every URL and token audience is
  `https://SERVICE-PROJECT_NUMBER.REGION.run.app`. Each service's `FYNIQ_GCP` names every class,
  including the service itself, so a URL that only exists after creation would be a cycle.
  `[verify in staging]` that Cloud Run accepts the deterministic URL as the audience.
- **Images are deployed, not planned.** Services start from a placeholder. The release workflow
  deploys by digest, and OpenTofu ignores the image (so does the drift check).
- **Hydra runs as two services.** `oauth` serves the public port. `oauth-admin` serves the admin
  port with internal ingress and no IAM check, as in the local stack, so the network is its boundary:
  it is reachable only through Direct VPC egress (the api and the worker), whose Hydra client sends no
  ID token `[verify in staging]`. All egress goes through the VPC, and Cloud NAT carries calls to providers. Hydra's
  schema migrates in its own job, as in the local stack.
- **The jobs class goes to the worker**, as in local mode. Its handler that starts Cloud Run jobs is
  P1's long work.
- **Database access.** Each service's `FYNIQ_DATABASE_URL` is a Secret Manager secret holding its own
  role's password, used over the private IP with TLS (`ENCRYPTED_ONLY`). The value is added out of
  band, never in state. IAM database authentication is the alternative to weigh in staging
  `[verify in staging]`.
- **CMEK service agents** come from `google_project_service_identity` (the google-beta provider, at
  the same version). Each agent gets its key before the resource that uses it.
- **The anchors' lock is a variable.** It is true only in production, in `fyniq-audit`. The
  retention has no default: the accountant confirms it before the first apply `[verify]`.
- **Plans only from `main` and tags.** Workload Identity Federation trusts nothing else. The
  workflow's log names the resources that change, never their values, because the logs are public.
- **The billing account's currency.** Budgets assume USD, as the PRD states them `[verify]`.

## Risks / Trade-offs

- [A zonal database is a single-zone outage risk] → accepted for P0 and early P1 by the cost decision;
  revisit with the staging month's measurements (NFR-COST-04).
- [Org policy blocks a needed global service] → list the exceptions explicitly in code with a reason.

## Migration Plan

Create `dev`, then `staging`, from CI. `production` is planned in P0 but applied only before go-live,
with the manual approval of §12.4.
