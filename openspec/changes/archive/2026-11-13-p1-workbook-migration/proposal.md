# Proposal

## Why

Fyniq replaces the `zh-sheet-accounting` workbook, so it goes live holding the books the workbook holds today. Nothing
yet moves them across or proves the move: there is no workbook import, no reconciliation of the imported books with
the workbook, no generic import of an entity from a file, no export of the whole organisation, and none of §17.1's
migration steps (M1 to M6) has a place to happen or to leave its evidence. FR-MIG-01 to FR-MIG-04 are M requirements of
the launch subset (PRD-001 §8.19, §17.1), and `p1-launch-readiness` waits for this change (the build plan's wave 7).

## What Changes

- **The owner's decisions of 12 Nov 2026, taken interactively (PRD-001 rev 0.254, FR-MIG-01):**
  - **Payroll history** becomes one posted summary journal a month, keeping the amounts actually paid and reported to
    the CPF Board, by salary account and department; the per-employee rows stay in the archived workbook until the
    payroll link (after go-live).
    - *Not chosen:* payroll through the opening balances alone, and waiting for the payroll link.
  - **The target** is an organisation with no posted entries; a failed or rejected import starts again in a new
    organisation, never mixed into live books.
    - *Not chosen:* merging into an existing organisation.
  - **Projects** become values of the project dimension, departments values of the department dimension, with their
    tags carried over; timesheets and per-person employee rows stay in the archive.
    - *Not chosen:* dropping the project tags.
- **The workbook import (FR-MIG-01):** the published `.xlsx` is read sheet by sheet into a typed model; a validation
  report lists, per sheet, the rows to import, the warnings and the errors before anything is committed; the commit runs
  as an operation through the same domain commands the API uses, so every record is validated, posted by the engine
  and audited. History for the current financial year, or a clean start from the Carry Forward sheet's balances and
  open items (§17.1 M3).
- **Opening open items (FR-GL-06's unbuilt part):** invoices and bills open at the books start become documents that
  can be paid and aged, without posting their revenue or cost a second time [accountant to confirm the clearing
  account].
- **Reconciliation (FR-MIG-02, QA-MIG-01):** the imported trial balance against the workbook's Trial Balance sheet, by
  account and month to the cent; the migration difference register with its three classes; the workbook's Checks
  status against Fyniq's (C.1); the accountant's sign-off.
- **Generic import (FR-MIG-03):** a CSV or XLSX template per entity, laid out as the workbook's sheet for it, with a dry
  run.
- **Full tenant export (FR-MIG-04):** asked for by the Owner and approved by a second approver (Appendix D's "Tenant
  export"); every table of the organisation as CSV, documents as JSON, files as they are, with a manifest of SHA-256
  hashes; made in the background and downloaded once ready.
- **The migration's steps (§17.1 M1 to M6):** a migration record in Settings › Import that holds each step's evidence
  and sign-off, from the frozen workbook and its Checks export to the archived workbook with its hash; the whole path
  rehearsed in a test on the synthetic sample workbook.
- **Permissions:** Appendix D gains the row "Workbook import (migration)", provisional and taken from the screen's
  roles: the Owner commits and archives, the Controller and the Accountant prepare and sign off, the Auditor views. The
  row lands with the code that grants it, in the first slice, since `TestTheGrantsAreAppendixD` holds the two equal.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None at the spec level: FR-MIG-01's decisions are in `data/migration` already (rev 0.254, stamped). The change builds
approved requirements, so it sets `skip_specs: true`.

## Impact

- **Requirements:** FR-MIG-01, FR-MIG-02, FR-MIG-03, FR-MIG-04, the unnumbered M1 to M6 of `data/migration`, FR-GL-06
  (opening open items), QA-MIG-01, FR-DOC-05 (evidence), AUD-05 (the export audited as one).
- **Screens:** `workbook-import` (Settings › Import, a wizard) and `data-export` (Settings › Data), both designed in
  `docs/design/screens/`; their fragments are checked against the build (local fragments only).
- **Code:** `internal/sheet` reads a workbook's sheets by name; a new `internal/migration` (the workbook model,
  validation, mapping, commit, reconciliation, the steps); a new `internal/tenantexport`; the API's migration, import
  and tenant-export operations; the outbox's workers for the import, the reconciliation and the export.
- **Data:** migrations for the migration record, its steps and register, the generic import's runs and the tenant
  export's requests; the opening-item flag on invoices and bills.
- **Ports:** the blob store (the uploaded workbook, the archive, the export), the outbox, the clock; no new cloud
  service.
- **Fixtures:** a values-only copy of the synthetic sample workbook, pinned to its commit, written by a script from the
  workbook repository.
- **Threat model:** a page for the import (an untrusted workbook from a person) and the export (all of an
  organisation's data leaving it).
