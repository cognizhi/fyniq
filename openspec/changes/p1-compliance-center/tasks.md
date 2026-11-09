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
- [ ] 1.2 The registry's check kinds `check <key>`, `verify <name>` and `attestation <kind>`, and the status `cloud`
  (design D1), in `control.schema.json` and `registry.go`. Each control also gets its frameworks, parsed from its
  mapping. `registry_test.go` fails on:
  - an unknown mapping token;
  - a `check` key not in the catalogue;
  - an undefined `verify` or `attestation`;
  - a `cloud` check that names no module test.

  `// req: CMP-01`, and the unnumbered "Control registry as code".
- [ ] 1.3 The control map (D2). Every control placed in this change names each running check, or is marked `cloud` or
  built by a later slice. The rows rev 0.240 moved keep `planned` with `arrives` naming their change.
  `go run -tags local ./cmd/jobs controls` lists each control with how it runs. The registry test lists the catalogue
  checks mapped to controls and what their warnings mean. `// req:` each CTL ID whose check is named here, in
  `registry_test.go`.

## 2. CI evidence from the running release (slice 2)

- [ ] 2.1 The `release-evidence` job in `release-please.yml` (D3):
  - it runs only when a release is created, and requires the tagged commit's CI run on main to have succeeded;
  - it attaches that run's evidence artefacts and a `controls-evidence.json` manifest to the GitHub release;
  - it holds `contents: write` for that job alone, with every action pinned by SHA.

  `make scan` passes (actionlint, zizmor). A script test builds the manifest from a fixture run.
  `// req: CMP-01`, `// req: CTL-CHG-06`, and the unnumbered "Evidence collected automatically".
- [ ] 2.2 `version.Release`, stamped only by the release image build, passed through the wiring to the API's
  dependencies (never imported by `internal/api`), with the repository's address. A resolver turns a CI check into
  passed-in-release with its asset link, or not assessed in a development build. Unit tests cover both builds.
  `// req: CMP-01`

## 3. The database and AI checks (slice 3)

- [ ] 3.1 `schema.verify` (platform job, nightly, worker; D4). It compares live grants, RLS and guard triggers with the
  embedded snapshot that `TestEachRoleHoldsExactlyItsGrants` keeps, and records its evidence. Tests:
  - a clean database passes;
  - a grant added by hand, a dropped `require_audit` trigger and an INSERT granted to `fyniq_ai` each fail, naming
    the role, table and privilege.

  `// req: CTL-FIN-02`, `// req: CTL-AUD-01`, `// req: CTL-AI-01`
- [ ] 3.2 CMP-AI-01 in CI. Each part has its own test:
  - `fyniq_ai` holds no write privilege on any table;
  - the `infra/` environment module's test shows the AI service account holds no role that writes data;
  - the AI tool registry, once it exists, holds only read-only tools; until then the test passes and says why.

  `// req: CMP-AI-01`, `// req: CTL-AI-01`
- [ ] 3.3 CTL-DP-04's archtest: no sign-in, step-up or recovery package reads an NRIC or FIN field, and no export
  password derives from one. `// req: CTL-DP-04`

## 4. Verifications per organisation, and notices (slice 4)

- [ ] 4.1 `fyniq.control_run` (insert-only, worker writes, RLS) and the `compliance.run` job (daily, after
  `checks.run`), with the four `verify` checks (D4). Tests seed a breach of each and see it fail:
  - a batch released by its preparer outside the team mode's exception;
  - a session used without a second factor;
  - an integration client without an owner or end date;
  - an export without its audit event.

  Attack tests cover the table. `// req: CTL-FIN-08`, `// req: CTL-IAM-01`, `// req: CTL-API-02`,
  `// req: CTL-AUD-05`
- [ ] 4.2 Control statuses for an organisation (`internal/domain/compliance`; D1). Every kind of check is covered:
  CI, job, `check`, `verify`, `attestation`, planned and `cloud`. The worst-of rule and not applicable are covered too.
  `compliance.run` sends `compliance.control_failed` (critical, emailed at once, to the owner role) once per failure,
  and writes `control.failed` and `control.recovered`. Tests cover:
  - a detective failure notified once;
  - a CI failure never notified;
  - a recovery recorded, not notified.

  `// req: CMP-01`, and the unnumbered "The Compliance Center shows controls and raises tasks" and "Failing controls
  block releases or page the owner".

## 5. Compliance › Controls (slice 5)

- [ ] 5.1 `listControls` (filters `framework` and `status`) and `getControl`, with V on "Compliance Center" and matrix
  rows. Each control comes with its checks, last runs, evidence links, owner and frameworks. Tests:
  - a control of each status;
  - the framework filter;
  - a Bookkeeper refused.

  `// req: CMP-01`, `// req: FR-CTL-05`
- [ ] 5.2 The screen Compliance › Controls (Expert, `/compliance/controls`):
  - the framework filter;
  - the list with statuses, as icon and label;
  - a control's checks and evidence;
  - the nightly ledger and audit-chain results.

  The Compliance page lists the module's pages, and the navigation opens it. The Inbox's Compliance section shows
  failing controls for the viewer's roles. It has the registry entries, help keys and the guide page
  `compliance-controls.md`, compiled into `kb/`. A component test with axe covers all passing, a failing control and
  not assessed. `// req: CMP-01`, `// req: FR-UX-29`

## 6. The risk register (slice 6)

- [ ] 6.1 `fyniq.risk`, `fyniq.risk_version` and `fyniq.risk_review` (D7), with attack tests, and the operations
  `listRisks`, `createRisk`, `reviseRisk` (If-Match) and `reviewRisk` on the "Compliance Center" row, with matrix
  rows. Tests:
  - a risk saved, revised and reviewed, each audited;
  - a stale version refused;
  - a Controller refused a write.

  `// req: CMP-06`
- [ ] 6.2 The seed. First-run setup loads R1–R19 from `compliance/risks.yaml` into the organisation it creates. A
  migration does the same for an installation already set up. A second organisation starts empty. Tests: setup seeds
  19 risks with owner roles and review dates, and a second organisation has none. `// req: CMP-06`
- [ ] 6.3 The Risks tab of Compliance › Risks and policies (Expert, `/compliance/risks`): the list with owner,
  rating, mitigation and review date, review overdue, and the review action. It has help keys and the guide page
  `compliance-risks-policies.md`, compiled into `kb/`. A component test with axe covers review overdue.
  `// req: CMP-06`

## 7. The policy library (slice 7)

- [ ] 7.1 The eight drafts in `compliance/policies/` (D8), each written from official sources with their URLs and
  retrieval dates, and [counsel to confirm] on each legal clause. A test checks that each draft has its front matter,
  sources and policy key, and that the eight keys are CMP-07's. `// req: CMP-07`
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
- [ ] 7.3 The Policies tab (versions, acknowledgement rate, drafting and publishing) and the member's "Acknowledge
  policy" card in the Inbox and on Home. It has help keys, and the guide page's policies section, compiled into `kb/`.
  A component test with axe covers acknowledgement pending. `// req: CMP-07`

## 8. Reviews and attestations (slice 8)

- [ ] 8.1 `listComplianceReviews` (D6), with V on "Compliance Center" and matrix rows. It covers the review log, the
  access review, the accountant reviews, the close sign-off, break-glass post-reviews, policy acknowledgements and
  risk reviews, each with its period, due date, owner role, last sign-off, evidence and where it is signed off. The
  `attestation` checks of the registry read from it. Tests: each source due, overdue and signed, from fixtures.
  `// req: CMP-03`, `// req: FR-CTL-06`, `// req: SEC-06`, `// req: FM-3`
- [ ] 8.2 The screen Compliance › Reviews and attestations (Basic and Expert, `/compliance/attestations`): due,
  overdue (⚠ older than 14 days) and signed, linking to where each is signed off. Policy acknowledgements are signed
  here. The Inbox's Compliance section shows the reviews due for the viewer's roles. It has help keys and the guide
  page `compliance-reviews.md`, compiled into `kb/`. A component test with axe covers nothing due, overdue and signed
  off. `// req: CMP-03`

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
