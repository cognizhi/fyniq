# Tasks

## 1. Storage and scanning

- [x] 1.1 Extend `BlobStore` with `SetRetainUntil`, which only extends, and `SetLegalHold`, and make `Delete` refuse a key under retention. Implement both adapters and extend the conformance suite. Test `// req: FR-DOC-02` (G4-068) (20 Oct 2026: `SetRetainUntil`, `SetLegalHold`, `ErrRetained` on `Delete` and `Put`; the local directory enforces them in code, the Cloud Storage adapter sends Locked retention and a temporary hold and checks them first; `porttest.BlobRetention` runs on the directory, and the emulator test covers all but extension, which fake-gcs-server refuses for a locked retention. **Left:** extension and the bucket's Object Retention Lock itself are proved at the cloud gate, §17.5 item 1)
- [x] 1.2 Add the `Scanner` port with a ClamAV adapter, an EICAR-flagging fake and the scanner service in the local stack as an optional profile. Test `// req: FR-DOC-01` (scan on upload) (20 Oct 2026: `ports.Scanner`, `local.EICARScanner`, `internal/adapters/clamav` on clamd's INSTREAM, `porttest.Scanner`, and the `scanner` profile in the local stack with `FYNIQ_CLAMD_ADDR`; a failing or missing scanner refuses the file. **Left:** clamd beside the API in the cloud, as a Cloud Run sidecar [verify], and its signature updates)
- [ ] 1.3 Safe-file handling: type from content, 25 MB limit, HEIC conversion, metadata stripping, macro, ZIP and XML checks, safe previews, file names as data. Test `// req: FR-DOC-08` with a fuzz corpus per type (20 Oct 2026, part: the type from the content for PDF, PNG, JPEG, WebP, CSV, text, XLSX, DOCX, EML and MSG; 25 MB; metadata stripped from JPEG, PNG and WebP; macros, ActiveX and legacy OLE refused; the archive limits read from the ZIP directory; file names kept as data; `FuzzStripMetadata` with a seed corpus. **Left:** HEIC conversion (refused until then, rev 0.38), safe PDF previews, the safe name in download headers (no download yet), XML parsing rules once a part is parsed, and a fuzz corpus per type)

## 2. Upload, attach, finalise

- [x] 2.1 Two-phase upload: the staging prefix, attaching to a draft, finalising through the outbox into the locked store, and the sweeper. Test `// req: FR-DOC-13` (G4-069) (20 Oct 2026: uploads staged under `staging/<org>/<uuidv7>`; the commit queued in the posting's transaction (`evidence.commit` on the worker) and hourly as a platform job, verified by hash, idempotent; the hourly `evidence.sweep` deletes day-old staging files no attached row uses and reports locked orphans. **Left:** the object generation in the row, which the port does not return yet)
- [x] 2.2 Write the evidence hash, uploader, source and parent to the audit trail in the same transaction; removal only from drafts; append-only once final; nightly re-verification. Test `// req: FR-DOC-07` (20 Oct 2026: migration 000035, FY120–FY124; the audit event carries both hashes, uploader, source and parent; removal only from a draft, append-only after, never deleted, retain-until only later; `evidence.verify` at 03:30; CTL-AUD-06's checks running. **Left:** supersede with a reason, and C.2 #31 as a check once the checks engine exists)
- [ ] 2.3 Issued documents stored with their hash, and proof that a re-issued copy is identical. Test `// req: FR-DOC-04` **Left:** with the issued documents of `p1-sales`

## 3. Rules, classes and search

- [ ] 3.1 Evidence rules per activity type as FR-ORG-08 settings. Required rules block and recommended rules warn. Test `// req: FR-DOC-06`, and the five defaults `// req: documents/attachments-and-evidence Default evidence for <area>`, each with a fixture per activity that exists today **Left**
- [ ] 3.2 Data classes per file, with unmasked NRIC flagged. Test `// req: FR-DOC-09` (P1 part) (20 Oct 2026, part: a data class per file; payroll and identity refused until FR-PAY-13, rev 0.38. **Left:** the NRIC flag)
- [ ] 3.3 Evidence search by activity, type, date and uploader, and the missing-evidence list, under the person's scope. Test `// req: FR-DOC-10` (P1 part) **Left**

## 4. API and screens

- [x] 4.1 Add `POST /v1/{collection}/{id}/evidence` (multipart, `Idempotency-Key`) to the contract; AI and MCP clients can only propose. Test `// req: FR-DOC-11` (20 Oct 2026: journals first: `addJournalEvidence`, `listJournalEvidence`, `getJournalEvidence`, `removeJournalEvidence`; the Evidence capability of Appendix D with the right to view the journal; AI and MCP clients refused. **Left:** the other collections as they arrive, and a download with a safe name)
- [ ] 4.2 Build the Evidence panel (several typed files with notes and dates) and evidence-search as designed. Tests `// req: FR-DOC-05` and `// req: FR-DOC-01` (attach and preview); help keys and `docs/user-guide/` pages compiled into `kb/` **Left**
- [ ] 4.3 Finish with `make check` and `make test`
