# Proposal

## Why

"Every figure traces to its source" needs the source kept. Bills, claims, payee changes, payment
releases, journals, reconciliations, closes, filings and equity records all carry evidence under the
evidence rules (§8.16, golden rule 7). Evidence is append-only once its parent is final, in storage
nobody can delete before retention ends. Wave 2's documents attach their files through this change,
so it lands in wave 1.

## What Changes

- **Attachments on any record (FR-DOC-01):** previews and malware scanning before a file is attached.
- **Retention-locked storage (FR-DOC-02):** per-object retain-until dates, which are only ever
  extended, legal hold, and no deletion by users (G4-068).
- **Issued documents (FR-DOC-04):** stored as issued, with a hash, so a re-issued copy can be proven
  identical.
- **The Evidence panel (FR-DOC-05):** on every record and important activity, holding several typed
  files with notes and dates.
- **Evidence rules (FR-DOC-06):** required, recommended or optional per activity type, with the
  §8.16 defaults for:
  - purchases, payee changes and payments;
  - journals, reconciliations and close;
  - filings, statutory payments and grant claims;
  - equity and investor records;
  - governance changes and control reviews.
- **Evidence integrity (FR-DOC-07):** the SHA-256, uploader, time, source and parent are written to
  the audit trail in the same transaction. Files can be removed only while the parent is a draft;
  retention-locked files are re-verified nightly.
- **Safe files (FR-DOC-08):**
  - the allowed types, decided from the content, up to 25 MB;
  - ClamAV scanning (SEC-24), HEIC converted, metadata stripped;
  - macros refused, ZIP and XML checked before parsing, safe PDF previews, and files treated as
    untrusted for the AI.
- **Two-phase upload (FR-DOC-13):** a staging area first; the locked store only when the parent is
  approved, posted or completed; orphans swept (G4-069).
- **Data classes (FR-DOC-09, P1 part):** a data class per file, and unmasked NRIC flagged. Redacted
  copies are P2.
- **Evidence search (FR-DOC-10, P1 part):** search by activity, type, date and uploader, and the
  missing-evidence list. Evidence packs are P2.
- **Evidence through the API (FR-DOC-11):** `POST /v1/{collection}/{id}/evidence`, multipart with
  `Idempotency-Key`. AI and MCP clients only propose.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None (`skip_specs: true`). This change builds `documents/attachments-and-evidence`:
- FR-DOC-01, 02, 04…08, 11 and 13;
- FR-DOC-09 and 10, P1 parts;
- the five default-evidence requirements.

The defaults for activities that do not exist yet (pay runs, filings, equity) ship as rules now and
start to apply when their modules land.

## Impact

- **Code:**
  - a `Scanner` port with a ClamAV adapter, run as a Cloud Run service in the cloud and as a container
    in the local stack (§12.2);
  - the `BlobStore` port gains retention operations, through the local directory and fake GCS
    adapters;
  - migrations for files, evidence links, rules and data classes;
  - the API and the screens.
- **Screens:** evidence-panel and evidence-search.
- **Local first:** the retention lock itself, on a real bucket with a locked policy, is proved at the
  cloud gate (§17.5 item 1). Locally the adapter enforces retain-until in code, and its conformance
  test runs against both adapters.
