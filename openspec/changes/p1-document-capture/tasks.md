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
- [x] 3.3 Suggested bank lines for a read document (design D8): waiting lines of the same amount and currency within five days of its date, the
  supplier's name first, for those who may see bank lines (`matches`; the web shows them). A suggestion only. Card lines are not matched.

## 4. The screen, the guide, the evals

- [x] 4.1 `document-inbox` with its help keys and the guide compiled into `kb/`; Vitest with axe (design D9). Tests
  `// req: FR-DOC-14`. (The requirements the pipeline only partly builds, FR-DOC-12 and FR-AP-12, are not tagged: bank matching,
  dimensions and terms are left.)
  As built: `DocumentInbox` at `/purchases/documents` (linked from Bills): upload and paste, the list by state, the file beside the
  model's reading in the AI's card with each field's confidence and what Fyniq checked, Have it read / Read it again / Dismiss, and the
  bill form filled from the reading (supplier matched, the account always the person's, amounts as decimal strings); no upload or draft
  for those who may not enter bills. The guide is `docs/user-guide/document-inbox.md`, compiled to `kb/pages/task/documents-inbox.md`.
- [x] 4.3 The web form for a draft claim from a receipt, the list of accepted mail waiting to be captured (one click each), and the NRIC
  flag's words on the row. Built with the masker (D3a).
- [ ] 4.4 **Left [live-verify]:** the background reading of mail as it arrives. The existing pattern is the knowledge-base compiler's own service token
  and class in `cmd/worker`, a security-identity addition; until the owner approves a `document_capture` service class the reading is on a
  person's request (D3). The capture evals (4.2) need the eval harness to ask a model a document's text and judge the reading's fields; it asks questions of
  the sample company today.
- [ ] 4.2 The `capture` eval suite and the red team's injected document; the threat model `email-in.md` grows (design D10).
  Tests `// req: QA-AI-02`, `// req: AI-18`.
