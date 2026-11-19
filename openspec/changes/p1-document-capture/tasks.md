## 1. The capture store

- [ ] 1.1 Migration (range 000950 to 000999): `document_capture` under FORCE RLS with its lifecycle guard, the API's and the
  worker's grants, the export catalogue, the schema golden, the data inventory class and its retention; the port
  `Captures` and its Postgres adapter and fakes (design D1). Tests `// req: FR-DOC-14`, an attack test on the guard.
- [ ] 1.2 Registering a capture from an upload or a paste, and adopting the documents of an accepted email message (design
  D2); `POST /v1/captures`, `GET /v1/captures`, `GET /v1/captures/{id}`, dismiss; the authorisation matrix's rows.
  Tests `// req: FR-DOC-12`, `// req: FR-DOC-14`, `// req: FR-AP-04`.

## 2. The reading

- [ ] 2.1 `internal/domain/capture`: the reading's schema, its fields with confidence, the platform's checks (totals,
  dates, registration number form, IRAS particulars, currency) and G4-029's rule (design D5). Tests
  `// req: FR-DOC-12`, `// req: FR-AR-05`, `// req: FR-AP-20`.
- [ ] 2.2 The internal operation and the outbox kind `capture.read`: the AI switch, route and budget, screening before and
  after, the model asked as `document_capture` with the file, no tools and a strict schema; the reading stored as the
  model's, once per document, a person may read again (design D3). Tests `// req: FR-DOC-12`, `// req: AI-18`,
  `// req: AI-28`, `// asvs: V…` on the no-tools, no-write rule.
- [ ] 2.3 Duplicates: the file's hash and the bill's identity, blocked with a link and a reasoned override (design D6).
  Tests `// req: FR-AP-12`.

## 3. The draft

- [ ] 3.1 Draft a bill from a read row, as the person, through the function `ap.draft_bill` shares; the claim likewise
  (design D4). Tests `// req: FR-AP-04`, `// req: FR-AP-12`, `// req: AI-30`.
- [ ] 3.2 Supplier defaults suggested from approved bills and saved as a supplier rule on confirmation (design D7). Tests
  `// req: FR-AP-12`.
- [ ] 3.3 Suggested bank or card lines for a receipt (design D8). Test `// req: FR-DOC-12`.

## 4. The screen, the guide, the evals

- [ ] 4.1 `document-inbox` with its help keys and the guide compiled into `kb/`; Vitest with axe (design D9). Tests
  `// req: FR-DOC-14`.
- [ ] 4.2 The `capture` eval suite and the red team's injected document; the threat model `email-in.md` grows (design D10).
  Tests `// req: QA-AI-02`, `// req: AI-18`.
