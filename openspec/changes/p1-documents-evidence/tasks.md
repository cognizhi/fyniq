# Tasks

## 1. Storage and scanning

- [ ] 1.1 Extend `BlobStore` with `SetRetainUntil`, which only extends, and `SetLegalHold`, and make `Delete` refuse a key under retention. Implement both adapters and extend the conformance suite. Test `// req: FR-DOC-02` (G4-068)
- [ ] 1.2 Add the `Scanner` port with a ClamAV adapter, an EICAR-flagging fake and the scanner service in the local stack as an optional profile. Test `// req: FR-DOC-01` (scan on upload)
- [ ] 1.3 Safe-file handling: type from content, 25 MB limit, HEIC conversion, metadata stripping, macro, ZIP and XML checks, safe previews, file names as data. Test `// req: FR-DOC-08` with a fuzz corpus per type

## 2. Upload, attach, finalise

- [ ] 2.1 Two-phase upload: the staging prefix, attaching to a draft, finalising through the outbox into the locked store, and the sweeper. Test `// req: FR-DOC-13` (G4-069)
- [ ] 2.2 Write the evidence hash, uploader, source and parent to the audit trail in the same transaction; removal only from drafts; append-only once final; nightly re-verification. Test `// req: FR-DOC-07`
- [ ] 2.3 Issued documents stored with their hash, and proof that a re-issued copy is identical. Test `// req: FR-DOC-04`

## 3. Rules, classes and search

- [ ] 3.1 Evidence rules per activity type as FR-ORG-08 settings. Required rules block and recommended rules warn. Test `// req: FR-DOC-06`, and the five defaults `// req: documents/attachments-and-evidence Default evidence for <area>`, each with a fixture per activity that exists today
- [ ] 3.2 Data classes per file, with unmasked NRIC flagged. Test `// req: FR-DOC-09` (P1 part)
- [ ] 3.3 Evidence search by activity, type, date and uploader, and the missing-evidence list, under the person's scope. Test `// req: FR-DOC-10` (P1 part)

## 4. API and screens

- [ ] 4.1 Add `POST /v1/{collection}/{id}/evidence` (multipart, `Idempotency-Key`) to the contract; AI and MCP clients can only propose. Test `// req: FR-DOC-11`
- [ ] 4.2 Build the Evidence panel (several typed files with notes and dates) and evidence-search as designed. Tests `// req: FR-DOC-05` and `// req: FR-DOC-01` (attach and preview); help keys and `docs/user-guide/` pages compiled into `kb/`
- [ ] 4.3 Finish with `make check` and `make test`
