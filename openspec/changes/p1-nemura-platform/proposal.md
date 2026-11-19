# Proposal

## Why

Nemura, the optional people platform (PRD-002), has no code yet. Every other Nemura change
(`p1-nemura-records-packs`, `-leave-time`, `-self-service-letters`, `-payroll`, `-fyniq-link`,
`-assistant-external`) needs the same ground to stand on: its own image, its own database with the
database-as-backstop rules, an audit chain, a sign-in and token issuer that work with no Fyniq at
all, Nemura's roles and the permission matrix, an API contract with Fyniq's conventions, its own
web app and design system, and the privacy and records rules that every later record type plugs
into. This change builds that ground once, so each later change adds records and screens, never
plumbing.

Timing (PRD-002 question 12, decided 16 Oct 2026): every Nemura P1 row is built after Fyniq's
go-live and before the first paid person, and none is in Fyniq's launch subset. The owner has asked
for the skeleton to start now. It stays dormant: its compose profile is off by default, nothing is
deployed to the cloud, and nothing in it is a launch gate (design D1).

## What Changes

- **A new image and service, `nemura`** (`cmd/nemura`, ADR-N1, NM-ARC-02): one static Go binary whose
  role (api, worker, executor, assistant, migrate) is chosen at start, so the one image runs as
  separate services each with its own identity and database role (NM-ARC-06). `/healthz`, `/readyz`
  and `/version` as every image has them (NFR-IMG-06). Its budget joins PRD-001 §12.9.
- **Its own database and migrations** (NM-ARC-02): a PostgreSQL database `nemura` with `nemura_*`
  roles, `organisation_id` on every row, forced row-level security and its own migration stream in
  `db/nemura/migrations/`, run by the same binary under its own migrator role.
- **Audit, outbox and approvals** (NM-ARC-05, NM-SEC-01, NM-ROLE-04, NM-ROLE-05): the hash-chained
  audit trail written in the same transaction as each change (AUD-01), the outbox for background
  work and the hand-off queue (ADR-T7, NM-NFR-03), and a dual-control approval primitive that
  everything later reuses (a role grant, a bank-detail change, a pay run, the Fyniq link).
- **Standalone identity** (NM-ARC-01, NM-ARC-06, ADR-N3): Nemura's own Ory Hydra instance as its
  token issuer (question 11), a local sign-in page and an Identity Platform adapter (one tenant per
  organisation, question 2), sessions, passkey step-up under the shared relying-party ID (question
  10), and its own API clients. Sign-in with no Fyniq running is tested.
- **Roles, the permission matrix and need to know** (NM-ROLE-01…05, NM-SEC-02): Appendix B as data,
  checked in the API and by row-level scope predicates, field-level rights, masking and an audited
  read of every restricted field, segregation and no self-escalation.
- **The API contract** (NM-ARC-03): `contracts/openapi/nemura/` with Fyniq's conventions, a stable
  `NM_*` error-code catalogue, its generated client, and the web app that calls only that client.
- **Nemura's web app and experience** (NM-UX-01…06, NM-NFR-02, NM-NFR-04): `apps/nemura-web` on
  Nemura's design system, workspaces and landing by role, one simple mode for employees, help on
  every screen, the "Needs additional verification" banner from the review register, localisation
  (`en-SG`), and print-ready documents. Built into the image.
- **Privacy and records** (NM-SEC-03, NM-SEC-04, NM-SEC-05, NM-SEC-07): a registry in which each
  record type declares its purpose, retention, restricted right and how it is exported, a reviewed
  deletion job with proof, the minimum retention periods, and tracked access requests with a due
  date (screen `nm-access-request`).
- **Non-functional rules** (NM-NFR-01…05): the Singapore region and scale to zero, performance
  budgets for My home and My pay, queued hand-off when Fyniq is down, the pay-day alert path as a
  mechanism, and localisation.
- **CI, images and local mode**: the image joins `make images`, the scans, signing and the
  reproducibility check; the `people` compose profile runs it beside the dependencies.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None (`skip_specs: true`). This change builds requirements already in the main specs:

- `people/platform`: the architecture decisions (unnumbered), NM-ARC-01…06 and NM-NFR-01…05;
- `people/security`: NM-SEC-01…05 and NM-SEC-07 (NM-SEC-06, grievances, is P2 and belongs to
  `p2-nemura-people`);
- `people/experience`: the permission matrix (unnumbered), NM-ROLE-01…05 and NM-UX-01…06.

Some requirements can only be proven in full once a later change supplies the object they govern.
This change builds the rule or mechanism and proves it on what exists, and says what is left, and
where it lands (design D13 and the coverage table in `tasks.md`). It never tags a requirement its
tests do not fully prove.

## Screens (docs/design/screens/inventory.json)

Already designed; this change builds them:

- `nm-sign-in`: in full.
- `nm-access-request`: in full, on the registry's record categories (NM-SEC-07).
- `nm-my-pay`: the page, its need-to-know rules, the print and PDF view and the empty and "not ready"
  states. The payslips and forms arrive with `p1-nemura-payroll`.

## Impact

- **Code:** `cmd/nemura`; `internal/nemura/…` (domain and HTTP); `internal/adapters/postgres/nemura`
  (storage, the only SQL for Nemura); `db/nemura/`; `contracts/openapi/nemura/`; `apps/nemura-web`;
  small changes to `internal/adapters/postgres` (the migration runner takes a migration set) and to
  `internal/archtest` (the rules in design D3).
- **Build and CI:** `GO_IMAGES` in the Makefile, `deploy/docker/budgets.json`, the Dockerfile's web
  stage for the `nemura` target, `.dockerignore`, `scripts/images.py`, `deploy/local/compose.yaml`,
  `scripts/dev.sh`. PRD-001 §12.9 gains the `nemura` row (a PRD revision, in the slice that adds it).
- **Docs:** `docs/user-guide/nemura/` and its knowledge-base pages (NM-UX-04), a threat model, the
  controls and data inventory for Nemura's data (NM-SEC-01, NM-SEC-03), and the runbooks.
- **Cloud:** none now. The OpenTofu services, the database, the identity tenants and the hostnames
  are written and validated offline in the last slice and applied only at the cloud gate
  (`[live-verify]`).
- **Needs the owner:** the "Decisions needed" list in `design.md`; none blocks the skeleton.
