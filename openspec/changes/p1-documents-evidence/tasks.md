# Tasks

## 1. Storage and scanning

- [x] 1.1 Extend `BlobStore` with `SetRetainUntil`, which only extends, and `SetLegalHold`, and make `Delete` refuse a key under retention. Implement both adapters and extend the conformance suite. Test `// req: FR-DOC-02` (G4-068) (20 Oct 2026: `SetRetainUntil`, `SetLegalHold`, `ErrRetained` on `Delete` and `Put`; the local directory enforces them in code, the Cloud Storage adapter sends Locked retention and a temporary hold and checks them first; `porttest.BlobRetention` runs on the directory, and the emulator test covers all but extension, which fake-gcs-server refuses for a locked retention. **Left:** extension and the bucket's Object Retention Lock itself are proved at the cloud gate, §17.5 item 1)
  (18 Nov 2026, G4-068 for the launch gate: a committed file's retain-until counts from the later of its commit and when
  its record's transactions complete (`evidence.Completion`: the last month a line of an invoice or a bill spreads
  into, else the record's date), so a contract spread over 2027 to 2029 is kept until the end of 2029's financial year
  plus the retention; `evidence.extend`, nightly at 03:50, moves it later when the record now completes later (an
  approved edit lengthening the spread), the locked copy first and then its row (`evidence.retention_extended`,
  audited); the database and the store refuse an earlier date. Tests `TestCompletion`,
  `TestRetentionFollowsTheRecordsObligations`, `TestAContractIsKeptUntilItsTransactionsComplete` (with CTL-AUD-03's
  records check). Guide `evidence-search.md`, compiled.)
- [x] 1.2 Add the `Scanner` port with a ClamAV adapter, an EICAR-flagging fake and the scanner service in the local stack as an optional profile. Test `// req: FR-DOC-01` (scan on upload) (20 Oct 2026: `ports.Scanner`, `local.EICARScanner`, `internal/adapters/clamav` on clamd's INSTREAM, `porttest.Scanner`, and the `scanner` profile in the local stack with `FYNIQ_CLAMD_ADDR`; a failing or missing scanner refuses the file. **Left:** clamd beside the API in the cloud, as a Cloud Run sidecar [verify], and its signature updates)
- [ ] 1.3 Safe-file handling: type from content, 25 MB limit, HEIC conversion, metadata stripping, macro, ZIP and XML checks, safe previews, file names as data. Test `// req: FR-DOC-08` with a fuzz corpus per type (20 Oct 2026, part: the type from the content for PDF, PNG, JPEG, WebP, CSV, text, XLSX, DOCX, EML and MSG; 25 MB; metadata stripped from JPEG, PNG and WebP; macros, ActiveX and legacy OLE refused; the archive limits read from the ZIP directory; file names kept as data; `FuzzStripMetadata` with a seed corpus. **Left:** HEIC conversion (refused until then, rev 0.38), safe PDF previews, the safe name in download headers (no download yet), XML parsing rules once a part is parsed, and a fuzz corpus per type)
  (17 Nov 2026, part: a HEIC photo is refused with its own message, to export it as JPEG (`IsHEIC`, by its file-type
  box); a file name loses its control, line-separating and invisible format characters too (a right-to-left override),
  and a safe name is its own safe name; the safe name is what the download headers carry (`createEvidenceDownload`, from
  the stored name); no part is parsed as XML (the NRIC reader takes the tags out without a parser, so no DTD or entity
  is read; the corpus holds a DOCX declaring an external entity); the fuzz corpus per type: `FuzzCheck`,
  `FuzzCheckArchive`, `FuzzUnmaskedNRIC` and `FuzzSafeName`, seeded with a file of each accepted type and each refused
  one, made in code. Tests `TestHEICIsRefusedWithItsOwnMessage` and the fuzz targets. **Left:** HEIC's conversion and
  safe PDF previews, which wait for the owner's choice of a converter and a renderer)

## 2. Upload, attach, finalise

- [x] 2.1 Two-phase upload: the staging prefix, attaching to a draft, finalising through the outbox into the locked store, and the sweeper. Test `// req: FR-DOC-13` (G4-069) (20 Oct 2026: uploads staged under `staging/<org>/<uuidv7>`; the commit queued in the posting's transaction (`evidence.commit` on the worker) and hourly as a platform job, verified by hash, idempotent; the hourly `evidence.sweep` deletes day-old staging files no attached row uses and reports locked orphans. **Left:** the object generation in the row, which the port does not return yet)
- [x] 2.2 Write the evidence hash, uploader, source and parent to the audit trail in the same transaction; removal only from drafts; append-only once final; nightly re-verification. Test `// req: FR-DOC-07` (20 Oct 2026: migration 000035, FY120–FY124; the audit event carries both hashes, uploader, source and parent; removal only from a draft, append-only after, never deleted, retain-until only later; `evidence.verify` at 03:30; CTL-AUD-06's checks running. **Left:** supersede with a reason, and C.2 #31 as a check once the checks engine exists)
- [x] 2.3 Issued documents stored with their hash, and proof that a re-issued copy is identical. Test `// req: FR-DOC-04` **Left:** with the issued documents of `p1-sales`
  (6 Nov 2026, PRD-001 rev 0.191: an invoice's, quote's, order's or statement's PDF is its evidence with its SHA-256 from
  its first sending (`salesmail`); every later sending of that version, not only a reminder, sends that file with its
  template (`sentPDF`), never a new rendering. Test `TestInvoicesAndQuotesAreEmailed`: sent twice, both sends name the
  same evidence and hash, and the file opened hashes to it. Report exports record their hash (FR-RPT-09); Nemura's
  payslips come with Nemura.)

## 3. Rules, classes and search

- [ ] 3.1 Evidence rules per activity type as FR-ORG-08 settings. Required rules block and recommended rules warn. Test `// req: FR-DOC-06`, and the five defaults `// req: documents/attachments-and-evidence Default evidence for <area>`, each with a fixture per activity that exists today; and C.2 #30, completed activities missing recommended evidence, on the checks engine (moved from p1-checks, rev 0.166: a check is built with the module it guards). Test `// req: C.2 #30` **Left**
  (6 Nov 2026, PRD-001 rev 0.182, on the owner's decisions of the same day: the defaults enforced now, and each
  activity's level a setting. `internal/domain/evidence/rules.go`: bills and manual journals above
  `evidence.journal_threshold` (S$1,000.00), each `evidence.<activity>` required, recommended or optional under dual
  control; a missing required file refuses the post, its request for approval and the approved post (DOC-10);
  `EvidenceFiles.Counts`. C.2 #30 (`evidence.missing`) lists the open months' posted records without their evidence
  while it is required or recommended. Expense claims keep FR-AP-15's thresholds; payee bank changes, founders' costs
  and settlements already require their files; FM-5 and the close checks hold the reconciliation and the close. The
  tests' books relax the two rules (`relaxEvidence`). Tests `TestEvidenceRulesHoldBillsAndJournals`. **Left:** the
  rules of activities not built yet (filings, CPF, share transfers, instruments, investor records, grants) with their
  modules; the PRD table's second recommended items (a business purpose note, a call-back note, an explanation).)
  (18 Nov 2026, PRD-001 rev 0.291: the table is data. `evidence.Catalogue` holds every row of §8.16 with its required and
  recommended items and the kinds of file that meet them; `GET /v1/evidence-rules` (`getEvidenceRules`, any member)
  answers it with the level in force for bills, manual journals and filings (CPF payments are filings), and the Evidence
  panel's **Rules for this activity** shows each item as ✖, ⚠ or ✔ against the files attached. The five defaults are
  tagged on `TestEvidenceRulesAreTheDefaults`, which pins every row of the table per area, built or not; the enforcement
  of the activities that exist today is `TestEvidenceRulesHoldBillsAndJournals` and the modules' own refusals (payee
  bank changes, claims, founders' costs, settlements and the equity register ask for their files before approval).
  **Left:** the rules of activities with no module (payment-batch release's bank confirmation, grant claims,
  governance changes, control evidence, investor record changes) apply with their modules; the second recommended items
  do not list on Checks (C.2 #30 lists the three configurable activities), only warn in the panel.)
- [x] 3.2 Data classes per file, with unmasked NRIC flagged. Test `// req: FR-DOC-09` (P1 part) (20 Oct 2026, part: a data class per file; payroll and identity refused until FR-PAY-13, rev 0.38. **Left:** the NRIC flag)
  (6 Nov 2026, PRD-001 rev 0.183: the NRIC flag. `evidence.UnmaskedNRIC` reads the text of plain text, CSV, email,
  XLSX and DOCX files and a PDF's uncompressed text; an S, T, F or G number counts only with its right check letter,
  an M number on its shape alone [verify]. The flag is kept on the file (migration 000237, `nric_flagged`), audited,
  answered as `unmasked_nric` on every record's evidence, and shown as **Shows an NRIC** in the Evidence panel. Tests
  `TestUnmaskedNRICsAreFound`, `TestAnUnmaskedNRICIsFlagged`, `JournalEvidence.test.tsx`. The redaction tool is P2.)
- [x] 3.3 Evidence search by activity, type, date and uploader, and the missing-evidence list, under the person's scope. Test `// req: FR-DOC-10` (P1 part) **Left**
  (6 Nov 2026, PRD-001 rev 0.184: `GET /v1/evidence` (`searchEvidence`, the evidence right without a qualifier) lists
  the files on the records whose Appendix D rows the person views, removed ones aside, by activity, evidence type, the
  record's date and uploader, newest record first, paged; `EvidenceFiles.Search`. The missing-evidence list is C.2 #30
  (`evidence.missing`). Screen: Accounting › Evidence (`/compliance/evidence`), guide `evidence-search.md`, compiled.
  Tests `TestEvidenceIsFoundByActivityTypeDateAndUploader`, `EvidenceSearch.test.tsx`. Packs are P2.)

## 4. API and screens

- [x] 4.1 Add `POST /v1/{collection}/{id}/evidence` (multipart, `Idempotency-Key`) to the contract; AI and MCP clients can only propose. Test `// req: FR-DOC-11` (20 Oct 2026: journals first: `addJournalEvidence`, `listJournalEvidence`, `getJournalEvidence`, `removeJournalEvidence`; the Evidence capability of Appendix D with the right to view the journal; AI and MCP clients refused. **Left:** the other collections as they arrive, and a download with a safe name)
- [ ] 4.2 Build the Evidence panel (several typed files with notes and dates) and evidence-search as designed. Tests `// req: FR-DOC-05` and `// req: FR-DOC-01` (attach and preview); help keys and `docs/user-guide/` pages compiled into `kb/` **Left**
  (6 Nov 2026, PRD-001 rev 0.188: opening a file. `GET /v1/evidence/{evidence_id}/file` sends a file in base64 with its
  name as data, read from staging or, once final, from the locked store (the API's `Evidence` store, read only;
  `[live-verify]` its grant in the cloud), checked against its SHA-256 and audited (`evidence.opened`); the record's
  own row decides past the policy, as the search does, or the person's own claim or bank change (`authz.Policy.ByRecord`,
  which the matrix reads as not found). The panel's **Show** puts a picture inline; **Save** saves any other file under
  `safeName`. Guide `evidence-search.md` (Open a file), `journals.md`. Tests `TestAnEvidenceFileOpensForWhoeverSeesItsRecord`,
  `JournalEvidence.test.tsx`. **Left:** PDF previews rendered to images (FR-DOC-08), the evidence-search screen's link
  to each file.)
  (17 Nov 2026: the evidence-search screen opens each file from its row, a picture shown below the list and any other
  file saved under a safe name (`openEvidence`, shared with the panel); it searches the equity register's, the
  statutory filings' and the GST forecasts' files too, which the contract's filters now name, so a filing's
  acknowledgement opens from its panel; **Only files I attached** filters by uploader; **Show more** pages; a filing
  missing its acknowledgement is listed as one. Tests `TestAFilingsAcknowledgementIsFoundAndOpens`,
  `EvidenceSearch.test.tsx`; guide `evidence-search.md`, compiled. **Left:** PDF previews rendered to images, with 1.3)
- [ ] 4.3 Finish with `make check` and `make test`
