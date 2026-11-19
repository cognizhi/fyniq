## Why

Bills and receipts arrive as files: a PDF on a supplier's email, a photo of a receipt, a screenshot. The people who run a
small company's books should not retype them. `p1-purchases` and `p1-expense-claims` built the bill and the claim by hand,
`p1-documents-evidence` the safe, scanned, retention-locked file store, and the email-in addresses and received mail were
built with `p1-documents-evidence` and `p1-expense-claims` (forwarded receipts). What is missing is the pipeline between a
captured file and a draft: classify it, read its fields with a confidence each, check it, find duplicates, and let a person
turn it into a draft in one action. The plan (`docs/prd/plans/p1-p3-build-plan-2026-10-16.md`) put this in wave 6, after
go-live. The owner is away until 21 Nov 2026, so it is built now, locally, on the recommended option (PRD-001 rev 0.370,
owner to confirm).

## What Changes

- **The document inbox (FR-DOC-12, FR-DOC-14; screen `document-inbox`)**: one row per captured file, from an upload, a
  paste or an accepted email-in message, with its status (received, reading, read, duplicate, drafted, dismissed,
  failed), the model's reading kept apart from the original, the checks, the duplicates and the possible bank matches.
- **The reading (FR-DOC-12, AI-18, AI-28)**: a read through the agent `document_capture` with the file as PDF or image
  input, no tools, a strict output schema and each field's confidence; screened before and after, within the AI switch
  and the budgets, at most once per document; the text it returns is untrusted data. The platform adds its own checks:
  the lines add up to the totals, the IRAS tax-invoice particulars the document lacks (FR-AR-05), the GST registration
  number's form, and a lower confidence where a check fails.
- **Duplicates (FR-AP-12)**: the same file (by its SHA-256) and the same bill (supplier, number, amount, date window) are
  blocked, with a link to the other.
- **The draft (FR-AP-04, FR-AP-12)**: a person who may enter bills or claims makes the draft with one action from the
  reading as they corrected it, through the same function `ap.draft_bill` and `exp.draft_claim` use (their task types are
  built by `p1-ai-proposals`). Supplier defaults (account, GST code, terms) are suggested from that supplier's approved
  bills and saved as a supplier rule once a person confirms them. The draft follows the ordinary approval policy (AI-30)
  and its input-tax facts stay "to confirm" (FR-AP-20, G4-029).
- **Receipt matching (FR-DOC-12)**: a receipt's amount, date and merchant suggest the card or bank lines it may match.
- **Not in this change**: the camera, statements and InvoiceNow inbound (P2); a proposal made when a document arrives
  with nobody present (design D4, an owner decision).

## Capabilities

### New Capabilities

None: the requirements are in the main specs (`documents/attachments-and-evidence`, `purchases/payables`). This change
builds them (`skip_specs: true`).

### Modified Capabilities

None.

## Impact

- **Requirements built** (PRD-001 §8.16, §8.6; rev 0.370): FR-DOC-12, FR-DOC-14, FR-AP-04, FR-AP-12.
- **Screens**: `document-inbox` (`docs/design/screens/inventory.json`), its fragment updated locally.
- **Code**: a migration for the capture store (under FORCE RLS, in the export catalogue, with its retention); a port and
  an adapter; `internal/domain/capture` (fields, checks, duplicates, matching); the API's capture operations and the
  internal read operation the worker calls; an outbox kind for the reading; the web app's inbox.
- **Security boundary**: the reading is the AI's input from untrusted files (AI-18), so its red-team cases and the
  threat model `docs/security/threat-models/email-in.md` grow. The AI gains no write: the draft is a person's action.
- **Depends on** `p1-ai-providers`, `p1-purchases`, `p1-documents-evidence`. **Uses** `p1-ai-proposals` (the draft
  functions are shared with its task types `ap.draft_bill` and `exp.draft_claim`).
