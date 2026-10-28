# Threat model: vendors' bank details

| | |
|---|---|
| Feature and requirement IDs | Changing a vendor's bank account, the freeze on paying it while a change waits, sealing and revealing account numbers: FR-CT-03, FR-CT-04, SOD-1, FM-1, FR-DOC-06, SEC-11, SEC-15, AUD-02 (PRD-001 rev 0.152; `p1-organisation-chart` task 4.2) |
| Author, reviewer, date | Claude Code for the founders, 27 Oct 2026; second review at sign-off (waived before production, AGENTS.md); money movement, so a co-founder review (#6) before production |
| Data and its class | Vendors' bank account numbers and IBANs (restricted: sealed, never logged), their banks' SWIFT/BIC codes, account names and countries, call-back notes, and the vendors' letters (evidence of data class bank). No card data |
| Who acts | The Accountant and the AP specialist, who prepare a change and ask for it (C on Appendix D's "Vendor bank details"); the Owner and the Controller, who approve it (A), and in Founder mode every founder; the Auditor and the Tax agent, who read it masked (M); the API's role `fyniq_api`; the field keys in Cloud KMS (`fields`, `fingerprints`) or `.data/keys/fields.key` locally |

## 1. What we are building

A vendor's bank account in one currency changes only through a change of its bank details. A person prepares it as a
draft: a Singapore account (the bank's SWIFT/BIC, the number, the name) or an overseas one (SWIFT/BIC, the number or
IBAN, the name, the bank's country). The number is sealed on save: AES-256-GCM under a fresh data key that a KMS key
wraps, bound to the change, its vendor and its currency. The change keeps the last four characters and a keyed
fingerprint of the bank and number. The vendor's letter, or a screenshot of its confirmation through a channel known
before, is attached; a call-back note is recommended. Asking for approval freezes payments to the vendor and tells every
founder at once. Another person approves it, which puts the account in force and retires the one it replaces. The full
number is revealed one account at a time, after step-up, and every reveal is audited.

Trust boundaries:
- **person ↔ API**: the Appendix D row, step-up, the second person, the masked view;
- **API ↔ KMS**: the api's account alone holds a role on the field keys;
- **API ↔ database**: `fyniq_api`, RLS per organisation, `payee_bank_account_guard`, `payee_bank_change_approved`;
- **API ↔ notifications**: the outbox, then email.

## 2. What can go wrong (STRIDE per boundary)

| # | Boundary or step | Threat (STRIDE) | Impact | Likelihood |
|---|---|---|---|---|
| T1 | person ↔ API | Spoofing and tampering: a fraudster's email ("we changed banks") gets an insider to change a vendor's account, and the next payment goes to the fraudster | The payment lost; the vendor still owed | High |
| T2 | person ↔ API | Elevation: one person changes a vendor's account and approves it, or changes it and pays it (SOD-1) | One insider diverts payments alone | Medium |
| T3 | person ↔ API | Tampering: a payment is planned or released to the new account while the change waits | Money sent to an account nobody approved | Medium |
| T4 | API ↔ database | Tampering: an account put in force, swapped, or its sealed number changed outside the approval | The payment file pays an account nobody approved | Low |
| T5 | API ↔ database, logs | Information disclosure: account numbers read from the database, a backup, the logs, the audit trail or a response | Personal and bank data exposed; an account used for fraud | Medium |
| T6 | person ↔ API | Information disclosure: an Auditor, the AI or an integration reads full numbers | As T5 | Low |
| T7 | API ↔ KMS | Information disclosure and denial: a field key leaked, or lost | Every sealed number readable, or none | Low |
| T8 | person ↔ API | Tampering: the same account given to two vendors, so a fraudster's account sits behind a real vendor's name | Payments to a vendor go to another | Low |
| T9 | person ↔ API | Repudiation: a change made and nobody told, or nobody able to say who approved it | A diversion noticed weeks later, with no one accountable | Medium |
| T10 | person ↔ API | Tampering: a waiting change edited after the approver read it, or approved twice | The approver signs one account, another comes into force | Low |

## 3. What we do about it

| Threat | Control | Test that proves it (`// req:` and `// asvs:`) |
|---|---|---|
| T1 | A change needs the vendor's letter or confirmation attached before it is asked for (PAYEE-11), checked again at the approval; the call-back note (a number known before, never one from the letter) is a warning when missing; another person approves; the approver sees the old and new account and can reveal both numbers to compare with the letter | `TestAChangeOfBankDetailsNeedsASecondPerson` (`// req: FR-CT-03`, `// asvs: V2.3.5`) |
| T2 | The requester never signs (APR-01, FY101); the preparer never signs either (PAYEE-18); the database posts a change only under an approved request that neither signed (FY237), in every team mode. SOD-1's payment half (who changed an account never releases a payment to it) arrives with payment batches | `TestABankDetailsChangeIsChecked` (`// req: SOD-1`), `TestAttackBankDetailsChangeOnlyByApproval`, `TestAttackPayeeBankAccountsKeepTheirChange` (`// req: QA-ENG-05`, `// asvs: V2.3.5`) |
| T3 | While a change waits, planning a payment date for the vendor is refused (PAYEE-12); recording a payment the bank made already is allowed with ⚠ (the owner's decision of 27 Oct 2026); payment batches will refuse the vendor | `TestAChangeOfBankDetailsNeedsASecondPerson` (`// req: master-data/contacts The freeze before payment batches (rev 0.152)`) |
| T4 | `payee_bank_account` is insert-only but for its state: kept until its change is approved, in force only with its change posted, retired only while another change replaces it; never updated otherwise, deleted or truncated, for any role or the table's owner (FY238); one in force per vendor and currency; `fyniq_api` alone inserts and moves it | `TestAttackPayeeBankAccountsKeepTheirChange` (`// asvs: V13.2.2`) |
| T5 | Numbers are sealed by the application (SEC-11) and are in no change version, response, log or audit event: the version keeps the last four characters and the fingerprint; the audit trail the same (AUD-02); the database holds ciphertext only | `TestAChangeOfBankDetailsNeedsASecondPerson` (the trail and the log hold no number; `// asvs: V14.2.4`), `TestTheEnvelopeSealsBoundToTheRecord`, `TestANumberIsSealedForItsChange` (`// asvs: V11.3.3`) |
| T6 | The full number only through `revealPayeeBankAccount`: C or A on the row, a person, stepped up in the last 5 minutes; a masked view (M) gets it masked and nothing is opened; the AI has no right on the row and no tool (§9.6) | `TestAChangeOfBankDetailsNeedsASecondPerson`, `TestAuthorisationMatrix` (`// asvs: V8.2.3`) |
| T7 | The keys never leave KMS; only the api's account holds a role on them; the wrapping key rotates every 90 days, older versions still unwrap; both keys are protected from destruction. Locally a 0600 file, never committed | `TestKMSFieldsConformance`, `TestFieldKeysConformance`, `tofu test` `the_sensitive_fields_are_sealed_by_the_api_only` |
| T8 | The fingerprint of the bank and number is compared across vendors' accounts in force: the same account at another vendor is a warning naming it (PAYEE-17, FR-CT-04) | `TestABankDetailsChangeIsChecked` (`// req: FR-CT-04`) |
| T9 | Every founder (everyone who holds A on the row) is told at once when a change is asked for and when it is approved: a security notice, always on, emailed at once; every step is audited with its actor, and the approval names its signers | `TestAChangeOfBankDetailsNeedsASecondPerson` |
| T10 | The approval's envelope holds the change's version, its status and the change it replaces: a new version, or another approval for the vendor and currency, voids it (FR-APR-05); only a draft is revised; another change of the same currency waiting refuses the ask (PAYEE-13) | `TestABankDetailsChangeIsChecked`, the approval engine's `TestApprovalsBlockOnChangeAndExpire` |

## 4. What remains

- Payment batches (FR-BNK-12, `p1-payments-out`) must refuse a vendor whose change waits, re-ask approval of a batch whose
  payee changed (G4-055), and refuse release by whoever changed the account (SOD-1). The Checks row "Vendor bank details
  changed in the last 30 days with payments pending" (⚠) arrives with Checks.
- The payee name check against the bank (P2) waits for a bank API.
- A recorded payment while a change waits is only flagged: the money already left the bank. The weekly review should
  look at such payments.
- ~~Employees' bank details for reimbursements arrive with payroll (`p1-payroll-link`) and will reuse the same sealing.~~
  People's own bank accounts for reimbursements arrived with `p1-payments-out` (rev 0.154): section 5.
- The KMS adapter is proved against the staging key ring at the cloud gate ([live-verify]).

## 5. People's own bank accounts for reimbursements (FR-AP-22; rev 0.154; `p1-payments-out` task 1)

A person keeps their own account for reimbursements: a Singapore bank's account in SGD, entered by the person under My
profile with a screenshot of their banking app or their statement's header, sealed and fingerprinted as a vendor's is
(under its own binding, so neither opens as the other), and approved by someone who holds A on "Payment batches"
(the Controller; in Founder mode, the other founder), never the person. While a change waits, payments to the person
wait. The person's operations are under `/v1/me` (Appendix D's "Expense claims", C own); the approver reads the change,
its files and the number under "Payment batches".

| # | Threat (STRIDE) | Control | Test that proves it |
|---|---|---|---|
| P1 | Elevation: a person approves their own account, so one person sends their reimbursements anywhere | The requester never signs (FY101); the person and whoever saved the change never sign (PAYEE-21); the database posts a person's change only under an approved request that neither the requester, the preparer nor the person signed (FY237), in every team mode | `TestAPersonsBankAccountNeedsAnotherPerson` (every role; `// req: FM-1`), `TestAttackPersonsBankAccountOnlyByApproval` (`// req: QA-ENG-05`, `// asvs: V8.2.2`) |
| P2 | Tampering: someone in finance changes another person's account to their own | Only the person prepares, asks for and reveals their own: another person's change is not found through `/v1/me`; the approver can read but not change it | `TestAPersonsBankAccountNeedsAnotherPerson` (`// asvs: V8.2.3`), `TestAuthorisationMatrix` |
| P3 | Tampering: a vendor's account entered as a person's, or one account shared by two payees, so money meant for one reaches another | The fingerprint is compared across every payee's account in force: the same account at a vendor or another person is a warning on the change, naming the vendor (PAYEE-17, FR-CT-04) | `TestAPersonsBankAccountIsChecked` (`// req: FR-CT-04`) |
| P4 | Information disclosure: a person's number read by someone without need | Sealed (SEC-11) under the person's own binding; the full number only through `revealMyBankAccount` (the person, stepped up) or `revealClaimantBankAccount` (A on "Payment batches", stepped up), each audited by its last four characters; never in a response, log or audit event otherwise | `TestAPersonsBankAccountNeedsAnotherPerson` (`// asvs: V14.2.4`), `TestANumberIsSealedForItsChange` (`// asvs: V11.3.3`) |
| P5 | Tampering: a reimbursement paid to an account while its change waits | A reimbursement recorded by hand while a change waits is flagged ⚠ (PAYEE-12): the money already left; payment batches will hold the person's claims | `TestAPersonsBankAccountNeedsAnotherPerson` |
| P6 | Tampering: an overseas or foreign-currency account, or one for someone who is not a member | Refused as entered (PAYEE-19, PAYEE-20) and by the database: a person's account is a Singapore account of an active member (FY238) | `TestAPersonsAccountIsSingaporeInSGD`, `TestAttackPersonsBankAccountOnlyByApproval` |

What remains for people's accounts: when Nemura is linked it holds them (FR-PPL), and payment batches hold a person's
claims while their change waits (`p1-payments-out` task 2.2).
