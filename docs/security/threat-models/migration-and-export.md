# Threat model: the workbook migration, the generic import and the full tenant export

| | |
|---|---|
| Feature and requirement IDs | The workbook import and its migration steps M1 to M6, the generic CSV/XLSX import and the full tenant export: FR-MIG-01, FR-MIG-02, FR-MIG-03, FR-MIG-04, AUD-05, FM-1, SEC-24 (PRD-001 rev 0.255 to 0.273; `p1-workbook-migration`, design D13) |
| Author, reviewer, date | Claude Code for the owner, 13 Nov 2026; second review waived before production (AGENTS.md), held with `needs-human-review` if wanted |
| Data and its class | The organisation's whole books: its chart, contacts, documents, payroll summaries, cap table and audit trail (confidential business data, some personal); vendors' bank account numbers and released payment files (sealed, SEC-11); the uploaded workbook and import files (untrusted) |
| Who acts | Members with C on "Workbook import (migration)" (upload, map, ask to commit), A on it (an Owner who did not ask approves the commit), A on "Workbook import sign-offs" (the Accountant signs the mapping, the register and the parallel month); members with each entity's C (the generic import); C on "Tenant export" (asks, saves), its A (approves); the worker (validates, commits, writes the export); anyone holding a 5-minute export link; the AI and MCP clients (none of these operations) |

## 1. What we are building

**The workbook import** (FR-MIG-01, §17.1). A person uploads the founders' finance workbook into a new organisation
that has posted nothing. The file is an untrusted upload: its type is decided from its content, it is scanned for
malware, and it is read within bounds (entries, sizes, rows and columns of the ZIP and its sheets). Only cached values
are read; a formula is never evaluated, and a workbook saved without its calculated values is refused. The validation
report (M2) names every problem by sheet, row and column and commits nothing. The Accountant signs the mapping onto
Fyniq's chart; the commit (M3) is an approval request that an Owner who did not ask approves, stepped up (FM-1); its
batches then enter each row through the command that enters it by hand, each record naming its row. The parity
report and its difference register (M4), the parallel month (M5) and the archive (M6) follow, each signed by the
Accountant; at M6 the migration's files are copied write-once to the retention-locked evidence store and the
migration closes, read only.

**The generic import** (FR-MIG-03). A file filled in an entity's template, CSV or XLSX, is the same untrusted upload,
of at most 1,000 rows. It is imported under the entity's own right C, each row through its command, in one unit of
work: a refused row refuses the whole file. Journals, invoices and bills are made drafts, posted later through their
own approvals; opening balances and budgets are drafts too. `Prefer: return=preview` reports and saves nothing.

**The full tenant export** (FR-MIG-04, design D10). A person with C on "Tenant export" asks; the answer is an approval
request, which another person who holds its A approves (FM-1). The approval queues the worker, which reads the
organisation through two `SECURITY INVOKER` database functions under its own grants and row-level security, and writes
one ZIP: every table of the catalogue as CSV, every document version, the evidence and released payment files, the
audit trail with its hashes, a README and a manifest of every file's SHA-256. Vendors' bank account numbers and the
payment files are opened into clear [counsel to confirm]. The file stays in the staging store for 24 hours. Only its
requester gets a link that saves it, valid for 5 minutes: a signed URL of the stored object in the cloud, or a signed
path of the API's own in an installation whose store cannot sign.

Trust boundaries: the internet ↔ API (uploads, the public export link); API ↔ worker (the outbox); worker ↔ database
(grants, RLS, the tenant functions); worker ↔ blob stores (staging, evidence); the requester's device ↔ wherever the
ZIP goes next.

## 2. What can go wrong (STRIDE per boundary)

| # | Boundary or step | Threat (STRIDE) | Impact | Likelihood |
|---|---|---|---|---|
| T1 | Internet → API (upload) | Denial of service: a ZIP bomb, a sheet of millions of rows, an oversized text | Memory and CPU of the API or worker | Medium |
| T2 | Internet → API (upload) | Tampering: malware inside the workbook or an import file, kept and later opened by someone | A member's device infected from Fyniq's store | Low |
| T3 | Upload → books | Tampering: formulas, injected account or GST codes, a contact named to collide, amounts with excess precision | Wrong postings; a spreadsheet formula run when an export is opened | Medium |
| T4 | API (commit) | Elevation / repudiation: the person who prepared the workbook and its mapping commits it alone, or a commit runs over a workbook or mapping changed after approval | Unreviewed books imported under one person's hand | Medium |
| T5 | API (generic import) | Elevation: an import makes records its importer could not enter by hand, or posts documents past their approvals | SoD broken through a file | Medium |
| T6 | API (export request) | Elevation: one person takes the organisation's whole data, bank numbers included | The organisation's data leaves under one hand | Medium |
| T7 | Worker → database | Information disclosure: the export carries secrets, key material, tokens or another organisation's rows | Credentials or other tenants' data leave Fyniq | Low |
| T8 | API (download) | Elevation: someone other than the requester saves the export, or saves it after its time | The data reaches a person the approval did not cover | Medium |
| T9 | Internet → API (link) | Information disclosure: the link is forwarded, kept in an access log, a browser history or a proxy, or guessed | A third party saves the ZIP within the link's 5 minutes | Low |
| T10 | Requester's device | Information disclosure: the ZIP, with bank numbers in clear, is mislaid after it leaves Fyniq | The organisation's data and its vendors' bank details exposed | Medium |
| T11 | Export → reader | Tampering: the ZIP or its audit trail altered after it was written, and the change not seen | A falsified record passed off as Fyniq's | Low |
| T12 | AI / MCP | Elevation: an agent imports, commits or exports | Golden rule 5 broken | Low |

SOD: the commit and the export are dual controlled in every team mode (FM-1): the requester never signs. The
Accountant's sign-offs are theirs alone. RLS: every tenant table has `organisation_id` and `FORCE ROW LEVEL
SECURITY`; the tenant functions run as their caller.

## 3. What we do about it

| Threat | Control | Test that proves it |
|---|---|---|
| T1 | The workbook is read within `sheet.WorkbookBounds` (400 entries, 64 MiB a part, 256 MiB in all, 20,000 rows, the column limit); an import file within the generic import's 1,000 rows and the upload's byte limit; each refusal says which bound | `TestReadXLSX`, `TestAWorkbookReadsItsSheetsByName` (bounds), `TestTheGenericImportReadsItsTemplatesAndTheWorkbooksSheets` (1,001 rows) |
| T2 | Every upload passes `evidence.Check`: its type from its content and the malware scan, refused DOC-03 when flagged, before it is read or kept | `TestAnInfectedUploadIsRefused` (the workbook, the parallel month's workbook and an import file) |
| T3 | Only cached values are read; a workbook saved without them is refused (MIGRATION-05); every value is checked by the command that enters it, with ENG-44's precision boundary; codes are mapped and signed, never trusted; every CSV Fyniq writes, the export's included, prefixes a text that a spreadsheet would read as a formula | `TestEachProblemNamesWhereItIsAndHowToFixIt`, `TestTheValidationReportCommitsNothing`, `TestCSVWritesNumbersAsNumbersAndNoFormula` |
| T4 | The commit is the `migration_commit` approval: an Owner who did not ask approves, stepped up; the envelope binds the workbook's hash, the report's and the signed mapping's version; once committed, the workbook and mapping are fixed (MIGRATION-14; the database refuses it, FY327); the import goes only into an organisation with no postings | `TestACommitResumesWhereItStopped` (own request refused), `TestAWorkbookImportGoesOnlyIntoAnOrganisationWithNoPostings`, `TestTheMappingIsSignedByTheAccountantAlone` |
| T5 | Each entity's import needs that entity's own C (the matrix's rows); each row goes through the command a person uses, with its checks; journals, invoices, bills, opening balances and budgets are drafts, posted only through their own approvals; each run is audited once as `import.run`, naming the file, its hash and what it made, beside each record's own event | `TestTheGenericImportEntersEachRowAsByHand`, `TestTheGenericImportMakesDocumentsDrafts`, `TestTheGenericImportEntersOpeningBalancesAndPlans`, the authorisation matrix |
| T6 | `requestTenantExport` makes the `tenant_export` approval; another person who holds A on "Tenant export" approves it; the requester's own approval is refused (APR-01); the approval, the link and the request are audited | `TestTheFullExportIsDualControlledAndSavedByItsRequester` |
| T7 | A catalogue in code names every table with an `organisation_id` as exported or excluded with why (idempotency keys, the outbox, rate-limit and refusal counters, person keys, connector secret references, the tenant directory) and drops token hashes; a table in neither list stops the export; `tenant_rows` reads only the caller's organisation, only tables of the catalogue's kind, under the worker's grants; the API's role cannot call it | `TestTheTenantExportCataloguesEveryTable`, `TestTheTenantExportWritesTheOrganisation` |
| T8 | Only the person who asked gets a link (REPORT-07), only while the export is ready (REPORT-05) and within its 24 hours (REPORT-06); the file is deleted when they are up; each link is a security event with the request's address and device (AUD-05, AUD-06); the report exports never show a full export | `TestTheFullExportIsDualControlledAndSavedByItsRequester`, the matrix's AUD-05 check |
| T9 | The link works for 5 minutes; the API's own link is HMAC-signed with a kind no evidence link has, and a forged or expired token answers the same 404; responses carry `Cache-Control: no-store`; in the cloud the link is the store's signed URL | `TestTheFullExportIsDualControlledAndSavedByItsRequester` (forged token) |
| T10 | The README says the ZIP holds bank account numbers in clear and how to keep it; the approval's diff says so to the approver | `TestTheTenantExportWritesTheOrganisation` (README) |
| T11 | The manifest names every file's SHA-256 and size, and the ZIP's own SHA-256 is recorded and shown with the link; the audit trail verifies outside Fyniq to the head the manifest names (`contracts/audit`) | `TestTheTenantExportWritesTheOrganisation` (hashes and chain) |
| T12 | No AI read tool, catalogue task type or MCP tool imports, commits or exports (§9.6's never-list: no write, approve, export or admin tool); Appendix D's AI column grants no C or A on any of these rows (a proposal at most), so every one of these operations refuses an agent | The never-list tests, the authorisation matrix (AI column) |

## 4. What remains

- **Bank numbers in clear** in the export are the design's choice for exit and portability [counsel to confirm]
  before the first real export.
- **The local link's token is in the URL's path**, as the evidence link's is (SEC-24): an access log or proxy between
  the browser and the API can record it for its 5 minutes. In the cloud the store's signed URL is used, whose
  signature is likewise in the URL; the 5-minute life bounds both.
- **The API's own link reads the whole ZIP into memory** before it serves it; it serves only installations whose store
  cannot sign URLs (local mode). [live-verify] the cloud's signed URL of the staging bucket, and that the worker's
  identity may decrypt with the field-sealing key to open the sealed values.
- **Once saved, the ZIP is outside Fyniq's controls**: the requester's device and wherever they put it.
- **One long read**: the worker writes the export in one transaction; a very large organisation's export holds it
  for minutes. Measured with the performance harness when it runs at 10 M lines.
