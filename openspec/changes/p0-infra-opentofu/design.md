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

## Risks / Trade-offs

- [A zonal database is a single-zone outage risk] → accepted for P0 and early P1 by the cost decision;
  revisit with the staging month's measurements (NFR-COST-04).
- [Org policy blocks a needed global service] → list the exceptions explicitly in code with a reason.

## Migration Plan

Create `dev`, then `staging`, from CI. `production` is planned in P0 but applied only before go-live,
with the manual approval of §12.4.
