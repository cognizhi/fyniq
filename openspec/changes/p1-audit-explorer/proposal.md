# Proposal

## Why

P0 built the audit trail's backbone:
- every saved change writes an AUD-01 event in the same transaction;
- the events form a per-organisation hash chain;
- the chain is anchored hourly and verified nightly (`p0-audit-chain`).

Nobody can read the trail yet, though. There is no explorer, no export and no "Verify integrity" for a person with
audit access. Sensitive values are only masked by name, and a salary is not masked at all. A period close takes a
hashed snapshot that nobody signs. Reads of full bank numbers are audited one by one, and exports only where a handler
remembered to. Nothing yet handles the retention of the trail, its purge, or a restore behind the published anchors.

The checks, the close and the reports are built now, and `p1-pdpa-retention` needs this change first. So this change
builds PRD-001 §11.5 and §8.15 for launch:
- **AUD-02:** sensitive values in the trail.
- **AUD-03:** "Verify integrity" for users; the nightly part was built in P0.
- **AUD-04:** period-close snapshots, signed and anchored.
- **AUD-05:** sensitive reads and every export audited.
- **AUD-06:** the trail's retention.
- **AUD-07 and FR-CTL-03:** the explorer and its export.
- **AUD-09:** purge and restore keep the chain verifiable.
- **AUD-10 (new in rev 0.230):** the ledger's purge keeps the books whole.

The auditor evidence pack (AUD-07's P2 part) is in `p2-` work. The analytical copy (§11.5 L5) waits for BigQuery
(§11.7, stage 2).

## What Changes

- **The owner's decisions of 8 Nov 2026, taken interactively (PRD-001 rev 0.230):**
  - **Sensitive values are proof only (AUD-02).** A sensitive value in `changes` (NRIC or FIN, a bank or card number,
    a salary) is stored as the organisation's keyed fingerprint (`ports.Cipher.Fingerprint`, HMAC-SHA256 under a key
    in Cloud KMS, SEC-11), with an identifier's last 4 characters as its hint.
    - The trail shows that the value changed, and whether it changed back. Nobody reads the value from the trail.
    - The alternatives were values encrypted for the Payroll admin and the DPO, which needs §11.8's per-person key
      service first, and both together.
  - **Security events keep their details for 400 days (AUD-06).** The setting `retention.security_events_days` is 400
    by default, under dual control (FM-1). At its end, a security event's IP address and device are erased and the event
    stays, so the chain still verifies. Security events are sign-ins, device and session events, sensitive reads and
    exports. The alternatives were 1 year, 2 years and 7 years.
  - **Open question 38's purge is built in full (AUD-09, new AUD-10).** It covers the trail's purge and the ledger's,
    with balances carried forward at the boundary.
    - The alternatives were the trail's purge now and the ledger's later, since no record can pass its retention
      before 2034, and neither now.
    - The owner chose both, so AUD-10 states how the ledger's part keeps the books whole.
- **Security events' details sit beside the chain (AUD-06).** From the event format `fyniq-audit-event@2`, a security
  event's IP address and user agent are kept beside the event, and its hashed body carries their keyed fingerprint. The
  chain still proves them while they exist. A daily job erases them after the retention, records each erasure in the
  trail and in a signed erasure register outside the database, and a restore re-applies the register. Other events keep
  their request details in the body, for as long as the records they are about.
- **Sensitive reads and exports (AUD-05).** Every operation that returns a file, or a full identifier, writes an audit
  event. A conventions test reads the contract and fails on any such operation that does not.
- **Period-close snapshots, signed and anchored (AUD-04).** The close's snapshot gains the subledger totals and the
  audit chain's head (`fyniq-close-snapshot@2`). After the close commits, it is signed and written to the write-once
  bucket. A reopen does the same for its before and after snapshots.
- **Verify integrity (AUD-03).** A person with audit access verifies a date range. The platform:
  - recomputes the range's links;
  - checks every chain anchor and close anchor in the range;
  - reports passed, failed (the first bad event and why) or explained (a purge boundary or a restore).
- **The audit trail explorer and export (FR-CTL-03, AUD-07):**
  - filters by user, AI originator, record, field, time, action and approval;
  - before and after values for each event, with sensitive values shown as "changed" and their hint;
  - an asynchronous CSV or XLSX export, which is itself audited.
- **Purge (AUD-09, AUD-10, open question 38).** The switch `retention.purge_after_retention` is off by default and
  turns on only under dual control. With it on, a job runs two purges:
  - **The trail's.** It purges only whole chain segments whose events have all passed the records' retention. It first
    writes a signed, anchored boundary record, and verification starts from the latest boundary.
  - **The ledger's.** It purges a financial year's journal entries once that year's records have passed their
    retention and every earlier year is purged. It first writes a signed ledger boundary record, then posts
    carry-forward entries dated the boundary:
    - balance-sheet accounts by account, dimension set and currency;
    - control accounts per open source document;
    - revenue and expense into retained earnings.

    Entries that a permanent register needs (share issues) are kept. The oracle independently checks that every report
    from the boundary on is unchanged. Reports before the boundary, or as at a revision before the purge, are refused
    with their reason.

  Each deletion goes through a database function that refuses anything past its boundary.
- **Restore behind the anchors (AUD-09).** A restore that lands behind published anchors is found, either by the
  restore runbook's first step or by the nightly verification. The platform:
  - records a signed recovery event naming the restored head and the anchored range now missing;
  - raises the alert, the ✖ check and the Owner's and DPO's notification;
  - continues the chain under a new anchor epoch.

  Verification reports the gap as explained, never clean.
- **Screen:** Compliance › Audit trail (Expert), designed on the compliance canvas. Its help keys, its guide page
  compiled into the knowledge base, and a threat model come with it.

## Capabilities

### New Capabilities

None. The requirements are approved in the main specs: `data/audit-trail` (AUD-02, AUD-03, AUD-04, AUD-05, AUD-06,
AUD-07, AUD-09, AUD-10 and the analytical-copy layer) and `controls/checks` (FR-CTL-03). This change sets
`skip_specs: true`.

### Modified Capabilities

None in this change. Rev 0.230's decisions are already in the main specs, stamped: AUD-02, AUD-06 and the new AUD-10.
If building shows a requirement is wrong, the PRD changes first (fyniq-openspec Recipe B).

## Impact

- **Domain:**
  - `internal/domain/audit`: fingerprints, security classes, range verification, boundaries, recovery and anchor epochs,
    and the erasure register;
  - `internal/domain/closing`: the snapshot @2 and the close anchor;
  - `internal/domain/retention` (new): the purge plan for the trail and the ledger, and the carry-forward entries, a new
    posting rule (`purge-carry-forward@1`) in archtest's money packages;
  - `internal/domain/balance`: refusing dates and revisions before a ledger boundary;
  - `internal/reportexport`: the `audit_trail` export.
- **Oracle:** the purge as a differential source. The oracle carries a generated ledger forward at a boundary from the
  rules, and the trial balances and sub-ledgers from the boundary on must match the engine's to the cent.
- **Database:**
  - the table of security events' request details, which only the erasure job deletes;
  - the audit and ledger boundary records, and the recovery records;
  - `fyniq.audit_purge` and `fyniq.ledger_purge`, the one way an event or a posting is deleted, with attack tests that
    INV-02, INV-03 and AUD-08 still hold everywhere else;
  - indexes for the explorer's filters;
  - the close's snapshot format.
- **API:**
  - the explorer: list events, get an event, export them;
  - `GET /v1/audit/verification`;
  - the audit kinds on file-returning and revealing operations;
  - authorisation-matrix rows on Appendix D's "Audit trail" row: V for Owner, Admin, Controller, Accountant, Auditor,
    Tax agent and DPO.
- **Jobs:**
  - the security-detail erasure job;
  - the purge job;
  - the close-anchor job, through the outbox;
  - the recovery step in the nightly verification and in the restore runbook.
- **Settings:** `retention.security_events_days` (400) and `retention.purge_after_retention` (off), both under dual
  control.
- **Web:** Compliance › Audit trail. The inventory row's roles follow Appendix D: Admin and DPO, not "Compliance
  officer".
- **Docs:**
  - the guide page `audit-trail.md`, compiled into `kb/`;
  - the runbook `docs/runbooks/audit-restore.md`;
  - a threat model;
  - the build plan's wave-5 paragraph.
- **No AI or MCP surface.** The AI holds nothing on the "Audit trail" row, and no tool reads the trail.
- **Second human reviewer.** The change touches the audit trail and, through the purge, posted history (AGENTS.md).
  The founders waived this before production, so the purge's slices are labelled `needs-human-review` to hold them for
  a person.
