# UAT: AI propose → approve → execute

- **Journey:** AI propose → approve → execute (QA-E2E-01)
- **People:** Ann Owner (Owner), Ben Founder (Owner and Controller)
- **Proves:** golden rule 5 and §9.6 (the AI proposes from the approved catalogue only; a named person approves; the
  executor carries it out), AI-32 (every task type off until the Owner turns it on, with a second approver), AI-07 (the
  summary written by Fyniq from the proposal and its dry run), AI-13 (a task type turned off), AI-61 to AI-66 (the
  company secretary's runs, findings and drafts, and what it never does), FR-APR-02 (the approver sees exactly what
  executes), AUD-01 (the audit trail names the proposal as the originator)
- **Organisation:** `make uat-organisation NAME="UAT AI Propose Pte Ltd" PEOPLE="Ann Owner:owner;Ben Founder:owner,controller" AI=routed`

At launch the AI proposes through the company secretary: its task types (`sec.create_task`, `sec.draft_reminder` and
`sec.draft_document`) each start off, and a proposal changes nothing until a person approves it.

## Before you start

- Turn the AI on as in the AI ask script, steps 2 and 3.
- On the local stack, `AI=routed` routes the organisation's assistant to local mode's scripted model, which makes only
  the browser journey's proposal. The company secretary's runs (steps 4, 5 and 8) need a live model `[live-verify]`;
  locally, turn on "Create a compliance task" (`sec.create_task`) in steps 2 and 3, and in place of steps 4 and 5 ask
  the assistant: "Create a compliance task titled "Book the room for the AGM" for the company secretary, due by 30 June
  2027, with the finding key agent:agm-room-2027." Its proposal is the one steps 6 and 7 approve and the executor
  carries out, at the stack's next tick (every 5 minutes by default). In the dev cloud the live model answers.

## Steps

| # | Who | Screen | Do | What to see | Evidence to keep |
|---|---|---|---|---|---|
| 1 | Ann | `ai-tasks` | Open Inbox › AI tasks, Catalogue. | "What the assistant may propose, and every proposal it has made. Nothing changes until a person approves it." Each task type is off. "Never proposed" lists what no task type may do: approving or rejecting anything, releasing or making a payment, changing a payee's bank details, filing with IRAS, CPF or ACRA, and the rest. | Screenshot of the catalogue |
| 2 | Ann | `ai-tasks` | Select "Turn on…" for the reminder drafts (`sec.draft_reminder`). | It waits for a second approver; the task type stays off until then. | Screenshot of the request |
| 3 | Ben | `approval-confirmation` | Approve the request in the Inbox and confirm. | The task type is on. | Screenshot of the catalogue |
| 4 | Ann | `company-secretary` | Open Compliance › company secretary. If it is off: "An Owner turns it on in Settings → Organisation, and a second approver confirms it"; do so with Ben. Then select "Run now". | "It checks the company's obligations on its schedule, explains what it finds, raises tasks and drafts reminders for a person to approve. It never files, signs, approves or marks an obligation as met." "A run is queued: it shows in its runs below." | Screenshot of the workspace |
| 5 | Ann | `agent-run-report` | Open the run's report when it finishes. | Each finding with its status (✔, ⚠, ✖, ⏰ or Not assessed); the summary never says all clear while one is ✖, ⏰ or Not assessed. Its drafts are under "Drafts awaiting approval". | Screenshot of the report |
| 6 | Ben | `ai-proposal-review` | Open the draft's proposal from the Inbox. | The AI label and "Tier 1 · routine". "Summary: Written by Fyniq from the proposal and its dry run, not by the AI." "What changes" and "Evidence". The AI's own words are marked "The AI wrote this. Check it against Fyniq's summary above, which says what really happens." | Screenshot of the proposal |
| 7 | Ben | `approval-confirmation` | Approve it and confirm. | The executor carries it out: the reminder draft exists, as the summary said, and the proposal shows executed. | Screenshot of the result |
| 8 | Ann | `ai-proposal-review` | Run the secretary again for a second proposal, open it and reject it with a reason. | Nothing changes; the proposal shows rejected with the reason. | Screenshot of the rejection |
| 9 | Ann | `audit-trail` | Open Compliance › Audit trail and find step 7's change. | Its originator is the AI proposal, Ben is the approver and the executor the actor; step 8 left no change. | Screenshot of the event, and the audit trail exported |
| 10 | Ann | `ai-tasks` | Turn the reminder drafts off. | Off at once; the next run drafts nothing of that type. | Screenshot of the catalogue |

## Sign-off

| Who | Name | Date | Result |
|---|---|---|---|
| Founder | | | |
| Founder | | | |
| Accountant | | | |
