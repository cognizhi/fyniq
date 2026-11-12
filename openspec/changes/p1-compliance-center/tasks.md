# Tasks

Each slice below is one pull request that passes `make test` on its own. Each one carries:
- its migration and attack tests;
- its API, with authorisation-matrix rows on Appendix D's "Compliance Center" or "Policy library" row;
- its screens, with help keys and guide sections compiled into the knowledge base;
- the tests behind every figure and every message it adds (`// req:` tags).

Before each slice, load `fyniq-feature-change` and `fyniq-security-compliance`. Also load `fyniq-design-system` for the
screens, `fyniq-release` for slice 2's workflow, and `fyniq-knowledge-base` for every guide page.

## 1. The decisions and the control map (slice 1)

- [x] 1.1 PRD-001 rev 0.240 records the owner's decisions of 9 Nov 2026, and the main specs are updated and stamped
  (`python3 scripts/prdspec.py stale` is clean, `make check` passes):
  - CMP-01: CI checks from the running release;
  - CMP-06: R1–R19 for the operator only;
  - CMP-07: drafts the Owner or DPO publishes, under the new Appendix D row **Policy library**;
  - CMP-AI-02 and CMP-AI-03 with `p1-ai-providers`.

  The build plan moves the control rows whose modules come later (`make roadmap`).
- [x] 1.2 The registry's check kinds `check <key>`, `verify <name>` and `attestation <kind>`, and the status `cloud`
  (design D1), in `control.schema.json` and `registry.go`. Each control also gets its frameworks, parsed from its
  mapping. `registry_test.go` fails on:
  - an unknown mapping token;
  - a `check` key not in the catalogue;
  - an undefined `verify` or `attestation`;
  - a `cloud` check that names no module test or infra workflow job.

  `// req: CMP-01`, and the unnumbered "Control registry as code".

  **What building added:**
    - **Several checks per frequency.** A frequency may have more than one check; the control takes the worst. A
      control may still have only one `job` check per frequency, since its evidence record is named by control and
      frequency.
    - **Cloud checks** name an `infra/` module test (`tofu <module>/<run>`) or the infra workflow's job (`ci plan`), and a
      module test may only be named by a cloud check.
    - **Arrivals.** A planned check's `arrives` starts with a change of the P1–P3 plan (`scripts/roadmap.py`), never a
      phase such as "P1".
    - **Frameworks.** Ten, from Appendix F's mapping keys: Singapore statute, PDPA, ISO/IEC 27001, SOC 2, CSA Cyber
      Essentials, CSA Cyber Trust, IMDA AI governance (AG and the Starter Kit), OWASP (ASVS included), COSO, and the
      International Standards on Auditing (CTL-FIN-10's ISA 240). A mapping is split at semicolons outside brackets.
    - **Declared names.** The four verifications (D4) and the seven attestations (D6) are declared in `registry.go`; the
      slices that build them run each declared name.
- [x] 1.3 The control map (D2). Every control placed in this change names each running check, or is marked `cloud` or
  built by a later slice. The rows rev 0.240 moved keep `planned` with `arrives` naming their change.
  `go run -tags local ./cmd/jobs controls` lists each control with how it runs. The registry test lists the catalogue
  checks mapped to controls and what their warnings mean. `// req:` each CTL ID whose check is named here, in
  `registry_test.go`.

  **What building added:**
    - **Counts.** Of 109 checks (99 before, one per frequency), 55 run (25 before), 10 run in the cloud and 44 are
      planned: 11 for later slices of this change, the rest with the change each row moved to.
    - **Rev 0.241.** `infra/` does not yet hold what six rows check in the cloud: the records bucket (CTL-AUD-03), the
      Data Access logs and their sink (CTL-AUD-04), the SLO alerts (CTL-RES-03), the heartbeat alert (CTL-RES-05),
      Binary Authorization (CTL-CHG-04) and the scan of non-production (CTL-CHG-05). They moved to `p1-operations-dr`
      and `p1-security-hardening`; what is built of them is named (the anchors bucket's lock, the separate projects,
      `scheduler.jobs-on-time`). 43 rows are closed here.
    - **Warnings.** `ledger.sub-ledgers`, `assets.register` and `equity.acra-register` warn about an amount or a
      difference awaiting review, not a date; the list in `registry_test.go` says so for each, and the dashboard reads
      them as overdue under D1.
    - **CTL-FIN-10** maps to the accountant's review, which Founder mode's FM-7 sets each quarter, not monthly; its
      evidence says so.

## 2. CI evidence from the running release (slice 2)

- [x] 2.1 The `release-evidence` job in `release-please.yml` (D3):
  - it runs only when a release is created, and requires the tagged commit's CI run on main to have succeeded;
  - it attaches that run's evidence artefacts and a `controls-evidence.json` manifest to the GitHub release;
  - it holds `contents: write` for that job alone, with every action pinned by SHA.

  `make scan` passes (actionlint, zizmor). A script test builds the manifest from a fixture run.
  `// req: CMP-01`, `// req: CTL-CHG-06`, and the unnumbered "Evidence collected automatically".

  **What building added:**
    - **Its own workflow.** The job is `.github/workflows/release-evidence.yml`, started when release-please publishes
      a release (its App token starts workflows), or by hand for a tag. It waits up to four hours for CI on the release
      commit, which would otherwise hold release-please's concurrency group and its release pull request.
    - **Both runs.** It waits for the commit's `ci.yml` and `controls.yml` runs (CTL-CHG-01's report), and refuses unless
      both succeeded. It attaches their artefacts as `<name>.zip` and their logs as `ci-logs.zip` and
      `controls-logs.zip`, so a check without an artefact, such as `make differential`, has its run's log.
    - **One table.** `compliance/release-assets.json` names each CI check's asset. The script and the Go registry
      (`ReleaseAssets.Asset`) both read it, so the manifest and the dashboard's links cannot differ.
      `scripts/release-evidence.py self-test` (in `make controls`) checks that each workflow uploads the artefacts the
      table names, and builds a manifest from a fixture run, refusing a failed or cancelled run, a missing run and a
      missing artefact.
    - **CI on main finishes.** `ci.yml` cancels only a pull request's older runs; a push to main no longer cancels the
      run of the commit before it, which may be a release's.
    - **CTL-CHG-06 E and CTL-CHG-09 E** run as `ci Release evidence (CMP-01)`, with the release's image reports.
    - [live-verify] The publish path (the runs API, the artefact downloads, the logs archive and the upload) runs on the
      first release.
- [x] 2.2 `version.Release`, stamped only by the release image build, passed through the wiring to the API's
  dependencies (never imported by `internal/api`), with the repository's address. A resolver turns a CI check into
  passed-in-release with its asset link, or not assessed in a development build. Unit tests cover both builds.
  `// req: CMP-01`

  **What building added:**
    - `make images RELEASE=vX.Y.Z` is the only build that stamps it, through the Dockerfile's `RELEASE` argument; `/version`
      reports it. The images job of a release (not built yet) uses it, and the release-please workflow says so.
    - Only a tag of the form vX.Y.Z is a release; any other value, or no repository, is a development build.
    - The resolver is `internal/domain/compliance` (`Release.CI`), which slice 4 extends.
    - `.dockerignore` lets `release-assets.json` into the image build, since the registry embeds it.

## 3. The database and AI checks (slice 3)

- [x] 3.1 `schema.verify` (platform job, nightly, worker; D4). It compares live grants, RLS and guard triggers with the
  embedded snapshot that `TestEachRoleHoldsExactlyItsGrants` keeps, and records its evidence. Tests:
  - a clean database passes;
  - a grant added by hand, a dropped `require_audit` trigger and an INSERT granted to `fyniq_ai` each fail, naming
    the role, table and privilege.

  `// req: CTL-FIN-02`, `// req: CTL-AUD-01`, `// req: CTL-AI-01`

  **What building added:**
    - **One reader.** The grants query and the snapshot's trigger and policy queries moved from the tests into the
      adapter (`catalogue.go`), so the tests write the goldens and the job reads the database the same way. The
      grants golden is unchanged. The schema snapshot gains a section, "row-level security": each table, enabled or
      not and forced or not.
    - **The release's snapshots** are the goldens, embedded in the build (`postgres.ReleaseGuards`).
    - **search_path.** The catalogue qualifies function names by the session's search_path, so the job reads the
      guards with the tests' path (schema fyniq), in a read-only transaction that `store.go` opens.
    - **Read as the worker.** The test reads the guards as `fyniq_worker`. It plants each change in a transaction that
      rolls back (GRANT, DROP TRIGGER and ALTER TABLE are transactional), so the shared database never changes.
    - **Schedule.** Nightly at 03:10, before the night's control evidence. C.2 #12 watches it like every scheduled
      job, and `compliance.evidence` records it.
    - [live-verify] The first run against the dev cloud's database, whose catalogue may carry grants of Cloud SQL's own
      roles.
- [x] 3.2 CMP-AI-01 in CI. Each part has its own test:
  - `fyniq_ai` holds no write privilege on any table;
  - the `infra/` environment module's test shows the AI service account holds no role that writes data;
  - the AI tool registry, once it exists, holds only read-only tools; until then the test passes and says why.

  `// req: CMP-AI-01`, `// req: CTL-AI-01`

  **What building added:**
    - **Database.** `TestTheAIRoleHoldsNoWritePrivilege` asks PostgreSQL itself (`has_table_privilege` and its kin), so
      a privilege through PUBLIC or a role counts too. It covers every table, view and sequence, the functions of
      schema fyniq, and CREATE on any schema. (`btree_gist`'s operator support in `fyniq_ext` is PUBLIC's by default and
      writes nothing; `fyniq_ai` has no USAGE there.)
    - **Infrastructure.** The environment module's test `the_ai_service_account_writes_nothing`: the AI orchestrator's
      account holds no key, secret, queue, project, invoker or job role.
    - **Tool registry.** The test reads `contracts/ai/tools/*.json` and requires `annotations.readOnlyHint`, as MCP
      annotates tools. `p1-ai-assistant` puts its registry there.
- [x] 3.3 CTL-DP-04's archtest: no sign-in, step-up or recovery package reads an NRIC or FIN field, and no export
  password derives from one. `// req: CTL-DP-04`

  **What building added:**
    - **Scope.** The packages `authn`, `signin`, `passkey` and `oauthflow`, and the API's and the store's session,
      sign-in, passkey, recovery and setup files, may not name an NRIC or FIN in any identifier or string; comments
      may.
    - **Passwords.** No function anywhere names both an NRIC or FIN and a password or passphrase. No export password
      exists yet; Nemura's payslip delivery (PRD-002 NM-PAY-04) is where one will.

## 4. Verifications per organisation, and notices (slice 4)

- [x] 4.1 `fyniq.control_run` (insert-only, worker writes, RLS) and the `compliance.run` job (daily, after
  `checks.run`), with the four `verify` checks (D4). Tests seed a breach of each and see it fail:
  - a batch released by its preparer outside the team mode's exception;
  - a session used without a second factor;
  - ~~an integration client without an owner or end date~~ (rev 0.242: built with `p1-mcp-first-party`);
  - an export without its audit event.

  Attack tests cover the table. `// req: CTL-FIN-08`, `// req: CTL-IAM-01`, `// req: CTL-API-02`,
  `// req: CTL-AUD-05`

  **What building added:**
    - **Three verifications, not four.** Fyniq registers no integration client until Connected apps (AI-46), so
      `integrations.owned` and CTL-API-02 move to `p1-mcp-first-party` (rev 0.242).
    - **Payments' dual control** reads each approval of a payment batch or bank change decided in the window, and
      each release. It finds:
      - an approval nobody signed;
      - an approval its preparer or requester signed;
      - a release under such an approval;
      - a release under a break-glass its Owner did not use for that batch.

      The rule is Go's (`dualControlFindings`), over the facts the store reads.
    - **The second factor.** Sessions are installation records that no role reads, so `fyniq.members_second_factor`
      (SECURITY DEFINER, the worker only, the transaction's organisation only) answers per active member: passkeys,
      sign-ins that passed a second factor, and sessions seen in the window that passed none. Audit events name the
      OAuth client, not the session, so "used beyond reading itself" is read as "seen at all". This matches the PRD's
      check, "users without MFA = 0".
    - **Exports and reads.** Evidence openings and reveals have no record but their audit event, so the
      reconciliation checks two things:
      - each report export asked for has its `report_export.requested` event;
      - the latest 1,000 events of each audited read keep their request details beside the chain (AUD-06).

      `compliance.AuditedReads` must equal the API's `auditedReads` (`TestEveryAuditedReadIsReconciled`).
    - **The run.** `compliance.run` runs daily at 04:35, after the checks. Each verification runs in its own unit of
      work, over the last 25 hours, and records its run: what it found (the first 50 items), how many, and in words.
      Breaches are seeded in the test past the database's own guards (triggers off, as the superuser).
    - **Statuses and notices (4.2)** are their own pull request.
- [x] 4.2 Control statuses for an organisation (`internal/domain/compliance`; D1). Every kind of check is covered:
  CI, job, `check`, `verify`, `attestation`, planned and `cloud`. The worst-of rule and not applicable are covered too.
  `compliance.run` sends `compliance.control_failed` (critical, emailed at once, to the owner role) once per failure,
  and writes `control.failed` and `control.recovered`. Tests cover:
  - a detective failure notified once;
  - a CI failure never notified;
  - a recovery recorded, not notified.

  `// req: CMP-01`, and the unnumbered "The Compliance Center shows controls and raises tasks" and "Failing controls
  block releases or page the owner".

  **What building added:**
    - **`fyniq.control_state`** (migration 000295) keeps one row per organisation and control, written by the worker
      at version 1 and then only as its next version, never deleted (FY308). A change of status is audited on the row
      (`require_audit_status`) as `control.failed`, `control.recovered` or `control.status_changed`. `failing_since` is
      set exactly while the control fails or is overdue, and keeps when the failure began, so a failure is told once.
    - **Reading each kind of check:**
      - a job, through `JobOutcomeOf`, which the night's evidence uses too: one that never ran reads as overdue;
      - a catalogue check: ⏰ due reads as overdue, like a warning, and `failed` as fail;
      - a verification's latest run: one older than 26 hours reads as overdue;
      - an attestation: not assessed until slice 8.

      A passing control says that each of its checks passed.
    - **Order.** `compliance.run` works out the statuses after each organisation's verifications, in one unit of work.
    - **The notice.** `compliance.control_failed` is a security notice: critical, locked and emailed at once. It goes to
      the members holding the control's owner role (the registry's owner, in lower case, is the role), and links to
      `/compliance/controls?control=<ID>`, the screen of slice 5.
    - **The release.** The worker and the jobs set the Kit's release from `version.Release`; the wiring never imports
      `internal/version`.

## 5. Compliance › Controls (slice 5)

- [x] 5.1 `listControls` (filters `framework` and `status`) and `getControl`, with V on "Compliance Center" and matrix
  rows. Each control comes with its checks, last runs, evidence links, owner and frameworks. Tests:
  - a control of each status;
  - the framework filter;
  - a Bookkeeper refused.

  `// req: CMP-01`, `// req: FR-CTL-05`

  **What building added:**
    - **The status shown** is the one `compliance.run` last saved (`control_state`), with each check's words and
      evidence. Before the first run, every control and check is "Not worked out yet". The API never reads job slots
      or GitHub.
    - **The response** has each control's frameworks (`FrameworkKey`, an enum of the registry's ten), every framework
      with how many controls it holds, and the release the platform runs (none in a development build).
    - **Checks** carry their kind (ci, job, check, verify, attestation, cloud, none), their state in the registry, and,
      for a planned check, the change it arrives with.
    - **Errors.** An unknown control is COMPLIANCE-01 (404). Both operations are exempt from cursor paging, with
      their reasons.
- [x] 5.2 The screen Compliance › Controls (Expert, `/compliance/controls`):
  - the framework filter;
  - the list with statuses, as icon and label;
  - a control's checks and evidence;
  - the nightly ledger and audit-chain results.

  The Compliance page lists the module's pages, and the navigation opens it. The Inbox's Compliance section shows
  failing controls for the viewer's roles. It has the registry entries, help keys and the guide page
  `compliance-controls.md`, compiled into `kb/`. A component test with axe covers all passing, a failing control and
  not assessed. `// req: CMP-01`, `// req: FR-UX-29`

  **What building added:**
    - **A Compliance page** (`/compliance`, Basic) lists the module's pages: the statutory calendar in Basic mode, and
      the controls, GST, evidence and audit trail too in Expert mode. The navigation's **Compliance** opens it, and
      the audit trail's breadcrumb leads to it.
    - **The screen** follows the fragment, with three differences:
      - the status counts are buttons that filter the list;
      - only frameworks with controls get a filter;
      - **Cloud posture** waits for `p1-security-hardening` (SEC-33).

      **Nightly verification** reads CTL-FIN-01, CTL-FIN-04 and CTL-AUD-02. A development build says why its CI
      checks are not assessed.
    - **The Inbox's Compliance section** lists the failing or overdue controls whose owner role the viewer holds. It
      asks the API only for roles that may read the Compliance Center.
    - **The guide page** `compliance-controls.md` is compiled into `kb/pages/task/compliance-controls.md`. The Checks
      guide now says where its nightly verification also shows, which links the two pages.

## 6. The risk register (slice 6)

- [x] 6.1 `fyniq.risk`, `fyniq.risk_version` and `fyniq.risk_review` (D7), with attack tests, and the operations
  `listRisks`, `createRisk`, `reviseRisk` (If-Match) and `reviewRisk` on the "Compliance Center" row, with matrix
  rows. Tests:
  - a risk saved, revised and reviewed, each audited;
  - a stale version refused;
  - a Controller refused a write.

  `// req: CMP-06`

  **What building added:**
    - **References** are R and a number, unique in the organisation: a new risk takes one more than the highest used.
    - **Reviewers** are named as records name actors (`user:<id>`), as the saver is: there is no foreign key to the
      installation's users.
    - **The next review.** A review of a risk's latest version sets when the next one falls due, and that date is the
      risk's until a later version sets another. `review_overdue` comes from it, and a closed risk is never overdue.
    - **Rights.** Appendix D gives only the Compliance officer C/A on "Compliance Center": the DPO adds, changes
      (C) and reviews (A) risks; everyone else with V reads them. Matrix rows are added.
    - **The database.** Versions and reviews are insert-only, and a risk row never changes (FY309, `audit_immutable`).
      Each write is audited on the risk (`risk.saved`, `risk.reviewed`), and the API refuses a stale version (If-Match).
- [x] 6.2 The seed. First-run setup loads R1–R19 from `compliance/risks.yaml` into the organisation it creates. A
  migration does the same for an installation already set up. A second organisation starts empty. Tests: setup seeds
  19 risks with owner roles and review dates, and a second organisation has none. `// req: CMP-06`

  **What building added:**
    - **Rev 0.243.** The seed is §19's risks, R1–R22 today. CMP-06's "R1–R19" was written before R20–R22.
    - **Setup.** The seed is `compliance/risks.yaml`, with §19's qualified ratings ("High (yearly)", "Certain",
      "Low–Medium") kept in each description. `CompleteSetup` creates the risks in the same transaction as the first
      organisation, saved by the first user.
    - **Not built:** the migration for an installation set up before this change. The installation does not record
      which organisation setup created, and no installation exists yet outside a developer's own database, which
      `make reset` sets up again.
- [x] 6.3 The Risks tab of Compliance › Risks and policies (Expert, `/compliance/risks`): the list with owner,
  rating, mitigation and review date, review overdue, and the review action. It has help keys and the guide page
  `compliance-risks-policies.md`, compiled into `kb/`. A component test with axe covers review overdue.
  `// req: CMP-06`

  **What building added:**
    - **The page** is `/compliance/risks`, and the Compliance page lists it. Until the policy library (slice 7) adds
      its tab, it shows the register alone.
    - **Each risk** is rated high, medium or low from its likelihood times its impact (high from 6, medium from 3),
      and the register lists the highest first.
    - **The Compliance officer** adds and changes risks in a dialog, and reviews one with a note; other roles only read.
    - **The guide page** `compliance-risks-policies.md` is compiled into `kb/pages/task/compliance-risks.md`. The
      Controls guide links to it.

## 7. The policy library (slice 7)

- [x] 7.1 The eight drafts in `compliance/policies/` (D8), each written from official sources with their URLs and
  retrieval dates, and [counsel to confirm] on each legal clause. A test checks that each draft has its front matter,
  sources and policy key, and that the eight keys are CMP-07's. `// req: CMP-07`

  **What building added:**
    - **The drafts** are `fyniq-policy-draft@1`, each with its key, title, sources (title, URL, retrieved 9 Nov 2026)
      and the controls it serves. `compliance.PolicyDrafts()` reads them from the embedded files with each text's
      SHA-256, and `TestThePolicyDrafts` checks them.
    - **[counsel to confirm]** marks the clauses that bear on the law. They include the breach timers in incident
      response, and the Data Protection Officer, consent, NRIC, retention, access and transfer clauses in data
      protection.
- [ ] 7.2 `fyniq.policy`, `fyniq.policy_version` and `fyniq.policy_acknowledgement` (D8), with attack tests, and the
  Appendix D row **Policy library** in `internal/authz`. The operations are `listPolicies`, `getPolicy`,
  `savePolicyDraft`, `publishPolicyVersion` (If-Match) and `acknowledgePolicyVersion` (own, signed in, never the AI),
  with matrix rows. Tests:
  - the first opening copies Fyniq's drafts;
  - a draft edited and published as version 1;
  - a published version never changes;
  - a member acknowledges once;
  - acknowledgement due 30 days after publication or joining;
  - the AI refused.

  `// req: CMP-07`, and the "Permission matrix: Policy library" spec text.

  **What building added:**
    - **The tables** (migration 000297) are `policy_draft` (one row for each revision of the organisation's draft),
      `policy_version` and `policy_acknowledgement`.
    - **Insert only.** All three are insert-only (FY310), and each row is audited on itself: `policy.draft_saved`,
      `policy.published`, `policy.acknowledged`.
    - **Numbering.** A revision or a version must follow the one before it.
    - **Checksums.** Each row's SHA-256 is checked against its text. A version records what it was published from:
      `fyniq-policy-draft@1` or `revision N`.
    - **Fyniq's drafts are not copied on first opening.** Until the organisation saves a draft of its own, Fyniq's
      draft is served from the embedded file as revision 0. Opening a policy therefore writes nothing, and a
      read stays a read.
    - **Who sees what:**
      - the draft's text is shown only to those who may edit the library;
      - its ETag `"r<revision>"` guards saving and publishing;
      - publishing a draft no different from the version in force is refused (422).
    - **Rights.** Appendix D's "Policy library" gives the Owner and the Compliance officer C/A, and every other role V.
      Acknowledging needs V and a signed-in session: an integration's token gets `SESSION_REQUIRED`, and the AI is
      refused.
    - **Acknowledging** applies to the current version only, once, with `If-Match: "v<n>"`.
    - **Due dates.** An acknowledgement falls due 30 days after the later of the version's publication and the
      member's joining (`AcknowledgeWithin`). It is overdue after that day in Singapore time.
    - **Tests:** `TestThePolicyLibrary`, `TestAttackThePolicyLibraryKeepsItsHistory` and matrix rows.
- [x] 7.3 The Policies tab (versions, acknowledgement rate, drafting and publishing) and the member's "Acknowledge
  policy" card in the Inbox and on Home. It has help keys, and the guide page's policies section, compiled into `kb/`.
  A component test with axe covers acknowledgement pending. `// req: CMP-07`

  **What building added:**
    - **No tabs.** The policy library sits below the risk register on `/compliance/risks`, one page called "Risks and
      policies". Each policy shows:
      - its version in force and how many of the members acknowledged it;
      - the reader's own acknowledgement;
      - for the Owner and the Compliance officer, the draft: Fyniq's ready to adopt, or an unpublished revision.
    - **Editing.** The editor saves a revision, then publishes it as the next version. Publishing waits until an edit
      is saved.
    - **Plain text.** A policy's text shows as plain, pre-wrapped Markdown: the web app has no Markdown renderer.
    - **The "Acknowledge policy" card** (Basic) is on Home and in the Inbox. It lists the versions waiting for the
      reader, each with its due day, or **Overdue**, and opens the text to read and accept. It shows nothing when
      none waits.
    - **Its own guide page.** `compliance-policies.md` (Basic, every role) has a page of its own instead of a section
      of the risk page, because every member acknowledges. It is compiled into `kb/pages/task/compliance-policies.md`,
      and the risk page links to it. The help keys are `policy.acknowledge` and `policy.publish`.
    - **Component tests with axe** cover:
      - acknowledgement pending and overdue;
      - nothing waiting;
      - a member's read-only library;
      - the Compliance officer adopting Fyniq's draft and saving an edit.

## 8. Reviews and attestations (slice 8)

- [x] 8.1 `listComplianceReviews` (D6), with V on "Compliance Center" and matrix rows. It covers the review log, the
  access review, the accountant reviews, the close sign-off, break-glass post-reviews, policy acknowledgements and
  risk reviews, each with its period, due date, owner role, last sign-off, evidence and where it is signed off. The
  `attestation` checks of the registry read from it. Tests: each source due, overdue and signed, from fixtures.
  `// req: CMP-03`, `// req: FR-CTL-06`, `// req: SEC-06`, `// req: FM-3`

  **What building added:**
    - **One reading.** The reviews are the domain's `compliance.Reviews`, which reads each source's own records in one
      unit of work, under the rule its Checks entry applies (C.2 #4, #7, #18, #53 and #61; CMP-06; CMP-07). Both
      `listComplianceReviews` and `compliance.run` read them.
    - **Each review** has its kind, the role that signs it off, its cadence, its status, the period that waits, the
      earliest due date, how many items wait and how many are overdue, and the latest sign-off with the signer's name.
      It also says in a sentence what waits, the page where it is signed off, what the sign-off keeps as evidence, and
      the controls that read it.
    - **The statuses** are `overdue`, `due`, `signed`, `nothing_due` (nothing has fallen due yet) and `not_applicable`.
    - **The accountant's review** follows FM-7: each quarter, in Founder mode, overdue 60 days after the quarter ends.
      D6 said "monthly". The registry's words for it are corrected.
    - **Team mode.** The review log and the accountant's review do not apply outside Founder mode.
    - **Risk reviews.** A risk waits only once its review date has passed; until then the screen shows the next date.
    - **Policy acknowledgements** fall due per member. The rule (`AcknowledgeBy`) moves to the domain, and the policy
      library reads it from there.
    - **The attestation checks** are on:
      - CTL-FIN-07 (W): the review log and the break-glass reviews;
      - CTL-IAM-02 (Q): the access review;
      - CTL-FIN-10 (M): the accountant's review;
      - CTL-FIN-09 (E): the close's sign-off.

      An overdue review makes its check overdue, and a review that does not apply makes it not applicable; anything
      else passes. Each night `compliance.run` reads the reviews, so a review that falls overdue tells its control's
      owner once (D5). No Appendix F control covers the policy acknowledgements or the risk reviews, so no check reads
      them.
    - **Tests:**
      - `TestEachReviewReadsItsOwnSource`: each source due, overdue and signed off, plus nothing due or not
        applicable;
      - `TestAControlTakesTheWorstOfItsChecks`: the attestation rows;
      - `TestTheRecurringReviews`: the API, against a real database;
      - the matrix row.
- [x] 8.2 The screen Compliance › Reviews and attestations (Basic and Expert, `/compliance/attestations`): due,
  overdue (⚠ older than 14 days) and signed, linking to where each is signed off. Policy acknowledgements are signed
  here. The Inbox's Compliance section shows the reviews due for the viewer's roles. It has help keys and the guide
  page `compliance-reviews.md`, compiled into `kb/`. A component test with axe covers nothing due, overdue and signed
  off. `// req: CMP-03`

  **What building added:**
    - **The page** `/compliance/attestations` (Basic) lists the reviews, overdue first.
      - A banner counts the overdue reviews.
      - **Over 14 days** marks a review more than 14 days past its due date.
      - Each review shows its owner role, cadence, period, due date (the next date for risk reviews), what waits and
        its last sign-off, and links to the page where it is signed off.
      - Expert mode adds the evidence and the controls.
    - **Policy acknowledgements.** The reader's own are listed at the top of the page, to read and accept.
    - **The Inbox** has "Reviews to sign off": the due or overdue reviews that the viewer's roles sign off. Policy
      acknowledgements stay on their own card.
    - **The Compliance page** lists it.
    - **Help.** The help keys are `compliance.attestations` and `inbox.reviews`. The guide page `compliance-reviews.md`
      is compiled into `kb/pages/task/compliance-reviews.md`, and the policy page links to it.

## 9. The canvas (slice 9)

- [ ] 9.1 The fragments `ComplianceCenter.html`, `Attestations.html` and `RiskPolicies.html` are checked against the
  build, the inventory rows follow Appendix D, and the compliance canvas is republished where they differ (Recipe C).
  This waits for the canvas hold to end, on 10 Nov 2026 06:00 SGT, and for access to the canvas from the new account.
  `python3 docs/design/screens/build.py check` passes.

## 10. Threat model and as built (slice 10)

- [ ] 10.1 The threat model `docs/security/threat-models/compliance-center.md`. It covers the registry and its
  evidence, the release evidence and its workflow permissions, `schema.verify`, the `verify` checks, notices, the risk
  register, the policies and acknowledgements, and who may change what. Each threat has its control and its test.
- [ ] 10.2 `make test` and `make up` pass on the last slice, and `make check` passes. The build plan's
  p1-compliance-center paragraph says what was built.

## Workflow follow-up

- `/opsx:verify p1-compliance-center`, then `/opsx:archive p1-compliance-center --skip-specs` once every task above is
  ticked.
- `p1-pdpa-retention` adds the PDPA registers (CMP-05) to the Compliance Center, and the rows CTL-DP-05 and CTL-DP-06.
- `p1-ai-providers` builds CMP-AI-02 and CMP-AI-03, and wave 6 the AI and MCP control rows, each on this change's
  registry kinds.
