## Context

Built already: the safe file store with scan and retention (`p1-documents-evidence`), the email-in addresses of the
organisation and of each member, and the messages received at them, with their senders, SPF/DKIM/DMARC, quarantine and
accept or reject (`internal/api/capture.go`, `inbound.go`), claim receipts forwarded to a claim
(`claim_evidence_message.go`), the bill and the claim, a duplicate warning on a bill (FR-AP-12's block was left for this
change), the model port with PDF and image input and a strict output schema (`internal/ports/model.go`), the router with
its agents (`document_capture` exists in migration 000356) and the kb compiler's pattern of an internal operation that
routes a model call. The AI proposal path is for AI-originated writes; capture's reading is a read.
Decisions here are on the recommended option while the owner is away, owner to confirm (PRD-001 rev 0.370).

## Goals / Non-Goals

**Goals:**
- A captured file becomes a draft bill or claim with one action, with each field's confidence and the platform's checks.
- The AI reads untrusted files and holds no write.
- Duplicates blocked; supplier defaults suggested and saved on confirmation.

**Non-Goals:**
- The camera, statements, contracts as structured records and InvoiceNow inbound (P2).
- A draft made at arrival with nobody present (D4).

## Decisions

**D1. The capture row and its statuses (FR-DOC-14).**
- `document_capture`: one row per file, `organisation_id` under FORCE RLS: the stored file (the existing file store's ID),
  its SHA-256, its source (`upload`, `paste`, `email`), the inbound message and member when from email, who captured it,
  the kind the reading classified (bill, receipt, sales invoice, other), the status, the reading, the checks, the
  duplicate link, the draft made (record kind and ID), and the times.
- Statuses: received, reading, read, duplicate, drafted, dismissed, failed. The guard trigger allows only the lifecycle's
  moves; the file and its hash never change; a delete is refused (a dismiss is a status, the file keeps its retention).
- The reading is kept as JSON apart from the file, with its model, prompt and schema versions and its screening result.

**D2. Sources in P1: upload, paste, accepted email.**
- Upload and paste use the existing safe upload (type from content, size limit, scanned) and then register the stored file
  as a capture. An email's documents are adopted, by a person's action (`POST /v1/captures/from-message`), from an `accepted`
  message of kind `documents` at the organisation's address or their own (a quarantined or rejected message yields nothing; a
  member's own mail is only theirs). Each file is adopted once.
- Camera, statements, contracts and InvoiceNow are P2 (FR-DOC-12's phase column).

**D3. The reading is a read through `document_capture` (AI-18, AI-28, AI-74), on a person's request.**
- (As built; the first plan was a worker job with its own service token.) `POST /v1/captures/{id}/read` runs the reading as the
  signed-in person, in the API: the AI switch, the route of the agent `document_capture` and the budgets apply as for any model call
  (AI-74, AI-13), the call is the person's in the AI activity log, and the model gets the file as a PDF or an image part with **no
  tools** and a strict output schema (each field a value, null when the document does not show it, and a confidence; amounts as decimal
  strings). The prompt is `document_capture@1`: the document is data, never instructions.
- The file's text (a PDF's, by `pdftext`) and its name are screened before the call and the answer after (Model Armor in the cloud, the
  local screen offline), so a document that makes the model repeat an instruction is stopped and fails with why. An image's pixels cannot
  be screened as text: its answer is `[live-verify]` against Model Armor's image support.
- The answer is parsed strictly (`capture.Parse`: the shape and nothing more, a kind it knows, decimal strings, confidences from 0 to 1),
  checked by platform code (D5) and kept as the model's reading. A document is read when a person asks, and read again only when a person
  asks again; a failure is a status with a plain note, never a retry loop.
- A background reading of mail as it arrives (a worker job with its own service token, as the knowledge-base compiler has) is left for
  later: the inbox lists what is waiting and the person reads it. Data class: business figures and personal data, so the route's
  Singapore-only rule for sensitive classes (AI-75) applies.

**D4. A person makes the draft; nothing at arrival with nobody present.**
- The inbox's action Draft runs as the signed-in person who may enter bills (or their own claim): the API validates the
  reading as they corrected it exactly as a person's own entry (same checks, same audit, the capture as source) and makes
  the draft through the shared function `ap.draft_bill` and `exp.draft_claim` use (p1-ai-proposals D3).
- Why not a proposal at arrival: a proposal needs a requester authenticated by a live turn or a run (AI-89, rev 0.280 and
  0.281); a capture from an email arrives with nobody present, so it would be a new exception on the AI write path. That
  is security posture and the owner's decision. Options for the owner: (a) as built, a person's one action (no new
  exception; one click per document); (b) a third requester authentication `ai_capture` for the member who owns the
  address (personal addresses only), with its database check and attack tests; (c) both, (b) for personal addresses.
  Recommendation: (a) now, (b) once the evals show the readings are right.
- Through the chat, a person can still ask the assistant to draft a bill from a document, which uses the proposal path
  (`ap.draft_bill`) with their live turn.

**D5. The platform's checks (FR-AR-05, FR-AP-20, G4-029).**
- On the reading, platform code (not the model) checks: lines sum to the totals within RND-GST-EXCL; the dates parse and
  are plausible; the GST registration number's form (M + digits, and the UEN's form); the IRAS tax-invoice particulars
  the document lacks (FR-AR-05 for a document claimed as input tax); the currency is an organisation currency; and
  lowers a field's confidence, never raises it. A field below 0.80 (a setting, default) is highlighted.
- Capture proposes the input-tax facts; eligibility stays "to confirm" until the approver confirms it (FR-AP-20, G4-029):
  the draft is made with the facts a person confirmed, never from an extracted GST amount alone.

**D6. Duplicates are blocked (FR-AP-12).**
- The file's SHA-256 matches another capture: the row is `duplicate` with a link. The bill's identity (supplier, number,
  total, date within the bill domain's `DuplicateWindow`) matches an existing bill or capture: blocked with a link; a
  person may override with a reason (audited), as for any duplicate warning.

**D7. Supplier defaults (FR-AP-12).**
- From the supplier's approved bills the inbox suggests the expense account, GST code, dimensions and terms most used;
  a person confirming them saves a supplier rule (`supplier_rule`, versioned, audited), applied as the draft's defaults.

**D8. Matching a receipt to bank and card lines.**
- A suggestion only: unreconciled bank lines of the same amount and currency within 5 days of the receipt's date, ranked
  by merchant-name similarity; shown in the inbox row, and followed by a person in the bank screen (FR-BNK).

**D9. Screens, help and the knowledge base.**
- `document-inbox` at `/inbox/documents`: the list by status, the reading beside the original, highlighted low
  confidence, the checks, Draft, Dismiss, Read again, the capture addresses with copy buttons. Basic shows the list and
  Draft; Expert the reading's confidences and checks. Help keys, the guide `docs/user-guide/document-inbox.md` compiled
  into `kb/`.

**D10. Evals and red team (QA-AI-02, AI-18).**
- A `capture` suite of synthetic documents (a receipt, a bill with GST, a foreign-currency bill, a blurry one, a
  document that carries an injection) judges the reading's fields exactly, in replay mode; real models are
  `[live-verify]`. The red team: an instruction in a PDF must change nothing and the reading must stay data.

## Risks / Trade-offs

- [A model misreads a figure] -> every field has its confidence, the platform's checks run, a person confirms the draft,
  and the draft's own approval policy applies before anything posts.
- [Injected text in a file] -> no tools and no write for the reading; its output is data; the red team proves it.
- [Cost of reading every file] -> once per document, the budgets and the AI switch apply; a failure is a status, not a loop.

## Migration Plan

Additive: a table, grants, a job kind, operations, a screen. Nothing reads until the AI is on for the organisation and
the agent has a route. Rolling back removes the job and operations; captured files stay in the store.

## Open Questions

- Owner: D4's options (a), (b), (c) above.
