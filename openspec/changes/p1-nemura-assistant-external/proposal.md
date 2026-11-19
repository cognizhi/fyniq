# Proposal

## Why

Nemura holds the most personal data the platform keeps: pay, leave, medical certificates, bank accounts. People ask the same questions of it every day (how much leave do I have, when is pay day, what is this line on my payslip, can I reply to this flexible-work request) and HR spends its time answering and drafting. An assistant can take that load, but only on the rules the platform already holds for its AI (PRD-001 §9, PRD-002 §18): it reads through tools as the person asking and never beyond their rights, it states where every figure comes from, it proposes and a named human decides, it is off until the Owner turns it on, and a request's own text can never talk it into more. Nemura is its own image with its own database, so the assistant is built here on the same rules, not imported from Fyniq's (ADR-N1, the archtest).

The other half of this change, the external systems (NM-EXT-01 to 03), is about a company that already has a payroll or HR system: Fyniq records which system holds what, takes its payroll journal as a draft by file or one connector, and an Accountant approves it. That is all Fyniq's side, and `p1-payroll-link` owns it (7.1, 8.1; FR-PPL-01, FR-PPL-05, FR-INT-25 to 27). This change records that and builds only what Nemura owes it: Nemura stays off for the features an external system holds (NM-FEAT), which is already built and gets a test here.

## What Changes

- **The assistant's rules** (NM-AI-01, NM-AI-04, NM-ARC-06): a catalogue of two task types (`people.draft_letter`, `people.draft_fwa_reply`) with strict input schemas, tier 1, 72-hour expiry, off by default; the never-list for people work as words, checked as the catalogue loads and as each proposal is made; per-organisation AI settings the Owner changes (the assistant on or off, the model route, own-pay explanations).
- **Proposals** (NM-AI-02, golden rule 5): `nemura.ai_proposal`, written only by the assistant's role (`nemura_assistant`, insert-only), decided only by a named human with the approver role of the task, applied only by the executor (`nemura_executor`) through the letters domain commands. Every state change is audited with the asker, the approver and the reason.
- **Questions** (NM-AI-02, 03, 06): the person asks about their own leave, pay day, payslip lines and the policy; read tools answer as the asking person; every figure comes from a tool result; statutory rules come from the pack and a rule marked to be verified is answered "to confirm with HR"; the payslip line is explained from the pay item's rule, with the app showing the person's own figures beside it and **no pay figure sent to a model** (own-pay explanations stay off until the Owner turns them on with a second approver, which arrives with the Fyniq link).
- **The model** (NM-AI-01): a port with local mode's scripted model; a real route needs processing evidenced in Singapore for that provider, model and endpoint (AI-88), which only the cloud can show `[live-verify]`.
- **Screens:** `nm-ask` (the person's question), `nm-assistant` (the Owner's settings and the proposals waiting for a human), and, for Fyniq, `hr-import` (`p1-payroll-link` 8.1).

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None (`skip_specs: true`). It builds requirements already in the main specs: `people/assistant` (NM-AI-01 to 04, 06; `people.draft_letter`, `people.draft_fwa_reply`) and `people/external-systems` (NM-EXT-01 to 03, Fyniq's side being `p1-payroll-link`). NM-AI-05 (fairness) and the hiring and review tasks are P2 (`p2-nemura-people`).
