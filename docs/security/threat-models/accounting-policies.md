# Threat model: the accounting framework and policies

| | |
|---|---|
| Feature and requirement IDs | The accounting framework and policies: FR-ORG-10, G4-017, Appendix D "Accounting framework and policies" (PRD-001 rev 0.62, the founders' decision of 21 Oct 2026) |
| Author, reviewer, date | Claude Code for the founders, 21 Oct 2026; second review at sign-off (waived before production, AGENTS.md) |
| Data and its class | The framework, its edition and each policy's words (internal); who wrote, confirmed and approved each version; no personal data beyond member IDs, no bank numbers or secrets |
| Who acts | The Owner, the Controller and the Accountant record versions; a member who holds the Accountant role confirms or approves them; the API's database role `fyniq_api` |

## 1. What we are building

A version of the framework, or of one policy, applies from the first day of a financial year until a later version
replaces it. The accountant confirms the framework (the company's eligibility for it) and approves each policy. A year
whose framework and policies in force are all signed has statements titled "financial statements" with the basis they are
prepared under; any other year has "management statements" naming no framework.

Trust boundaries: **person ↔ API** (the Appendix D row, SEC-02); **API ↔ database** (`fyniq_api`, RLS per organisation).

## 2. What can go wrong (STRIDE per boundary)

| # | Boundary or step | Threat (STRIDE) | Impact | Likelihood |
|---|---|---|---|---|
| T1 | person ↔ API | Elevation of privilege: a founder approves their own policy words, so statements claim an accountant's approval nobody gave | Statements titled "financial statements" without a second person's review | Medium |
| T2 | person ↔ API | Tampering: approved words are changed afterwards, keeping the approval | Statements prepared under a policy the accountant never saw | Low |
| T3 | API ↔ database | Tampering: a fault in the API rewrites a version, withdraws a signature or signs as its writer | As T1 and T2 | Low |
| T4 | person ↔ API | Repudiation: nobody can tell who recorded or signed a version | No evidence for the auditor | Low |
| T5 | person ↔ API | Spoofing: an AI client signs on a person's behalf | An approval no person gave | Low |

## 3. What we do about it

| Threat | Control | Test that proves it (`// req:`) |
|---|---|---|
| T1 | Confirming and approving need A on the row, which only the Accountant role holds; the domain refuses the version's writer (ACCOUNTING-02), and so does the database (`FY161`) | `TestAccountingFrameworkAndPolicies`, `TestAccountingVersionsNeverChangeAndTheirWriterNeverSignsThem`, `TestAuthorisationMatrix` (FR-ORG-10) |
| T2 | A version never changes: a correction is a new version, unsigned, which replaces the old one for its year; the year is unapproved until the new one is signed | `TestAYearsStatementsAreManagementStatementsUntilTheAccountantApproves`, `TestAccountingFrameworkAndPolicies` |
| T3 | `fyniq_api` may update only the signature columns; `accounting_version_guard` refuses any other change, a delete and a second or withdrawn signature, even for the tables' owner (`FY160`) | `TestAccountingVersionsNeverChangeAndTheirWriterNeverSignsThem` |
| T4 | Every version and signature is audited with its actor in the same transaction (AUD-01), and the versions keep their writer and signer | The same test |
| T5 | The AI column holds V only on the row, and the AI never approves (§9.6) | `TestAuthorisationMatrix` |

## 4. What remains

- A founder who also holds the Accountant role may approve another founder's words. The founders accepted this on
  21 Oct 2026 (the recommended option); "approved" then means a second person reviewed it, not necessarily an outside
  accountant.
- The statements that show the title arrive with `p1-reports`, which must read the title and basis from
  `GET /v1/organisation/accounting` rather than work them out again.
