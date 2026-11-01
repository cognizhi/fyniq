# Design

## Context

What exists that the checks reuse:

- **The close's checks** (`internal/domain/closing`, `run`): each finding is a `Check{key, severity, title, detail}`,
  recorded with the close (AUD-04). They cover:
  - earlier periods closed;
  - INV-08 balances, and the trial balance;
  - drafts and automatic reversals;
  - revaluation (C.2 #59);
  - the sub-ledgers against 1100, 2010, 2410, the assets and the deferrals (INV-07, C.2 #2);
  - the statements' placement (C.1 #4, #5);
  - the bank (`bankChecks`), tolerances, recurring journals and bills (C.1 #28, #29), variances (C.2 #57), the
    checklist (C.2 #58), and opening balances.
- **Document validation:** each source keeps its own ✖ errors and ⚠ warnings, worked out by its domain package
  (`bill.Validate`'s `Warnings`, the journal rules' severities, bank lines' categorisation).
- **Platform jobs that verify** (`outbox.PlatformSchedules`): each records its outcome in the trail of the
  organisation it found wrong.
  - `ledger.verify` (INV-08) and `audit.verify` (INV-12);
  - `evidence.verify` (FR-DOC-07);
  - `email.domains` (FR-INT-21, which writes a failed check for C.2 #37);
  - each schedule's `Stale` age (C.2 #12).
- **Screens:** `Checks.html` (`core` canvas), Home's pulse card with books health, and `AccessReview.html` (new,
  `settings-admin`).
- **Ports touched (ADR-T8):** `storage` only (new repositories: checks, check owners, access review attestations).
  No cloud dependency. The nightly run is a platform job, and changes reach it through the outbox (ADR-T7).

## Goals / Non-Goals

**Goals:**

- one catalogue that the Checks page, books health and the period close read, so a check never says two things;
- a check reruns within about a minute of a relevant change;
- every live check of the modules built has a fixture that makes it fire and one that makes it pass (QA-RPT-02).

**Non-Goals:**

- the checks of later modules (proposal);
- alerting by email or chat when a check turns ✖ (not in FR-CTL-01; notification rules may add it later);
- the Compliance Center's control view of the nightly result (`p1-compliance-center` reads this change's runs).

## Decisions

### D1 A catalogue of definitions as code

`internal/domain/checks` holds one `Definition` per Appendix C row:

| Field | What it holds |
|---|---|
| `Key` | stable, such as `books.debits-equal-credits` |
| `Ref` | its Appendix C reference (`C.1 #1`) |
| `Area` | Books, Setup, Transactions, Invoices, Bills, Journals, Assets, Sales, Recurring, Bank, Closing, Planning, Ledger, Audit, Controls, Banking, Purchases, Scheduler, Parameters, FX, Documents, Evidence, Email, Integrations, Claims, Tax or Settlements |
| `Worst` | the worst severity it can give: ✖ error, ⚠ warning or ⏰ due |
| `Title` and `Explain` | the plain words; Expert adds the rule IDs |
| `Owner` | the default owner role (D8) |
| `View` | the Appendix D capability, and its qualifier, that a viewer needs to see its items (D7) |
| `Module` | the organisation's feature switch it needs (FR-ORG-06), or none |
| `Triggers` | the audit object types whose change reruns it (D5), or none for the nightly-only checks |
| `Run(ctx, Env) (Result, error)` | `Env` carries the unit of work, the organisation's today, the base currency, the periods, the ledger revision, and the latest outcomes of the verifying platform jobs |

A `Result` holds:

- a status: ok, warning, error, due, not assessed, or failed (the run itself);
- the count of offending items;
- the first 50 items, each with its kind, ID, label and detail;
- a detail sentence, and the fix in words;
- for **not assessed**, the input it needs and the setting that holds it (ENG-23 words).

A run that returns an error records **failed**, which counts as ✖ (rev 0.162), and the error stays in the server's
log (never in the result: SEC-15).

*Alternative:* checks as SQL views. Rejected: the close's checks and the documents' validation are Go, and ENG-04
keeps every ledger figure behind the balance service.

### D2 The close's checks feed the catalogue

The close keeps its `run` and its record (AUD-04), unchanged in behaviour.

- The catalogue's close-backed definitions run `closing.Run` once, for the **period to close next**: the earliest
  open period whose month has ended, or none. Each takes the findings of its own close keys. For example:
  - `balances` → C.2 #1;
  - `trial-balance` → C.1 #1;
  - `payables`, `receivables`, `deposits`, `assets` and `deferrals` → C.2 #2;
  - `statements` → C.1 #4 and #5;
  - `recurring*` → C.1 #28 and #29;
  - `flux` → C.2 #57;
  - `checklist` → C.2 #58;
  - `revaluation` → C.2 #59;
  - `reversals` → C.2 #56.
- The close's keys stay stable, so recorded closes still read.
- New close-relevant checks (such as C.1 #33) are added to the close through the catalogue, not duplicated.

*Alternative:* move the close's code into `checks`. Rejected for now: it would rewrite tested code that the close's
attestation records depend on, for no change in behaviour.

### D3 What a check reads

- **Now checks** read the books as they stand, such as overdue invoices or the suspense balance.
- **Month checks** read each open period and the period to close next, and name the month in each item, such as the
  balance sheet balancing every month (C.1 #2) or a bank balance below zero at a month end (C.1 #31).

All ledger figures come from the balance service and the statements at one ledger revision, which the run records
(ENG-04, FR-RPT-08).

### D4 Storage

| Table | Holds | Rules |
|---|---|---|
| `check_state` | the latest result per check: status, count, items (JSON, at most 50), detail, missing input, `run_at`, the run's trigger and ledger revision, `version` | one row per organisation and key, replaced by each run; a change of status audited as `check.status_changed`, with the before and after status and count |
| `check_run` | each run: trigger (`change`, `nightly`, `request`), who asked, start, finish, ledger revision, the keys run | append-only; nightly runs also keep their full results, as the evidence the Compliance Center reads (CMP) |
| `check_owner` | a named person per check (D8) | versioned, audited |
| `access_review_attestation` | one per organisation and calendar quarter | append-only (D10) |

- Every table carries `organisation_id` with RLS forced (INV-06).
- Writes come from the worker role (`fyniq_worker`), except owners and attestations, which the API writes.
- Change and request runs older than 90 days are purged by the nightly job, through a function only the worker may
  call. Nightly runs are kept 13 months.

*Alternative:* every run's every result, append-only. Rejected: a busy day coalesces to hundreds of runs, about 60
results each, for no reader. The status history lives in the audit trail.

### D5 When checks run

- **On a relevant change (FR-CTL-01):**
  - after a write commits, the command's unit of work enqueues an outbox item `checks.refresh`. Its key is per
    organisation and is free again once the item runs, so a burst of writes makes one run. It is delayed 20 seconds
    to coalesce;
  - the job reads the organisation's audit events since its watermark, takes their object types, and runs the
    definitions whose `Triggers` name one, plus every definition that has never run;
  - writes from the scheduler and the worker (recurring entries, depreciation, revaluation, bank sync) enqueue it the
    same way, so no path is missed;
  - the enqueue sits in one place: `storage`'s audit recorder marks the unit of work, and the adapter adds the
    outbox row at commit.
- **Nightly (FR-CTL-01, FR-CTL-05):**
  - the platform job `checks.run` at 04:15 Singapore time runs every definition for every organisation, after
    `ledger.verify`, `audit.verify` and `evidence.verify`, whose outcomes it reads (D6);
  - it records a `check_run` with the full results.
- **On request:**
  - `runChecks` (Run now) enqueues the same job for the caller's organisation, at most once a minute (RateLimit
    headers);
  - the page shows "Running…" until the run's `run_at` moves.

### D6 The verifying jobs feed their checks

C.2 #1, #3 and #31 do not recompute in a change run. Each reads the latest outcome its platform job recorded, and
says when it ran:

- the balances (`ledger.verify`), the audit chain (`audit.verify`) and evidence hashes (`evidence.verify`);
- for the close-backed C.2 #1, the close's own run when the close ran later.

The other job-backed checks:

- C.2 #12 reads each platform schedule's last success against its `Stale` age, and the organisation's dead outbox
  rows.
- C.2 #37 reads `email.domains`' failed checks for the organisation's sending domains.
- A job that has never run leaves its check **not assessed**, naming the job.

FR-CTL-05's result on Checks is these checks, together with C.2 #2's sub-ledgers, shown as one "Nightly ledger
verification" group.

### D7 Who sees what

- **A new Appendix D row, "Checks and books health"** (PRD rev 0.163 with the first build slice, because
  `authztest` reads the matrix):
  - V for the Owner, Administrator, Controller, Accountant, Bookkeeper, Payment releaser, Auditor, Tax agent, DPO and
    AI;
  - V (sales), V (purchases) and V (payroll) for the AR specialist, the AP specialist and the Payroll administrator;
  - C for the Owner and the Controller, who name owners (D8).
- **Checks the viewer sees:** every reader sees every check's status, count and words. Its items show only to a
  viewer who holds the definition's `View`. Otherwise "3 items in Purchases: you cannot open them".
  - A qualified viewer (sales, purchases or payroll) sees only the checks of that area.
- **Run now:** any reader.
- **Books health on Home:** shown to a reader; otherwise the pulse card's third cell keeps net burn.
- **The access review** follows "Users, roles, team mode": V to read the report (Owner, Administrator, Controller,
  Auditor, DPO), A to attest (Owner).

### D8 Owners

- The catalogue names each check's default owner role, such as the Accountant for invoices, the Controller for the
  close and the ledger, and the Owner for controls.
- `setCheckOwner` lets an Owner or Controller name a person instead, or clear it:
  - with If-Match;
  - audited `check.owner_set`;
  - the person must be an active member who may see the check's items (D7), or it is refused with CHECK-02.
- The Checks page shows the person, or the role.

### D9 Books health and the Checks page

- **Books health** comes from `check_state`:
  - **problems** when any status is error or failed;
  - **review** when any is warning or due;
  - **OK** otherwise;
  - counts of ✖, ⚠, ⏰, OK and not assessed (rev 0.162).
- **Home's pulse card** takes it as its third cell, with chips by severity and "Open all checks". Net burn moves to
  Basic's tiles (`p1-dashboard-kpis` D5 named it as the placeholder).
- **`/checks`** shows:
  - the summary;
  - the "Nightly ledger verification" group;
  - each area's checks: chip (icon and text), words, owner and last run;
  - the items with their links (FR-UX-04), and a fix where one exists;
  - Run now;
  - the states of the screen's inventory row.
- **Basic and Expert:** Basic shows the same severities in plain words. Expert adds the rule IDs and Appendix C
  references.

### D10 The access review

- **`getAccessReview`** builds the report live:
  - each member's roles, last sign-in (the latest session) and second factor (passkey or authenticator, Expert);
  - SoD conflicts (the team mode's conflicts with what it allows: FM rules);
  - external users with `RolesExpireAt`, flagging one expired and still holding roles;
  - members not signed in for 30 days;
  - the organisation's integration clients and connectors with their health.
- **`attestAccessReview`**:
  - for the latest calendar quarter that has ended and is not attested;
  - an Owner, with step-up, an optional note and If-Match on the review's ETag;
  - records the report's canonical JSON and its SHA-256, audited `access_review.attested`;
  - CHECK-03 when that quarter is attested already, or has not ended.
- **C.2 #7** warns when a calendar quarter ended more than 14 days ago without its attestation, counting from the
  organisation's first member.
- **CSV download:** the report as shown, logged as an export (AUD-07).

### D11 Each check's fixture

Every definition gets a domain test with a fixture that makes it fire and one that makes it pass, tagged
`// req: C.1 #n` or `// req: C.2 #n` (QA-RPT-02, QA-TRACE-01). The engine has property tests:

- a run with no change leaves `check_state` and the audit trail as they were;
- books health is the worst status.

## Risks / Trade-offs

- **[A change run reads a lot]** Validating every open document on each change is heavy for a larger book.
  → Definitions scope themselves to documents changed since their last run, plus their open set. Runs are coalesced.
  Each run is timed and logged, and NFR performance budgets are checked in `p1-operations-dr`.
- **[The close and Checks disagree]** If the close's run and the catalogue drift apart, two answers appear.
  → One code path (D2), and a test that runs both on one fixture and compares.
- **[Items leak data]** An item's label names a supplier, an employee or a person's roles.
  → Items are filtered on read by `View` (D7). A test reads as each matrix role.
- **[Not assessed hides problems]** A check that cannot run never turns red. → It counts beside the score, names
  its input, and the Checks page lists not-assessed checks first in its group.

## Migration Plan

- The migrations add tables only. The close's checks keep their keys and their stored records.
- Rollback is the migrations' down steps. No ledger data changes.
- The first nightly run fills `check_state` for every organisation. Until then each check is not assessed, naming
  "the first run".

## Open Questions

- Which settings tab C.1 #9's employee half opens comes with the payroll link. Until then, its contacts and FX
  halves run alone.
