# Threat model: the Compliance Center (controls, evidence, verifications, reviews, risks, policies)

| | |
|---|---|
| Feature and requirement IDs | The Compliance Center: CMP-01, CMP-03, CMP-06, CMP-07, CMP-AI-01, FR-CTL-05, FR-CTL-06, the Appendix F control rows it closes (CTL-FIN-*, CTL-AUD-01/02/05/06, CTL-IAM-*, CTL-CHG-*, CTL-DP-01/02/04/07, CTL-AI-01, CTL-STAT-*), and Appendix D's rows "Compliance Center" and "Policy library" (PRD-001 rev 0.240 to rev 0.243; `p1-compliance-center` task 10.1) |
| Author, reviewer, date | Claude Code for the founders, 9 Nov 2026; second review at sign-off (waived before production, AGENTS.md) |
| Data and its class | The control registry and its evidence (internal). Each organisation's control runs and states, with what a verification found: batch, approval and member IDs (confidential). The release evidence on GitHub: test reports, scan reports, SBOMs and CI logs (internal, public once the repository is). The risk register (confidential: it names the company's weaknesses). The policy library's drafts, versions and acknowledgements (internal; who acknowledged is personal data) |
| Who acts | Appendix D: "Compliance Center" V (Owner, Administrator, Controller, Accountant, Auditor) reads; the DPO holds C/A and saves and reviews risks. "Policy library": every member reads and acknowledges their own; the Owner and the DPO draft and publish. The worker (`fyniq_worker`) runs `schema.verify` and `compliance.run` and writes runs and states. The `release-evidence` workflow attaches evidence to a GitHub release. The AI holds nothing on either row |

## 1. What we are building

- **The control registry as code (CMP-01).** One file per Appendix F control in `compliance/controls/`, each check
  named by its kind: a CI check (`make`, `test`, `ci`), a platform `job`, a catalogue `check`, a `verify`ication, an
  `attestation`, or a `cloud` check, or `planned` with the change it arrives with.
- **CI evidence from the running release (rev 0.240).** The `release-evidence` workflow waits for the tagged commit's
  CI runs and attaches their artefacts, logs and a manifest to the GitHub release. The platform knows its release only
  from `version.Release`, stamped by `make images RELEASE=vX.Y.Z`.
- **`schema.verify` (nightly).** It compares the live grants, row-level security and guard triggers with the
  snapshot embedded in the release.
- **`compliance.run` (nightly, per organisation).** It runs the verifications (payments' dual control, second factors,
  audited reads and exports), records each run, works out every control's status as the worst of its checks', and
  tells a control's owner role once when a detective check newly fails it.
- **Reviews and attestations (CMP-03).** Seven recurring reviews, each read from its own source's records; the
  attestation checks of four controls read them.
- **The risk register (CMP-06)** and **the policy library (CMP-07)**: insert-only versions, reviews, drafts,
  published versions and acknowledgements.

Trust boundaries:

- **person ↔ API**: Appendix D's two rows and SEC-02;
- **worker ↔ database**: the runs and states the worker alone writes; the catalogue it reads;
- **CI ↔ GitHub release**: the workflow's token and what it attaches;
- **build ↔ image**: who may stamp a release into a build;
- **operator ↔ database**: changes made by hand outside migrations.

## 2. What can go wrong (STRIDE per boundary)

| # | Boundary or step | Threat (STRIDE) | Impact | Likelihood |
|---|---|---|---|---|
| T1 | repository | Tampering: a control's PRD text drifts from Appendix F, or a check names a test, job, check or verification that does not exist, so a control looks covered when nothing runs | A control reported as passing that nothing proves | Medium |
| T2 | CI ↔ GitHub release | Spoofing and tampering: evidence attached from a failed or cancelled run, from another commit, or forged; the workflow's token used for more than attaching | Evidence that proves nothing; a token that can push code | Low |
| T3 | build ↔ image | Spoofing: a development build, or an image someone stamped, claims a release and shows its CI checks as passed | Controls shown as passing with no evidence behind them | Low |
| T4 | operator ↔ database | Tampering and elevation of privilege: a grant, a row-level security policy or a guard trigger changed by hand; the AI's role given a write | The database stops being the backstop (INV-01 to INV-13), unnoticed | Low |
| T5 | worker ↔ database | Tampering and repudiation: a control run or state rewritten or deleted to hide a failure, or a status changed without its audit event | A failure hidden from the dashboard and the trail | Low |
| T6 | worker ↔ database | Tampering (false negative): a verification misses a breach: a batch released by its preparer, a session without a second factor, an export without its event | A broken control reported as passing | Medium |
| T7 | worker | Denial of service and repudiation: `compliance.run` or `schema.verify` stops running, and the last good result keeps showing | A stale "pass" | Medium |
| T8 | worker ↔ email | Information disclosure and denial of service: a notice goes to the wrong people, or a failing control notifies every night | Weaknesses told to people who should not know; real alerts ignored | Low |
| T9 | person ↔ API | Information disclosure: the dashboard, the runs' findings, the reviews or the risk register reach a role without V, the AI or an MCP client | The company's weaknesses and its members' second-factor status leak | Low |
| T10 | person ↔ API | Tampering and repudiation: a risk's history rewritten, a risk deleted, a review backdated, or a stale version saved over a newer one | The register no longer shows what was known when | Low |
| T11 | person ↔ API | Tampering and repudiation: a published policy changed after members accepted it, or an acknowledgement given for someone else, by a token or by the AI | Members bound to text they never saw; acknowledgements nobody gave | Low |
| T12 | person ↔ API | Elevation of privilege: a member, an integration or the AI drafts or publishes a policy, or saves a risk | The company's rules written by someone not entitled to | Low |
| T13 | repository | Tampering (wrong content): a clause of Fyniq's starting drafts misstates the law (PDPA breach timers, the DPO, NRIC) and is adopted without review | The company follows a wrong rule | Medium |
| T14 | person ↔ API | Repudiation: a review shown as signed off that nobody signed, or an overdue one shown as on time | A missed access review or close goes unseen | Low |
| T15 | API ↔ database | Information disclosure: the operator's own risks (R1–R22) are loaded into a customer's organisation | The operator's weaknesses disclosed to a customer | Low |

## 3. What we do about it

| Threat | Control | Test that proves it (`// req:`) |
|---|---|---|
| T1 | `make controls` keeps each file's PRD fields equal to Appendix F (`scripts/controls.py sync`). The registry test refuses a running check whose make target, Go test, CI job, platform job, catalogue key, verification or attestation does not exist, a cloud check that names no module test, and a planned check with no change of the plan; the checks must cover the PRD's frequencies in order | `TestTheControlRegistry`, `TestFrequencies`, `TestTheCatalogueChecksThatProveControls`, `TestMappingFrameworks` (CMP-01) |
| T2 | The workflow has `permissions: {}` at the top; its one job holds `contents: write` and `actions: read` only; every action is pinned by commit, and checkout keeps no credentials. `release-evidence.py` refuses unless the tagged commit's `ci.yml` and `controls.yml` runs both succeeded, and refuses a missing run or artefact. `make scan` runs actionlint and zizmor on it | `scripts/release-evidence.py self-test` (in `make controls`: a failed, cancelled or missing run and a missing artefact refused), `TestEveryCICheckHasItsReleaseAsset` (CTL-CHG-06, CTL-CHG-11) |
| T3 | Only a `vX.Y.Z` tag with the repository's address is a release; anything else is a development build, whose CI checks are not assessed and say why. The release is stamped only by `make images RELEASE=…`; `internal/api` never imports `internal/version` | `TestCIChecksReadTheReleaseThePlatformRuns`, `TestAControlTakesTheWorstOfItsChecks` (a development build) (CMP-01) |
| T4 | `schema.verify` reads the live catalogue as the worker, in a read-only transaction, and compares it with the release's embedded goldens: each grant, policy, RLS flag and guard trigger, by role, table and privilege. A difference fails the job, whose control is a detective check | `TestSchemaVerifyFindsWhatWasChangedByHand`, `TestSchemaVerifyNamesEachDifference`, `TestTheAIRoleHoldsNoWritePrivilege`, `TestEachRoleHoldsExactlyItsGrants` (CTL-FIN-02, CTL-AUD-01, CTL-AI-01, CMP-AI-01) |
| T5 | `control_run` is insert-only and the worker's alone (FY307); `control_state` is written at its next version only, never deleted (FY308), and a change of status must carry its audit event in the same transaction | `TestAttackControlRunsAreTheWorkersRecord`, `TestAttackControlStatesAreTheWorkersRecord` (AUD-01, AUD-08) |
| T6 | Each verification's rule is Go's, over facts the store reads; a seeded breach of each must fail it. `compliance.AuditedReads` must equal the API's audited reads | `TestEachVerificationFindsItsBreach`, `TestWhatBreaksDualControl`, `TestWhatBreaksTheSecondFactor`, `TestEveryRunningVerificationIsBuilt`, `TestEveryAuditedReadIsReconciled` (CTL-FIN-08, CTL-IAM-01, CTL-AUD-05) |
| T7 | A verification older than 26 hours reads as overdue, and a job that never ran or ran late reads as overdue through `JobOutcomeOf`. C.2 #12 watches every scheduled job | `TestAControlTakesTheWorstOfItsChecks` (a stale verification, a job never run), `TestThePlatformChecks` (C.2 #12) |
| T8 | The notice goes only to the members holding the control's owner role, and only when a detective check newly fails it: a control still failing, a recovery and a CI check tell nobody | `TestAFailingControlIsToldToItsOwnerRole`, `TestWhenAFailingControlNotifies`, `TestAFailingControlIsToldOnceAndItsRecoveryRecorded` (CMP-01) |
| T9 | Every Compliance Center operation needs V on its row; the AI holds nothing on it and no MCP tool reads it; the Inbox asks only for roles that may read | `TestTheControlDashboard`, `TestTheRecurringReviews`, `TestTheRiskRegister` (a Bookkeeper refused), `TestAuthorisationMatrix` |
| T10 | A risk is versions and reviews, insert-only, and a risk row never changes (FY309); each write is audited on the risk in its transaction; saving needs If-Match on the version; only the DPO creates (C) and reviews (A) | `TestAttackTheRiskRegisterKeepsItsHistory`, `TestTheRiskRegister` (a stale version and a Controller refused) (CMP-06) |
| T11 | Drafts, versions and acknowledgements are insert-only (FY310), each row's SHA-256 checked against its text by the database; a version never changes. An acknowledgement is the signed-in member's own, of the current version, once; a token gets SESSION_REQUIRED and the AI is refused | `TestAttackThePolicyLibraryKeepsItsHistory`, `TestThePolicyLibrary` (CMP-07) |
| T12 | "Policy library" gives C/A to the Owner and the DPO only; the draft's text reaches only them; "Compliance Center" gives C/A to the DPO only; the AI holds nothing on either | `TestThePolicyLibrary`, `TestTheRiskRegister`, `TestAuthorisationMatrix`, `TestTheGrantsAreAppendixD` |
| T13 | Each draft lists its official sources with their URLs and retrieval dates and marks every clause bearing on the law [counsel to confirm]; the editor says to have counsel confirm them; nothing is in force until a person publishes it | `TestThePolicyDrafts` (CMP-07) |
| T14 | No separate attestation record exists to forge: each review is read from its source's own records (the review log, the access review's attestations, the accountant's reviews, the closes, the break-glasses, the acknowledgements, the risks), each insert-only and guarded at its source, under the rule its Checks entry applies | `TestEachReviewReadsItsOwnSource`, `TestTheRecurringReviews`, `TestAControlTakesTheWorstOfItsChecks` (attestations) (CMP-03) |
| T15 | The seed runs only in first-run setup, in the organisation setup creates, in the same transaction; every other organisation starts empty | `TestFirstRunSetup`, `TestTheOperatorsStartingRisks` (CMP-06) |

## 4. What remains

- **[live-verify] The release evidence's publish path.** The runs API, the artefact downloads, the logs archive and
  the upload run for the first time on the first release.
- **[live-verify] `schema.verify` in the dev cloud.** Cloud SQL's own roles may hold grants the goldens do not list;
  the first run will say.
- **Self-stamped images.** Anyone who builds an image can stamp a release into it. Only production's admission of
  signed images (CTL-CHG-04, moved to `p1-security-hardening`) makes the release trustworthy there.
- **Counsel has not reviewed the policy drafts.** The [counsel to confirm] clauses stand until a lawyer confirms them.
- **No Appendix F control covers the policy acknowledgements or the risk reviews.** They show on Reviews and
  attestations, but no control's status reads them.
- **A day's lag.** A status is what `compliance.run` last saved; a failure between runs shows the next night, though
  the catalogue checks behind most controls run on their own triggers.
- **Earlier installations** do not get the operator's risks: the migration for them is not built (no installation
  exists yet outside a developer's database).
