---
name: fh-ai-proposals
description: Add or change what the Finance Hub AI assistant can read or propose, safely — AI read tools on the semantic layer with on-behalf-of authorisation and AI exclusions, catalogue task types (proposal JSON Schema, domain command mapping, dry-run ledger impact, risk tier, limits, approver roles), the never-list, the proposal executor and its own service identity, numeric grounding, evals (golden questions, proposal evals, red team; replay vs live), prompt and model changes, and the dual-approved catalogue change process. Use whenever you touch contracts/ai/, the ai-orchestrator, the proposal service or executor, a system prompt, a model ID or endpoint, the semantic layer, AI settings or kill switches, or when someone asks "let the AI do X", "add an AI suggestion", "why did the AI say Y", "the AI made up a number" or "switch the model".
---

# AI read tools and proposals

**Humans decide; AI proposes** (PRD-001 rev 0.2 principle 4, §9). The AI reads within the
signed-in user's permissions, answers with figures that come from tools, and changes data **only**
by creating a proposal of an approved task type that a named human approves, every time. This is
enforced by architecture (no write credential, a separate executor identity), not by prompt.

> **Status:** no code exists yet. This skill states the target conventions (§9, §12.3,
> Appendix E). Commands marked *(P0)* arrive in P0.

## Where things live

| Path | Content |
|---|---|
| `contracts/ai/tools/*.yaml` | Read-tool definitions: name, description, input JSON Schema, semantic-layer query, scopes |
| `contracts/ai/tasks/*.yaml` | Catalogue task types: schema, domain command, tier, limits, approver roles, expiry, version |
| `contracts/ai/prompts/` | Versioned system prompts (`prompt_version`) |
| `contracts/ai/evals/` | Golden questions (QA-AI-01), proposal scenarios (QA-AI-02), red-team corpus (QA-AI-03), replay cassettes |
| `cmd/ai` | The ai-orchestrator: tool loop, Agent Platform calls, numeric grounding; **no database write role** |
| `cmd/executor` | The proposal executor: its own Cloud Run worker pool, service account and database role (§12.3) |
| `internal/semantic/` | Named measures and dimensions compiled to SQL by platform code (AI-03) |

**One source, three outputs.** Each tool and task YAML generates: (1) the tool definition the
model sees, (2) the MCP tool (**fh-mcp-server**), and (3) the OpenAPI description of the matching
API operation (**fh-api-integrations**, FR-INT-13). Write descriptions once, for humans and
models together.

```yaml
# contracts/ai/tasks/bank.categorise_lines.yaml
id: bank.categorise_lines
version: 1
name: Categorise bank lines
description: >
  Assign category, contact, GST code and dimensions to unreconciled bank lines.
  Creates a pending proposal; nothing changes until a human approves it.
  Use only lines returned by bank.unreconciled_lines in this conversation.
command: CategoriseBankLines          # domain command, same code as the UI
tier: 1                               # 1 routine/reversible · 2 posts or changes master data · 3 reserved
limits: { max_items: 100, max_amount_per_item: "10000.00", currency: SGD }
approver_roles: [bookkeeper, accountant, controller]
bulk_approval: true                   # tier 1 only (AI-32)
expiry_hours: 72
enabled_by_default: false             # Owner enables per organisation, dual-controlled (AI-32)
scope: fh.propose.bank.categorise_lines
input_schema:
  type: object
  additionalProperties: false
  required: [items]
  properties:
    items:
      type: array
      maxItems: 100
      items:
        type: object
        additionalProperties: false
        required: [bank_line_id, account_code, gst_code]
        properties:
          bank_line_id: { type: string, format: uuid }
          account_code: { type: string, pattern: "^[0-9]{4}$" }
          contact_id: { type: string, format: uuid }
          gst_code: { enum: [SR, ZR, ES, OS, TX, IM, ZP, BL, NR, EP, OP, NA] }
          dimensions: { type: object, additionalProperties: { type: string } }
          rationale: { type: string, maxLength: 500 }
evals: [contracts/ai/evals/proposals/bank.categorise_lines/*.yaml]
```

## Adding a read tool

1. **Need it?** Prefer a new measure or dimension in the semantic layer over a bespoke tool.
   The model never writes SQL against production (AI-03).
2. **Define** it in `contracts/ai/tools/`: a narrow purpose, a strict input schema, row and date
   limits (AI-04), and the scope (`fh.read.<area>`).
3. **Authorise on behalf of the user** (AI-01): the orchestrator passes an on-behalf-of token;
   the read API applies RLS, masking and the **AI exclusions** (AI-02: never NRIC/FIN, full bank
   numbers, secrets, authentication data or full home addresses; payroll only with the setting
   and the role).
4. **Shape the result**: structured, summarised when large, with `untrusted_text` labels on free
   text from bank descriptions, emails and documents (AI-18), and record IDs for citations.
5. **Test**: golden questions that use the tool, exclusion tests, and red-team cases that plant
   injections in the data it returns.

**Know-how is not a read tool's job.** "How do I…" and policy questions are answered from the
compiled knowledge base via `kb.index`, `kb.read` and `kb.search` (PRD §9.11, AI-49…58). Pages
never hold organisation figures, so a question that needs a number still goes to a read tool.
Changing the knowledge base itself is the **fh-knowledge-base** skill.

## Adding a catalogue task type

| Step | Rule | Requirement |
|---|---|---|
| 1. Check the never-list | Approvals, payments, payee bank details, users/roles/policies/AI settings/catalogue, statutory parameters, period reopen, voids and deletions, filings, tenant export, the audit trail: **never**. The proposal service rejects them even if added. | §9.6, CTL-AI-04 |
| 2. Map to one domain command | The same command the UI uses; no AI-only write path | §9.3 |
| 3. Strict JSON Schema | `additionalProperties: false`; money as decimal strings; IDs only from records the AI read this turn | AI-27 |
| 4. Dry run | The command in a rolled-back transaction (or a pure simulation) producing `impact`: ledger lines, balances before/after, checks that change, a document diff | §9.3, FR-INT-14 |
| 5. Tier and limits | Tier 1 routine and reversible; tier 2 posts or changes master data; tier 3 reserved (two approvers). Limits per item and in total. | AI-09, Appendix E |
| 6. Approver roles | Tier 1: bookkeeper or above (the requester may approve); tier 2: the target permission, Controller above the limit | AI-10 |
| 7. Business approval still applies | An executed proposal creates records that follow normal approval policies | AI-30 |
| 8. Undo | Define the reversal (reversing entries, previous master-data version) | AI-31 |
| 9. Evals | Proposal scenarios with expected payloads; red-team cases | QA-AI-02/03 |
| 10. Change process | A catalogue change is data, versioned, and **dual-approved** (SOD-8); enabled per organisation, off by default | AI-32, CMP-AI-03 |

## What a proposal stores (§9.5)

`task_type` + `task_version`; `payload` + `payload_hash` (SHA-256 of canonical JSON); `items[]`
(partial approval re-hashes the subset); `rationale` + `evidence_refs[]`; `impact`;
`base_versions`; `risk_tier` + `required_approvals`; `originator` (AI principal, requester,
conversation, model ID, prompt and tool versions); `approvals[]` (approver, time, method, approved
hash); `execution`; `expires_at`.

## The executor

- Runs only approved proposals, from the `proposal_execution` River queue, in **its own worker
  pool and identity** (rev 0.2 fix: never the shared worker that handles integration input).
- Re-validates against current data, checks `base_versions` (stale → `Stale`), refuses any
  payload whose hash differs from the approved hash (AI-P5), and executes **as the approving
  human** with the AI recorded as originator.
- Idempotent (proposal ID as key), atomic (one transaction), and never bypasses period locks,
  approval policies or validations (AI-11).

## Numeric grounding (AI-08)

Every number in an answer or proposal summary must match a value a tool returned in that turn
(after normalising format) or come from the calculator tool. Unmatched numbers are regenerated or
shown as "unverified". Proposal summaries are generated from the payload and impact, not from free
model text. Test: the golden set requires **100%** numeric accuracy.

## Evals and model changes

| Suite | Content | Gate |
|---|---|---|
| QA-AI-01 golden questions | ≥ 200 questions on the sample company with exact expected figures | 100% numeric accuracy |
| QA-AI-02 proposal evals | Expected payloads per task type; precision and recall for categorisation and matching | Thresholds per task type |
| QA-AI-03 red team | Injection strings in bank descriptions, PDFs and emails; attempts to propose never-list actions; exfiltration of excluded fields | 0 successes |

- `make evals` *(P0)* runs in **replay mode** by default: recorded responses keyed by prompt,
  tools and model version; deterministic, offline, no credentials (DEV-02).
- `FH_AI_LIVE=1 make evals` *(P0)* uses a real model (the Claude API with a developer key, or a
  developer's own Google Cloud project) on **synthetic sample data only**. CI runs live evals before
  any model, prompt or tool change (CMP-AI-03), then refreshes the cassettes.
- Model IDs and endpoints are configuration, changed only with dual approval, per ADR-T2 (§12.5):
  Option A (Singapore endpoint) at go-live.

## Kill switches and budgets (AI-13)

The Owner can turn off the assistant, the `propose` tool, or individual task types instantly. Token
budgets per organisation and per user raise alerts. Every turn, tool call and proposal goes to the
AI activity log (AI-12).

## Commits

`feat(ai): add bank.match_payments task type (tier 1)` with the evals that ran in the body.
Catalogue changes also need the dual approval record. Use scope `contracts` for schema-only edits.

## Definition of done

- [ ] Not on the never-list; mapped to one domain command; strict schema; limits and tier set.
- [ ] Dry-run impact complete (ledger lines, balances, checks, diff) and shown in the review UI.
- [ ] Reads go through the semantic layer with on-behalf-of authorisation and AI exclusions.
- [ ] Executor path tested: approval-hash mismatch refused, stale base refused, idempotent replay.
- [ ] QA-AI-01…03 green in replay and, for model, prompt or tool changes, in live mode.
- [ ] CMP-AI-01 still green: AI identities hold no write privilege except proposal insert.
- [ ] Off by default; enabling it is dual-controlled; the AI tasks page lists it.
