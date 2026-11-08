# Design

## Context

- **What P0 built (`p0-audit-chain`, `internal/domain/audit`):**
  - **The event.** `fyniq-audit-event@1` is canonical JSON, stored as text in `fyniq.audit_event` and hashed exactly as
    stored: `hash = SHA-256(prev_hash ‖ body)`.
  - **The chain.** It is per organisation, appended under `audit_chain`'s head with a transaction-scoped lock.
  - **Append-only.** A trigger refuses UPDATE, DELETE and TRUNCATE (FY060, AUD-08), and another refuses an audited change
    without an event.
  - **Generated columns.** `action`, `object_type` and `object_id`, with one index on (organisation, transaction,
    object).
- **Masking today (`mask.go`, SEC-15).**
  - Secrets are removed by field name.
  - Identifiers under a "number" field name keep their last 4 characters.
  - NRIC numbers and tokens are found by shape in any text.
  - A salary is not masked, and a last-4 mask cannot show that a value changed back.
- **Anchors and verification.** `audit.anchor` (hourly) signs each chain head with `ports.Signer` (Cloud KMS EC P-256;
  local Ed25519) and writes it once to `audit-anchors/<org>/<seq>` through `ports.BlobStore.Create`. In the cloud the
  bucket is retention-locked; locally it is fake GCS. `audit.verify` (nightly) recomputes every chain from its first
  event and checks every anchor. Its outcome is an audit event that C.2 #3 reads. Both run through the platform's
  schedules (ADR-T7).
- **Keyed fingerprints already exist.** `ports.Cipher` (SEC-11; `internal/fieldseal`) has
  `Fingerprint(scope, value)`. In the cloud it is HMAC-SHA256 by Cloud KMS `macSign` (`gcp.KMSFields`); locally it uses
  a key file. Its doc names AUD-02. One fingerprint is one KMS call.
- **The close (`internal/domain/closing`).** It takes a snapshot under the ledger lock (ENG-47 step 2):
  - the trial balance at the close's revision, as canonical JSON (RFC 8785) with its SHA-256, in `period_close`;
  - a reopen records its after snapshot the same way.

  Nothing signs or anchors either snapshot.
- **Reads and exports today:**
  - **Reveals.** Revealing a payee's or claimant's full bank number writes `document.bank_account_revealed`.
  - **Handler-level exports.** The statements, cap table, related parties, director statement and access review exports
    each write an `export` event.
  - **Async exports.** `reportexport` audits the exports it runs.
  - **Gaps.** Downloading evidence, a payment file and the brand files are not audited as exports.

  A file travels inside JSON as `content_base64`; no operation returns a bare file.
- **Retention.** `retention.records_years` (7) and `evidence.RetainUntil` give a record's retain-until (the end of its
  financial year plus the years, FR-DOC-02). Open question 38 keeps ledger entries and their audit events for life
  unless an organisation switches their purge on under dual control (PRD-001 rev 0.25). The owner chose on 8 Nov 2026
  to build both purges now (rev 0.230, AUD-10).
- **The ledger.**
  - `journal_entry` and `journal_line` are insert-only: privileges are revoked and a trigger refuses changes (INV-02).
    Locked periods refuse postings (INV-03).
  - There are no closing entries: revenue and expense fold into retained earnings when read (FR-GL-10).
  - Three tables point at entries: `bank_match` and `bank_transfer` (reconciliation), and `share_issue` (the register
    of share issues, kept for the life of the company).
  - The sub-ledgers read each source document's control lines (`balance.ControlBySource`).
- **Access.** Appendix D's "Audit trail" row (`authz.AuditTrail`) gives V to Owner, Admin, Controller, Accountant,
  Auditor, Tax agent and DPO; nobody has C or A, and the AI has nothing.
- **Screen.** AuditTrail is designed on the compliance canvas (`docs/design/screens/src/AuditTrail.html`). The
  canvases are held until Tue 10 Nov 2026 06:00 SGT (the account switch).

## Goals / Non-Goals

**Goals:**
- A person with audit access can find any change, see what changed, prove that the trail is whole for any range, and
  export what they found. Every one of those reads that reveals or exports is itself in the trail.
- No sensitive value is readable from the trail, and no security event's IP or device outlives 400 days, while the
  chain stays verifiable.
- A close is a signed, anchored statement of the books and the trail at that moment.
- Purge and restore can never make the trail read as clean when it is not.

**Non-Goals:**
- **The auditor evidence pack** with the verification results (AUD-07's P2 part) and the auditor workspace (SEC-06,
  its own change).
- **The analytical copy** (§11.5 L5): it waits for BigQuery (§11.7 stage 2).
- **Per-person cohort keys and crypto-shredding** of payroll values (§11.8, open question 39): `p1-pdpa-retention`
  builds them and extends this change's erasure register to key destruction.
- **The purge of documents, attachments and bank statements** (§11.9's own purge job for documents). A purged year's
  invoices, bills and statement lines stay, and say that their postings were purged. Their own purge belongs with the
  documents, after their retain-until (FR-DOC-02).
- **SEC-32's incident register** (`p1-security-hardening`). This change raises the alert, the ✖ check and the
  notifications, and names the runbook.

## Decisions

### D1. Proof only: keyed fingerprints for sensitive values (AUD-02; owner, rev 0.230)

- **Which values.** A value in `changes` under a sensitive field name is replaced by
  `{"fingerprint": "fp1:<base64url HMAC>", "hint": "*****567D"}`. Sensitive names are the identifier names masked
  today (`nric`, `fin`, `bank_account`, `account_number`, `iban`, `card_number`, `paynow`) plus the pay names
  (`salary`, `wage`, `gross_pay`, `basic_pay`).
  - The hint is an identifier's last 4 characters; a pay field has no hint.
  - Equal values have equal fingerprints within the organisation, so "changed" and "changed back" both show.
  - Secrets stay `[redacted]`, and text keeps its NRIC and token masking.
- **Where it runs.** The unit of work fingerprints values when it records the event, with the deployment's
  `ports.Cipher`; the domain's `audit.New` stays pure and marks which fields are sensitive. The event format becomes
  `fyniq-audit-event@2`. A v1 event keeps verifying, since verification hashes stored text and never parses its schema.
- **Cost.** One KMS `macSign` per sensitive value. Sensitive values are rare: a payee's bank details, a pay figure.
- **Alternatives:**
  - encrypted values readable by the Payroll admin and the DPO (the owner chose proof only);
  - a fingerprint key held by Fyniq, which would make leaking that one secret enough to test guesses of a salary
    offline.

### D2. Security events keep their details beside the chain (AUD-06; owner, rev 0.230)

- **Which events.** A security event is one whose action is in `audit.SecurityActions`: sign-in, sign-out, session,
  device, passkey and factor events, every reveal (D3) and every export (D3). A conventions test lists the actions
  emitted in the code and refuses one that matches a security pattern (`auth.`, `.revealed`, `.exported`, `export`) but
  is not in the list.
- **What is stored where.** A security event's `request.ip` and `request.user_agent` go to
  `fyniq.audit_request_detail` (organisation, event ID, IP, user agent), inserted in the same transaction. The body
  keeps `request.id` and `request.session` and carries `request.detail`, the fingerprint of the two values.
  - While the row exists, verification checks it against that fingerprint, so an altered IP is caught.
  - Other events keep their request details in the body: they are about records, retained as long as the records.
- **Erasure.** `audit.erase` (daily, worker role) deletes the rows older than `retention.security_events_days` (400;
  `Days`, min 30, max 3,650; dual control). No other role may delete; a trigger refuses a row younger than the setting
  allows. Each run then:
  - writes one audit event (`audit.details_erased`: the cut-off, the count, the first and last sequence numbers);
  - writes a signed erasure-register entry to the write-once bucket (`audit-erasures/<org>/<cut-off>.json`), D8.
- **Alternatives:**
  - encrypting the details under a key per organisation and year, then destroying the key: it needs the cohort-key
    service first, and a KMS wrap on every sign-in;
  - an unkeyed hash, which an IPv4 address does not survive: 2^32 guesses;
  - keeping the details in the body: then erasing them breaks the chain.

### D3. Every sensitive read and every export is audited (AUD-05)

- **What counts.** An operation whose success response carries a file (a schema with `content_base64`, or a link to
  one), or a full identifier (a reveal), has an **audit kind**, `export` or `read`, in `internal/api`'s registry.
- **The event.** Unless its handler already records a richer event, a hook writes, in its own unit of work after the
  handler succeeds:
  - the operation;
  - the object from the path;
  - the format and parameters;
  - the file's SHA-256 when there is one.

  If the event cannot be written, the response is an error: there is no unaudited export.
- **The test.** A conventions test walks the generated contract. It fails on a file-carrying or revealing operation
  with no kind, and on a kind whose call writes no event.
- **Payroll.** Fyniq has no payroll reads yet; `p1-payroll-link` registers its operations with the same kinds.
- **The explorer's own reads.** They are not sensitive reads, since the trail holds no sensitive values (D1). Its
  export is an export.
- **Alternative:** per-handler calls only, which is how today's gaps came about.

### D4. Close snapshots are signed and anchored (AUD-04)

- **What the snapshot holds.** `fyniq-close-snapshot@2` adds two things to today's snapshot:
  - **The subledger totals** at the period end, for every control account the close's own reconciliations compute:
    receivables, payables, the fixed-asset register, director and founder balances, and those built later.
  - **The audit chain's head** (sequence and hash) read under the same ledger lock.

  The trial balance and revision are as today.
- **Signing.** After the close commits, the outbox job `audit.anchor_close` signs a close anchor and writes it once to
  `audit-anchors/<org>/close/<period>/<revision>.json`. The anchor holds the organisation, the period, the revision,
  the snapshot's SHA-256 and the chain head. A reopen's before and after snapshots are anchored the same way.
- **Verification.** It recomputes each stored snapshot's hash and checks it, and the chain head, against its anchor.
- **Alternative:** signing inside the close's transaction, which is a KMS call while holding the ledger lock (ADR-T7
  prefers the outbox).

### D5. Verify integrity for a range (AUD-03)

`GET /v1/audit/verification?from=&to=` (dates, at most 366 days apart) answers with a result for the range:
- **The checks:**
  - it finds the events in the range;
  - it checks the first one's `prev_hash` against the event before it, or the latest boundary (D7);
  - it recomputes every link and checks every security event's details fingerprint (D2);
  - it checks every chain anchor and close anchor in the range, with their signatures.
- **The outcome:**
  - **passed;**
  - **failed**, with the first bad sequence number and why: a broken link, an anchor mismatch, a bad signature or a
    missing event;
  - **explained**, with each purge boundary or recovery gap (D8) and its record.
- **Shape.** It is a GET because it saves nothing but its own audit event, `audit.verified_by_member`.
- **The nightly job** keeps verifying the whole chain, and now uses the same function.

### D6. The explorer and its export (FR-CTL-03, AUD-07)

- **Listing.** `GET /v1/audit/events` is cursor-paged by sequence, newest first, with filters:
  - the actor (a member), and the AI originator (any, or one proposal);
  - the record (type and ID), and a field (a key in `changes`);
  - from and to;
  - the action (or its prefix), and the approval.

  Expression indexes on the actor, the time and the originator come in a migration; a field filter uses the body's
  `changes` keys.
- **One event.** `GET /v1/audit/events/{id}` returns the event with its before and after values:
  - a sensitive value shows as `changed`, `changed back` or `unchanged`, with its hint (D1);
  - a security event shows its IP and device, or the day they were erased (D2);
  - every event shows its sequence, `prev_hash` and `hash`.
- **Export.** It is the asynchronous report export `audit_trail` (CSV or XLSX, the list's filters), audited like every
  report export and counted as an export (D3).
- **Access.** V on "Audit trail" for every operation; the matrix rows prove it, and that the AI and the other roles are
  refused.

### D7. Purge: the trail's segments and the ledger's years, each behind a boundary (AUD-09, AUD-10, Q38; owner)

- **The switch.** `retention.purge_after_retention` is a switch, off by default, changed under dual control (FM-1)
  like every FM-1 setting. One switch covers both purges, as Q38 couples ledger entries and their audit events.
- **The job.** `retention.purge` runs weekly on the worker role. The ledger's year goes first (D7b), then the trail's
  segment (D7a), each in its own transactions and steps, so a failure part-way leaves a boundary record and nothing
  half-deleted.

#### D7a. The trail's segments

- **Which events can go.** With the switch on, an event's retain-until is `evidence.RetainUntil` of its time and
  `retention.records_years`.
  - The purge job finds the longest run of events from the chain's start whose
    retain-until has passed. It stops at the first event that has not passed, or that is about a record on legal hold.
  - A security event's own retention is shorter, but its position in the chain still waits for the records around
    it.
- **The order of a purge:**
  1. Write the **boundary record**: the organisation, the last purged sequence and hash, the setting change's approval
     and the counts by action class. It goes to `fyniq.audit_boundary` and is signed to
     `audit-anchors/<org>/boundary/<seq>.json`.
  2. Call `fyniq.audit_purge(last_seq)`. This SECURITY DEFINER function belongs to the migrator and is executable only
     by `fyniq_worker`. It deletes the organisation's events up to `last_seq` only when a boundary at exactly that
     sequence exists and the switch is on. It lifts the immutability trigger for that statement alone, by a
     transaction-local flag that only it sets.
  3. Write the purge's own audit event.
- **Verification** starts at the latest boundary: the first remaining event's `prev_hash` must equal the boundary's hash.
- **The attack tests** try to:
  - delete events directly;
  - delete through the function without a boundary, or past it;
  - call it as `fyniq_api`;
  - set the flag by hand.
- **Alternative:** a role allowed to DELETE, which any bug could use.

#### D7b. The ledger's years (AUD-10)

- **Which year.** The oldest financial year Y not yet purged. It goes only when all of these hold:
  - the end of Y plus `retention.records_years` has passed;
  - every period of Y is locked;
  - no record dated in Y is on legal hold.

  The boundary B is the first day of year Y+1.
- **What is purged.** Every entry dated before B, except (rev 0.238):
  - entries a permanent register points at (`share_issue`: the register of members is kept for life, INV-13), the
    equity register's (`equity_transaction`), any with a line on share capital, and the asset register's depreciation
    and disposals, which it reads month by month for as long as an asset is held;
  - entries a reconciliation of the bank names (its `bank_match` and `bank_transfer` rows), so no statement line kept
    reads as unreconciled;
  - entries that kept entries reverse, or are reversed by, followed until nothing changes, so no reversal names an
    entry that is gone.
- **The carry-forward** (posting rule `purge-carry-forward@1`, in `internal/domain/retention`, a pure function of the
  lines being purged; rev 0.238). Each purged line is summed, amount and base amount apart, under a key of its source
  document and:
  - **Revenue and expense:** the retained earnings account, its dimension set and the base currency, the amount being
    the base amount, because year-end folds them there (FR-GL-10). Read at any date from B on, the fold gives the same
    figures.
  - **Every other account:** its account, dimension set, currency, person and instrument. Every sub-ledger reads its
    account by document, and the reconciliations of an account with its sub-ledger (`balance.Control`,
    `subledger.Reconcile`) read it by source type: receivables and payables, deposits, the spreads' holding accounts,
    the asset register. So each document's part stays with it (INV-07), and each director's account, shareholder's
    application monies and SAFE still read their own (FR-EQ-01, FR-EQ-18, INV-13). A document whose lines net to zero
    on an account (an item reversed or credited under its own document) carries nothing there; an invoice and the
    separate receipt that settled it each carry their own line, which net to zero on the account.

  A key whose amount and base amount are both zero carries nothing. Each other key carries as lines:
  - in the base currency, one line of its sum;
  - in another currency with an amount of zero, one base-only line (amount 0, rate 1) of its base;
  - otherwise one line of its amount at the rate base ÷ amount rounded half away from zero to 10 decimals (1 when that
    is not above zero, or does not fit a rate's 10 integer digits), whose base is the FX rule's (RND-FX), then a
    base-only line of what is left of the base, if anything (INV-05), as the FX revaluation writes.

  The entries: one per source document with a balance left, under the document's own source type and ID, dated the
  day before B in the purged year's last period, so every report from B on reads them as its opening balance and none
  as a movement. Each purged entry balances, so each document's carry-forward does: its revenue and expense in retained
  earnings balance what its balance-sheet lines carry, and no suspense line is needed. Documents come by source type
  and ID; within an entry, keys run by account code, dimension set, currency, person and instrument. By construction,
  the carry-forward lines sum to exactly what is purged, so every balance from B on is unchanged and every entry still
  balances (INV-01). The retained earnings account is not revenue or expense; the purge refuses a chart where it is.
- **The order**, in one transaction of the worker's (as built, slice 9):
  1. The **ledger boundary record** is signed and written to `ledger-boundaries/<org>/<B>-<hash>.json` in the anchors'
     bucket, then recorded in `fyniq.ledger_boundary`: B, the counts, the SHA-256 of the purged lines in canonical form
     (an attestation of exactly what went) and of the carry-forward, and the switch's approval. Its guard checks the
     switch, the organisation's financial years, that B is a year's first day at least 7 years past, that the year is
     the oldest still on the books, and that every period before B is closed; it stamps the transaction.
  2. The job posts the carry-forward entries (rule `purge-carry-forward@1`, dated B − 1) through the posting service
     (`journal.CarryForward`, the one path to the ledger). Only an entry of the transaction that recorded a boundary
     at the next day may carry that rule or the source `ledger_boundary`; it may
     post into the closed period, its lines may name an account, a person or an instrument that changed since, a base
     residual may be base-only, and a document's release or depreciation rules do not apply to it.
  3. `fyniq.ledger_purge(B, entries)` (SECURITY DEFINER, the migrator's, executable only by `fyniq_worker`) checks that
     each entry is dated before B, refuses one a register or the bank's reconciliation needs and a reversal split from
     what it reverses, keeps a **tombstone** of each in `fyniq.ledger_purged_entry` (its posting identity, source and
     dates), deletes the lines and entries under its own flag (INV-02 is lifted for those statements only, and only
     behind a boundary of the same transaction), rebuilds the balances cache, marks the purged periods `purged_on` and
     records the boundary's carried revision. A boundary commits only with its purge done.
     Before it deletes, it checks that the carry-forward this transaction posted replaces the purged lines exactly, by
     account, dimension set, currency, person, instrument and source (revenue and expense folded into retained
     earnings, which counts whole): FY305 otherwise, so not even the table's owner purges without it.
  4. The job compares the balances cache with the lines (INV-08) and commits. It writes `ledger.purged` and each
     period's `period.purged`.
- **What readers do.**
  - The balance service refuses a day before the latest boundary, a period that ends before it, and an "as at"
    revision earlier than the purge's (ENG-46), because history before it no longer exists to reproduce: all give
    `BALANCE-PURGED` with the boundary. A position, a general ledger or a year's result from B on reads the purged
    periods' balances (the carry-forward) as its opening.
  - `Ledger.ByIdentity` asked for a purged posting returns its document's latest carry-forward (or an empty entry when
    it left nothing), marked `Carries`: every sub-ledger that reads a document's posting by identity (receivables,
    payables, deposits, claims, voids) reads the balance it left. `Ledger.PurgedOfSource` lists a document's purged
    postings, which says that they were purged and when; each document's own view shows it with the screen (slice 11).
  - A purged period never reopens. Nightly verification (INV-08) recomputes from the carry-forward lines on.
- **Two implementations agree.** The oracle's own purge, written from AUD-10 and not from the Go code, carries a
  generated ledger forward at a boundary. The differential source `purge` compares the engine's and the oracle's trial
  balances and sub-ledgers at dates from B on, and both against the unpurged ledger's. Hypothesis searches for a
  ledger where they differ.
- **The attack tests** try to:
  - delete or update a posting directly (INV-02);
  - post into a locked period (INV-03);
  - call `ledger_purge` without a boundary, as `fyniq_api`, or with the switch off;
  - set the flag by hand;
  - purge a year before an earlier one.
- **Alternatives:**
  - one carry-forward line per account, losing the open items' sources (INV-07 breaks);
  - closing entries at every year-end, which FR-GL-10 chose against;
  - leaving postings for life, which is Q38's default and the owner's alternative.

### D8. Restore behind the anchors: recovery events and anchor epochs (AUD-09, G4-070, G4-071)

- **The fault.** A restore can land behind an anchor already published. The chain then continues from the restored
  head, and its new events take sequence numbers the old anchors already sign.
- **Anchor epochs.** Anchors therefore gain an epoch: epoch 0 is today's keys, and epoch *n* writes under
  `audit-anchors/<org>/e<n>/…`.
- **The recovery step** (`audit.recover`, run first by the restore runbook and by the nightly verification) compares
  the database's chain with the latest epoch's anchors. When the head is behind the last anchor, or an anchored hash
  differs, it:
  1. Writes a signed **recovery event**: the restored head (the last sequence whose hash matches every anchor), the
     restore time, and the anchored range now missing. It goes to `fyniq.audit_recovery`, to
     `audit-anchors/<org>/recovery/<n>.json`, and into the chain as `audit.restored`.
  2. Starts epoch *n*+1.
  3. Raises the ops alert, the ✖ check (C.2 #3) until the Owner acknowledges it, and the Owner's and DPO's
     notification.
  4. Names the runbook `docs/runbooks/audit-restore.md`.
- **Reporting the gap.** Verification reports the missing range as explained, with the recovery record, never as clean.
- **The erasure register (G4-070).** The restore runbook's next step re-applies the erasure register (D2): it deletes
  again every detail erased after the backup was taken, before the copy opens. `p1-pdpa-retention` adds key
  destruction to the same register.
- **Alternatives:**
  - deleting or rewriting the old anchors, which the retention lock forbids, and rightly;
  - refusing writes until a person intervenes, which stops the business for a recovery the runbook can record.

### D9. Access, settings and audit events

- **Access.** All read operations need V on "Audit trail". The two settings follow the settings' own row, under dual
  control. The jobs run as `fyniq_worker`, and nothing is added for the AI or MCP.
- **New audit events:**
  - `audit.verified_by_member`;
  - `audit.details_erased`;
  - `audit.purged`, `ledger.purged`, `audit.restored` and `audit.close_anchored`;
  - every export or read the hook records.

  The setting changes are the settings' own events.
- **Ports and adapters (ADR-T8).** `ports.Cipher` (Cloud KMS fields, local key file), `ports.Signer` (Cloud KMS, local
  Ed25519) and `ports.BlobStore` (Cloud Storage with Bucket Lock, fake GCS). No new port and no new cloud dependency.

### D10. The screen

- **The page.** Compliance › Audit trail (Expert, `/compliance/audit-trail`), as designed:
  - filters;
  - event rows with actor, on-behalf-of, action, object and reason;
  - a side peek with field-level before → after;
  - "Verify integrity" for a range, with its passed, failed or explained result;
  - export.
- **Inventory.** The row's roles become Appendix D's: Admin and DPO, not "Compliance officer".
- **Canvas.** The fragment is checked against the build and the compliance canvas republished after 10 Nov 2026
  06:00 SGT.
- **Help.** Help keys, the guide page `audit-trail.md`, and its knowledge-base page.

## Risks / Trade-offs

- [A KMS `macSign` per sensitive value and per security event adds latency to a sign-in or a payee change] → Both are
  rare, and the call happens once per event. Bulk paths that touch many sensitive values (an import of payees) batch
  their fingerprints before the transaction.
- [A field holding a sensitive value under an unlisted name escapes D1] → The list is one place (`mask.go`), the
  existing shape masks still run on all text, and the conventions test of D3 lists every field name in the contract
  that looks like an identifier or pay field.
- [An erasure job that fails silently keeps IPs past 400 days] → The job's last run is a C.2 check, like the nightly
  verification; it warns once a run is two days late.
- [A restore that the runbook skips leaves new events overlapping old anchors until the nightly run] → The nightly
  verification runs the recovery step, and the result is never clean in between: a mismatch fails until the recovery
  event explains it.
- [Purging is irreversible] → It is off by default, needs dual control, deletes only behind a signed boundary, and
  cannot run before the records' retention has passed (2034 at the earliest).
- [A carry-forward that differs from what it replaces would silently change every later report] → It is a pure function
  of the purged lines, checked three ways before anything is deleted:
  - its lines sum to the purged lines per account, dimension set and currency;
  - the oracle's own purge agrees on generated ledgers;
  - the trial balance and sub-ledgers at B are compared before and after in the job's own transaction, which refuses to
    commit on a difference.
- [A reader that sums lines around the balance service would read a purged history wrongly] → ENG-04's archtest already
  refuses such readers. The balance service's `BALANCE-PURGED` is the one place a date before B is refused.
- [Entries kept for a register leave lines before B] → They are excluded from the carry-forward, which sums only what
  is purged, so balances stay whole. Their documents keep their entries.

## Migration Plan

- **Formats.** v1 events stay as they are; new events are v2. The close snapshot moves from @1 to @2 the same way.
- **Development data.** No production data exists, so request details in v1 events on development databases stay in
  their bodies.
- **Migrations.** The new tables and functions are additive, each with its validate migration where a constraint is
  added NOT VALID. The expression indexes are created concurrently.
- **Rollback.** The jobs are switched off by removing their schedules. A purge already run cannot be rolled back, which
  is why its switch is dual-controlled and off.

## Open Questions

- **Who acknowledges a recovery (D8).** Nobody holds A on "Audit trail", so the Owner acknowledges for now. The
  incident register of SEC-32 (`p1-security-hardening`) may take this over.
