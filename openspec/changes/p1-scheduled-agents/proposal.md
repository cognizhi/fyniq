## Why

The owner put the company secretary on the launch path in full, with the AI changes it needs (PRD-001 rev 0.274). Its
assistant is a scheduled agent: on a schedule, or when something happens, it checks the books and the company's
obligations, reports, raises tasks and drafts what is due (§9.12). The assistant (`p1-ai-assistant`) answers people
in turns, and `p1-ai-proposals-core` gives the AI one way to change anything, a proposal a person approves. Neither runs
on its own. This change is the framework every scheduled agent runs on, with the company secretary's assistant as its
first agent and its two proposals: a task the model suggests (`sec.create_task`) and a reminder it drafts
(`sec.draft_reminder`). The secretary's own checks, corporate profile and documents follow in `p1-company-secretary`,
on this framework.

## What Changes

- **Agent skills (AI-61, CTL-AI-09)**: each agent is its skill file, `agents/<name>/SKILL.md`. The front matter names
  its version, the read tools it may call, the outputs it may produce and its permission profile; the text is its
  instructions. Skill files are hash-pinned per release in `agents/agents.lock.json`, so a change is a new version that
  CI checks. A run offers the model only the skill's tools that the release's registry has, and only the outputs whose
  task types are built and on.
- **Schedules and triggers (AI-62, the recommended default schedule)**: per organisation and per agent, as settings with
  recommended defaults (FR-ORG-08):
  - the switch that enables the agent: off by default, on only with a second approver (FM-1), off at once;
  - the schedule's entries, cron-style in Singapore time: the daily monitor at 07:00, the weekly cross-check on Monday
    at 08:00, after each month's close, and one month after the FY end;
  - the event triggers: a period closed, shares issued or transferred, a filing recorded, a deadline 30, 14, 7 or 1
    days ahead;
  - quiet hours, recipients and channels (Inbox and email), escalation to the Owner 7 days before a deadline, pause,
    and "Run now".
  Schedules live in the database: the tick finds the due runs, enqueues each at its exact time, and each finished run
  enqueues its next slot (ADR-T7).
- **Identity and permissions (AI-63, AI-89 rev 0.281)**: a run acts as the agent's non-human principal on behalf of
  the Owner who enabled it. Its delegated token reads as that Owner, within the agent's permission profile and the AI
  exclusions, with no payroll unless AI payroll access is on. A run's proposals are requested for that Owner,
  authenticated by the run's grant (`ai_run`), and that Owner may sign their tier-1 proposals.
- **Deterministic first (AI-64)**: the worker runs the deterministic checks first: the statutory calendar's deadlines
  and windows, and the checks engine. It records each finding with its status (✔ ⚠ ✖ ⏰ or Not assessed), rule, source,
  evidence and next step. The model is asked only when an input changed (the business date, every deadline window and
  the pack versions included) and the schedule entry uses it. The model then explains, cross-checks and drafts, through
  one internal API operation that asks the AI service (rev 0.281).
- **Outputs (AI-65)**:
  - an append-only **run report**, with the ledger revision, the pack versions and the skill version used, and the
    model's words stored as written and labelled AI. It never says "all clear" while anything is ✖, ⏰ or Not assessed.
  - **compliance tasks** with an owner and a due date, one open task per finding. A deterministic finding creates or
    updates its task through the system principal; a task the model suggests is a `sec.create_task` proposal.
  - **reminders** the model drafts, as `sec.draft_reminder` proposals, sent only after a person approves them.
- **What an agent never does (AI-66)**: no write tool and no output beyond the report, its tasks and its proposals.
  The never-list holds for agents as for turns; a skill naming anything else is refused as it loads.
- **Budgets and failure (AI-67, C.2 #36)**:
  - a token budget per run and per month and a cap on tool calls;
  - the Owner's instant pause;
  - a failed, skipped or late run is ⚠ on Checks, with the tick as the watchdog;
  - the deterministic deadline monitor (CMP-02) keeps running whatever the agent's state.
- **Agent evals (QA-AI-07)**: an `agent` suite kind. Golden companies with planted problems must have every planted ✖
  and ⏰ found, no false "all clear" and no action from a red-team case. The launch's checks bring the first cases;
  `p1-company-secretary` adds its own.
- **The screens**:
  - `company-secretary` (Compliance → Company secretary): the latest run's summary, open findings, the obligations,
    drafts awaiting approval, the history, the schedule, Run now and Pause;
  - `agent-run-report`: one run's findings, inputs and the model's words.

## Capabilities

### New Capabilities

None: the requirements are in the main specs already (`ai/scheduled-agents`, `ai/task-catalogue`, `ai/evaluation`,
`compliance/ai-and-api-controls`, `controls/platform-checks`). This change builds them (`skip_specs: true`).

### Modified Capabilities

None. PRD-001 rev 0.281 records the owner's two decisions this proposal rests on (the `worker` row of §12; AI-89), and
the AI-89 requirement in `ai/proposals` gains their scenario.

## Impact

- **Requirements built** (PRD-001 §9.12, §9.10, Appendix C.2, Appendix E, Appendix F.7):
  - AI-61 to AI-67, and the unnumbered "AI company secretary assistant" and "Recommended default schedule" rows
    (`ai/scheduled-agents`);
  - `sec.create_task` and `sec.draft_reminder` (`ai/task-catalogue`);
  - QA-AI-07 (`ai/evaluation`), CTL-AI-09 (`compliance/ai-and-api-controls`), C.2 #36 (`controls/platform-checks`).
- **Screens**: `company-secretary`, `agent-run-report` (fragments updated locally; no Claude Design publishing).
- **Code**: `agents/` loaded and pinned; new settings and their kinds (cron, time range); the agent run store and the
  compliance task store (migrations under FORCE RLS, with guards); the tick's schedule job; the worker's run job; one
  internal API operation for the model step; the AI service's runs; the two task types' commands for the executor; the
  checks; the screens; the evals.
- **Security**: the AI write path gains a second caller, a run, through the same proposal path. The worker gains one
  narrow edge to the API. Threat model `docs/security/threat-models/scheduled-agents.md` for security review #4.
- **Depends on** `p1-ai-assistant` and `p1-ai-proposals-core` (built, in review). `p1-company-secretary` builds on it.
