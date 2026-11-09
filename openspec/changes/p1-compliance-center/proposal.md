# Proposal

## Why

P0 put the control registry in the repository (`compliance/controls/`, 67 Appendix F controls). The nightly
`compliance.evidence` job records the evidence of the controls a platform job checks. Since then P1 has built most of
what the controls guard:
- the checks engine and its catalogue;
- the access review, the review log and the accountant reviews;
- the close;
- the statutory calendar;
- the founders' and equity registers;
- the audit explorer.

The registry has not kept up. It marks 74 of its 99 checks "planned", many of which now run as tests, jobs or checks.
Nobody in the app can see any of it. There is no control dashboard, no list of the reviews that fall due, no risk
register and no policy library. Nothing raises a task or a notice when a control fails.

This change builds PRD-001 §14.1 and §14.7 for launch:
- **CMP-01:** the control dashboard.
- **CMP-03:** the reviews and attestations.
- **CMP-06:** the risk register.
- **CMP-07:** the policy library.
- **CMP-AI-01:** the AI write-grant control test.
- **The Appendix F rows whose modules are built**, each closed when its check runs.

It comes before `p1-pdpa-retention`, which adds the PDPA registers (CMP-05) to the same Compliance Center.

## What Changes

- **The owner's decisions of 9 Nov 2026, taken interactively (PRD-001 rev 0.240):**
  - **CI-checked controls show the running release (CMP-01, §14.1).** A release workflow job attaches the evidence of
    the checks that ran in CI to the GitHub release: the test, scan and image reports, and the SBOM. No release ships
    with a failing gate, so the dashboard shows those checks as passed in the release the platform runs, with links to
    that evidence. A development build shows them as not assessed. No release has been cut yet (`VERSION` is 0.0.0).
    The alternatives were:
    - CI writing into the evidence bucket, which needs the cloud first;
    - "not assessed" until the cloud.
  - **R1–R19 belong to the installation's operator (CMP-06).** They are loaded only into the organisation that
    first-run setup creates. Each has an owner role and a quarterly review date. Every other organisation starts with
    an empty register. The alternatives were seeding every organisation, or an import on request.
  - **Policies are drafted for adoption (CMP-07).** The eight policies start as drafts written from official sources
    (CSA's Cyber Essentials and Cyber Trust, the PDPC's guides, the IMDA frameworks), tagged [counsel to confirm] where
    the law bears on them.
    - The Owner or the DPO publishes a version, under a new Appendix D row, **Policy library**.
    - Every member acknowledges each published version, within 30 days.
    - The alternatives were an empty library for uploads, and both.
  - **AI governance comes with the AI.** CMP-AI-01's grant test is built now. CMP-AI-02 (the AI inventory and DPIA) and
    CMP-AI-03 (eval-gated, dual-approved AI changes) move to `p1-ai-providers`, where models and prompts first exist.
    The alternatives were all three now with empty states, and all three in wave 6.
- **Each control row is built with the module it guards** (the build plan's rule). The rows whose checks need a later
  module move to it:
  - the AI and MCP rows (CTL-AI-02 to CTL-AI-09, CTL-MCP-01 to CTL-MCP-04) to wave 6;
  - CTL-DP-05 and CTL-DP-06 to `p1-pdpa-retention`;
  - CTL-DP-03, CTL-CHG-07 and CTL-RES-04 to `p1-security-hardening`;
  - CTL-RES-02 to `p1-operations-dr`.

  Building the control map found six more rows whose cloud configuration `infra/` does not hold yet (rev 0.241):
  - CTL-AUD-03, CTL-AUD-04, CTL-RES-03 and CTL-RES-05 to `p1-operations-dr`;
  - CTL-CHG-04 and CTL-CHG-05 to `p1-security-hardening`.

  Building the verifications found that Fyniq registers no integration client yet (rev 0.242): CTL-API-02 moves to
  `p1-mcp-first-party`, whose Connected apps first registers them.

  The 42 control rows left are closed here.
- **The registry names every check that runs.** A check may now be:
  - a check of the organisation's catalogue (Appendix C), such as `ledger.sub-ledgers` for CTL-FIN-04;
  - an attestation;
  - the release's CI evidence;
  - as before, a make target, a test, a CI job or a platform job.

  The checks that already run are named. The missing ones are built:
  - a nightly check of the database's grants and guards (CTL-FIN-02, CTL-AUD-01, CTL-AI-01);
  - the nightly reconciliation of exports and sensitive reads with the trail (CTL-AUD-05);
  - payments' dual control (CTL-FIN-08);
  - MFA for every member (CTL-IAM-01);
  - integration clients' owner, class and expiry (CTL-API-02);
  - NRIC never used to authenticate (CTL-DP-04).

  Checks that only the cloud can run show as not assessed and are tagged `[live-verify]`.
- **The control dashboard (CMP-01).** Each control shows:
  - its status: pass, fail, overdue, not applicable, or not assessed, naming what is missing;
  - each check's last run and evidence;
  - its owner and its frameworks.

  Filters cover Singapore statute, PDPA, ISO 27001, SOC 2, CSA Cyber Essentials and Cyber Trust, IMDA agentic AI,
  OWASP and COSO. The ledger and audit-chain verifications (FR-CTL-05) are shown with them.
- **Failing controls raise a task and a notice (§14.1).** A failing control or an overdue review becomes an Inbox
  item for its owner's role. A detective control that newly fails also sends a critical notice, emailed at once. CI
  gates already block a release.
- **Reviews and attestations (CMP-03).** One list shows every recurring review with its due date, its owner, the last
  sign-off and its evidence:
  - the weekly review log (Founder mode);
  - the quarterly access review;
  - the accountant reviews (journal review);
  - the monthly close sign-off;
  - break-glass post-reviews;
  - policy acknowledgements;
  - risk reviews.

  Each review is still signed off where it lives. Policy acknowledgements are new, and signed off here.
- **The risk register (CMP-06)** holds risks with an owner role, likelihood, impact, mitigation and review date, each
  saved as a version. A review records who reviewed and when.
- **The policy library (CMP-07)** holds the eight policies, with their drafts, published versions and every member's
  acknowledgement of each version.
- **Screens:** Compliance › Controls (Expert), Reviews and attestations (Basic and Expert), and Risks and policies
  (Expert, with a policy card for every member). They are designed on the compliance canvas after the design hold ends
  on 10 Nov 2026 06:00 SGT. Their help keys and guide pages are compiled into the knowledge base.

## Capabilities

### New Capabilities

None. The requirements are approved in the main specs:
- `compliance/compliance-center`: CMP-01, CMP-03, CMP-06, CMP-07 and its unnumbered §14.1 rows;
- `compliance/ai-governance`: CMP-AI-01;
- `compliance/financial-controls`, `compliance/technology-controls` and `compliance/ai-and-api-controls`: the
  Appendix F rows placed here.

This change sets `skip_specs: true`.

### Modified Capabilities

None in this change. Rev 0.240's decisions are already in the main specs and stamped:
- `compliance/compliance-center`: CMP-01, CMP-06, CMP-07 and the CI evidence;
- `compliance/ai-governance`: CMP-AI-02 and CMP-AI-03 built with `p1-ai-providers`;
- `access/roles-and-segregation`: the Policy library row.

If building shows a requirement is wrong, the PRD changes first (fyniq-openspec Recipe B).

## Impact

- **Registry:**
  - `compliance/`: the check kinds `check`, `attestation` and `release`;
  - the controls' frameworks parsed from their mappings, with every token recognised;
  - every control's checks named or handed on;
  - `cmd/jobs controls` (the dry run) updated.
- **Domain:**
  - `internal/domain/compliance` (new): a control's status from its checks, the reviews due, risks and policies;
  - the new checks above, as platform jobs or catalogue checks.
- **Database:**
  - `fyniq.risk` with its versions;
  - `fyniq.policy` and `fyniq.policy_version`, with the drafts' text and hash;
  - `fyniq.policy_acknowledgement`, insert-only and one per member per version.

  All are under RLS with attack tests.
- **API:**
  - `GET /v1/compliance/controls` and `GET /v1/compliance/controls/{control_id}`;
  - `GET /v1/compliance/reviews`;
  - the risks: list, create, revise, review;
  - the policies: list, get, publish a version, acknowledge.

  Matrix rows cover Appendix D's "Compliance Center" and the new "Policy library".
- **Setup:** first-run setup loads R1–R19 into the organisation it creates. A migration does the same for an
  installation already set up.
- **CI:** `release-please.yml` gains a job that attaches the main CI run's evidence and an SBOM to the release.
  `infra/` gains a test that the AI service account holds no write role.
- **Notifications:** `compliance.control_failed`, critical, to the control owner's role.
- **Web:** three screens, the Inbox's compliance section, and the Compliance navigation.
- **Docs:**
  - the eight draft policies in `compliance/policies/`, from official sources;
  - guide pages compiled into `kb/`;
  - a threat model;
  - the build plan's wave-5 paragraph.
- **No AI or MCP surface.** The AI holds nothing on either row, and no tool reads the registry, risks or policies.
