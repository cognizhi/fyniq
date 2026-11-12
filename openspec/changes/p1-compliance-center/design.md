# Design

## Context

- **The registry (P0, `compliance/`).**
  - **Files.** Each control is a YAML file with the PRD's row (kept equal by `scripts/controls.py`), an `owner` role
    and `checks`: one per frequency (CI, D, N, W, M, Q, S, E), each with a `status` (`running`, `planned` or `manual`).
  - **How a check runs.** A running check says how in `how`: `make <target>`, `test <Go test>`, `ci <CI job>` or
    `job <platform job>`. A planned one says when in `arrives`.
  - **Code.** `registry.go` embeds the files, and `registry_test.go` checks them against the schema and that every
    running check names something that exists.
  - **Evidence.** `evidence.go` is the nightly `compliance.evidence` job. It writes one write-once record per `job`
    check to the evidence store (`compliance/evidence/<date>/`), and fails when a check's job failed or is stale.
  - **Counts today.** Of 99 checks, 25 run and 74 are planned. Many planned ones now run under another name, as a test,
    a job or a check of the catalogue.
- **The checks engine (`p1-checks`, `internal/domain/checks`).**
  - The daily `checks.run` job runs each organisation's catalogue (Appendix C), and each check reports `ok`,
    `warning`, `error` or `not_assessed` with what it needs.
  - Among the catalogue's checks are ones that answer for controls: `ledger.sub-ledgers`, `controls.sod-conflicts`,
    `controls.review-log-overdue`, `controls.access-review-overdue`, `controls.accountant-review-overdue`,
    `close.month-unclosed`, `close.checklist`, `close.closed-unchanged`, `founders.*`, `equity.*`,
    `parameters.packs-current`, `gst.return-due`, `compliance.deadlines`, `scheduler.jobs-on-time`,
    `audit.chain-intact` and `evidence.hashes-verified`.
- **The reviews that exist, each signed off where it lives:**
  - the review log (Founder mode, weekly, C.2 #4);
  - the access review (quarterly, Settings › Access review, FR-CTL-06, C.2 #7);
  - the accountant reviews (C.2 #61);
  - the close and its checklist (C.2 #18, #58);
  - break-glass post-reviews (C.2 #53).
- **Builds.**
  - `internal/version` holds `Version` (`0.0.0-dev` unstamped), `Commit` and `BuildTime`, stamped by the image build.
  - `internal/api` must not import it: CI's reload timing edits it. The API gets its version through `Deps.Version`.
  - `VERSION` is 0.0.0: no release has been cut. `release-please.yml` tags and publishes releases, and has no job
    after that yet.
  - CI runs on every push to main and keeps its evidence for 90 days as artefacts: `test-report`,
    `security-scan-reports`, `image-reports` (SBOMs, scans, signatures) and `control-ctl-chg-01`.
- **Access.** Appendix D's "Compliance Center" row (`authz.ComplianceCenter`) gives V to Owner, Admin, Controller,
  Accountant and Auditor, C/A to the DPO, and nothing to the AI. Rev 0.240 adds **Policy library**: Owner and DPO
  C/A, every other member V, the AI nothing.
- **Setup.** First-run setup (`internal/api/setup.go`, FR-ADM-06) creates the installation's first organisation once.
- **Screens.**
  - `ComplianceCenter`, `Attestations` and `RiskPolicies` are fragments on the compliance canvas, held until
    Tue 10 Nov 2026 06:00 SGT.
  - The Compliance navigation opens the statutory calendar. Evidence and Audit trail are reached from Accounting.

## Goals / Non-Goals

**Goals:**
- A person with access sees every control the organisation answers for, whether it holds today, why, and the evidence,
  without anyone filling in a status by hand.
- A control that fails, or a review that falls due, reaches its owner as an Inbox item, and as a notice when it newly
  fails.
- Every running check in the registry names what runs it, and every planned one names the change that brings it.

**Non-Goals:**
- **Evidence export per framework and period (CMP-04).** It is P2.
- **The PDPA registers (CMP-05).** They come with `p1-pdpa-retention`.
- **Cloud posture findings (SEC-33).** They come with `p1-security-hardening`.
- **Copying CI's evidence into the evidence bucket.** It waits for the cloud (`[live-verify]`).
- **Uploading a policy as a file.** The owner chose drafts.

## Decisions

### D1. Check kinds and statuses in the registry

A check's `how` gains three kinds, and its `status` gains one value:

| `how` | Runs | Its status on the dashboard |
|---|---|---|
| `make …`, `test …`, `ci …` | In CI | From the release the platform runs (D3) |
| `job <name>` | As a platform job | The job's latest run: passed and fresh for its frequency → pass; failed → fail; not run on time → overdue |
| `check <key>` (new) | In the organisation's catalogue | The check's latest result: `ok` → pass, `warning` → overdue, `error` → fail, `not_assessed` → not assessed with what it needs |
| `verify <name>` (new) | In `compliance.run` (D4) | Its latest run for the organisation, as for a job |
| `attestation <kind>` (new) | By a person (D6) | Signed off for the current period → pass; past due → overdue |

- **Several checks per frequency.** A frequency may have more than one check, such as CTL-FIN-09's close checklist and
  months left open; each is listed, and the control takes the worst of them.
- **Planned checks.** A `planned` check is not assessed, naming what `arrives`, such as "arrives with
  `p1-ai-proposals`".
- **Cloud checks.** The new status `cloud` marks a check built and tested in `infra/` that runs only in the cloud. It
  is not assessed, "runs in the cloud".
- **A control's status.** It is the worst of its checks', in the order fail, overdue, not assessed, pass. It is not
  applicable when every check is not applicable.
- **Warnings as "overdue".** A warning reads as overdue because every catalogue check that answers for a control warns
  about something falling due: a review, a deadline, a pack. A registry test lists those checks, so a new mapping is
  read before it is trusted.
- **Alternatives.** A status typed in by hand was rejected: CMP-01's statuses come from checks. A single platform
  status per control was rejected too: the catalogue's checks are per organisation.

### D2. The control map: every row named, built or handed on

The registry is the plan's record of each control:

- **Running already, under another name.** These checks are named as they are. Examples: CTL-FIN-04 `check
  ledger.sub-ledgers`; CTL-FIN-07 `check controls.sod-conflicts` (N) and `check controls.review-log-overdue` (W);
  CTL-FIN-09 `check close.month-unclosed` and `check close.checklist`; CTL-FIN-10 `check
  controls.accountant-review-overdue`; CTL-FIN-11 and CTL-FIN-12 their `founders.*` and `equity.*` checks; CTL-IAM-02
  `check controls.access-review-overdue` and `check controls.external-users-past-expiry`; CTL-STAT-01 `check
  gst.return-due`; CTL-STAT-02 to CTL-STAT-04 `check compliance.deadlines`; CTL-RES-05 `check scheduler.jobs-on-time`;
  CTL-AUD-05 (CI) `test TestEveryFileAndRevealIsAudited`; CTL-FIN-06 (CI) the pack's pinned tests.
- **Built in this change (D4):**
  - CTL-FIN-02 N, CTL-AUD-01 N and CTL-AI-01 N: `job schema.verify`;
  - CTL-AI-01 CI (CMP-AI-01);
  - CTL-DP-04 CI;
  - CTL-FIN-08 N, CTL-IAM-01 N, CTL-API-02 N and CTL-AUD-05 N: as `verify` checks;
  - CTL-CHG-06 E: the release's SBOM.
- **Cloud.** CTL-CHG-02 D, CTL-DP-01, CTL-DP-07, CTL-IAM-03, CTL-IAM-04 and CTL-RES-01 are `cloud`, each naming its
  `tofu test` or the infra workflow that runs once the founder's organisation and projects exist. (rev 0.241) Building
  the map found that `infra/` does not yet hold the rest of the cloud rows this list first named: the records bucket
  (CTL-AUD-03), the Data Access logs and their sink (CTL-AUD-04), the SLO alerts (CTL-RES-03), the heartbeat alert
  (CTL-RES-05), Binary Authorization (CTL-CHG-04) and the scan of non-production (CTL-CHG-05). They move to
  `p1-operations-dr` and `p1-security-hardening`. CTL-CHG-09 E reads the release's image reports (D3).
- **Handed on.** The rows rev 0.240 and rev 0.241 moved keep `planned` for what is not built, with `arrives` naming
  their change. What is built of them is named as it runs, `cloud` included.

`registry_test.go` also checks:
- each `check` key exists in the catalogue;
- each `verify` and `attestation` name is defined;
- each `cloud` check names a module test (`tofu <module>/<run>`) or the infra workflow's job;
- no row placed in this change is left `planned` once its task is done.

### D3. CI evidence from the running release

- **The release job.** A new job in `release-please.yml`, `release-evidence`, runs only when a release is created.
  1. It finds the CI run of the tagged commit on main (`gh run list --workflow ci.yml --commit <sha>`) and requires it
     to have succeeded.
  2. It downloads that run's evidence artefacts and attaches them to the GitHub release as assets, kept for the life
     of the release.
  3. It writes a manifest, `controls-evidence.json`: each CI check of the registry, with the asset that holds its
     evidence.
  4. The SBOMs in `image-reports` give CTL-CHG-06's evidence.
  5. Its permissions are `contents: write` for that job only, and its actions are pinned by SHA (SEC-28).
- **The running build.** The release image build stamps `version.Release` with the tag; every other build leaves it
  empty. The wiring passes `Release` and the repository's address to the API's dependencies, never through an import
  of `internal/version`. A CI check then:
  - passes "in release vX.Y.Z" when the build is a release, linking to
    `<repository>/releases/download/<tag>/<asset>`;
  - otherwise is not assessed: "a development build: its checks passed in CI, but it carries no release's evidence".
- **No live calls.** The dashboard never calls GitHub. It links to the release and its manifest, so it works offline
  and in local mode.
- **Alternatives.** These were the owner's decision of 9 Nov 2026:
  - CI writing to the evidence bucket, which needs Workload Identity into the cloud (`[live-verify]` until then);
  - "not assessed until the cloud", which hides two thirds of the running checks.

### D4. New checks

- **`schema.verify` (platform job, nightly, worker).** It reads the database's grants, row-level security and guard
  triggers from the catalogue and compares them with the snapshot the release was built with. That snapshot is the
  golden `TestEachRoleHoldsExactlyItsGrants` already keeps, embedded. Any difference fails the job and its evidence,
  naming the role, table and privilege. It proves three controls:
  - CTL-FIN-02: posted ledger and audit tables stay immutable;
  - CTL-AUD-01: every audited table keeps its `require_audit` trigger;
  - CTL-AI-01: the AI's role keeps no write grant.
- **CMP-AI-01 (CI):**
  - a Postgres test that `fyniq_ai` holds no INSERT, UPDATE, DELETE or TRUNCATE on any table. Proposals do not exist
    yet; their table's single INSERT grant joins with `p1-ai-proposals`.
  - an `infra/` module test that the AI service account holds no role that writes data: no Cloud SQL client beyond
    the `fyniq_ai` login, and no write role on any bucket, queue or secret.
  - a test that reads the AI tool registry when one exists and fails on any tool not marked read-only. It passes
    vacuously until `p1-ai-assistant` adds the registry, and its message says so.
- **CTL-DP-04 (CI).** An archtest: no sign-in, step-up or recovery package reads an NRIC or FIN field, and no file
  export takes a password derived from one.
- **`compliance.run` (platform job, daily, worker, after `checks.run`).** For each organisation it runs the `verify`
  checks and records each result in `fyniq.control_run`, then computes every control's status and sends notices (D5):
  - `verify payments.dual-control` (CTL-FIN-08 N): every payment batch released, and every payee and claimant bank
    change approved, in the last day was decided by someone other than whoever prepared it, or falls under the team
    mode's recorded exception;
  - `verify members.second-factor` (CTL-IAM-01 N): no session of the last day was used beyond reading itself without a
    second factor (SEC-01), and every active member has one registered;
  - `verify integrations.owned` (CTL-API-02 N): every integration client has an owner, a class and an unexpired end
    date, and holds only its class's scopes. (rev 0.242) Fyniq registers no integration client until Connected apps
    (AI-46), so it is built with `p1-mcp-first-party`;
  - `verify audit.exports-reconciled` (CTL-AUD-05 N): every report export, evidence opening and reveal of the last day
    has its audit event, sampled up to 1,000 per kind.

  `fyniq.control_run` (org, control, check, ran_at, ok, detail) is insert-only, written by the worker and read under RLS.
- **Why `verify` checks and not new Appendix C rows.** These verifications prove controls rather than state the
  books, and the control dashboard is where they are read. New catalogue rows would duplicate them on Checks, and
  would need Appendix C changes for no user-facing gain.

### D5. Failing controls: Inbox items and notices

- **Inbox.** The Compliance section lists, for the viewer's roles, the controls failing or overdue whose owner role
  they hold, and the reviews due (D6). It is computed on read from the same sources.
- **Notices.** `compliance.run` compares each control's status with its last run's. A **detective** check (a job,
  `check`, `verify` or `attestation`) that newly fails or falls overdue sends `compliance.control_failed`:
  - critical, emailed at once;
  - to the members holding the control's owner role;
  - once per failure, keyed by the control and the day it started failing.

  A return to pass is recorded but not notified.
- **CI checks never notify.** They are preventive: a failing gate blocks the merge and the release (§14.1 item 5).

### D6. Reviews and attestations

- **One list.** `GET /v1/compliance/reviews` lists every recurring review, each with its period, due date, owner role,
  last sign-off (who and when), evidence, and where it is signed off. It reads each from its own store:
  - the review log, weekly in Founder mode;
  - the access review, each quarter;
  - the accountant reviews: the journal review, monthly;
  - the close sign-off, monthly;
  - break-glass post-reviews, on each use;
  - policy acknowledgements (D8);
  - risk reviews (D7).

  "Overdue" uses each source's own rule; a review log entry, for example, is overdue after 14 days.
- **Sign-off.** Policy acknowledgements are signed off on this page, and risk reviews on the risks page. Every other
  review keeps its own screen.
- **Alternative.** A generic attestation table holding every review was rejected: it would copy what each source
  already records.

### D7. The risk register

- **Tables.**
  - `fyniq.risk` (org, id, `ref` such as `R1` or `R-20`) and `fyniq.risk_version`, insert-only, one row per saved
    version. A version holds the title, description, owner role, likelihood and impact (`low`, `medium` or `high`, as
    §19 rates them), mitigation, review interval (quarterly), next review date, and `open` or `closed`.
  - `fyniq.risk_review`, insert-only: the risk version reviewed, who reviewed it, when, and a note. A review moves the
    next review date on by the interval.
  - Saving uses If-Match on the risk's version. Each save is audited: `risk.saved` and `risk.reviewed`.
- **Rights.** Under Appendix D's "Compliance Center" row, the DPO creates, revises and reviews, and everyone else with
  V reads.
- **Seed (the owner's decision).** First-run setup loads R1–R19 into the organisation it creates, in the same unit of
  work.
  - **Text.** The text is §19's as it stands at the build, without its revision tags, embedded as
    `compliance/risks.yaml`.
  - **Owner roles.** These go by area, as the registry's owners do: financial to the Controller, data to the DPO,
    platform and delivery to the Owner.
  - **First review.** It is due at the end of the next quarter.
  - **Earlier installations.** A migration seeds the organisation that setup created where setup ran before this
    change, found by the setup record. Every other organisation starts empty.

### D8. The policy library

- **Drafts.** Fyniq's starting drafts are `compliance/policies/<key>.md`, embedded, one per CMP-07 policy:
  `information-security`, `acceptable-use`, `access-control`, `incident-response`, `backup`, `change-management`,
  `ai-use` and `data-protection`.
  - Each has front matter: title, draft version (`fyniq-policy-draft@1`), and its sources as official URLs with the
    retrieval date.
  - Each states what Fyniq itself enforces and points to the control that proves it.
  - The text is written for a company of a few people from:
    - CSA's Cyber Essentials and Cyber Trust;
    - the PDPC's guides: the data protection management programme, managing and notifying data breaches, and the
      advisory guidelines;
    - IMDA's Model AI Governance Frameworks.
  - [counsel to confirm] marks each clause where the law bears on it, such as notification timers and transfers.
- **Tables.**
  - `fyniq.policy` (org, key, title).
  - `fyniq.policy_version`, insert-only: policy, version, body (Markdown), SHA-256, `draft` or `published`, the draft
    it started from, and who published it and when. An organisation's first draft is Fyniq's starting draft, copied
    when the library is first opened, and an Owner or DPO edits a draft before publishing. Publishing a draft makes it
    the next version, with If-Match; a published version never changes.
  - `fyniq.policy_acknowledgement`, insert-only, unique per member and version: who and when.
- **Rights and acknowledgement.**
  - Under Appendix D's new **Policy library** row, the Owner and the DPO draft and publish; every other member reads
    the published versions.
  - Each member acknowledges each published version for themselves. It is due 30 days after the later of the
    version's publication and the member joining.
  - Each action is audited: `policy.draft_saved`, `policy.published` and `policy.acknowledged`.
- **The AI** holds nothing on the row.

### D9. API and screens

- **Operations.** All under the "Compliance" tag:
  - `listControls` (V, filters `framework` and `status`) and `getControl`;
  - `listComplianceReviews` (V);
  - `listRisks`, `createRisk`, `reviseRisk` and `reviewRisk` (C/A on Compliance Center);
  - `listPolicies` and `getPolicy` (V on Policy library);
  - `savePolicyDraft` and `publishPolicyVersion` (C/A on Policy library);
  - `acknowledgePolicyVersion` (any member, for themselves, signed in to Fyniq, never the AI).

  The lists are whole: 67 controls, a register of tens of risks, eight policies. Each is exempt from cursor paging
  with that reason. Writes take Idempotency-Key, and versioned records take ETag and If-Match (§8.18.1).
- **Screens:**
  - Compliance › Controls (Expert): filters, the list, and a control's checks with their evidence.
  - Compliance › Reviews and attestations (Basic and Expert): what is due, overdue and signed.
  - Compliance › Risks and policies (Expert): two tabs.
  - A policy card for every member, in the Inbox and on Home, while a version waits for their acknowledgement.
- **Navigation.** A Compliance page lists the module's pages, as Accounting does, and the navigation opens it.

### D10. Ports, jobs and events

- **Ports (ADR-T8).** A new storage port, `storage.Compliance`, covers risks, policies, acknowledgements and control
  runs, with its Postgres adapter and the differential memory store. The release's evidence is only linked, so no port
  reaches GitHub, and no cloud SDK is imported outside `internal/adapters/<provider>/`.
- **Jobs (ADR-T7).** `schema.verify` and `compliance.run` run through the platform's schedules, with their heartbeats
  and C.2 #12's watch. `compliance.evidence` records `schema.verify` like any job check.
- **Audit events (AUD-01).** `risk.saved`, `risk.reviewed`, `policy.draft_saved`, `policy.published` and
  `policy.acknowledged`, each in the same transaction. `compliance.run` writes `control.failed` and `control.recovered`
  to the organisation's trail when a status changes.

## Risks / Trade-offs

- **[Until the first release, every CI check reads "not assessed".]** → The dashboard says why: "a development build".
  The first release cut by release-please turns them to passed, with evidence, without a code change.
- **[A catalogue warning read as "overdue" may not mean a due date for a future mapping.]** → `registry_test.go` lists
  the mapped checks and their meaning, and fails on a mapping it does not list.
- **[`schema.verify` compares with the release's snapshot, so a migration applied without its release would fail it.]**
  → That is the point. The deploy applies migrations and the release together (§12.10), and the job names the
  difference.
- **[The drafts read as legal advice.]** → They state what Fyniq enforces and cite their sources. [counsel to confirm]
  marks the legal clauses, and nothing is published until an Owner or DPO publishes it.
- **[Notice fatigue.]** → A notice goes out once per failure, and returns to pass are silent.
- **[The R1–R19 text goes stale as §19 changes.]** → The seed is a snapshot. A later §19 revision changes the
  operator's register by a new risk version, never by an edit.

## Migration Plan

1. **Migrations.**
   - New tables: `risk`, `risk_version`, `risk_review`, `policy`, `policy_version`, `policy_acknowledgement` and
     `control_run`, each with RLS and grants.
   - The seed for an installation already set up.

   All are additive, with expand-and-contract not needed.
2. **The release workflow job** waits for the first release created after it merges.
3. **Rollback.** Revert the code. The new tables stay, and nothing else reads them.

## Open Questions

- **The R1–R19 owner roles** follow the registry's area rule. A founder can change any of them in the app, so the
  build's choice needs no decision now.
