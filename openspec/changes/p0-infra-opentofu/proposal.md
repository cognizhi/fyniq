# Proposal

## Why

Staging must exist for P0's exit items 3, 4, 6 and 7, and every cloud resource must be code with drift
detected (NFR-PORT-05, CTL-CHG-02). P0 writes the Google Cloud modules behind variables that AWS and Azure
modules can later share (ADR-T8).

## What Changes

- OpenTofu modules in `infra/gcp/` with one root per environment (`dev`, `staging`, `production`), each in
  its own project (CTL-CHG-05), and remote state in a locked bucket.
- An organisation policy restricting resource locations to Singapore (`asia-southeast1`) (CTL-DP-07).
- Cloud Run services and jobs per worker class, each with its own service account (SEC-03); Cloud Tasks
  queues and the Cloud Scheduler tick (ADR-T7).
- Cloud SQL for PostgreSQL 18 (ADR-D1), zonal at first, with backups and point-in-time recovery, and CMEK
  (CTL-DP-01).
- Secret Manager for every secret (SEC-12), KMS keys (CMEK, audit anchor signing), Artifact Registry.
- Workload Identity Federation for CI, with no service-account keys (SEC-04, CTL-IAM-04); no standing
  privileged human access (CTL-IAM-03).
- The retention-locked bucket for audit anchors (CTL-AUD-02).
- Cloud Billing budgets as code: US$400 a month for production and US$120 for development and staging,
  alerting at 50%, 80% and 100% and never capping (open question 9, rev 0.20); the amounts are variables.
- A nightly drift check that fails on any difference from the code (CTL-CHG-02).

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None (`skip_specs: true`). It builds NFR-PORT-05 (`platform/portability-and-cost`), SEC-03, SEC-04
(`security/identity-and-access`), SEC-12 (`security/data-protection`) and the controls CTL-CHG-02,
CTL-CHG-05, CTL-DP-01, CTL-DP-07, CTL-IAM-03 and CTL-IAM-04 (`compliance/technology-controls`).

## Impact

- `infra/gcp/`, a `tofu-plan` job in CI, the release workflow's deploy step.
- **Needs the founder first:** a Google Cloud organisation and billing account, and the three projects
  (or the right to create them).
- **Second human reviewer recommended:** the security reviewer for IAM and Workload Identity Federation.
