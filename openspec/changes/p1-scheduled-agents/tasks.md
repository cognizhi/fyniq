## 1. Skills

- [x] 1.1 `internal/agents` loads `agents/<name>/SKILL.md` (`agent-skill@1` front matter) for the release's agents (the
  company secretary). It refuses an unknown output, a tool that is not a read tool's name, or a never-listed task type.
  `agents/agents.lock.json` pins each skill's version and hash. Also: the `company_secretary` row of `ai_agent`, and the
  `.dockerignore` and Dockerfile lines for the embedded `agents/` (design D1). Tests `// req: AI-61`,
  `// req: CTL-AI-09`.
  As built: the top-level package `agents` embeds `*/SKILL.md`; `internal/agents` parses the front matter strictly
  (unknown fields refused) and checks each skill:
  - its name and schema;
  - its tools are reads (`aitool.ReadName`, now exported) within its profile (`compliance-read`);
  - its outputs include `run_report`, and the rest are task types off the never-list.
  A run offers the skill's tools that the registry has (`Offered`) and the task types the catalogue has (`Proposes`):
  not `kb.*` or `sec.draft_document` yet. The skill's text now says Fyniq runs the deterministic checks and makes
  their tasks, and adds the Not assessed rule; it is pinned at version 1. Migration 000383 adds the
  `company_secretary` agent.

## 2. Configuration

- [x] 2.1 The setting kinds `cron` (five fields, Singapore time) and `time_range`. The `agents.company_secretary.*`
  settings with their recommended defaults: the switch on only with a second approver and off at once, pause on at
  once, the schedule's entries, events, quiet hours, recipients, channels, escalation, the tool-call and per-run token
  caps. The settings page shows them (design D2). Tests `// req: AI-62`, `// req: AI-63`,
  `// req: ai/scheduled-agents Recommended default schedule`.
  As built:
  - `internal/domain/cron` parses five-field cron in Singapore time (with `Next`) and quiet hours (`ParseRange`,
    wrapping past midnight); both accept `off`.
  - The settings gain the kinds `cron`, `time_range`, `choices` (several of a list, kept in its order) and `tokens`.
  - The thirteen `agents.company_secretary.*` keys are dual-controlled, except `paused`, which applies at once both
    ways. `enabled` also turns off at once.
  - The settings page reads the new kinds in words ("Daily at 07:00, Singapore time") and offers the choices as
    checkboxes.
  - The guide's section "Company secretary assistant" compiles into `kb/pages/concept/settings-organisation.md`.

## 3. Stores

- [x] 3.1 `agent_run`: migration, guard (forward only, each step by its role, nothing changes once ended), grants, the
  storage port and the postgres adapter, with attack tests (design D3). Tests `// req: AI-65`, `// req: AI-67`.
  As built: migration 000384 under guard FY350. A slot's run is queued once (unique per agent, trigger and slot, so a
  second queueing finds it); only the worker moves it: queued → running → done, failed or skipped. Its report (the
  fingerprint, ledger revision, pack versions, knowledge-base snapshot, findings, summary and the model's words) is
  written once, as it ends. The API may queue Run now and read. The table is in the tenant export and the data
  inventory (`agent-runs`).
- [x] 3.2 `compliance_task`: migration, guard (one open task per finding), grants, port and adapter. Also the API's
  list, get, done and dismiss operations, the permission matrix's row and attack tests (design D7). Tests
  `// req: AI-65`.
  As built:
  - Migration 000385 under guard FY351, with one open task per finding (a unique partial index, `STORAGE-DUPLICATE-01`).
  - The worker raises a run's tasks, updates them while open and clears them; the executor raises an approved
    proposal's.
  - The API's `/v1/compliance/tasks` lists and reads them under V on the AI company secretary's row. Done and dismiss
    need C there, which the Owner may use, and a person with a Fyniq user; they work over If-Match, and a dismissal
    needs a reason (`TASK-01`).
  - A closed task stays closed. The tenant export and the data inventory (`compliance-tasks`) hold the table.

## 4. Schedules

- [x] 4.1 The outbox's optional run-at time (the Cloud Tasks adapter's schedule time; the local queue holds it). The
  job `agents.schedule` every 5 minutes queues each due slot and enqueues it at its time; a finished run enqueues its
  next slot (design D4). Tests `// req: AI-62`.
  As built:
  - `ports.Task.At` carries the time: Cloud Tasks gets it as `scheduleTime`, and the local push queue waits for it.
  - `outbox.Dispatcher.EnqueueAt` and `DispatchAt` run a scheduled row's deadline from its time.
  - `agents.Scheduler` (job `agents.schedule`, every tick) reads each agent's settings on the day. For an agent on and
    not paused, it queues each entry's slots from the later of an hour ago and when it was turned on, to 7 minutes
    ahead, each once (the run's unique slot), and enqueues each at its slot.
  - A run acts for the person who asked for the switch's approved change (`Approvals().LastApproved`).
  - The settings' own rule (Appendix D's row): turning the agent on needs A (the Owner); turning it off, its schedule
    and the rest need C, which the Owner may use.
  - A finished run's own next slot is queued by the run job (task 5.1, `Scheduler.Queue`); the tick queues every due
    slot anyway.
- [x] 4.2 The watchdog: a slot late by more than 30 minutes, or a run failed or skipped, raises C.2 #36 (⚠) on
  Checks (design D4). Tests `// req: AI-67`, `// req: C.2 #36`.
  As built: the check `agents.runs` (C.2 #36, ⚠) compares the last 25 hours, from when the agent was turned on, with
  its runs. It lists each run that failed or was skipped, with why; each still queued more than 30 minutes after its
  slot; and each slot of a schedule entry no run came for, unless the agent is paused. Its items open on the AI company
  secretary's row. The checks engine raises it, so it shows on Checks.
- [x] 4.3 Event triggers: a period closed, a share issue or transfer, and a calendar filing recorded queue an event
  run (design D4). Tests `// req: AI-62`.
  As built: `API.agentEvent` queues, in the change's own unit of work, the run each running agent owes the event, once
  per event's key (`event:<kind>:<key>`). It is called after a period's close is attested (`period_closed`), an equity
  transaction is effective (`shares_changed`) and a calendar filing is recorded (`filing_recorded`), each when the
  agent's events include it. Officer, address and controller changes join with `p1-company-secretary`. Deadline windows
  are the daily monitor's.

## 5. The run

- [x] 5.1 The worker's job `agents.run` (design D5 steps 1–4, 6–7):
  - it skips a run with why when the agent is off or paused, or the AI is off;
  - it runs the deterministic findings from the statutory calendar and the checks engine, and lists the Not assessed
    items;
  - it creates, updates and clears the one open task per finding, escalating to the Owner;
  - it takes the fingerprint and writes the report, never "all clear" with anything open;
  - it tells the recipients on their channels, honouring quiet hours.
  Tests `// req: AI-64`, `// req: AI-65`, `// req: AI-66`, `// req: G4-076`.
  As built: `agents.Runner` is the worker's `agents.run`.
  - A run before its slot holds until it is due (`outbox.Held`). A skill changed since the run was queued, the agent off
    or paused, the AI switched off, or the person it acts for no longer an Owner each skip the run with why.
  - The findings: the statutory calendar's items in the next 30 days or overdue (⏰ with their window), the six checks
    the checklist reads, and the corporate profile's checks Not assessed until it can be recorded.
  - The fingerprint is the findings' keys, statuses and windows and the packs read, so a window crossed changes it
    with no data change.
  - The model is asked only when the fingerprint changed, for the monitor and events (task 5.3 wires the step). Tasks
    are raised, updated and cleared in the unit of work that finishes the run.
  - When something is new, the recipients hear (`agent.report`, in the Inbox and the daily email, which waits past
    quiet hours), the Owner too when a deadline is 7 days away or less. A scheduled run queues its entry's next slot.
- [x] 5.2 The run's grant (`ai_grant` with a run): the Owner's reads within the agent's profile and its tools only,
  proposing only with an output's task type on. The requester method `ai_run` is allowed in the database for
  `ai_proposal` requests from a run alone, with its attack test; the Owner may sign tier 1 (design D6). Tests
  `// req: AI-63`, `// req: AI-89`.
  As built: migration 000386 (validated in 000387).
  - `ai_grant` holds a turn or a run, never both. A run's grant holds its tools and task types, lives at most 30 minutes
    (a turn's still 5) and is one per run. `ai_grant_for` resolves it to the Owner, the run and its agent's name
    (`company_secretary`).
  - The API's `issueRunGrant` issues it for a running run alone: the skill's tools that the registry has, and its outputs
    that are task types on and built. Task 5.3 calls it.
  - `callAITool` refuses a run any other tool (`AI-TOOL-05`), and `proposeAITask` any other task type.
  - A run's proposal comes from the running run, for the Owner it acts for, `agent` the agent's name. Its request's
    method is `ai_run`; the new trigger `approval_request_ai_requester` ties `ai_run` and `ai_turn` each to its own
    origin.
  - The audit names the agent (`ai`, `company_secretary`) on behalf of the Owner.
- [x] 5.3 The model step: `POST /internal/v1/agent-runs/{id}/model` for the worker's identity alone. The API issues the
  run's token and asks the AI service's `/runs` with the skill, its tools and the findings. The model's structured
  summary and proposals come back to the report. The rejection reasons of the run's earlier proposals reach its next
  run (design D5 step 5, D6). Tests `// req: AI-64`, `// req: AI-65`, `// req: AI-14`.
  As built:
  - `runAgentModel` takes the worker's service token of the class `agent_runs` alone; no other token opens it, and that
    token opens nothing else. It checks the run is running and its skill unchanged, then asks `agents.Skipped` again.
    It issues the run's grant (task 5.2), asks the AI service's `/runs`, screens the model's words and ends the grant.
  - The AI service's `Run` shares the turn's tool loop: the skill's instructions and a "How you report" section are
    the system prompt, and the findings arrive as data. The run's tools alone are offered, its model calls are routed
    for the agent (`company_secretary`), and its words are grounded as an answer's are. It stops at the run's tool calls
    or tokens (`agents.company_secretary.tool_calls`, `run_tokens`). Its proposals record `company_secretary@1` as their
    prompt.
  - The newest five rejected proposals of the agent's, for the Owner, reach its next run as `untrusted_text`.
  - The worker's `agents.APIModel` is the run job's model step when the kit names the API's internal URL: in the stack,
    `FYNIQ_API_INTERNAL_URL`. In the cloud, `FYNIQ_GCP`'s `api` entry and `invokers.agent_runs` (the worker's account,
    infra) [live-verify].

## 6. Task types

- [x] 6.1 `sec.create_task`: its catalogue file and its command `CreateComplianceTask`, deduplicated per finding, with
  the executor's grants for it (design D7). Tests `// req: ai/task-catalogue sec.create_task Create a compliance task`.
  As built:
  - `contracts/ai/tasks/sec.create_task.json` (tier 1, one item, the Owner and the Company secretary approve, 72 hours)
    is pinned in `tasks.lock.json`. `proposal.Release` gains its command.
  - A task the model suggests names a finding of its own (`agent:…`). The dry run refuses a finding with a task open or
    a proposal for it waiting. The executor raises it from the proposal (`RaiseForProposal`, the grants of task 3.2) as
    the approving person, and a task opened meanwhile leaves the proposal Stale. The command's `Actor` carries the
    proposal it runs.
  - Its policy, `createComplianceTask`, is C on the AI company secretary's row, the Owner too, and no API operation of
    its own: `proposal.CommandPolicy` finds a command's policy where the approval rule reads it.
  - Appendix D's note (rev 0.280) gives the Company secretary A on every `sec.*` proposal. They reach the three
    operations that sign a request (`authz.SignsRequests`), and `proposal.Approves` lets them sign `sec.*` proposals
    alone; the matrix expects it from the note.
  - Its proposal suite is `evals/sec-create-task` (QA-AI-02). Its guide section is in `ai-proposals.md`, compiled into
    `ai.proposals`.
- [x] 6.2 `sec.draft_reminder`: its catalogue file and its command `DraftReminder`, which renders the email in the dry
  run and sends it once as the approving person (design D8). Tests
  `// req: ai/task-catalogue sec.draft_reminder Draft a reminder about an obligation`.
  As built:
  - The payload names a recipients' role (`to_role`) in place of a member: the model reads no member's ID. The
    reminder goes to each member holding that role with a sign-in email.
  - The dry run renders the email and counts its recipients; a role no one can be emailed in is refused. Executed, the
    reminder is queued once per recipient through the email outbox (purpose `secretary_reminder`), ending by saying the
    AI drafted it and a person approved it.
  - Migration 000388 (validated in 000389): the executor queues that purpose and no other, and no other role queues it.
    It reads `member_emails()` and the messages it queues. The executor passes its outbox dispatcher to commands
    (`Actor.Outbox`).
  - The delivery log's purposes gain `remittance` (missing since FR-BNK-12) and `secretary_reminder`.
  - Its proposal suite is `evals/sec-draft-reminder`, for which the eval harness adds a Company secretary with a
    verified email. Its guide section is in `ai-proposals.md`.

## 7. The screens

- [ ] 7.1 Compliance → Company secretary (`company-secretary`): the latest summary (Basic and Expert), open findings with
  Not assessed first, the obligations, the drafts awaiting approval, the history with "new since last run", the
  schedule's settings, Run now, Pause, and who the agent acts for. Help keys, its guide page compiled into `kb/`, and
  the fragment updated locally (design D9). Tests `// req: FR-CS-04`, `// req: AI-62`, Vitest with axe.
- [ ] 7.2 A run's report (`agent-run-report`) and the Inbox's tasks. Tests `// req: AI-65`, Vitest with axe.

## 8. Evals and controls

- [ ] 8.1 The `agent` suite kind and its harness: golden companies with planted problems, findings by key and status,
  no "all clear", drafts matching their task type, red-team cases with no action. The launch's cases are G4-076, an
  overdue filing, no FY end and an injected memo (design D10). Tests `// req: QA-AI-07`.
- [ ] 8.2 `verify ai.agent-runs` nightly; `compliance/controls/CTL-AI-09.yaml` running (design D11). Tests
  `// req: CTL-AI-09`.

## 9. The finish

- [ ] 9.1 `docs/security/threat-models/scheduled-agents.md` for security review #4; the build plan's paragraph;
  `make check`, `make test` and `make evals` pass.
