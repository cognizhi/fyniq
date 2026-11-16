# Threat model: scheduled agents

| | |
|---|---|
| Feature and requirement IDs | The scheduled agents and the company secretary assistant, their first agent: skills pinned per release, schedules and event triggers, the run's identity and grant, deterministic findings first, compliance tasks, the model step through the API, the two `sec.*` task types, the workspace and its runs, the agent evals and the controls: AI-61 to AI-67, the "AI company secretary assistant" and "Recommended default schedule" rows, `sec.create_task`, `sec.draft_reminder`, QA-AI-07, CTL-AI-09, C.2 #36 (PRD-001 rev 0.274, 0.280 and 0.281; `p1-scheduled-agents`); and, with `p1-company-secretary` (rev 0.282, 0.283), the corporate profile's papers and minute book and the third task type, `sec.draft_document` (FR-CS-05) |
| Author, reviewer, date | Claude Code for the owner, 16 Nov 2026, for security review #4; second review waived before production (AGENTS.md), held with `needs-human-review` if wanted |
| Data and its class | Runs and their reports: findings, Fyniq's summary, the model's words, the ledger revision and packs read (confidential: business records, which may name officers); compliance tasks (confidential); the run's delegated token (secret); the worker's service token for the API (secret); proposals and reminder emails (confidential; an email names a member) |
| Who acts | The Owner who turned the agent on, whom its runs act for; a second approver, who confirms turning it on; the worker, which queues, starts and ends runs and raises their tasks; the API, which issues the run's token and asks the AI service; the AI service and the model, which read and propose with the run's token; the Owner and the Company secretary, who approve its proposals; the executor, which runs them; the Owner and the Administrator, who close tasks and ask for Run now; MCP and integration clients (none of these operations) |

## 1. What we are building

**An agent is its skill, pinned** (AI-61, CTL-AI-09, design D1). `agents/<name>/SKILL.md` names its version, its read
tools, its outputs and its permission profile, and its text is its instructions. `agents/agents.lock.json` pins each
skill's version and hash, so a change is a new version CI checks. A skill naming a tool beyond its profile, an output
beyond its report and catalogue task types, or anything on §9.6's never-list is refused as it loads.

**Off until two people turn it on** (AI-62, AI-63, design D2). The agent's settings are organisation settings. Turning it
on waits for a second approver; turning it off or pausing it takes effect at once. Its schedule, events, quiet hours,
recipients, escalation and caps are dual-controlled settings too.

**Runs from the database, at their time** (AI-62, ADR-T7, design D4). The tick's `agents.schedule` queues each due slot
once (a slot is unique in the database) and enqueues it at its time. An event (a period closed, shares changed, a
filing recorded) queues its run in the change's own transaction. Run now queues one for the person who asked. The
watchdog (C.2 #36) shows a slot late by 30 minutes, or a run failed or skipped, as ⚠ on Checks.

**Deterministic first** (AI-64, AI-65, design D5). The worker runs a run:
- it skips it with why when the agent is off or paused, the AI is off, its skill changed, or the person it acts for
  is no longer an Owner;
- it makes the findings from the statutory calendar and the checks engine, and lists what it cannot assess;
- it keeps one open task per finding, raised and cleared through the system principal;
- it writes the report once, never "all clear" with anything open;
- it asks the model only when an input changed, through one internal API operation.

**The model step through the API** (rev 0.281, design D5 step 5, D6). The worker calls `runAgentModel` with its own
service token, which opens that operation alone. The API checks the run again, then issues the run's delegated token:
- it reads as the Owner the run acts for, with the AI exclusions and masking, for at most 30 minutes;
- it holds the skill's tools and the task types it may propose, and the API refuses any other;
- its proposals are requested for that Owner as `ai_run`, which the database allows for a run's proposal alone.
The API asks the AI service's `/runs` with the skill and the findings as data, screens the model's words, and ends
the token. The agent's rejected proposals reach its next run as untrusted text.

**Two task types** (Appendix E, design D7, D8). `sec.create_task` makes a task for a finding of the model's own, never
a second for one with a task open or proposed. `sec.draft_reminder` emails the members of a role, once, as the
approving person, only after approval. The Owner or the Company secretary approves either: Appendix D's note gives the
Company secretary A on `sec.*` proposals alone.

**A third, `sec.draft_document`** (`p1-company-secretary` design D6). The model names a kind of corporate document and
the fields its template asks for. The dry run fills Fyniq's template from the records and drafts nothing; executed as
the approving person, the executor drafts the paper, naming the proposal. Nothing is signed, filed or recorded: a
person has the paper signed outside Fyniq, attaches the signed copy and asks another person to record it in the
minute book. Papers name directors, members and people present (the data inventory's officers class). The executor
reads the register of members to name a members' resolution's signers.

Trust boundaries: model ↔ AI service (the words and calls the model makes); AI service ↔ API (the internal operations,
two tokens); worker ↔ API (the model step, the worker's token); API and worker ↔ database (guards, grants, RLS);
executor ↔ email outbox (the reminder); the person's browser ↔ API (the workspace, Run now, the tasks).

## 2. What can go wrong (STRIDE per boundary)

| # | Boundary or step | Threat (STRIDE) | Impact | Likelihood |
|---|---|---|---|---|
| T1 | Records → model | Tampering / elevation: an injection in a record (a contact's name, a memo, a document) tells the run's model to call something filed, say all clear, or propose a change | A false all clear, or a change the Owner never asked for waits for approval | High |
| T2 | Skill | Tampering / elevation: a skill changed to name a write tool, a never-listed task type or more tools, without review | The agent gains reach it must never have | Low |
| T3 | Run's token | Elevation: the run's token reads beyond the Owner's rights or the skill's tools, outlives the run, or proposes a type its skill does not output | Data or writes beyond the agent's bounds | Medium |
| T4 | Worker ↔ API | Spoofing / elevation: another caller uses the model step, or the worker's token opens other internal operations | A model step for someone else's run, or the worker reading as a person | Low |
| T5 | Findings and report | Repudiation / tampering: a report rewritten after the fact, a finding hidden, or "all clear" said while something is due | Directors misled about a deadline | Medium |
| T6 | Schedule and enablement | Elevation: one person turns the agent on, widens its schedule or caps alone; a run acts for someone no longer an Owner | Runs on someone's authority without the second approver | Low |
| T7 | Proposals from a run | Spoofing: a run's proposal requested as a turn, a turn's as a run, for someone the run does not act for, or after the run ended | An approval request on the wrong authority | Low |
| T8 | Reminder email | Information disclosure / spoofing: a reminder sent before approval, twice, to someone outside the organisation, or by a role other than the executor | Mail the company never approved | Low |
| T9 | Approval by the Company secretary | Elevation: the Company secretary's approval reaching beyond `sec.*` proposals | Approvals by a role that holds no A elsewhere | Low |
| T10 | Cost and availability | Denial of service: a run looping on tool calls, spending tokens, or runs failing unseen | Spend, and deadlines unwatched | Medium |
| T11 | Papers from a proposal | Tampering / elevation: a drafted paper taken as signed or filed, a text drafted that differs from the one approved, a signed paper's text changed after, or the executor's new read reaching beyond the register | Directors sign, or the minute book keeps, what nobody approved | Medium |
| T12 | Papers and personal data | Information disclosure: a paper carrying a full NRIC, the DPO reading a paper's names, or a PDF downloaded unseen | Personal data where it should not be | Low |

## 3. What we do about it

| Threat | Control | Test that proves it |
|---|---|---|
| T1 | The findings are Fyniq's own and the model never changes them; they reach the model as data. Fyniq's summary is written by code and never says all clear while anything is open. The model's words are grounded, screened and labelled AI. A proposal still needs a person's approval, of a type the skill outputs. The red-team case of the launch suite plants an injection in a contact's name | `TestTheAgentSuites` (`injected-memo`), `TestARunsStepStaysWithinItsSkill`, `TestASummaryIsNeverAnAllClearWithAnythingOpen`, `TestTheWorkerAsksTheModelForARun` |
| T2 | Skills load strictly, within their profile, with the never-list in code; a change without a new version fails CI (`agents.lock.json`, CTL-AI-09) | `TestTheReleasesSkillsLoad`, `TestASkillBeyondItsBoundsIsRefused`, `TestEveryChangeToASkillIsANewVersion` |
| T3 | The run's grant holds its tools and task types, is one per run, lives at most 30 minutes and ends with the step; `callAITool` and `proposeAITask` refuse anything else; reads are the Owner's, through the public read operations, with the exclusions. The nightly `verify ai.agent-runs` checks each run's tools and proposals against its pinned skill (CTL-AI-09) | `TestAttackARunsGrantIsItsRunsAlone`, `TestARunReadsAndProposesWithinItsSkill`, `TestEveryAgentRunStaysInsideItsSkill` |
| T4 | `runAgentModel` takes only the `agent_runs` service token, and that token opens no other internal operation; the AI service's token does not open it; a run of another organisation is not found. In the cloud it is the worker's own account, as `invokers.agent_runs` names it `[live-verify]` | `TestTheWorkerAsksTheModelForARun` |
| T5 | `agent_run`'s guard: forward only, each step by the worker, its report and its tools written once as it ends, nothing changed after, never deleted; every step audited. One open task per finding, unique in the database | `TestAgentRunsRunForwardOnce`, `TestAttackAgentRunsKeepTheirCourse`, `TestAFindingHasOneOpenTask`, `TestAttackComplianceTasksKeepTheirCourse` |
| T6 | Enablement and every setting but pausing wait for a second approver; a run acts for the Owner who asked for the approved change, and is skipped once they are no longer an Owner | `TestAnAgentsSlotsAreQueuedOnceAtTheirTime`, `TestARunChecksFirstAndKeepsOneTaskPerFinding`, `TestTheCompanySecretaryWorkspace` |
| T7 | The proposal guard requires a run's proposal to come from a running run for the person it acts for; `approval_request_ai_requester` ties `ai_run` and `ai_turn` each to its own origin, for `ai_proposal` requests alone | `TestAttackARunsProposalIsAskedByItsRun` |
| T8 | The dry run renders and sends nothing; the executor queues the email once per recipient, only after approval, to members with a sign-in email; only the executor queues the purpose `secretary_reminder`, and it queues no other | `TestTheAIDraftsAReminderSentOnceApproved`, `TestAttackOnlyAnApprovedProposalSendsAReminder` |
| T9 | The Company secretary reaches only the three operations that sign a request, and `proposal.Approves` lets them sign `sec.*` proposals alone; the matrix expects it from Appendix D's note | `TestTheSecretaryApprovesTheSecProposalsAlone`, `TestTheAIProposesATaskTheSecretaryApproves`, `TestAuthorisationMatrix` |
| T10 | The run's reads (20) and tokens (200,000) are caps the AI service stops at; the agent's monthly AI budget meters its model calls; a failed, skipped or late run is ⚠ on Checks (C.2 #36); (rev 0.283) a run whose AI step cannot run still ends with its findings and tasks, its report saying why, and is ⚠ too; the deterministic deadline monitor keeps running whatever the agent's state | `TestARunsStepStaysWithinItsSkill`, `TestTheTickWatchesTheAgentsRuns`, `TestARunKeepsItsFindingsWithoutItsAIStep` |
| T11 | The task type and command hold no sign, file, lodge or record word (§9.6); every paper says it is Fyniq's draft, never signed or filed by Fyniq. The text the person approved is the proposal's base: when the records change it, the proposal is stale and drafts nothing (AI-P5). Once a signed copy is attached, the text stays; recording it in the minute book needs another person's approval, and a recorded paper never changes (EQUITY-05). The executor gains SELECT on `holding_movement` alone, which `grants.golden` pins | `TestTheAIDraftsACorporateDocument`, `TestTheMinuteBook`, `TestEachRoleHoldsExactlyItsGrants`, `TestEveryChangeToATaskTypeIsANewVersion` |
| T12 | A paper's fields and text refuse a full NRIC or FIN, and so does the database's guard (FY356); the DPO sees what each paper is, not its text, and its PDF masked; each PDF download is a security event in the audit trail (AUD-05) | `TestADocumentThatDoesNotFit`, `TestAttackCorporateDocumentsKeepTheirRules`, `TestTheCorporateDocuments`, `TestEveryFileAndRevealIsAudited`, `TestAuthorisationMatrix` |

SOD: the run proposes for the Owner, who may sign their own tier-1 proposal (AI-89 rev 0.281); every other change of
the agent's settings needs a second person. RLS: `agent_run` and `compliance_task` have `organisation_id` and
`FORCE ROW LEVEL SECURITY`.

The agent evals (QA-AI-07) judge each case by its findings, no all clear, its tasks and exactly its proposals; the
harness checks itself with a missing finding, an "all clear" and a repeated injection (`TestTheAgentSuites`).

## 4. What remains

- **Recorded models.** No model's runs are recorded for the agent suites yet; the release gate (`FYNIQ_EVALS_RELEASE=1`)
  fails until they are, on the synthetic company only `[live-verify]`.
- **The cloud path.** The worker's call to the API (`invokers.agent_runs`, the `api` entry of `FYNIQ_GCP`) and Model
  Armor's screening of the model's words run in the dev cloud `[live-verify]`.
- **The secretary's own checks.** The corporate profile, the ACRA pack's cross-checks and `sec.draft_document` arrived
  with `p1-company-secretary` (the launch suite's G4-102 and G4-110 cases; `sec-draft-document`'s proposal cases).
- **The papers' wording.** Fyniq's templates await a lawyer (the review register's `papers` area); the notice period,
  the minute book's month and the written resolutions' majorities wait for `sg-acra@2026.2` (rev 0.283).
- **An injection the person's rights allow.** A turned model can still propose a task or a reminder within the skill;
  the Owner or the Company secretary reads Fyniq's summary and decides.
