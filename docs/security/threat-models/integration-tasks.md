# Threat model: tasks made by integrations

| | |
|---|---|
| Feature and requirement IDs | FR-INT-24 (inbound tasks from ticketing and other integrations), FR-INT-02 (client scopes); p2-api-third-parties task 2; Appendix D row "Tasks made by integrations" (rev 0.609) |
| Author, reviewer, date | Claude Code for the owner, 21 Nov 2026; second review waived before production (AGENTS.md), held with `needs-human-review` |
| Data and its class | A task's title, detail, due date, link and the client's reference (internal; the client is told not to send personal data or amounts); the client's ID |
| Who acts | An integration API client with the scope `fyniq.write.tasks` acting for a member holding C on the row (Owner, Administrator); people with V on the row read |

## 1. What we are building

A ticketing system or another integration makes tasks for people to do, and updates and finishes only the tasks it made. The tasks live in their own table (`integration_task`, migration 001152), apart from the compliance tasks the scheduled agents and approved proposals raise (AI-65).
Trust boundaries: the client to the API (token, scope, network allow-list, rate limits); the API to the database (RLS, column grants, the guard trigger).

## 2. What can go wrong

| # | Threat | Control | Test |
|---|---|---|---|
| T1 | A client completes, dismisses or edits a statutory or agent task | The operations read only `integration_task`; a compliance task's ID is not found there; the scope maps to no row of the compliance tasks (they stay under the AI company secretary's row, which no scope reaches); `fyniq_api` has no column grant that lets a client's call do more than the API already does for a person | `TestNoOneButAScopedIntegrationClientTouchesAnIntegrationTask`, `TestATaskAnIntegrationMadeIsTheClientsOwn` |
| T2 | One client reads or changes another client's tasks | The client is the OAuth client behind the call, stored at creation and never changed (FYT01); a read filters on it, an update names it in its `WHERE`, another client's task is "not found" | `TestAnIntegrationClientMakesAndUpdatesItsOwnTasks` |
| T3 | A task becomes a way into financial records | The scope `fyniq.write.tasks` covers one row; no financial row, approval, release or evidence is in it | `TestNoOneButAScopedIntegrationClientTouchesAnIntegrationTask` (reads and a write of the books refused), `TestScopesNarrowAClientToWholeAreas` |
| T4 | A person in the browser, the AI, an agent-class client, the link's client or a client acting for no member makes these tasks | The handler refuses all but a scoped, integration-class client acting for a member; the AI column of the row is none | the same attack test, the matrix rows |
| T5 | A finished task is reopened, or history is rewritten | The guard trigger: a finished task stays finished, identity never changes, each change is the next version, nothing is deleted; every change is audited | `TestATaskAnIntegrationMadeIsTheClientsOwn` |
| T6 | A link or text turns a person's click into an attack | The link must be https with no credentials; the web app shows text as text; `[left]` the Inbox rendering of these tasks arrives with the screen | the first test (http link refused) |
| T7 | A flood of tasks | The client's own write rate limit (FR-INT-16), a retried create finds the task by reference | the contract tests |

## 3. Left

A person closing or editing a client's task in the Inbox; retention of finished tasks; `[live-verify]` a run with a real ticketing system's webhooks.
