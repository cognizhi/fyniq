# Recipes for common changes

Each recipe lists **every** place that usually has to change. The gates (contract tests,
`oasdiff`, squawk, the authorisation matrix, coverage of visible numbers and messages, mode
parity) catch many misses, but not all of them. Paths follow PRD-001 §12.4; the code arrives in
P0. Search by identifier: paths and names will settle during P0.

Contents:

1. Add a field to an existing resource
2. Add a report
3. Add a check
4. Add a permission or role capability
5. Add an event or a webhook
6. Add a statutory parameter
7. Add a KPI or dashboard tile
8. Add a document type or posting source

---

## 1. Add a field to an existing resource

Example: "PO number" on bills (`po_number`).

| Step | Where | Detail |
|---|---|---|
| Requirement | PRD §8.6 | Extend FR-AP-02 or add `FR-AP-12 (rev 0.N)` |
| Contract | `contracts/openapi/` | Optional property on `Bill`, `BillCreate`, `BillUpdate`; description, example, max length; filterable if users search by it (FR-INT-10). Optional + additive = no `/v2` |
| Migration | `db/migrations/NNNNNN_bill_po_number.sql` | `ALTER TABLE bill_version ADD COLUMN po_number text` (nullable; no default rewrite); index `(organisation_id, po_number)` if filtered. squawk must pass (no table rewrite, no long lock) |
| Domain | `internal/ap/bill` | Field in the command and the version snapshot; validation (trim, NFKC normalise like FR-BNK-07, length); error code `AP_BILL_PO_TOO_LONG` with a plain message |
| Posting | — | None (does not change the ledger) |
| Audit | same transaction | Appears in the `changes` diff automatically if it is on the versioned row; assert it in the command test |
| Permissions | Appendix D | Inherits "Bills, vendor credits"; no new capability |
| AI | `contracts/ai/` | Add to the bill read tool's columns if useful; `ap.draft_bill` may fill it (schema update + proposal eval) |
| UI | `apps/web` | Field in the bill form's **Expert** field group (registry tag `expert`); the Basic form shows it read-only in "advanced details" when set (FR-UX-25); column in the bills grid |
| Import/export | migration templates, CSV/XLSX exports | Column appended; import template updated (FR-MIG-03) |
| Tests | — | Contract, validation messages (each error code triggered, QA-RPT-02), audit diff, UI component, mode parity |
| Commit | — | `feat(api): add PO number to bills` |

## 2. Add a report

Example: "Spend by supplier and department".

| Step | Where | Detail |
|---|---|---|
| Requirement | PRD §8.12 | New `FR-RPT-…` with columns, filters, comparatives, drill-down and exports |
| Data | balance service / semantic layer | **Read through the balance service** (ENG-04); add semantic-layer measures and dimensions if AI or `/v1/metrics:query` should reach it (AI-03, FR-INT-15) |
| Contract | `contracts/openapi/` | `GET /v1/reports/…` with parameters; response records the ledger high-water mark (FR-RPT-08) |
| Exports | XLSX (excelize), PDF (Typst), CSV | Streamed; large exports asynchronous (202 + operation) with signed link and audit event (FR-RPT-09) |
| Oracle | `oracle/` | An independent expectation for **every visible figure** (QA-RPT-01) from the inputs and the documented rules, never from engine output |
| UI | `apps/web` | Statement table (TanStack) with sticky header and first column; every figure a drill-down button (FR-UX-04); Basic shows plain explanations, Expert shows codes and comparatives (`pages/reports.md`) |
| Permissions | Appendix D | Which roles see it; masking of payroll lines |
| Tests | — | Coverage report 100% of visible figures; export round-trip (QA-RPT-03); visual snapshot in both themes (QA-RPT-04); p95 ≤ 2 s at 1 M lines (NFR-PERF-02) |
| Commit | — | `feat(api): add spend by supplier and department report` |

## 3. Add a check

Example: "Bills above S$X without an attachment" (Appendix C.2 #20).

| Step | Where | Detail |
|---|---|---|
| Requirement | Appendix C | New row with area, wording and severity (✖ blocks close, ⚠ needs review, ⏰ due) |
| Rule | checks engine | Runs on every relevant change and nightly (FR-CTL-01); returns offending items for drill-down |
| Wording | message catalogue | Plain English: what is wrong and how to fix it; "Fix it" action where possible; owner role |
| Settings | organisation settings | Thresholds (e.g. S$X) are settings with defaults, never literals |
| Close | period close | ✖ checks block the close (FR-GL-07) |
| AI | — | The Review lane may surface it; no write |
| UI | Checks page, Home books health, record right panel | Chip with icon + label; appears in **both** modes (safety information, FR-UX-24) |
| Tests | — | A fixture that triggers it and one that does not; status-coverage test lists it (QA-RPT-02) |
| Commit | — | `feat(compliance): check bills without attachments` |

## 4. Add a permission or role capability

Example: "Approver may approve expense claims in their department".

| Step | Where | Detail |
|---|---|---|
| Requirement | §6.2, Appendix D | Update the matrix row; check SoD conflicts (§6.3) and team modes (§6.4) |
| Policy | authorisation layer | Role + attribute condition (department, amount); masking rules |
| Database | RLS policies | Same rule in RLS for sensitive tables (SEC-02) |
| Approvals | FR-APR-01 policies | If it is an approval capability, add the policy condition |
| AI | AI-01 | AI on behalf of the user can never exceed the user; check the AI column of Appendix D |
| MCP/API scopes | §9.7, FR-INT-02 | Does a scope need to exist for agents or integrations? Agents stay proposal-only (AI-34) |
| Tests | QA-SEC-03 | Every role × the endpoint × allowed/denied, per team mode; masking asserted |
| Controls | Appendix F | CTL-FIN-07 / CTL-IAM-06 evidence updated if the matrix changed |
| Commit | — | `feat(security): let approvers approve department claims` |

## 5. Add an event or a webhook

Example: `fyniq.invoice.paid.v1`.

| Step | Where | Detail |
|---|---|---|
| Requirement | FR-INT-12, FR-AUT-04 | Event name, trigger, payload, who may subscribe |
| Schema | `contracts/events/` (AsyncAPI 3) | CloudEvents 1.0 envelope; `type: fyniq.invoice.paid.v1`; payload is IDs and safe fields (never NRIC, bank numbers, salaries) |
| Emission | transactional outbox | Written in the **same transaction** as the change; delivered by a named Cloud Task to a handler or Cloud Run job (River locally), idempotent on the outbox ID (ADR-T7) |
| Webhook delivery | worker | Standard Webhooks signature (HMAC-SHA256, timestamp, `webhook-id`), retries for 3 days, dead-letter view, endpoint verification |
| Permissions | subscription scopes | A subscriber only receives events for data its client may read |
| Versioning | §12.10 | A breaking payload change creates `.v2`; `.v1` keeps flowing until its sunset |
| Tests | — | Outbox written in the same transaction (kill between commit and delivery: no loss, no phantom); signature verification; retry and dead-letter |
| Commit | — | `feat(api): emit invoice paid events and webhooks` |

## 6. Add a statutory parameter

Example: a new CPF rate table effective 1 Jan 2027. **Use the `fyniq-sg-statutory` skill**; the
outline is:

| Step | Where | Detail |
|---|---|---|
| Source | official page | URL, retrieval date, verifier (ENG-30) |
| Pack | `packs/sg-cpf@2027.1` | New version with `effective_from`; never edit a past version (ENG-31) |
| Pinned tests | QA-ENG-08 | Hand-calculated cases from the official tables, independent of the pack data |
| Approval | ENG-32, SOD-10 | Dual approval before activation |
| Release | ENG-33 | Pack release independent of the platform version |
| Commit | — | `feat(packs): CPF rates from 1 Jan 2027` |

## 7. Add a KPI or dashboard tile

| Step | Where | Detail |
|---|---|---|
| Requirement | FR-KPI-02/03 | Definition in words, formula, period, comparison |
| Definition | KPI code (versioned) | Reads the balance service or semantic layer; shown on hover (FR-KPI-03) |
| Oracle | `oracle/` | Independent expectation for the sample company |
| UI | stat tile (MASTER §7) | Figure + change with sign and arrow (never colour alone), "updated" time, drill-down; Basic or Expert tag |
| AI | semantic layer | Named measure so the assistant can answer with it |
| Commit | — | `feat(api): add burn multiple KPI` |

## 8. Add a document type or posting source

The largest change: follow every row of the SKILL.md checklist, and in addition:

1. Write the posting rule in PRD Appendix B first (debit, credit, rounding policy, edge cases).
2. Implement it in the **oracle** from that text before or alongside the engine.
3. Differential tests on generated scenarios (QA-ENG-02) and property tests (QA-ENG-03).
4. Replay test for the rule version (QA-ENG-06).
5. Subledger vs control account verification if it has a subledger (INV-07).
6. Edge-case fixtures for every ✖/⚠ branch (QA-ENG-07).
7. See **fyniq-accounting-domain** for the engine's conventions.
