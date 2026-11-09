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

- [ ] 4.1 `compliance/data-inventory.yaml` (D3): every personal-data class Fyniq holds, with "NRIC/FIN" and "card data"
  as not held. A test reads the schema snapshot and fails on a personal-looking column no class names.
  `// req: CMP-PD-02`, `// req: SEC-14`
- [ ] 4.2 `data_inventory_entry` (versioned, DPO C) for the organisation's own processing; the retention schedule from
  the catalogue and the settings; the quarterly review as the attestation `data-inventory`, read by CTL-DP-05's Q
  check. Attack tests and matrix rows. `// req: CMP-PD-02`, `// req: CMP-05`, `// req: CTL-DP-05`
- [ ] 4.3 The Inventory tab of Compliance › Data protection (Expert, `/compliance/data-protection`), with the retention
  schedule and the DPO tab (contact, website block, policies, penalty). Help keys, the guide page
  `compliance-data-protection.md`, component tests with axe. `// req: CMP-05`

## 5. DPIAs (slice 5)

- [ ] 5.1 `dpia` and `dpia_version` (D4), insert-only, the DPO approving stepped up, with Fyniq's drafts for the payroll
  link and the AI assistant in `compliance/dpias/`. The attestation `dpia` reads a class or entry that needs one.
  Attack tests and matrix rows. `// req: CMP-PD-08`
- [ ] 5.2 The DPIAs tab, with its help keys and guide section. `// req: CMP-PD-08`

## 6. Access and correction requests (slice 6)

- [ ] 6.1 `pd_request` and its steps (D5): the 30-day timer and the written extension; a member's own request under
  "Own personal-data requests"; the DPO's report of a person's data from the catalogue. Notices at 7 days and overdue;
  the attestation `pd-requests`. Attack tests and matrix rows. `// req: CMP-PD-04`
- [ ] 6.2 The Requests tab and Settings › My personal data (Basic). Help keys, guide sections, component tests with axe.
  `// req: CMP-PD-04`

## 7. The breach register and the tabletop (slice 7)

- [ ] 7.1 `pd_breach` and its steps (D6): the assessment timer (30 days) and the PDPC timer (3 calendar days after the
  decision); the prescribed-data rule, checked against the Notification of Data Breaches Regulations 2021; critical
  notices. The tabletop record. The attestations `breach` and `breach-tabletop`, read by CTL-DP-06. Attack tests and
  matrix rows. `// req: CMP-PD-05`, `// req: CTL-DP-06`
- [ ] 7.2 The Breaches tab, with its timers. Help keys, guide sections, component tests with axe. `// req: CMP-PD-05`

## 8. Overseas transfers (slice 8)

- [ ] 8.1 `pd_transfer` (D7), versioned. A catalogue flow marked cross-border without a current entry reads as due.
  Attack tests and matrix rows. `// req: CMP-PD-06`
- [ ] 8.2 The Transfers tab, with its help keys and guide section. `// req: CMP-PD-06`

## 9. Person keys and the event format (slice 9)

- [ ] 9.1 `person_key` (D8), with the guarded delete and attack tests. A contact's `individual` field. `// req: SEC-11`
- [ ] 9.2 `fyniq-audit-event@3`: personal values of a member or an individual contact are sealed under the person's key
  for the event's financial year. The chain hashes the sealed text. `contracts/audit/` gains the v3 schema. Tests:
  - sealed and opened;
  - the chain verifies;
  - every personal key in the action catalogue's changes is sealed;
  - `@1` and `@2` events still verify.

  `// req: AUD-02` and the unnumbered "Personal data in immutable records is crypto-shredded at the end of retention".
- [ ] 9.3 The audit explorer opens sealed values for V on "Audit trail", and says when a value was shredded. Component
  test. `// req: FR-CTL-03`

## 10. Shredding, the register and anonymisation (slice 10)

- [ ] 10.1 `pdpa.shred` (weekly; D9), the approval object `personal_data_shredding` (the DPO, stepped up), the signed
  `fyniq-shredding@1` register entry before the keys are deleted, and `pdpa.keys_shredded`. Tests:
  - one cohort shredded, a later one readable;
  - the chain verifies;
  - nothing shredded unapproved;
  - the register written first.

  The unnumbered "Personal data in immutable records…" and "Shredded keys do not survive in backups or exports".
- [ ] 10.2 The restore step re-applies the shredding and anonymisation register before the copy opens, checking each
  signature. A test restores past a shredding. The runbook is updated. The unnumbered "Shredded keys do not survive…".
- [ ] 10.3 Anonymising a contact (D10), audited and registered. Tests: fields replaced, documents unchanged, re-applied
  on restore. The unnumbered "Retention of personal data not needed for records".
- [ ] 10.4 The Shredding tab: the runs waiting, approved and done. Help keys, guide section, component test with axe.

## 11. NRIC, encryption, residency, logs and backups (slice 11)

- [ ] 11.1 The archtest of D11: no NRIC, FIN, card number or CVV in a column, an API field or an event field.
  `// req: CMP-PD-07`, `// req: SEC-14`
- [ ] 11.2 HSTS from the API, with a test. The `infra/` retention of application logs (30 days) and of Cloud SQL's
  backups, plus the monthly export, each with a module test, `[live-verify]`. `// req: SEC-10`, `// req: SEC-13`,
  and the unnumbered "Retention of application logs" and "Retention of backups".
- [ ] 11.3 AI conversations' retention (Open Questions): decided when this slice is reached. Either a PRD revision moves
  it to `p1-ai-assistant`, or the setting is built here. The unnumbered "Retention of AI conversations".

## 12. Canvas, threat model and as built (slice 12)

- [ ] 12.1 The fragments `DataProtection.html` and `PrivacyNotices.html` are checked against the build, and the
  canvases are republished where they differ. This waits for the design account. `python3 docs/design/screens/build.py
  check` passes.
- [ ] 12.2 The threat model `docs/security/threat-models/pdpa-retention.md`. It covers the registers, the timers,
  personal keys, shredding, the register, the restore, anonymisation, notices and consent, and who may see what.
- [ ] 12.3 `make test` and `make up` pass on the last slice. The build plan's paragraph says what was built.

## Workflow follow-up

- `/opsx:verify p1-pdpa-retention`, then `/opsx:archive p1-pdpa-retention --skip-specs` once every task is ticked.
- Counsel confirms the [counsel to confirm] clauses before payroll data is loaded (open question 39).
