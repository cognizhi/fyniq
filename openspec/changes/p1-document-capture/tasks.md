## 1. The capture store

- [x] 1.1 Migration (range 000950 to 000999): `document_capture` under FORCE RLS with its lifecycle guard, the API's and the
  worker's grants, the export catalogue, the schema golden, the data inventory class and its retention; the port
  `Captures` and its Postgres adapter and fakes (design D1). Tests `// req: FR-DOC-14`, an attack test on the guard.
- [x] 1.2 Registering a capture from an upload or a paste, and adopting the documents of an accepted email message (design
  D2); `POST /v1/captures`, `GET /v1/captures`, `GET /v1/captures/{id}`, dismiss; the authorisation matrix's rows.
  Tests `// req: FR-DOC-12`, `// req: FR-DOC-14`, `// req: FR-AP-04`.
  As built: nine operations under "Document inbox" (capture, capture from message, list, get, file, read, dismiss, draft a bill, draft a
  claim), all for those who may enter bills (a claim's draft also needs the claim right), none for the AI; the file is downloaded as an
  attachment, never inline. The same file again is a `duplicate` of the first.

## 2. The reading

- [x] 2.1 `internal/domain/capture`: the reading's schema, its fields with confidence, the platform's checks (totals,
  dates, registration number form, IRAS particulars, currency) and G4-029's rule (design D5). Tests
  `// req: FR-DOC-12`, `// req: FR-AR-05`, `// req: FR-AP-20`.
- [x] 2.2 The reading (design D3, as built: on a person's request, not a worker job): the AI switch, route and budget, screening
  before and after, the model asked as `document_capture` with the file, no tools and a strict schema; the reading stored as the
  model's, a person may read again. Tests `// req: FR-DOC-12`, `// req: AI-18`, `// asvs: V8.2.2` on the no-tools rule and the
  injected document stopped by screening. **Left:** the background reading of mail as it arrives (a worker job and its service token).
- [x] 2.3 Duplicates: the file's hash and the bill's identity, blocked with a link and a reasoned override (design D6).
  Tests `// req: FR-AP-12`.

## 3. The draft

- [x] 3.1 Draft a bill from a read row, as the person, through the function `ap.draft_bill` shares; the claim likewise
  (design D4). Tests `// req: FR-AP-04`, `// req: FR-AP-12`, `// req: AI-30`.
- [x] 3.2 Supplier defaults suggested from approved bills and saved as a supplier rule on confirmation (design D7). Tests
  `// req: FR-AP-12`.
  As built (design D7, revised): no new table. A supplier's rule is its contact's own default account and GST code (columns it already
  has): the read capture suggests them from the supplier's posted bills (the contact's own first), and a person who confirms the draft
  with `save_defaults` keeps the first line's as the contact's defaults, as them, where it has none (a default it holds is never replaced).
  The terms and dimensions the PRD names stay with the contact's own fields; the web form pre-selects the suggested account for the person
  to check.
- [ ] 3.3 Suggested bank or card lines for a receipt (design D8). Test `// req: FR-DOC-12`.

## 4. The screen, the guide, the evals

- [x] 4.1 `document-inbox` with its help keys and the guide compiled into `kb/`; Vitest with axe (design D9). Tests
  `// req: FR-DOC-14`.
  As built: `DocumentInbox` at `/purchases/documents` (linked from Bills): upload and paste, the list by state, the file beside the
  model's reading in the AI's card with each field's confidence and what Fyniq checked, Have it read / Read it again / Dismiss, and the
  bill form filled from the reading (supplier matched, the account always the person's, amounts as decimal strings); no upload or draft
  for those who may not enter bills. The guide is `docs/user-guide/document-inbox.md`, compiled to `kb/pages/task/documents-inbox.md`.
  **Left:** the claim form for a receipt (the API's `draftClaimFromCapture` is built and tested), the pending-mail list with
  one-click adoption, and the matches to bank or card lines (3.3).
- [ ] 4.2 The `capture` eval suite and the red team's injected document; the threat model `email-in.md` grows (design D10).
  Tests `// req: QA-AI-02`, `// req: AI-18`.
