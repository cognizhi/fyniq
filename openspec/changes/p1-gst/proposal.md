# Proposal

## Why

Cognizhi is not registered for GST (question 2), but two GST duties apply to it now. It must apply to register within
30 days of a forecast that its taxable supplies will exceed S$1 million in the next 12 months, or by 30 January after a
calendar year above it (IRAS). Every company also files ECI, Form C-S/C and the annual return, holds or dispenses with
its AGM, and keeps its registers of controllers on time. The workbook tested the threshold (FR-TAX-05). Nothing yet
tells the founders a deadline is coming, and nothing shows a missed one on Checks.

This change builds the launch part of PRD-001 §8.11 and §14.7: FR-TAX-05 (C.1 #37), FR-TAX-07 and CMP-02 (C.2 #13,
C.2 #15). The build plan says "Monitor and calendar at launch; F5 at registration (Q2)". The GST return itself (FR-TAX-01
to 04, FR-TAX-12, C.2 #46/#47) waits for registration and stays in this change's tasks, unticked.

## What Changes

- **The owner's decisions of 7 Nov 2026, taken interactively (PRD-001 rev 0.208):**
  - **Who records a filing (FR-TAX-07).** A new Appendix D row, **Statutory calendar**:
    - **Create:** the Owner, the Finance controller, the Accountant and the Tax agent;
    - **View:** the Administrator, the Auditor and the Compliance officer.

    The row is added with the build. The alternatives were: the module rows by area (tax under GST returns, payroll
    under Pay runs, corporate under Cap table), where an Owner could not mark the annual return; and the Compliance
    Center row as it stands, where only the DPO creates.
  - **CPF and IR8A/AIS before the payroll link.** An effective-dated organisation setting, **Pays staff**, from the day
    it is turned on. With it off, the calendar shows neither. The alternatives were: waiting for `p1-payroll-link`,
    which leaves FR-TAX-07 open; and always showing them, which gives false deadlines every month.
  - **The early warning (FR-TAX-05, C.1 #37).** ⚠ at 80% of the threshold, an organisation setting
    [accountant to confirm]. The alternatives were: a projection of the year to date (noisy in January); and the two
    tests only (no notice before liability).
- **Statutory packs (ENG-30, §10.6):**
  - **`sg-gst@2026.3`:** `sg-gst@2026.2` plus the registration threshold, S$1,000,000, as a limit (IRAS, "Do I need
    to register for GST").
  - **`sg-deadlines@2026.1`, a new pack:**
    - **The due-date rules:**
      - GST F5: one month after the period end;
      - CPF: the 14th of the next month, on the next working day;
      - IR8A/AIS: 1 March;
      - ECI: three months after the FY end;
      - Form C-S/C: 30 November of the year after the FY end's year;
      - the annual return: seven months after the FY end, eight with an overseas branch register;
      - the AGM: six months after the FY end, or the financial statements sent within five months;
      - applying for GST registration and when it starts: 30 January and 1 March, or 30 days and two months after a
        forecast.
    - **Singapore's public holidays** for 2026 and 2027 (MOM).
    - **IRAS's one-off extension:** an F5 due between 22 December 2026 and 3 January 2027 is due on 9 January 2027.
  - Each row carries its official source, and the pinned cases are worked from IRAS's, CPF Board's, ACRA's and MOM's
    own examples (QA-ENG-08).
- **The GST registration monitor (FR-TAX-05, C.1 #37):**
  - **The retrospective test** adds up the calendar year's taxable supplies from posted invoices, credit notes and
    bank transactions: the lines on revenue accounts whose code is reported in Box 1 or Box 2, at their base amounts.
    Exempt and out-of-scope supplies and sales of capital assets are left out [accountant to confirm].
  - **The prospective test** reads the latest forecast of the next 12 months, recorded with its evidence.
  - **⚠ (C.1 #37)** when either test is met while the organisation is not registered, or when the year so far
    reaches the warning percentage. The monitor states what to do and by when.
- **The statutory calendar (FR-TAX-07, CMP-02):**
  - **Generated, not stored.** Items come from the pack's rules and the organisation's facts:
    - its financial years;
    - its GST registrations and their return periods;
    - the settings Pays staff, AGM route and Overseas branch register;
    - the registers' own deadlines, which come from their existing records and checks: register-of-controllers
      updates (FR-EQ-25), share-issue approvals and returns of allotment (FR-EQ-09) and equity steps (FR-EQ-17).
  - **Recording a filing.** A person records an item as filed, with the filing reference and the acknowledgement as
    evidence, or as not required with the reason, such as the ECI waiver (IRAS). A record is withdrawn, never edited.
  - **Reminders** at 30, 14, 7 and 1 days, through the notification centre (FR-NTF-01).
  - **A "Deadlines" section in the Inbox.**
  - **Checks:**
    - C.2 #13 ⏰: a GST return period ended and not filed, or due within 7 days;
    - C.2 #15 ⏰: a statutory deadline within 14 days or past;
    - Not assessed while the financial year is not set.
- **Screens:**
  - `statutory-calendar` (`/compliance/calendar`);
  - the monitor half of `gst-returns` (`/compliance/gst`), whose return periods appear once the organisation is
    registered;
  - the Inbox's Deadlines section;
  - the new settings in Settings › Organisation.

  `gst-return` and `gst-adjustments` wait for registration.

Out of scope, and why:
- **At GST registration (question 2):** the F5 return, its filing record and lock, the IRAS audit listings, the
  adjustment queue, reverse charge (question 29), C.2 #46 and C.2 #47 (FR-TAX-01 to 04, FR-TAX-12). The calendar's F5
  items are recorded by hand until then.
- **`p1-company-secretary`:** the `sg-acra` pack (FR-CS-02), which moves the register deadlines out of code. The AI
  company secretary is out of scope too.
- **`p1-payroll-link`:** CPF and AIS derived from pay runs, and C.2 #14.
- **P2:** filing through IRAS's APIs and the #SFFS extension of Form C-S to 15 December (`p2-tax-filings`).

## Capabilities

### New Capabilities

None. The requirements are approved in:
- `tax/gst-and-filings` (FR-TAX-05, FR-TAX-07);
- `compliance/compliance-center` (CMP-02);
- `controls/checks` (C.1 #37);
- `controls/platform-checks` (C.2 #13, C.2 #15).

### Modified Capabilities

None as a delta. PRD-001 rev 0.208 records the owner's three decisions in FR-TAX-05 and FR-TAX-07. The main spec
`tax/gst-and-filings` is updated from it and stamped in the same commit (Recipe B). This change only builds approved
requirements: `skip_specs: true`. Appendix D's new row goes into the PRD, `access/roles-and-segregation` and
`internal/authz` together, in the slice that builds it.

## Impact

- **Packs:** `packs/sg-gst/2026.3/` and `packs/sg-deadlines/2026.1/`. `internal/domain/pack` reads the new tables, the
  due-date rule and the working-day roll. `migrate up` loads both versions.
- **Database:**
  - `gst_forecast`: append-only, with evidence;
  - `statutory_filing`: append-only, a withdrawal superseding a record;
  - both under FORCE RLS and audited.
- **Domain:**
  - `internal/domain/gst`: taxable supplies and the monitor;
  - `internal/domain/deadlines`, new: the calendar;
  - `internal/domain/checks`: C.1 #37, C.2 #13, C.2 #15;
  - `internal/notify/reminders`: the daily `notify.deadlines` job.
- **API:** the GST monitor and forecasts under `GSTReturns`; the calendar and filings under the new `StatutoryCalendar`
  row; evidence on both.
- **Settings:**
  - `gst.threshold_warning_percent` (80);
  - `calendar.pays_staff` (off);
  - `corporate.agm_route` (hold);
  - `corporate.overseas_branch_register` (off).
- **Web:**
  - the StatutoryCalendar and GstReturns screens, the Inbox section, and the help keys;
  - the guide pages `statutory-calendar` and `gst-registration`, compiled into `kb/`;
  - the compliance canvas updated as built.
- **Depends on:**
  - `p1-statutory-packs` (pack format, loader);
  - the checks engine;
  - the notification centre;
  - evidence;
  - `p1-equity-register` and `p1-founders-money`, for the registers' deadlines.
