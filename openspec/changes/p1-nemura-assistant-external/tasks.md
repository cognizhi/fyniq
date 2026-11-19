# Tasks

Each numbered group is one slice and ends with the local gates green (`make check`, the Go tests of the touched packages in both flavours, `make api-check`, `make db-lint`, the web typecheck and tests). `[live-verify]` marks what only the cloud can show.

Load before each slice: `fyniq-ai-proposals`, `fyniq-security-compliance`, `fyniq-feature-change`, `fyniq-api-integrations`, `fyniq-design-system` and `fyniq-knowledge-base` (screens), `fyniq-quality-gates`, `fyniq-release`.

## 1. The rules, the settings and the proposals (slice 1)

- [x] 1.1 The people never-list as words and the catalogue of `people.draft_letter` and `people.draft_fwa_reply` (strict input schemas, tier 1, approver roles, 72-hour expiry), both refused as they load and as each proposal is made. A test names every NM-AI-04 sentence. `// req: NM-AI-04, NM-AI-01`
- [x] 1.2 AI settings per organisation, off by default, changed by the Owner and audited: the assistant on or off, the route, own-pay explanations (cannot be turned on without a second approver). `// req: NM-AI-01, NM-AI-06`
- [x] 1.3 `nemura.ai_proposal` (insert-only for `nemura_assistant`; the decider is another person with the task's approver role; expiry at 72 hours; the final text and its hash), its API (list, read, approve, reject, edit before approving), the audit events and the data inventory row. `// req: NM-AI-04, NM-ARC-06`

## 2. Questions (slice 2)

- [x] 2.1 Read tools in the asking person's session (my leave balances and requests, my pay day, the policy and pack answers with their rule and `[verify]` flag, a payslip line's rule), grounding (every figure from a tool result in the turn) and the model port with the scripted model. `// req: NM-AI-02, NM-AI-03, NM-AI-06`
- [x] 2.2 `nm-ask` and the ask endpoint; no pay figure in any model input; "to confirm with HR" for a rule marked to be verified; the knowledge base's people pages cited. `// req: NM-AI-03, NM-AI-06`

## 3. Drafts and the executor (slice 3)

- [x] 3.1 `people.draft_letter` and `people.draft_fwa_reply` proposals made from a request, with the fields filled from the record; `nm-assistant` shows what waits for a human. `// req: NM-AI-02`
- [x] 3.2 The executor applies an approved proposal's stored text through the letters domain commands, once; an expired, rejected or edited-after-approval proposal applies nothing. `// req: NM-AI-04`

## 4. Evals and red team (slice 4)

- [x] 4.1 Golden questions from the pack and the oracle, and the red-team suite (an injected instruction in a request, a request for another person's pay, for an approval, for a bank account change, for a role change), through the real runtime with the scripted model. `// req: NM-AI-04`
- [x] 4.2 `agents/people-assistant/SKILL.md`, the guide pages and their knowledge-base pages; a switched-off feature's tools are absent (NM-EXT-01's Nemura side). `// req: NM-AI-02, NM-EXT-01`
- [x] 4.3 Verify the whole change; the second reviewer's list: (1) `nemura_assistant` can insert a proposal and its audit event and nothing else (the grants golden and `TestEachRoleHoldsExactlyItsGrants`); (2) the executor attaches a draft and changes no request (`permission denied` tested); (3) the never-list as words, on tasks, fields and tools; (4) no pay figure, NRIC or bank account reaches a model; (5) a request's own words are quoted as data (the red-team suite); (6) own pay refused by a database check; (7) the model route is the scripted one until evidenced `[live-verify]`.

## Coverage

| Requirement | Built in | Status | Left to |
|---|---|---|---|
| NM-AI-01 Same rules as Fyniq's AI | 1.1, 1.2 | | the route's Singapore evidence `[live-verify]` |
| NM-AI-02 Skills baked in | 2.1, 3.1, 4.2 | | hiring and review tasks: `p2-nemura-people` |
| NM-AI-03 Knowledge | 2.2 | | |
| NM-AI-04 Never-list | 1.1, 1.3, 3.2, 4.1 | | |
| NM-AI-06 Own pay | 1.2, 2.2 | off until a second approver exists | `p1-nemura-fyniq-link` |
| people.draft_letter, people.draft_fwa_reply | 1.1, 3.1, 3.2 | | |
| NM-EXT-01 Fyniq without Nemura | 4.2 | Nemura's side only | `p1-payroll-link` 8.1 |
| NM-EXT-02 Connectors | | none here | `p1-payroll-link` 7.1; the provider connector after its API is verified `[verify]` |
| NM-EXT-03 Imported journals are drafts | | none here | `p1-payroll-link` 7.1 |

## Left (slice 3)

- The assistant's and the executor's own processes (`NEMURA_ROLE=assistant` and `executor`) are not built (`cmd/nemura/main_test.go` says so), so a draft request answers `NM_UNAVAILABLE` in a deployment until they are; the code path, the roles and the grants are proven by the database tests. The queued `assistant.apply` item (class executor) waits for the executor's sweep, which is `ExecutorService.ApplyApproved`.
- A draft letter's fields are the values the request already holds; filling the template's own fields from the record again is the people team's check step.

## Left (slice 4)

- A recorded replay of a real model's answers (`evals/cassettes`) waits for a route evidenced in Singapore `[live-verify]`; the suites run deterministically through the scripted model, which is the recorded run for now.
- NM-EXT-01 to 03 (Fyniq without Nemura, the payroll file import, one connector, imported journals as drafts) are Fyniq's side and stay in `p1-payroll-link` 7.1 and 8.1; the Nemura side (a tool of a switched-off feature is absent) is built and tested.
- The Payroll feature cannot be switched on through the API ("needs the Owner and a second approver ... cannot be switched on yet"): `p1-nemura-platform` 6.5 and the Fyniq link's second approver own it. The assistant's pay tools are absent until it is on, which is the safe default.
