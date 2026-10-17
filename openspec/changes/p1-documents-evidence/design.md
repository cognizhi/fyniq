# Design

## Context

- `ports.BlobStore` exists, with `Put`, `Create` (write-once), `Get`, `List` and `Delete`, through a
  local directory or fake GCS (`deploy/local/compose.yaml`). P0's audit anchors already use `Create`.
- `fyniq.document` and `document_version` (000005) keep documents with immutable versions.
- There is no scanner.

## Goals / Non-Goals

**Goals:**
- A file reaches a final record only after a clean scan, with its hash audited in the same
  transaction, in a store from which nobody can delete it before retain-until.

**Non-Goals:**
- Redaction and evidence packs (P2).
- Inbound email capture (`p1-templates-email` feeds files in through this change's upload path).

## Decisions

- **Two stores, two phases.**
  - Uploads go to a staging prefix with no lock. After a clean scan the file is attached to the
    draft parent and stays in staging.
  - When the parent becomes final, the outbox copies it to the locked store, sets retain-until from
    the object's retention rule (§11.9), verifies the hash and marks the link final.
  - A sweeper removes staging objects with no row, and reports locked objects with no row as
    incidents, never deleting them (FR-DOC-13).
  - Alternative considered: writing straight to the locked store. Rejected because a rolled-back
    posting would leave an undeletable file (G4-069).
- **Retention in the port.**
  - `BlobStore` gains `SetRetainUntil(key, time)`, which can only extend, and `SetLegalHold(key,
    bool)`. `Delete` refuses a key under retention.
  - The GCS adapter maps these to object retention and holds; the local adapter enforces them in
    code. One conformance suite tests both (NFR-PORT-04).
  - The database's retain-until is authoritative, and object metadata never is (FR-DOC-02).
- **A scanner port.**
  - `Scanner.Scan(r) (verdict, signature)`.
  - The ClamAV adapter speaks clamd's protocol to the scanner service. Unit tests use a fake that
    flags the EICAR test string.
  - Content-type sniffing, HEIC conversion, metadata stripping, macro and ZIP checks, and XML without
    DTDs run in the API before the scan, on bounded memory.
- **Evidence rules as data.** Rules per activity type ship as FR-ORG-08 settings with their
  recommended values. A required rule blocks completion with a plain message (ENG-23); a recommended
  rule shows ⚠.
- **Hashes.**
  - SHA-256 of the original and of each derived file (the converted JPEG, the stripped PDF).
  - The audit event carries both. The nightly job re-hashes locked objects and raises an incident on
    a mismatch (FR-DOC-07).
- **Ports and adapters (ADR-T8):** `BlobStore` (local, GCS; extended), `Scanner` (new: ClamAV, fake),
  storage, queue.
- **Audit events (AUD-01):**
  - file uploaded, scanned, attached, removed from a draft, finalised, superseded;
  - retain-until extended, legal hold set or cleared;
  - hash re-verification failed.

## Risks / Trade-offs

- [ClamAV's signature database is large, which strains the image budget and cold starts] → The
  scanner is its own service outside the Go image budgets (§12.9), and signatures update on a
  schedule. Locally it is an optional profile in the stack, and tests use the fake.
- [Hostile files reach a parser] → Every parser runs with size and time limits, file names are treated
  as data, and the fuzz cases cover each allowed type.
