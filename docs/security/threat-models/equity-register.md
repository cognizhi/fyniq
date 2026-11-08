# Threat model: the equity register

| | |
|---|---|
| Feature and requirement IDs | The equity register's holders and share classes: FR-EQ-15, FR-EQ-16, CMP-PD-07 (PRD-001 rev 0.203, rev 0.204); `p1-equity-register` slice 1. Equity transactions: FR-EQ-17, INV-13, FM-9 (rev 0.205), slice 2a; their ACRA steps and money from the bank (rev 0.206), slice 2b. Later slices (the ACRA steps and bank matching, the cap table, instruments, options, rounds, controllers, the ACRA reconciliation) extend this model. |
| Author, reviewer, date | Claude Code for the founders, 7 Nov 2026; reviewer: a founder at sign-off (second human review waived before production, AGENTS.md) |
| Data and its class | Holders' identity: an individual's identifier type and last 4 characters, nationality, address and contact people (personal data); identity documents (data class identity); share classes and their rights (confidential); the register of share issues |
| Who acts | The Accountant drafts (C/P); an Owner or the Controller approves (A); the Auditor and Tax agent read; the DPO reads masked; Employees and the Board viewer list no holders until their own holding and grants arrive with the cap table |

## 1. What we are building

A holder is a contact marked shareholder or investor. Its details are a holder record document, drafted with its
identity document as evidence and recorded on another person's approval. A share class is a document of its rights,
recorded the same way. Trust boundaries:
- the preparer → API: a record is hostile input;
- the API → the approval engine: a second person decides;
- the API → readers: personal data is shown by role;
- the evidence store → readers: an identity document.

## 2. What can go wrong (STRIDE per boundary)

| # | Boundary or step | Threat (STRIDE) | Impact | Likelihood |
|---|---|---|---|---|
| E1 | preparer → API | Information disclosure: a full NRIC or FIN stored as the identifier, in the address or in a note | Excess personal data held (PDPA, NRIC guidance) | Medium |
| E2 | preparer → API | Tampering: a record written straight to the database with extra keys, bypassing the API's checks | The rules above are bypassed | Low |
| E3 | approval | Elevation: the preparer, or the holder themselves, records their own details | A holder changes their own record | Medium |
| E4 | readers | Information disclosure: the DPO, an Employee or the Board viewer reads every holder's details, or opens an identity document | Personal data of every investor exposed | Medium |
| E5 | share issues | Tampering: an issue names a class of another account, or none once the account has classes | The cap table counts shares in the wrong class | Low |
| E6 | records | Repudiation: a recorded record or class is changed in place | History lost; no trail | Low |
| E7 | transactions | Elevation: a holder approves, or records effective, a transaction that gives them shares | A founder allots or transfers shares to themselves | Medium |
| E8 | transactions | Tampering: shares appear in a holding with no approved transaction, a transfer creates shares, or a holder gives away shares they do not hold | The cap table no longer matches the law | Low |
| E9 | transactions | Tampering: a cash issue is made effective before it is paid, or share capital moves outside the register | Capital stated that was never paid | Low |
| E10 | approvals | Elevation: the Accountant's new reach into the approvals lets them approve what they should only review | The review becomes a second approval by the wrong person | Low |
| E11 | ACRA steps | Repudiation: a lodgement date is changed after the fact, or recorded for a transaction it does not belong to | A missed statutory deadline is hidden | Low |
| E12 | bank → 2040 | Tampering: money in or a refund out is put on 2040 against no one, or against a customer, so a cash issue looks paid | Shares allotted against another person's money | Medium |

## 3. What we do about it

| Threat | Control | Test that proves it (`// req:` and `// asvs:`) |
|---|---|---|
| E1 | An individual's identifier is its type and last 4 characters only (the owner's decision of 7 Nov 2026). The API and the database (FY285) refuse anything longer, and any text in the record that shows an NRIC or FIN in full | `TestAHolderRecordKeepsTheLast4Only`, `TestAttackEquityRecordsKeepTheirRules`, `TestHoldersAndShareClasses` (V8.2.2, V14.2.1) |
| E2 | The database's trigger on every holder record version refuses a key outside the record's own and the identifier rules above, whoever writes it (FY285) | `TestAttackEquityRecordsKeepTheirRules` |
| E3 | Recording needs the approval of another person who holds A on the cap table: never the requester (APR) and never the member linked to the holder (`holderRecordExecutable`), both stepped up | `TestHoldersAndShareClasses` |
| E4 | The reads are Unqualified, so Employees and the Board viewer list no holders. The DPO reads a record masked. A masked reader never opens a file, and no identity-class file is opened in the app until FR-DOC-09's permissions exist | `TestHoldersAndShareClasses`, the authorisation matrix (V8.2.1) |
| E5 | FY287: an issue names a recorded class of its own account, and must once its account has one | `TestAttackEquityRecordsKeepTheirRules` |
| E6 | A recorded record or class changes no more (EQUITY-05). A later one supersedes it, and each version is audited | `TestHoldersAndShareClasses` |
| E7 | No member who signs in as a holder a transaction is for approves it (equity_transaction.approve's executable check) or records its effective date (403 APR-15); the Accountant reviews it when every other approver benefits (FM-9) | `TestEquityTransactions` |
| E8 | Every holding is the sum of its holding movements; the database refuses a movement outside an approved transaction made effective once or a share issue, a transfer that does not net to zero, a holding below zero on any day, and any change (FY288) | `TestAttackHoldingsMoveOnlyWithTheirTransactions`, `TestEquityTransactions` |
| E9 | A cash issue waits until its subscriber's share application monies cover it; share capital takes postings from the register, the opening balances or a conversion's movements only (FY007, FY289); 2040 names its subscriber on every line (FY250) | `TestEquityTransactions`, `TestAttackHoldingsMoveOnlyWithTheirTransactions` |
| E11 | A step is of its transaction's kind, names an approved transaction not yet effective, and never changes (FY290); a later one supersedes it and both stay | `TestAttackEquityStepsKeepTheirRules`, `TestEquityTransactions` |
| E12 | A bank line on 2040 names its subscriber, a shareholder or investor (LEDGER-41, FY250); a cash issue counts only its own subscriber's monies on its day | `TestShareApplicationMoniesFromTheBank`, `TestEquityTransactions` |
| E10 | The Accountant signs a step only when it names the accountant's review; a policy cannot name a role without A on the object's row (CheckSteps), so only the engine's FM-9 step does; FR-APR-10's recheck holds them to it | `TestEquityTransactions`, `TestTwoPersonRuleForASetting` (APR-05 for any other step), the authorisation matrix |

## 4. What remains

- Each holder's own view of their holding and grants arrives with the cap table (slice 3) and the option pool (slice 5).
- The `fyniq.read.equity` scope arrives with `p1-mcp-first-party`. Until then, no AI tool reads equity data.
