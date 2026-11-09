# Threat model: the PDPA programme and retention (registers, timers, person keys, shredding, anonymisation)

| | |
|---|---|
| Feature and requirement IDs | The PDPA programme and the end of retention: CMP-05, CMP-PD-01 to CMP-PD-09, CTL-DP-05, CTL-DP-06, CTL-DP-07, SEC-10, SEC-11, SEC-13, SEC-14, AUD-02, §11.8 and §11.9's unnumbered requirements (crypto-shredding, shredded keys in backups, anonymisation, retention of logs and backups), and Appendix D's rows "Data protection (PDPA)" and "Own personal-data requests" (PRD-001 rev 0.244; `p1-pdpa-retention` task 12.2) |
| Author, reviewer, date | Claude Code for the founders, 10 Nov 2026; second review at sign-off (waived before production, AGENTS.md) |
| Data and its class | The personal-data inventory, the DPIAs and the transfers (internal). Access and correction requests, breaches, consent records and anonymisations (confidential: they name people and the company's failures). The DPO's published contact (public). Person keys (secret: wrapped by the deployment's key-encryption key). The shredding and anonymisation registers (internal: key IDs and contact IDs, never names). Personal values sealed in the audit trail (confidential) |
| Who acts | Appendix D: "Data protection (PDPA)" gives the DPO C/A, and the Owner and the Auditor V. "Own personal-data requests" gives every member C on their own. The worker (`fyniq_worker`) runs `pdpa.shred`, lists runs, writes the shredding register and deletes keys through the guarded function; the restore step re-applies the registers. The API, the worker and the executor make and read person keys while they write audit events. The AI holds nothing on either row |

## 1. What we are building

- **The DPO and the notices (CMP-PD-01, CMP-PD-03).** The DPO's business contact on every email and document, the
  privacy notices drafted for adoption, and consent recorded where a purpose needs it.
- **The registers.** The data inventory and its quarterly review (CMP-PD-02, CTL-DP-05), the DPIAs (CMP-PD-08), access
  and correction requests with their 30 days (CMP-PD-04), breaches with their 30-day and 3-day timers and the tabletop
  (CMP-PD-05, CTL-DP-06), and overseas transfers with Fyniq's own flows abroad (CMP-PD-06, CTL-DP-07). Every register
  is insert-only and audited, and each feeds a review on Reviews and attestations.
- **Person keys and `fyniq-audit-event@3` (§11.8, AUD-02).** In an event about a person, their name, emails, phone and
  address are sealed under their own key for the event's financial year.
- **Shredding (§11.8).** A year past its records' retention is listed; the DPO approves it, stepped up; the daily job
  writes the signed register entry, then deletes that year's keys through a guarded function.
- **Anonymisation (§11.9).** The DPO replaces a person's contact details from their answered request, register first.
- **The restore** re-applies the shredding and anonymisation registers before a restored copy opens.
- **Minimisation and transport (CMP-PD-07, SEC-14, SEC-10, SEC-13).** No column, API field or event field for an NRIC,
  FIN or card data; HSTS on every answer; logs kept 30 days and backups 35.

Trust boundaries:

- **person ↔ API**: Appendix D's two rows, step-up for the irreversible actions, and SEC-02;
- **API, worker, executor ↔ database**: the registers' guards, the person keys, the guarded functions;
- **service ↔ key-encryption key**: Cloud KMS in the cloud, a key file locally, wrapping person keys;
- **worker ↔ register bucket**: the write-once, signed shredding and anonymisation registers;
- **backup ↔ restored copy**: what a restore brings back;
- **Fyniq ↔ providers abroad**: sign-in and email (the transfers).

## 2. What can go wrong (STRIDE per boundary)

| # | Boundary or step | Threat (STRIDE) | Impact | Likelihood |
|---|---|---|---|---|
| T1 | person ↔ API | Information disclosure: requests, breaches, the data held about a person, or anonymisations reach a role without V on "Data protection (PDPA)", a token, the AI or an MCP client | People's requests and the company's breaches leak | Low |
| T2 | person ↔ API | Elevation of privilege and repudiation: someone other than the DPO approves a DPIA or a shredding, or anonymises a contact; a token or the AI does it; nobody is named | An irreversible act with no accountable person | Low |
| T3 | API ↔ database | Tampering and repudiation: a register's history rewritten: a request's steps reordered or backdated, a breach decided not notifiable after the fact, a transfer's flow swapped, a consent deleted | The company cannot show what it did, or when | Low |
| T4 | API ↔ database | Tampering (statutory): a breach of prescribed data, or of 500 or more people, decided not notifiable; the PDPC reported before a decision; a request closed unanswered | A notifiable breach goes unreported; a fine (CMP-PD-09) | Medium |
| T5 | worker | Denial of service and repudiation: a timer runs out unseen (a request's 30 days, a breach's 30 days or 3 days, a transfer's review) | A statutory deadline missed | Medium |
| T6 | API, worker ↔ database | Information disclosure: a person's name or email kept in the trail in clear, or sealed under the wrong person's key, or a sealed value opened in another event or organisation | Personal data outlives its retention; the trail leaks across tenants | Low |
| T7 | service ↔ key-encryption key | Information disclosure: person keys readable without the key-encryption key, or a key-encryption key in the repository or an image | Every sealed value opens forever | Low |
| T8 | worker ↔ database | Tampering and elevation of privilege: a person key deleted outside an approved run, before retention ends, or by the API; a run approved twice, re-ordered or deleted | Records the law requires destroyed early; an approval nobody gave | Low |
| T9 | worker ↔ register bucket | Repudiation and tampering: keys deleted with no register entry, or an entry forged so a restore deletes keys nobody approved | A shredding no one can prove, or a forged one | Low |
| T10 | backup ↔ restored copy | Information disclosure: a restore from an older backup brings back keys already shredded, or contact details already anonymised | Erased personal data resurrected (PDPA s25) | Medium |
| T11 | API ↔ database | Tampering: the shredding breaks the hash chain, or the anonymisation rewrites issued documents | The trail no longer verifies; documents no longer match what was issued | Low |
| T12 | API, worker ↔ database | Information disclosure: an NRIC, FIN or card number stored in a column, sent in an API field or written to an event | The data Fyniq promised never to hold | Low |
| T13 | person ↔ API | Information disclosure: a browser downgraded to plain HTTP | Sessions and personal data read in transit | Low |
| T14 | Fyniq ↔ providers abroad | Information disclosure: personal data leaves Singapore with no assessed basis | A transfer limitation breach (CMP-PD-06) | Medium |
| T15 | repository | Tampering (wrong content): a notice, a DPIA or the guide misstates the law (timers, prescribed data, the DPO's duties) | The company follows a wrong rule | Medium |

## 3. What we do about it

| Threat | Control | Test that proves it (`// req:`) |
|---|---|---|
| T1 | Every operation needs V on "Data protection (PDPA)", or is the member's own; the report of a person's data is an audited read (`personal_data.exported`); the AI and MCP hold nothing on the rows | `TestAccessAndCorrectionRequests`, `TestTheBreachRegister`, `TestShreddingAndAnonymisation` (a Controller refused), `TestAuthorisationMatrix` (CMP-PD-04, AUD-05) |
| T2 | Approving a DPIA or a shredding run and anonymising need A or C on the row, a signed-in person, step-up within 5 minutes, and If-Match; each records the person by name in its row and its audit event | `TestTheImpactAssessments`, `TestShreddingAndAnonymisation` (no step-up refused, a stale version refused, a second approval refused) (CMP-PD-08) |
| T3 | Every register is insert-only, its order kept by a guard trigger (FY311 to FY316), each row audited in its transaction | `TestAttackConsentKeepsItsHistory`, `TestAttackTheDataInventoryKeepsItsHistory`, `TestAttackImpactAssessmentsKeepTheirHistory`, `TestAttackRequestsKeepTheirSteps`, `TestAttackTheBreachRegisterKeepsItsRules`, `TestAttackTheTransfersKeepTheirHistory` (QA-ENG-05, AUD-01) |
| T4 | The database works out prescribed data (the Notification of Data Breaches Regulations 2021, reg 3 and the Schedule) and refuses a breach of it, or of 500 or more people, decided not notifiable; the PDPC step needs a notifiable decision; a request closes only after its answer. Recording a breach, and deciding it notifiable, send a locked critical notice | `TestAttackTheBreachRegisterKeepsItsRules`, `TestTheBreachRegister`, `TestAttackRequestsKeepTheirSteps` (CMP-PD-05, CMP-PD-04) |
| T5 | Each timer is a review read from its register's own rows, shown on Reviews and attestations and in the DPO's Inbox; CTL-DP-05, CTL-DP-06 and CTL-DP-07 read them nightly | `TestEachReviewReadsItsOwnSource`, `TestTheRecurringReviews` (CMP-03, CTL-DP-06, CTL-DP-07) |
| T6 | Sealing happens in the one place events are written, for every event about a person, at any depth, after masking; the associated data binds the organisation, the event and the field; person keys are under row-level security | `TestPersonalValuesAreSealed`, `TestTheSubjectOfAnEvent`, `TestPersonalValuesAreSealedUnderThePersonsKey` (another organisation opens nothing) (AUD-02, SEC-11) |
| T7 | A person key is a fresh data key wrapped by `ports.Cipher` (Cloud KMS; a key file locally, never committed), bound to its organisation, person and year; the read-only role cannot read keys | `TestPersonalValuesAreSealedUnderThePersonsKey`, `TestEachRoleHoldsExactlyItsGrants` (SEC-11) |
| T8 | No role holds DELETE on person keys; a key goes only inside its approved, registered run, for its own year, after `retention.records_years` (FY317); a run moves only listed → approved → done and never goes (FY318); only the worker may call the guarded functions | `TestAttackShreddingNeedsTheDPOAndTheRegister` (ten refusals) |
| T9 | The register entry is written and signed before the keys go; with the bucket down nothing is shredded; the restore checks each entry's signature and refuses one signed by another key | `TestAttackShreddingNeedsTheDPOAndTheRegister` |
| T10 | The restore step re-applies the shredding and anonymisation registers after the erasure register, before the copy opens; the runbook names the case where a copy agrees with its anchors | `TestAttackShreddingNeedsTheDPOAndTheRegister` (a run and an anonymisation the copy lacks, applied again) |
| T11 | The chain hashes the sealed text, so a shredded key leaves every hash as it was; anonymisation changes only the contact's emails, phone and address, never documents; its event names the fields, not the values | `TestPersonalValuesAreSealedUnderThePersonsKey`, `TestAttackShreddingNeedsTheDPOAndTheRegister` (the chain verifies after shredding), `TestShreddingAndAnonymisation` |
| T12 | An architecture test refuses any non-boolean column, API field, or event field named for an NRIC, a FIN or card data; the inventory records both as not held | `TestNoNRICOrCardDataIsHeld`, `TestTheDataInventory` (CMP-PD-07, SEC-14) |
| T13 | Every service's answers carry HSTS for two years, subdomains included; TLS ends at Cloud Run and Caddy | `TestEveryAnswerCarriesHSTS` (SEC-10) |
| T14 | Each catalogue flow abroad (sign-in, email) needs an open, assessed transfer; a flow without one waits on the reviews list, and CTL-DP-07 reads it daily | `TestTheTransferRegister`, `TestEachReviewReadsItsOwnSource` (CMP-PD-06, CTL-DP-07) |
| T15 | Every draft (notices, DPIAs) cites its official sources and marks each clause bearing on the law [counsel to confirm]; nothing is in force until the DPO adopts it | `TestTheNoticeDrafts`, `TestTheDPIADrafts` (CMP-PD-03, CMP-PD-08) |

## 4. What remains

- **Counsel has not reviewed** the notices, the DPIAs or the approach to crypto-shredding (open question 39). The
  [counsel to confirm] clauses stand until a lawyer confirms them.
- **Events about other objects keep their personal values.** Sealing goes by the event's subject: an email message's
  recipient, a suppression's address or a notification preference's email stay in clear in the trail.
- **The worker is trusted with the restore.** `fyniq.reapply_shredding` deletes the keys a register entry names; the
  worker checks the entry's signature, and the database cannot. A compromised worker could shred early.
- **A restore within the hour.** A copy restored from a backup taken shortly before an anchor can agree with its
  anchors and skip the recovery step's re-application; the runbook asks the operator to compare the register.
- **[live-verify]** where sign-in and email providers process data, the log bucket's location and retention, and the
  backups' retention, in the dev cloud.
- **AI connections abroad** join the transfers when `p1-ai-providers` declares them (AI-75); who records their
  assessment (the Owner or the DPO) is decided there.
- **Issued documents after an anonymisation** are not covered by a test.
