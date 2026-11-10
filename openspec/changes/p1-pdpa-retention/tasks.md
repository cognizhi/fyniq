# Tasks

Each slice below is one pull request that passes `make test` on its own. Each one carries:
- its migration and attack tests;
- its API, with authorisation-matrix rows on Appendix D's "Data protection (PDPA)", "Own personal-data requests",
  "Policy library" or "Organisation profile" row;
- its screens, with help keys and guide sections compiled into the knowledge base;
- the tests behind every figure and every message it adds (`// req:` tags).

Before each slice, load `fyniq-feature-change` and `fyniq-security-compliance`. Also load `fyniq-design-system` for the
screens, and `fyniq-knowledge-base` for every guide page. Slice 9 also loads `fyniq-accounting-domain` for the audit
event format.

## 1. The decisions (slice 1)

- [x] 1.1 PRD-001 rev 0.244 records the owner's decisions of 9 Nov 2026. The main specs are updated and stamped:
  `compliance/pdpa`, `data/backup-and-recovery` and `access/roles-and-segregation`. `python3 scripts/prdspec.py stale`
  is clean, `make roadmap` places the two new Appendix D rows, and `make check` passes. The decisions:
  - shredding by register;
  - the audit trail's personal values;
  - the DPO approves each run;
  - the DPO's contact in the app and on the website;
  - notices drafted, adopted and acknowledged;
  - the two Appendix D rows;
  - the penalty confirmed.

## 2. The DPO and penalty awareness (slice 2)

- [x] 2.1 The organisation profile's `dpo_user_id`, `dpo_name` and `dpo_email` (D1). The profile's API saves and
  audits them, with a warning when the member does not hold the Compliance officer role. Tests: saved and audited; a
  non-member refused; the warning. `// req: CMP-PD-01`

  **What building added:**
    - **An outsourced DPO.** The PDPA allows one, so `dpo_user_id` is optional. A DPO with a name and an email but no
      member is an outside firm.
    - **ORG-11** refuses a member named without the published contact. Naming someone who is not a member is refused
      on `dpo_user_id`.
    - **The warning** is the response's `dpo_holds_role`: false when the member named does not hold the Compliance
      officer role.
    - **The database.** Migrations 000298 and 000299 add the three columns, with a check: a member named needs the
      contact. Changes are audited as profile changes, on the organisation's row.
    - **Tests:** `TestTheDPOsContact` (domain) and `TestTheDataProtectionOfficer` (API).

- [x] 2.2 The DPO's contact in the footer of every email Fyniq sends, and in the default templates' footer block for
  new versions; the copy-ready website block. Tests: an email carries it; an issued document is unchanged.
  `// req: CMP-PD-01`

  **What building added:**
    - **Emails.** `email.QueueWith` reads the profile in the same unit of work and ends every message with the contact,
      below a plain-text signature line: "Questions about your personal data: <name>, Data Protection Officer,
      <email>". No line is added while no DPO is named.
    - **Documents.** The template field catalogue gains `organisation.dpo_name` and `organisation.dpo_email`. The default
      footer adds "Personal data: <name>, <email>", which is left out while both are empty. Templates already published
      keep their own footer, and issued documents keep their PDFs.
    - **The website text.** Settings › Organisation has a **Data Protection Officer** card with the text to publish and
      **Copy the text**, and the warning about the role. The profile's dialog names the DPO from the members.
    - **Help.** The help key is `organisation.dpo`, and the guide's new section "Data Protection Officer" is compiled
      into `settings.organisation`.
    - **Tests:**
      - `TestTheLayoutFollowsTheTemplate`: the footer with and without the contact;
      - `TestTheDataProtectionOfficer`: an email's footer;
      - the screen's component tests, with axe.

- [x] 2.3 R7's description states the penalty (D13), and a test pins the figures to CMP-PD-09. `// req: CMP-PD-09`

  **What building added:**
    - **R7's description** gives the figures and their source, PDPA s48J(3) and reg 10A.
      `TestThePDPAPenaltyIsInTheRegister` pins them.
    - **Every organisation** sees the penalty on the Data protection page, which slice 4 builds.

## 3. Privacy notices and consent (slice 3)

- [x] 3.1 The policy library's keys `notice-members` and `notice-contacts` (D2), with Fyniq's drafts in
  `compliance/notices/` from the PDPC's guides, marked [counsel to confirm]. A migration widens the key checks. Tests:
  the drafts' front matter and sources; a notice published and acknowledged as a policy is. `// req: CMP-PD-03`

  **What building added:**
    - **The registry.** `compliance.LibraryKeys` holds the eight policies and the two notices.
      `compliance.Acknowledged` is false only for the contacts' notice. The drafts are `fyniq-notice-draft@1`, written
      from the PDPC's guidelines on key concepts and on employment, its page on the obligations, and the Guide to
      Notification.
    - **The database.** Migrations 000300 and 000301 widen the library's key checks, and the version's `from_draft` check
      so that it takes `fyniq-notice-draft`.
    - **The API:**
      - `listPolicies` takes `kind` (`policy`, the default, `notice` or `all`);
      - each item has its `kind`, and a notice has its `summary`: the first paragraph of the version in force;
      - acknowledging the contacts' notice is refused (422), and members have no `mine` on it.
    - **Reviews.** The reviews list counts the members' notice among the acknowledgements.
    - **What Fyniq sends.** An email to a customer or supplier (invoice, quote, reminder, statement, remittance) ends
      with the contacts' summary and the DPO's contact. An email to a member ends with the DPO's contact alone.
    - **Where the code lives.** `NoticeSummary` is in `internal/domain/organisation`, so that `internal/email` does not
      import the registry, which would make an import cycle in its tests.
    - **Tests:**
      - `TestTheNoticeDrafts`;
      - `TestThePrivacyNotices`;
      - `TestWhatFyniqSendsEndsWithThePrivacyFooter`;
      - `TestAttackConsentKeepsItsHistory`, a notice's version from an unknown draft.

- [x] 3.2 Joining shows the members' notice in force and records its acknowledgement in the same unit of work. A later
  version waits on the "Acknowledge policy" card. Tests: joining records it; joining before any version records
  nothing. `// req: CMP-PD-03`

  **What building added:**
    - **Not in the join's transaction.** An invitation code reveals its organisation only when it is redeemed, so the
      page cannot show that organisation's notice before joining.
    - **Right after joining** Fyniq opens Home, where the "Acknowledge policy" card lists the members' notice first.
      The acknowledgement is the library's own operation, due 30 days after joining, as a policy is. D2's "in the same
      unit of work" is not built.

- [x] 3.3 `consent_purpose` and `consent_record` (D2), insert-only and audited, under "Data protection (PDPA)", with
  attack tests and matrix rows. Tests: consent given and withdrawn; none asked by default. `// req: CMP-PD-03`

  **What building added:**
    - **Tables.** Migration 000302 adds `consent_purpose` and `consent_record`, insert-only (FY311).
    - **The database's rules:**
      - a purpose is for members or contacts, and its records name `member:<user id>` or `contact:<contact id>` to
        match;
      - a consent is given only when none stands, and withdrawn only when one does.
    - **Audit.** Each write is audited: `consent_purpose.created`, `consent.given` and `consent.withdrawn`.
    - **The API.** `listConsentPurposes` (with how many consents stand), `createConsentPurpose`, `listConsents` (by
      purpose or person) and `recordConsent`, under "Data protection (PDPA)". The DPO creates; the Owner and the
      Auditor read. A purpose that needs no consent refuses a record.
    - **Not built:**
      - retiring a purpose: it never changes, so a new one is added instead;
      - a member recording their own consent, which waits for slice 6's My personal data.

- [x] 3.4 The screen Compliance › Privacy notices and consent (Expert, `/compliance/privacy/notices`): the notices with
  their versions and acknowledgements, purposes and consents; the join page's notice. Help keys, the guide page
  `compliance-privacy-notices.md` compiled into `kb/`, and component tests with axe. `// req: CMP-PD-03`

  **What building added:**
    - **The screen.** It lists the two notices with the library's component. The contacts' notice says it is given in
      what Fyniq sends. Under the notices:
      - the purposes, with the consents standing;
      - the Compliance officer's dialogs to add a purpose and to record a consent given or withdrawn;
      - every record.
    - **The join page's notice** is the Home card, as 3.2 says.
    - **Navigation.** The Compliance page lists the screen.
    - **Help.** The help keys are `compliance.privacy-notices`, `consent.purpose` and `consent.record`. The guide page
      is compiled into `kb/pages/task/compliance-privacy-notices.md`, and the policies page links to it.

## 4. The data inventory and retention schedule (slice 4)

- [x] 4.1 `compliance/data-inventory.yaml` (D3): every personal-data class Fyniq holds, with "NRIC/FIN" and "card data"
  as not held. A test reads the schema snapshot and fails on a personal-looking column no class names.
  `// req: CMP-PD-02`, `// req: SEC-14`

  **What building added:**
    - **Eight classes:** members; sign-in security; contacts; email-in senders; the bank accounts people are paid to;
      consent; the organisation's own published contacts; files.
    - **`not_personal`** names eight columns whose names look personal but whose data is the company's own.
    - **`not_held`** names NRIC and FIN numbers, and card data.
    - **`TestTheDataInventory`** reads the schema snapshot. It fails on any column whose name looks personal that
      neither a class nor `not_personal` names, and on a named column the schema does not hold. It found
      `organisation.dpo_name`, missing from the first draft.

- [x] 4.2 `data_inventory_entry` (versioned, DPO C) for the organisation's own processing; the retention schedule from
  the catalogue and the settings; the quarterly review as the attestation `data-inventory`, read by CTL-DP-05's Q
  check. Attack tests and matrix rows. `// req: CMP-PD-02`, `// req: CMP-05`, `// req: CTL-DP-05`

  **What building added:**
    - **Entries.** Migration 000303 adds `data_inventory_entry`: versions under an `entry_id`, retired rather than
      deleted.
    - **Reviews.** `data_inventory_review`: one per quarter, whose date must be a quarter's first day.
    - **The database's rules.** Both are insert-only (FY312) and audited (`data_inventory_entry.saved`,
      `data_inventory.reviewed`).
    - **The API.** `getDataInventory` returns:
      - the catalogue and what is not held;
      - the entries;
      - a retention schedule built from the settings in force: records, security details, AI conversations (whose
        setting exists, 365 days), logs and backups;
      - the review.

      `createDataInventoryEntry`, `reviseDataInventoryEntry` (If-Match) and `reviewDataInventory` (A, for the quarter
      waiting only) complete it.
    - **The review** keeps the access review's timing: once a quarter ends, overdue 14 days after.
    - **Reviews list.** It gains the attestation `data-inventory`, which CTL-DP-05's Q check now reads.
    - **Tests:**
      - `TestThePersonalDataInventory`;
      - `TestAttackTheDataInventoryKeepsItsHistory`;
      - the review in `TestEachReviewReadsItsOwnSource`;
      - matrix rows.

- [x] 4.3 The Inventory tab of Compliance › Data protection (Expert, `/compliance/data-protection`), with the retention
  schedule and the DPO tab (contact, website block, policies, penalty). Help keys, the guide page
  `compliance-data-protection.md`, component tests with axe. `// req: CMP-05`

  **What building added:**
    - **The screen.** Compliance › Data protection (Expert) has two tabs; later slices add theirs.
      - **Inventory:** the review's banner with **Record the review**; what Fyniq holds (with where, who, protection and
        abroad in Expert); what it never holds; the company's own entries with add, change and retire; the retention
        schedule.
      - **Data Protection Officer:** the contact card reused from Settings, links to the data protection policy and
        the notices, and the penalty.
    - **Navigation.** The Compliance page lists the screen.
    - **Help.** The help keys are `compliance.data-protection`, `data-protection.inventory`, `data-protection.entry` and
      `data-protection.review`. The guide page is compiled into `kb/`, and the reviews guide gains the inventory's
      review.

## 5. DPIAs (slice 5)

- [x] 5.1 `dpia` and `dpia_version` (D4), insert-only, the DPO approving stepped up, with Fyniq's drafts for the payroll
  link and the AI assistant in `compliance/dpias/`. The attestation `dpia` reads a class or entry that needs one.
  Attack tests and matrix rows. `// req: CMP-PD-08`

  **What building added:**
    - **Tables.** Migration 000304 adds `dpia_version` and `dpia_approval`, insert-only (FY313), both audited
      (`dpia.saved`, `dpia.approved`).
    - **The database's rules:**
      - the text's SHA-256 is checked;
      - an approval is only of the latest version;
      - a key is `payroll`, `ai-assistant` or `other-<uuid>`, with an optional inventory entry.
    - **Fyniq's drafts** (`fyniq-dpia-draft@1`) have the sections of the PDPC's guide. They are written from that
      guide, the PDPC's guidelines on employment and on NRIC (payroll), and its AI recommendation and generative AI
      guidelines (the assistant).
    - **When a DPIA is due.** Each of Fyniq's is required once its feature switch is on (`features.payroll`,
      `features.ai_assistant`). An organisation's DPIA is required while the open inventory entry it assesses needs
      one.
    - **The API:** `listDPIAs`, `getDPIA`, `createDPIA`, `saveDPIA` (If-Match) and `approveDPIA` (A, stepped up).
      Approving Fyniq's draft adopts it as version 1, and a later version needs approving again.
    - **Reviews and controls.** The attestation `dpia` treats a required processing without an approved assessment as
      overdue, because the processing is already live. CTL-DP-05's Q check reads it.
    - **Tests:**
      - `TestTheDPIADrafts`;
      - `TestTheImpactAssessments`;
      - `TestAttackImpactAssessmentsKeepTheirHistory`;
      - the review in `TestEachReviewReadsItsOwnSource`;
      - matrix rows.

- [x] 5.2 The DPIAs tab, with its help keys and guide section. `// req: CMP-PD-08`

  **What building added:**
    - **The tab.** Impact assessments shows each DPIA's reason, version and approval. Its dialog shows the text, which
      the Compliance officer saves as the next version or approves. Others read it.
    - **Help.** The help keys are `data-protection.dpias` and `data-protection.dpia-approve`, and the guide gains a
      section.

## 6. Access and correction requests (slice 6)

- [x] 6.1 `pd_request` and its steps (D5): the 30-day timer and the written extension; a member's own request under
  "Own personal-data requests"; the DPO's report of a person's data from the catalogue. Notices at 7 days and overdue;
  the attestation `pd-requests`. Attack tests and matrix rows. `// req: CMP-PD-04`

  **What building added:**
    - **Tables.** Migration 000305 adds `pd_request` and `pd_request_step`, insert-only (FY314) and audited
      (`pd_request.received`, `pd_request.<step>`).
    - **The database's rules:**
      - `due_on` is the receipt day plus 30;
      - a request about someone other than a member names who asked;
      - each step comes once and in order, and nothing follows closing;
      - closing needs an answer;
      - an extension comes before the answer, within the first 30 days, and names a later day;
      - an answer carries its outcome.
    - **The API:**
      - the DPO's register: `listPersonalDataRequests`, `createPersonalDataRequest` and `stepPersonalDataRequest`
        (If-Match `"s<steps>"`);
      - `getPersonalDataReport`, the subject's data from the inventory's classes;
      - a member's own requests: `askAboutMyPersonalData`, signed in only, and `listMyPersonalDataRequests`, under
        "Own personal-data requests".
    - **The report is audited.** Reading it writes the security event `personal_data.exported` (AUD-05), which joins
      both lists of audited reads.
    - **Notices.** A new request sends the locked security notice `pdpa.request_received` to whoever holds C on "Data
      protection (PDPA)".
    - **Not built: separate day-7 and overdue emails.** The reviews list's attestation `pd-requests` and the DPO's
      Inbox ("Reviews to sign off") show what is due and overdue.
    - **Inventory.** It gains the class `pdpa-requests`.
    - **Contract fixes.** The reviews' kind and cadence enums now name `dpia`, `pd-requests` and their cadences, which
      slice 5 left out. `getDPIA`'s exemption has its full reason.
    - **Tests:** `TestAccessAndCorrectionRequests`, `TestAttackRequestsKeepTheirSteps` and matrix rows.

- [x] 6.2 The Requests tab and Settings › My personal data (Basic). Help keys, guide sections, component tests with axe.
  `// req: CMP-PD-04`

  **What building added:**
    - **The DPO's tab.** Requests shows each request with its status, due day and overdue chip. It has **Record a
      request**, **Next step** (offering only the steps that may follow) and **Data held**, which reads the report.
    - **The member's section.** "My personal data" is a section of Settings › Profile, beside the member's bank
      account, rather than a page of its own. A member asks to see or correct their data and follows their requests.
    - **Help.** The help keys are `data-protection.requests` and `my-data.ask`. The guides gain sections: the DPO's on
      Data protection, the member's on Profile and security, and the two new rows of the reviews table.

## 7. The breach register and the tabletop (slice 7)

- [x] 7.1 `pd_breach` and its steps (D6): the assessment timer (30 days) and the PDPC timer (3 calendar days after the
  decision); the prescribed-data rule, checked against the Notification of Data Breaches Regulations 2021; critical
  notices. The tabletop record. The attestations `breach` and `breach-tabletop`, read by CTL-DP-06. Attack tests and
  matrix rows. `// req: CMP-PD-05`, `// req: CTL-DP-06`

  **What building added:**
    - **Tables.** Migration 000306 adds `pd_breach`, `pd_breach_step` and `pd_breach_exercise`, insert-only (FY315) and
      audited (`breach.recorded`, `breach.<step>`, `breach_exercise.recorded`).
    - **The database's rules:**
      - `assessment_due_on` is the day of awareness, in Singapore, plus 30;
      - `prescribed` is worked out on insert: a name or an identification number together with pay, card or bank
        account numbers, or with credentials (the Regulations' reg 3 and Schedule);
      - each step comes once, nothing follows closing, and the decision follows the assessment;
      - a breach of prescribed data, or of 500 or more people, is never decided not notifiable;
      - a notifiable decision sets `pdpc_due_on` to its day plus 3; the PDPC step needs that decision and carries the
        PDPC's reference;
      - closing needs the decision and, for a notifiable breach, the PDPC step.
    - **The API:** `listBreaches`, `recordBreach`, `stepBreach` (If-Match `"s<steps>"`), `listBreachExercises` and
      `recordBreachExercise`, all under "Data protection (PDPA)".
    - **Notices.** Recording a breach, and deciding it notifiable, send the locked critical notice `pdpa.breach` to the
      DPO, the Owner and the Auditor. As for requests, no separate overdue emails: the reviews list and the DPO's Inbox
      show the timers.
    - **Reviews.** `breach` is overdue while an open breach is past its assessment day or its PDPC day.
      `breach-tabletop` is due 6 months after the last exercise, or after the organisation's start, and waits from 30
      days before. CTL-DP-06 reads both.
    - **Inventory.** It gains the class `breaches`.
    - **Tests:** `TestTheBreachRegister`, `TestAttackTheBreachRegisterKeepsItsRules`, the breach and tabletop cases of
      `TestEachReviewReadsItsOwnSource`, and matrix rows.

- [x] 7.2 The Breaches tab, with its timers. Help keys, guide sections, component tests with axe. `// req: CMP-PD-05`

  **What building added:**
    - **The tab.** Breaches lists each breach with its people, kinds of data, status and the day due next, a
      **Prescribed data** chip and an overdue chip. It has **Record a breach** and **Next step**, which offers only the
      steps the database accepts and disables "No" for a breach the law deems notifiable. **Tabletop exercises** lists
      the rehearsals and has **Record an exercise**.
    - **Help.** The help keys are `data-protection.breaches` and `data-protection.tabletop`. The guide gains the
      section "Breaches" on Data protection and two rows of the reviews table, compiled into
      `compliance.data-protection` and `compliance.reviews`.

## 8. Overseas transfers (slice 8)

- [x] 8.1 `pd_transfer` (D7), versioned. A catalogue flow marked cross-border without a current entry reads as due.
  Attack tests and matrix rows. `// req: CMP-PD-06`

  **What building added:**
    - **Table.** Migration 000307 adds `pd_transfer`: versions that are insert-only (FY316) and audited
      (`transfer.saved`).
    - **The database's rules:**
      - a version follows the transfer's last and keeps the flow it covers;
      - the basis is one of the transfer limitation obligation's: a contract, binding corporate rules, another law or
        binding instrument, a certification such as APEC CBPR, or consent;
      - the next review comes after the assessment.
    - **The catalogue.** `compliance/data-inventory.yaml` gains `flows`: `sign-in` (Google, Zoho or another OpenID
      Connect provider; Zoho's runs in the United States) and `email` (ZeptoMail or Resend, outside Singapore per
      FR-INT-21). Both are cross-border, and where each provider processes is marked [live-verify].
      - The AI connections outside Singapore join the flows when `p1-ai-providers` declares them (AI-75).
      - P1 keeps backups and the DR export in Singapore (SEC-13), so there is no DR flow yet.
      - `TestTheDataInventory` checks that the flows name real classes.
    - **The API:** `getTransfers` returns the flows, each with the open transfer covering it, and the transfers.
      `createTransfer` and `reviseTransfer` (If-Match `"v<n>"`) are for the DPO under "Data protection (PDPA)".
      Names that would clash with the bank transfers carry `PD`.
    - **The review `transfers`.** A flow abroad without an open transfer waits, with no day. A transfer waits from
      30 days before its next review and is overdue after it. CTL-DP-07 gains the daily check `attestation transfers`
      for its "documented transfers".
    - **Not decided here: who records an AI connection's assessment.** AI-75 says the Owner records it. Appendix D
      gives the DPO C and the Owner V on "Data protection (PDPA)", so `p1-ai-providers` decides whether the Owner also
      records it.
    - **Tests:** `TestTheTransferRegister`, `TestAttackTheTransfersKeepTheirHistory`, the transfers case of
      `TestEachReviewReadsItsOwnSource`, and matrix rows.

- [x] 8.2 The Transfers tab, with its help keys and guide section. `// req: CMP-PD-06`

  **What building added:**
    - **The tab.** It lists Fyniq's flows abroad, each **Assessed** or **Needs an assessment**, with **Record the
      assessment**. Below are the transfers, with their basis, next review and **Review due** or **Review overdue**
      chips, **Change**, **Retire it** and **Record a transfer** for the company's own.
    - **Help.** The help key is `data-protection.transfers`. The guide gains the section "Overseas transfers" and a
      row of the reviews table, compiled into `compliance.data-protection` and `compliance.reviews`.

## 9. Person keys and the event format (slice 9)

- [x] 9.1 `person_key` (D8), with the guarded delete and attack tests. A contact's `individual` field. `// req: SEC-11`

  **What building added:**
    - **The key table.** Migration 000308 adds `person_key`: one data key per person and financial year (`cohort`, the
      year's last day), wrapped by the deployment's `ports.Cipher` with the organisation, person and year as
      associated data.
    - **Who may touch keys.** The API, the worker and the executor add and read keys; the read-only role reads none.
      No role holds DELETE. A key never changes (FY317), and the owner deletes one only inside a shredding run, which
      marks its transaction (`fyniq.shredding_run`).
    - **Not built yet: the guarded delete function.** It arrives with the run itself in slice 10.
    - **Who is a person.** Besides members and contacts (D8), people invited (`invitation:<id>`) and people asking to
      join (`join_request:<id>`) are persons too: their names and emails reach the trail before they are members. A
      contact is a person when it is a director or `individual` is set. The contact API and form gain `individual`
      ("A person, not a company").
    - **Key creation is not audited.** A key is key material, and the event that first uses it is audited.
    - **Tests:** `TestPersonalValuesAreSealedUnderThePersonsKey` (attacks, roles, RLS, uniqueness).
- [x] 9.2 `fyniq-audit-event@3`: personal values of a member or an individual contact are sealed under the person's key
  for the event's financial year. The chain hashes the sealed text. `contracts/audit/` gains the v3 schema. Tests:
  - sealed and opened;
  - the chain verifies;
  - every personal key in the action catalogue's changes is sealed;
  - `@1` and `@2` events still verify.

  `// req: AUD-02` and the unnumbered "Personal data in immutable records is crypto-shredded at the end of retention".

  **What building added:**
    - **What is sealed.** In an event about a person (`audit.Subject`), every value of a field named as personal (a
      name, an email address, a phone number or an address, singular or plural, at any depth) is sealed after masking
      as `{"pk", "ct"}`: AES-256-GCM under the person's key for the financial year, with
      `fyniq-audit-personal@1:<organisation>:<event>:<field path>` as associated data. Masking passes sealed values
      through untouched.
    - **The financial year.** It is the organisation's, in its time zone, or the calendar year while the years are not
      set.
    - **Contract.** `contracts/audit/` gains the v3 schema and a README section.
    - **Not sealed: no action catalogue.** None exists, so the rule goes by subject and field name. Events about other
      objects keep their values, such as an email message's recipient, a suppression's address or a notification
      preference's email. That is a gap to close with their classes' own retention.
    - **Tests:**
      - `TestPersonalValuesAreSealed` (domain): every personal key at every depth, the associated data, masking, and a
        failing seal fails the event;
      - `TestTheSubjectOfAnEvent` and `TestTheCohortIsTheFinancialYear`;
      - the adapter test: two financial years, one shredded while the later one opens, and the chain verifies;
      - `TestEveryEventMatchesTheAUD01Schema` and `TestSuperAdmins` now check v3, while `@1` and `@2` events still
        verify.
- [x] 9.3 The audit explorer opens sealed values for V on "Audit trail", and says when a value was shredded. Component
  test. `// req: FR-CTL-03`

  **What building added:**
    - **The explorer.** `getAuditEvent` opens each sealed value (`storage.Audit.OpenSealed`) and marks the change
      `sealed: opened`, or `shredded` once the key is gone, with null values. The list and the export keep the sealed
      text.
    - **The screen** shows a **Sealed** chip beside an opened value and **Shredded** in place of one.
    - **Not built yet: the shredding's date.** It comes from the register in slice 10.
    - **Opening is not an audited read.** The same values show on the contact's own page.
    - **Tests:** `TestTheExplorerOpensSealedValues` and the explorer's component test with axe.

## 10. Shredding, the register and anonymisation (slice 10)

- [x] 10.1 `pdpa.shred` (weekly; D9), the approval object `personal_data_shredding` (the DPO, stepped up), the signed
  `fyniq-shredding@1` register entry before the keys are deleted, and `pdpa.keys_shredded`. Tests:
  - one cohort shredded, a later one readable;
  - the chain verifies;
  - nothing shredded unapproved;
  - the register written first.

  The unnumbered "Personal data in immutable records…" and "Shredded keys do not survive in backups or exports".
- [x] 10.2 The restore step re-applies the shredding and anonymisation register before the copy opens, checking each
  signature. A test restores past a shredding. The runbook is updated. The unnumbered "Shredded keys do not survive…".
- [x] 10.3 Anonymising a contact (D10), audited and registered. Tests: fields replaced, documents unchanged, re-applied
  on restore. The unnumbered "Retention of personal data not needed for records".
- [x] 10.4 The Shredding tab: the runs waiting, approved and done. Help keys, guide section, component test with axe.

  **What building added (10.1 to 10.4):**
    - **Tables.** Migration 000309 adds `pd_shredding_run` (one per financial year, `listed`, then `approved`, then
      `done`; FY318) and `pd_anonymisation`, both audited. It replaces 000308's key guard: a person key goes only
      inside its run, once approved and registered, for the key's own year, after `retention.records_years` (at least
      7).
    - **Two guarded functions.** `fyniq.shred_person_keys` is the worker's and deletes an approved, registered run's
      keys. `fyniq.reapply_shredding` is the restore's: it recreates a run the copy lacks, marked re-applied.
      `fyniq.anonymise_contact` is the API's and the worker's, and needs the recorded anonymisation first.
    - **The DPO approves inline, not through the approval engine.** `approveShreddingRun` is stepped up with If-Match,
      as the DPIA approvals are, rather than an approval object `personal_data_shredding`. The run records the DPO as
      its approver, which the scenarios ask for.
    - **The job runs daily, not weekly.** `pdpa.shred` lists the years whose retention has ended (one run each; the
      locked notice `pdpa.shredding_due` goes to those who approve on "Data protection (PDPA)"). It also shreds the
      approved runs, so an approval takes effect by the next morning.
    - **The register lists key IDs, not fingerprints of the people.** Each shredding writes the signed
      `fyniq-shredding@1` entry (`pdpa-shredding/<organisation>/<run>.json`: the year, the key IDs, the approver),
      then deletes the keys, then records `pdpa.keys_shredded`. The key IDs identify what was destroyed without
      naming anyone.
    - **The restore.** It re-applies both registers after the erasure register (`audit.ReapplyPDPA` in
      `audit.Recover`), checking each signature. The runbook says how, and warns of a copy that agrees with its
      anchors while lacking a shredding.
    - **Anonymisation (D10).** The DPO anonymises a person's contact (a director, or one marked as a person) from
      their answered request, stepped up, with a reason. The signed `fyniq-anonymisation@1` entry is written first.
      Then the emails and the phone go (the columns cannot hold a word) and the address reads Anonymised; the legal
      name stays. The audit event lists the fields, never their values.
    - **Not tested: issued documents after an anonymisation.** No test issues a document to an anonymised contact.
    - **The explorer** now says the year and the day a value was shredded.
    - **The screen.** The Shredding tab lists the runs (Waits for approval, Approved, Shredded) with **Approve**, and
      the anonymised contacts; the Requests tab gains **Anonymise** on an answered request about a contact. The help
      keys are `data-protection.shredding` and `data-protection.anonymise`, with the guide section "Shredding at the
      end of retention".
    - **Tests:**
      - `TestAttackShreddingNeedsTheDPOAndTheRegister`: listed but not shredded; ten attacks; one approval; the register
        down means nothing shredded; the register first; one year shredded while the later one opens; the chain
        verifies; the restore re-applies a missing run and an anonymisation and refuses a foreign signature;
      - `TestShreddingAndAnonymisation` (API), and matrix rows;
      - the Shredding tab's component test with axe.

## 11. NRIC, encryption, residency, logs and backups (slice 11)

- [x] 11.1 The archtest of D11: no NRIC, FIN, card number or CVV in a column, an API field or an event field.
  `// req: CMP-PD-07`, `// req: SEC-14`

  **What building added:**
    - **The rule.** `TestNoNRICOrCardDataIsHeld` refuses any column of the schema snapshot, any property of the API's
      schemas, and any audit change key in the code (`"field": {…}`) named with the word nric, fin, cvv, cvc or pan,
      or card number, security code or id number.
    - **The exception.** A true-or-false flag about one is allowed: `evidence.nric_flagged` and the evidence views'
      `unmasked_nric`. Those are the only matches today.
    - **The inventory** already records NRIC and card data as not held (slice 4).
- [x] 11.2 HSTS from the API, with a test. The `infra/` retention of application logs (30 days) and of Cloud SQL's
  backups, plus the monthly export, each with a module test, `[live-verify]`. `// req: SEC-10`, `// req: SEC-13`,
  and the unnumbered "Retention of application logs" and "Retention of backups".

  **What building added:**
    - **HSTS.** The HTTP shell every service runs in (`internal/httpserver`) sets
      `Strict-Transport-Security: max-age=63072000; includeSubDomains` on every answer: a route's, a probe's and a 404.
      It has no `preload`. Test: `TestEveryAnswerCarriesHSTS`.
    - **Backups.** Cloud SQL keeps daily backups 35 days (from 14), beside the 7 days of point-in-time recovery the
      Enterprise edition has.
    - **Application logs.** The `_Default` log bucket keeps 30 days (`logging.tf`).
    - **Module tests.** Both have asserts in `environment.tftest.hcl`. `make infra-check` passes on mock providers;
      the real buckets are `[live-verify]`.
    - **Not built here: the monthly snapshot kept one year.** It is DR-04's monthly export, which `p1-operations-dr`
      builds with its bucket.
- [x] 11.3 AI conversations' retention (Open Questions): decided when this slice is reached. Either a PRD revision moves
  it to `p1-ai-assistant`, or the setting is built here. The unnumbered "Retention of AI conversations".

  **Decided (the owner, 10 Nov 2026; PRD-001 rev 0.245):**
    - **Moved.** The purge job is built with `p1-ai-assistant`, which keeps the conversations and can test the purge
      on them. The plan maps the requirement there (`scripts/roadmap.py`).
    - **What stays here.** The setting `retention.ai_conversations_days` (365), which this change built, and its row
      in the retention schedule.
    - **Not chosen:** a placeholder job here, or leaving the task open until the assistant lands.

## 12. Canvas, threat model and as built (slice 12)

- [ ] 12.1 The fragments `DataProtection.html` and `PrivacyNotices.html` are checked against the build, and the
  canvases are republished where they differ. This waits for the design account. `python3 docs/design/screens/build.py
  check` passes.
- [x] 12.2 The threat model `docs/security/threat-models/pdpa-retention.md`. It covers the registers, the timers,
  personal keys, shredding, the register, the restore, anonymisation, notices and consent, and who may see what.

  **Progress (10 Nov 2026):**
    - `DataProtection.html` is redrawn as built: the seven tabs, with Breaches shown (the register with its timer and
      Prescribed data chip, Next step, and the tabletop exercises). The inventory row names the tabs. The row wraps at
      344 px.
    - `PrivacyNotices.html` is still to check.
    - Every publish waits for the design account (the owner's decision of 10 Nov 2026): this Claude account cannot open
      the canvases.

  **What building added:** fifteen threats across six boundaries, each mapped to its control and the tests that prove
  it. "What remains" lists:
    - counsel;
    - events about other objects keeping their values;
    - the worker trusted with the restore;
    - a restore within the hour of an anchor;
    - `[live-verify]`;
    - the AI connections abroad;
    - documents after an anonymisation.
- [x] 12.3 `make test` and `make up` pass on the last slice. The build plan's paragraph says what was built.

  **Done on 10 Nov 2026, on #539's branch:**
    - `make test` passed in full, including the authorisation matrix (33 minutes) and the oracle;
    - `scripts/local-smoke.sh` (QA-LOCAL-01) passed: `make up` from the local images on port 8090, since another
      project held 8080 on this machine;
    - the build plan's paragraph is written with slice 12.

## Workflow follow-up

- `/opsx:verify p1-pdpa-retention`, then `/opsx:archive p1-pdpa-retention --skip-specs` once every task is ticked.
- Counsel confirms the [counsel to confirm] clauses before payroll data is loaded (open question 39).
