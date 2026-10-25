# Threat model: bank import, matching and reconciliation

| | |
|---|---|
| Feature and requirement IDs | Money accounts (FR-BNK-01), statement import (FR-BNK-02), duplicates (FR-BNK-04), rules (FR-BNK-05), the Categorise queue and matching (FR-BNK-06, FR-BNK-07), settlement differences (FR-BNK-08), transfers (FR-BNK-09), reconciliation (FR-BNK-10), suspense (FR-BNK-11), cash position (FR-BNK-13), auto-reconcile (FR-BNK-14), bank data connectors and sync (FR-BNK-15, FR-BNK-16), DBS by email (FR-BNK-17, UC-BNK-01, UC-BNK-02), Wise and Airwallex (FR-BNK-18, UC-BNK-04), the source switch (UC-BNK-03); the owner's decisions of 25 Oct 2026 (PRD-001 rev 0.140) |
| Author, reviewer, date | Claude Code for the owner, 25 Oct 2026; second review waived before production (AGENTS.md), held with `needs-human-review` if wanted |
| Data and its class | Statement lines: amounts, dates, counterparties' names and references (confidential business data; a counterparty name can be personal data); the company's own account numbers (internal, masked in logs); Wise and Airwallex API keys and webhook secrets (secrets); statement files and emailed reports (untrusted documents); the postings, matches and reconciliations they lead to (ledger, append-only) |
| Who acts | The Bookkeeper and Accountant (categorise, prepare: CP on "Bank transactions, categorise", C on "Bank reconciliation sign-off"); the Controller (approves, signs off: A); the Owner and Administrator (bank connections: A and C on "Bank data… connections"); the system principal (auto-reconcile, FR-AUT-02); the worker (sync, imports from mail); DBS, Wise and Airwallex (reports, APIs and webhooks from the internet); the AI (proposals only, later) |

## 1. What we are building

Bank data reaches Fyniq three ways, all producing one model of statements and lines:
- **files** a person uploads (CSV, XLSX, OFX/QFX, camt.053, MT940), checked, scanned and kept as untrusted uploads;
- **emailed reports and alerts** DBS sends to the organisation's bank-ingest address, accepted only from the bank
  addresses each money account registers, with DMARC passing;
- **API connectors** (Wise, Airwallex) the worker pulls on the scheduler tick through the egress guard, with
  read-only keys kept in the secrets project; their webhooks only prompt a pull.

Every import checks continuity (each opening balance equals the previous closing balance) and duplicates (a
line is dropped only on proof). A person categorises lines, matches them to invoices and bills (which creates
the receipt or bill payment through the existing commands), records transfers, or posts to suspense; the system
principal auto-reconciles exact matches and lines of approved auto-apply rules, within caps. Each money account
and period is reconciled by one person and signed off by another; the close refuses a period whose accounts are
not.

Trust boundaries: browser ↔ API (imports, coding, matching, sign-off); provider ↔ API (webhooks, from the
internet); mail provider ↔ worker (reports and alerts, from the internet); worker ↔ provider (pulls, over the
egress guard); API and worker ↔ database (RLS on every banking table); API ↔ secrets project (organisation keys).

## 2. What can go wrong (STRIDE per boundary)

| # | Boundary or step | Threat (STRIDE) | Impact | Likelihood |
|---|---|---|---|---|
| T1 | Mail → worker | Spoofing: a forged "DBS" report or alert, from another sender or with a spoofed From | Fake lines in the books; an invoice shown "likely paid" | Medium: mail is easy to forge |
| T2 | Mail → worker | Tampering / replay: a genuine report sent again, or an old one re-sent | Lines counted twice | Medium |
| T3 | Browser or mail → parser | Denial of service / tampering: a malformed or oversized file (a zip bomb in XLSX, deep or huge XML in camt.053, a gigantic CSV, a field that never ends) | The worker or API stalls; a parser misreads amounts | Medium: statement files are untrusted input |
| T4 | Parser → books | Tampering: a parser misreads a sign, a decimal or a date, or a float creeps in | Wrong postings that look right | Medium |
| T5 | Import | Tampering / denial: a real line dropped as a duplicate, or a duplicate kept twice | The books disagree with the bank | Medium (G4-042) |
| T6 | Import | Tampering: a gap in the statement chain filled silently, or a statement for another account accepted | Missing or misplaced money | Low |
| T7 | Provider → API | Spoofing / replay: a forged or replayed Wise or Airwallex webhook | Data pushed into the books, or a sync storm | Medium |
| T8 | Worker → provider | Information disclosure / SSRF: a configured endpoint aimed at an internal address; a key that can move money | Internal services reached; funds moved | Low: read-only keys, the egress guard |
| T9 | API → secrets project | Elevation: the API's account can create or read other secrets | The database password or the setup code reached | Medium without the dedicated project |
| T10 | Auto-reconcile | Tampering / elevation: the system principal reconciles a line to the wrong document, beyond its caps, or by a rule nobody approved | Wrong settlements without a click | Medium |
| T11 | Rules | Elevation: a rule's author approves its own auto-apply, or an AI-proposed rule is approved by the one who requested it (G4-059) | One person decides what posts unseen | Low |
| T12 | Matching | Tampering: a line matched twice, or matched to a receipt that a hand entry already recorded, so money posts twice | AR or AP wrong | Medium |
| T13 | Reconciliation | Repudiation / elevation: the preparer signs off their own reconciliation, or a signed-off one changes after the close | The control proves nothing | Low |
| T14 | API ↔ database | Information disclosure / elevation: one organisation reads or changes another's lines, rules or reconciliations | Cross-tenant disclosure | Low |
| T15 | Logs, audit, browser | Information disclosure: full account numbers, keys or counterparties' details in logs or traces | Data leaked to whoever reads logs | Medium |
| T16 | Ledger | Tampering: a line's facts or a posting changed after import to hide something | Lost evidence | Low |
| T17 | AI and MCP | Elevation: the AI or an MCP client categorises, matches or auto-applies by itself | The never-list bypassed (golden rule 5) | Low: no route today |
| T18 | Rules | Denial of service: a rule's regular expression runs away over a long description, in the queue or a back-test | Categorising and back-tests stall | Low |

SOD: categorising and preparing are separate from signing off and from approving auto-apply (FM-1, FM-5). No
money leaves through anything here: bank connectors are read-only and payments go out as approved files
(`p1-payments-out`). RLS: every banking table has `organisation_id` and `FORCE ROW LEVEL SECURITY`. The AI and
MCP have no route to any operation here; AI suggestions arrive later as proposals only (AI-P4).

## 3. What we do about it

| Threat | Control | Test that proves it (`// req:` and `// asvs:`) |
|---|---|---|
| T1 | The bank-ingest address accepts only the bank addresses its money accounts register, with DMARC passing; anything else is quarantined for a person; an alert is a pending signal that posts nothing | The bank-ingest tests of task 7.1 (a registered sender accepted, another sender and a forged one quarantined) and 7.3 (an alert posts nothing) (`// req: FR-INT-22`, `// req: FR-BNK-17`) |
| T2 | The duplicate rules of T5 apply to emailed reports too; a re-sent report adds nothing | The import tests of task 3.2 (a re-import adds nothing) |
| T3 | Parsers are bounded (file size, lines, field length, XML depth and entity expansion off, the zip limits of the evidence upload) and fuzzed; a refusal names the line and field | The parser fuzz tests and refusal tests of task 3.1 (`// req: FR-BNK-02`, `// asvs: V5.2.1`, `V5.2.3`, `V1.5.1`) |
| T4 | Exact decimals only (no float, ENG-10, the architecture test); golden fixtures per format; the preview before anything is added; the continuity check and the e-statement tie-out | The golden-fixture tests of task 3.1; the continuity tests of task 3.2 |
| T5 | A line is dropped only when the bank's reference matches or the running balance proves the overlap; otherwise it is kept and marked possible duplicate (C.2 #51); identical lines in one statement are both kept | G4-042 in task 3.2 (`// req: FR-BNK-04`, `// req: FR-BNK-16`) |
| T6 | Each opening balance must equal the previous closing balance, else a gap is recorded and shown, never filled; a statement whose account identifier is not the money account's is refused | The continuity and account tests of task 3.2 |
| T7 | Webhooks are verified (Wise: RSA-SHA256 with its public key; Airwallex: HMAC-SHA256 over the timestamp and raw body within five minutes) and only prompt a pull; their body is never used as data; unknown, disabled or uninstalled connectors answer 404 | The webhook tests of tasks 8.2 to 8.4 (`// req: FR-BNK-18`, `// asvs: V13.3.4`) |
| T8 | Read-only keys (scoped where offered); every request through the egress guard to the manifest's destinations (SEC-36) | The connector contract tests of 8.3 and 8.4 and the egress tests of `p1-connector-framework` (`// req: SEC-36`) |
| T9 | Organisation connector secrets live in a dedicated secrets project where the API's account is the only manager; no Secret Manager create right elsewhere | The environment module's `tofu test` and the infra rules of task 8.1 (`// req: SEC-12`, `// asvs: V13.3.2`) |
| T10 | Auto-reconcile takes exactly-one matches and approved rules only, within per-line and per-rule daily caps; each is recorded with what did it, listed, undoable while the period is open, and sampled weekly (FM-3) | The auto-reconcile tests of task 5.1 (`// req: FR-BNK-14`, `// req: FR-AUT-02`, `// asvs: V2.3.2`) |
| T11 | Auto-apply is an approval object (FM-1) showing the back-test; the rule records an AI origin for G4-059 | The approval tests of task 5.2 (`// req: FR-BNK-14`, `// asvs: V2.3.5`) |
| T12 | A line is matched once (a unique constraint); a hand-recorded receipt or payment is matched, never recorded again; the receipt and payment commands keep their own idempotency | The matching tests of task 4.3 and the attack tests of task 2.2 (`// req: FR-BNK-07`) |
| T13 | The signer must be another person with the sign-off right, with step-up; a signed-off reconciliation is immutable and locked with its period | The reconciliation tests of task 6.1 and the attack tests of task 2.2 (`// req: FR-BNK-10`, `// asvs: V2.3.5`) |
| T14 | `organisation_id` and FORCE RLS on every banking table; the API checks the Appendix D rows before the store | The attack tests of tasks 2.1 and 2.2, the tenancy goldens and the matrix rows of task 9.1 (`// req: INV-06`, `// asvs: V8.4.1`) |
| T15 | Account numbers are masked in logs and the audit trail (SEC-15); keys never leave the secret store; counterparties' details stay in the database, not in logs or traces | The logging masking tests and the connector key tests (`// req: SEC-15`, `// asvs: V16.2.5`) |
| T16 | A line's facts never change and its status moves only forward (a guard trigger); corrections are reversals; every change is audited in its transaction; every match but a transfer's names a document of its kind whose entry posts the line's amount through its account (FY228), a matched document is not reversed until its match is undone (FY229), a transfer's lines are its legs' money out and in, of its amounts (FY223), and an undo reverses what was made from the line | The guard attack tests of task 2.1 (`// req: AUD-01`, `// asvs: V16.3.3`); `TestBankMatchingKeepsWhatItRecorded`, `TestCategorisingABankLinePostsIt` (task 4.1) `TestMatchingABankLineSettlesWhatItPays` (task 4.3) and `TestTransfersAreRecordedOnceAndMatchedOnBothAccounts` (task 4.4) |
| T17 | No AI or MCP route reaches a banking operation; AI suggestions arrive later as proposals | The never-list test, with the AI tool surface; until then no route exists (`// req: AI-P4`) |
| T18 | Rules match with RE2, in time linear in the text, so no pattern runs away; a pattern is at most 200 characters and a description at most 500 (the readers' limit); a pattern RE2 cannot run is refused when the rule is saved | `TestARuleRegexCannotRunAway` (task 4.2, `// req: FR-BNK-05`) |

## 4. What remains

- DBS's emailed report format is `[verify]` until a redacted sample of Cognizhi's report; until then DBS lines come
  from uploaded MT940 or CSV exports.
- Wise's read-only tokens for Singapore business profiles, Airwallex's scoped keys, and both providers' hosts are
  `[verify]` against their pages before the manifests are pinned (tasks 8.3, 8.4).
- A bank connector key with more than read access, if a provider offers no read-only key, would be a residual
  risk for the owner to accept in writing before that connector is enabled.
- The never-list test (T17) arrives with the AI tool surface.
