## Context

The assistant answers a person's turn through the AI service (`cmd/ai`), which holds no database code and reaches the
platform only through the API's internal operations (`p1-ai-assistant`, rev 0.277). The AI changes anything only by a
proposal a person approves, run by the executor (`p1-ai-proposals-core`). Background work goes through the outbox to
the worker, and the tick (`internal/outbox/tick.go`) runs the platform's jobs on fixed intervals (ADR-T7). The
statutory calendar knows every deadline and its windows (FR-TAX-07, CMP-02), and the checks engine every C.2 check.
`agents/company-secretary/SKILL.md` exists as a draft. Nothing yet runs an agent, keeps a run or holds a task.

The owner decided on 15 Nov 2026 (rev 0.281):
1. A run's model step goes through one internal API operation, called by the worker with its own service token. The
   API issues the run's token and asks the AI service, which stays reached only through the API.
2. A run's proposals are requested as a turn's, for the Owner the run acts for, authenticated by the run's grant
   (`ai_run`). That Owner may sign the run's tier-1 proposals.

## Goals / Non-Goals

**Goals:**
- The framework every scheduled agent runs on: skills, schedules and triggers, identity, runs, reports, tasks,
  proposals, budgets, the watchdog, evals and controls.
- The company secretary's assistant as its first agent, with the deterministic checks the release has today: the
  statutory calendar and the checks engine.
- `sec.create_task` and `sec.draft_reminder` built end to end.

**Non-Goals:**
- The corporate profile, the ACRA pack, the FR-CS-03 cross-checks, `sec.draft_document` and the Company secretary
  role's own rights: `p1-company-secretary`, on this framework. Until then their checklist items report Not assessed,
  naming the input they wait for.
- Knowledge-base reads (`kb.*`) and their snapshot: `p1-knowledge-base`. A run records "no knowledge base" as its
  snapshot.
- Slack (P2). People assistants for Nemura (PRD-002).

## Decisions

**D1. Skills are data, pinned per release (AI-61, CTL-AI-09).**
- `internal/agents` loads `agents/<name>/SKILL.md`. The front matter (`agent-skill@1`) names the agent, its version,
  its tools, its outputs and its permission profile; the text below it is the agent's instructions.
- `agents/agents.lock.json` pins each skill's version and SHA-256. CI fails on a changed skill without a new version,
  or on a version going back, as for the task catalogue.
- A skill is refused as it loads when:
  - an output is not `run_report` or a catalogue task type;
  - a tool is not a read tool's name;
  - an output's task type is on the never-list.
- The release's agents are a fixed list in code (the company secretary for now). Nemura's `people-assistant` is not
  Fyniq's.
- A run offers the model the skill's tools that the release's read-tool registry has, and nothing else: the `kb.*`
  tools are not offered until `p1-knowledge-base`. The outputs it offers are those whose task types are built and on
  for the organisation.
- The skill's text is the run's system prompt, recorded with its version and hash. The agent's route is its own:
  `company_secretary` is a new row of `ai_agent`, routed and budgeted like the others (AI-74).

**D2. Configuration is settings (AI-62, AI-63, FR-ORG-08).**
- Each agent's configuration is organisation settings under `agents.<agent>.*`, with recommended defaults, effective
  dating and audit.
- The switch `agents.company_secretary.enabled` is off by default. Turning it on needs a second approver (FM-1), and
  turning it off takes effect at once. The Owner who turned it on is the one runs act for. If they stop being an
  Owner, runs stop and the agent shows "needs re-enabling".
- `.paused` is the Owner's instant pause (AI-13): on at once.
- The schedule entries are a new setting kind, `cron`: five fields, in Singapore time, validated:
  - `.monitor` `0 7 * * *` (model only if something changed);
  - `.cross_check` `0 8 * * 1` (model);
  - `.after_close` (switch, on; model);
  - `.fy_end_pack` (switch, on; model).
- `.events` is a list of the event triggers that are on: period closed, shares issued or transferred, filing
  recorded, deadline windows. Officer, address and controller changes join with `p1-company-secretary`.
- `.quiet_hours` is a new kind, `time_range`, recommended `22:00-07:00`. During quiet hours a run still runs and
  records; emails wait until the hours end.
- `.recipients` (roles, recommended `owner,company_secretary`) and `.channels` (`inbox,email`).
- `.escalate_days` (7) and `.tool_calls` (a count, recommended 20, at most the API's limit).
- Token budgets per run and per month are the agent's AI budgets (`p1-ai-providers`); the per-run cap is a setting
  `.run_tokens`.

**D3. Runs are kept, append-only (AI-65, AI-67).**
- `agent_run` (FORCE RLS) holds:
  - the agent, its skill version and hash, and the trigger (`schedule:<entry>`, `event:<kind>:<key>`, `run_now`);
  - the slot it was due at, and its status: queued → running → done | failed | skipped (with why);
  - the inputs' fingerprint, the ledger revision, the pack versions and the knowledge-base snapshot;
  - the findings, whether the model ran, tokens, tool calls, the model's words and the next step.
- A guard lets it move only forward, each step by its own role (the worker queues and runs, the API records the model
  step). Nothing changes once done, failed or skipped.
- `(organisation, agent, trigger, slot)` is unique, so a slot runs once however often it is enqueued.

**D4. Schedules live in the database, and the tick is the watchdog (AI-62, AI-67, ADR-T7).**
- A platform job, `agents.schedule`, runs every 5 minutes. For each organisation whose agent is on and not paused, it
  works out the slots due before the next tick from the cron settings, in Singapore time. It queues each slot's run
  and enqueues it to run at its slot.
- The outbox gains an optional time to run at, so a run starts at its exact time rather than at the tick (the Cloud
  Tasks adapter's schedule time; the local queue holds it until then).
- A finished run enqueues its own next slot.
- Event triggers come from what the domain already emits: a period closed, a share issue or transfer, a calendar
  filing recorded. The daily monitor covers deadline windows, since the business date is one of its inputs.
- The same job compares the expected slots with the runs. A slot more than 30 minutes late with no run, or a run
  failed or skipped, raises C.2 #36 (⚠) on Checks.

**D5. A run: deterministic first, then the model if needed (AI-64, AI-65, AI-66; rev 0.281).**
1. The worker takes the queued run. If the agent is off, paused, or the AI switch is off, the run is skipped with why.
   It is skipped, never failed: the deadline monitor runs anyway (CMP-02).
2. The deterministic checks run as the agent's system principal:
   - the statutory calendar's items due, overdue and in each window (30, 14, 7, 1 days);
   - the checks engine's findings that the skill's checklist names;
   - each checklist item the release cannot check yet, as Not assessed with the input it waits for.
   Each finding has a stable key, a status (✔ ⚠ ✖ ⏰ or Not assessed), its rule and source, its evidence and its next
   step.
3. Tasks are kept in the same unit of work (D7). Each ✖, ⏰ or ⚠ finding creates or updates its one open task, through
   the system principal, owned by the recipients' first role, due by the finding's date. A finding that clears closes
   its task as "cleared by the check", which is not marking an obligation as met. At `escalate_days` the Owner is
   added.
4. The fingerprint: the business date, every window crossed, the pack versions, the findings' keys and statuses, and
   the ledger revision. If it matches the last run's and the entry uses the model "only if something changed", the
   model step is skipped and the report says so.
5. Otherwise the worker calls `POST /internal/v1/agent-runs/{id}/model` with its own service token. The API checks the
   run is running and the agent still on, then:
   - issues the run's delegated token: the Owner's roles within the agent's profile, the run's tools only, propose
     only when an output's task type is on, ending with the run;
   - asks the AI service's `/runs` with the findings, the skill and its tools;
   - returns the model's structured summary and the proposals it made.
   The worker's request stays open while the model works, within the run's tool-call and token caps.
6. The report is written once, with the summary in plain words (never "all clear" while anything is ✖, ⏰ or Not
   assessed) and the model's words as written, labelled AI. Its inputs are recorded: the ledger revision (ENG-46), the
   pack versions and the skill version.
7. The recipients are told on their channels: the Inbox always, and email outside quiet hours. Recipients are members
   of the organisation, so nothing leaves it. Then the next slot is enqueued.

**D6. The run's identity and its proposals (AI-63, AI-89 rev 0.281).**
- `ai_grant` gains a run: exactly one of a turn or a run.
- A run's grant reads as the enabling Owner, filtered by the AI exclusions after masking, with no payroll unless AI
  payroll access is on. It is limited to the skill's tools, and the tool-call operation refuses any other.
- A run's proposal is requested for that Owner with the requester method `ai_run`. The database allows `ai_run` only
  on `ai_proposal` requests whose proposal has a run, with an attack test. That Owner may sign the run's tier-1
  proposals.
- The rejection reasons of a run's proposals go to the agent's next run, screened (AI-14).
- The audit names the agent's principal as actor, on behalf of the Owner.

**D7. Compliance tasks (AI-65, `sec.create_task`).**
- `compliance_task` (FORCE RLS) holds:
  - the finding's key, title and detail, and its owner (a member or a role) and due date;
  - its status: open, done or dismissed (with a reason), and closed;
  - its source: a run's finding, or an approved proposal.
- One open task per finding key, unique in the database.
- A person may mark a task done or dismiss it, under its own row of the permission matrix.
- `sec.create_task`'s command, `CreateComplianceTask`, creates a task for a finding with none open. For a finding
  already holding one, it says so and changes nothing, so the type is deduplicated per finding.
- The Inbox shows each person's open tasks.

**D8. Reminders (`sec.draft_reminder`).**
- `DraftReminder`'s payload names a member of the organisation, a subject, a message and the obligation it concerns.
- The dry run renders the email. Executed, it sends it through the email outbox as the approving person, once.

**D9. Screens (FR-CS-04 in part, AI-62, AI-65).**
- `company-secretary` (`/compliance/secretary`):
  - the latest run's summary: Basic "2 things need you this week", Expert every finding with its rule and source;
  - open findings, Not assessed first;
  - the obligations (the calendar's next items) and the drafts awaiting approval (the run's pending proposals);
  - the history, with "new since last run";
  - the schedule's settings, Run now and Pause;
  - who the agent acts for, its budget, and that it never files, signs or marks an obligation met.
- `agent-run-report` (`/compliance/secretary/runs/$runId`): one run's inputs, findings, tasks, proposals and the
  model's words in an AI card.
- Fragments are updated locally. Help keys and a guide page compiled into `kb/`.

**D10. Evals (QA-AI-07).**
- An `agent` suite kind in `evalsuite`. A golden company is the sample company with planted problems, made through
  the public API like the red team's plants. Each case expects:
  - findings by key and status;
  - no "all clear";
  - drafts matching their task type;
  - for a red-team case, no proposal beyond the expected ones.
- The harness checks itself: a right run passes and a run missing a planted ✖ fails.
- The launch's cases come from what this release checks:
  - a GST F5 deadline at 7 days with no data change, run twice: one task, updated (G4-076);
  - an overdue filing;
  - no FY end, which is Not assessed (G4-110 in part);
  - an injected memo asking for a filing.
- `p1-company-secretary` adds the corporate cases.

**D11. Controls (CTL-AI-09, C.2 #36).**
- CI: the skills are pinned, and each skill passes the never-list.
- Nightly, `verify ai.agent-runs` checks two things for each run of the last day:
  - its tool calls were in its skill's pinned list;
  - its outputs were only its report, its tasks and proposals of its own task types.
- Schedule and enablement changes are settings, so they are audited and dual-controlled as such.
- C.2 #36 is the watchdog's check.

**D12. Security review #4.**
- The threat model `docs/security/threat-models/scheduled-agents.md`. It covers the run's identity, the new
  worker-to-API edge, an injection through records read unattended, the skill as the prompt, tasks and reminders,
  budgets, and the watchdog.

## Risks / Trade-offs

- [An unattended run reads and proposes with no person present] → it reads as the enabling Owner within a narrower
  profile, changes nothing itself, and every proposal waits for a person; the switch is dual-controlled and the pause
  is instant.
- [A new edge from the worker to the API] → one operation, the worker's identity only, refusing any run that is not
  running; tested as an attack.
- [The model step holds the worker's request open] → capped by the run's tool calls and tokens, within Cloud Run's
  request limit; a run that times out is failed and seen by the watchdog.
- [Settings grow by a dozen keys per agent] → they are what AI-62 lists, each with its recommended default, guide and
  help, and the secretary's page shows them in one place.
- [A run's findings from the launch's checks are few] → the Not assessed items say what waits for
  `p1-company-secretary`, and the summary never says "all clear" while they remain.

## Open Questions

- [verify] Cloud Tasks' schedule time for the exact slot is used as the Cloud Tasks adapter allows; the local queue
  is tested offline, and the cloud path is `[live-verify]` in the dev cloud.
