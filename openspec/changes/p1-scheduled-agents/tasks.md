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

- [ ] 3.1 `agent_run`: migration, guard (forward only, each step by its role, nothing changes once ended), grants, the
  storage port and the postgres adapter, with attack tests (design D3). Tests `// req: AI-65`, `// req: AI-67`.
- [ ] 3.2 `compliance_task`: migration, guard (one open task per finding), grants, port and adapter. Also the API's
  list, get, done and dismiss operations, the permission matrix's row and attack tests (design D7). Tests
  `// req: AI-65`.

## 4. Schedules

- [ ] 4.1 The outbox's optional run-at time (the Cloud Tasks adapter's schedule time; the local queue holds it). The
  job `agents.schedule` every 5 minutes queues each due slot and enqueues it at its time; a finished run enqueues its
  next slot (design D4). Tests `// req: AI-62`.
- [ ] 4.2 The watchdog: a slot late by more than 30 minutes, or a run failed or skipped, raises C.2 #36 (⚠) on
  Checks (design D4). Tests `// req: AI-67`, `// req: C.2 #36`.
- [ ] 4.3 Event triggers: a period closed, a share issue or transfer, and a calendar filing recorded queue an event
  run (design D4). Tests `// req: AI-62`.

## 5. The run

- [ ] 5.1 The worker's job `agents.run` (design D5 steps 1–4, 6–7):
  - it skips a run with why when the agent is off or paused, or the AI is off;
  - it runs the deterministic findings from the statutory calendar and the checks engine, and lists the Not assessed
    items;
  - it creates, updates and clears the one open task per finding, escalating to the Owner;
  - it takes the fingerprint and writes the report, never "all clear" with anything open;
  - it tells the recipients on their channels, honouring quiet hours.
  Tests `// req: AI-64`, `// req: AI-65`, `// req: AI-66`, `// req: G4-076`.
- [ ] 5.2 The run's grant (`ai_grant` with a run): the Owner's reads within the agent's profile and its tools only,
  proposing only with an output's task type on. The requester method `ai_run` is allowed in the database for
  `ai_proposal` requests from a run alone, with its attack test; the Owner may sign tier 1 (design D6). Tests
  `// req: AI-63`, `// req: AI-89`.
- [ ] 5.3 The model step: `POST /internal/v1/agent-runs/{id}/model` for the worker's identity alone. The API issues the
  run's token and asks the AI service's `/runs` with the skill, its tools and the findings. The model's structured
  summary and proposals come back to the report. The rejection reasons of the run's earlier proposals reach its next
  run (design D5 step 5, D6). Tests `// req: AI-64`, `// req: AI-65`, `// req: AI-14`.

## 6. Task types

- [ ] 6.1 `sec.create_task`: its catalogue file and its command `CreateComplianceTask`, deduplicated per finding, with
  the executor's grants for it (design D7). Tests `// req: ai/task-catalogue sec.create_task Create a compliance task`.
- [ ] 6.2 `sec.draft_reminder`: its catalogue file and its command `DraftReminder`, which renders the email in the dry
  run and sends it once as the approving person (design D8). Tests
  `// req: ai/task-catalogue sec.draft_reminder Draft a reminder about an obligation`.

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
