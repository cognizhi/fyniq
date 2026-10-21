# Threat model: changing posted opening balances

| | |
|---|---|
| Feature and requirement IDs | Changes to posted opening balances: FR-GL-06, FR-GL-12, FM-1, FR-APR-02…05 (PRD-001 rev 0.66, provisional) |
| Author, reviewer, date | Claude Code for the founders, 22 Oct 2026; second review at sign-off (waived before production, AGENTS.md) |
| Data and its class | Each account's balance at the books start (internal); the reason; who asked and who approved; no personal data beyond member IDs, no bank numbers or secrets |
| Who acts | The Accountant (P on Journals) asks; another member who holds A on Journals (the Controller) approves; the API's database role `fyniq_api` |

## 1. What we are building

Posted opening balances change only through an approval request, in every team mode. The request keeps the new
content beside its envelope, bound by the content's SHA-256; the envelope also holds the balances' version, status and
debits. On the approval, in one transaction, the balances' next version is saved, the entry they had is reversed on its
date and the new entry posts at the books start.

Trust boundaries: **person ↔ API** (the Appendix D row, SEC-02, step-up); **API ↔ database** (`fyniq_api`, RLS per
organisation; the document and ledger guards).

## 2. What can go wrong (STRIDE per boundary)

| # | Boundary or step | Threat (STRIDE) | Impact | Likelihood |
|---|---|---|---|---|
| T1 | person ↔ API | Elevation of privilege: one founder rewrites the balances alone | Every later balance sheet starts from figures nobody else saw | Medium |
| T2 | person ↔ API | Tampering: the approver signs one set of balances and another posts | As T1 | Low |
| T3 | person ↔ API | Tampering: the balances change between the request and the approval, and the approval posts over the change | A change approved against figures that are no longer there | Low |
| T4 | API ↔ database | Tampering: a fault posts the new version without reversing the old one, or rewrites posted history | The ledger counts the opening balances twice, or loses its history | Low |
| T5 | person ↔ API | Repudiation: nobody can tell who asked, who approved, or why | No evidence for the auditor | Low |
| T6 | person ↔ API | Spoofing: the AI or an integration asks or approves | A change no person stands behind | Low |

## 3. What we do about it

| Threat | Control | Test that proves it (`// req:`) |
|---|---|---|
| T1 | No operation changes posted balances directly; asking needs P and step-up, approving A and step-up, and the requester never approves (FM-1; the database refuses a requester's signature too) | `TestChangingPostedOpeningBalances` (FM-1), `TestAuthorisationMatrix` |
| T2 | The content is kept beside the envelope and bound by its SHA-256; execution re-hashes it and refuses a mismatch | `TestChangingPostedOpeningBalances` |
| T3 | The envelope holds the balances' version, status and debits; any change voids the request (FR-APR-05) | The approval engine's dependency tests |
| T4 | `document_edit_posted` refuses at commit an edit without the new version's entry and the old entry's reversal (FY055); posted entries never change (INV-02) | `TestAttackAnEditReversesAndPostsTogether`, `TestChangingPostedOpeningBalances` |
| T5 | The request keeps the reason and the changed lines; the edit's audit event names the approval, and the version records who prepared it and who posted it | `TestChangingPostedOpeningBalances` |
| T6 | Asking and approving need a person signed in to Fyniq; the AI holds no write on Journals (§9.6) | `TestAuthorisationMatrix` |

## 4. What remains

- Reading FM-1 as applying in every team mode is provisional (rev 0.66); the founders confirm it at sign-off.
- A change posts on the approval, so its month must still be open then; the request is refused while it is closed, but
  a month closed after the request makes the approval fail with STORAGE-INV-03, and the request stays to be rejected.
