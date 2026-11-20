# The link between Fyniq and Nemura: the envelope and its fixtures

PRD-001 FR-PPL-15 and PRD-002 NM-INT-10. Every event and call between the apps is an **envelope**: a header, the link key's signature over the
header, and the payload. The rule is decided once (`openspec/changes/p1-payroll-link/design.md` D2) and implemented twice, because Nemura imports
nothing of Fyniq's code (`internal/archtest`): `internal/domain/peoplelink` and `internal/nemura/link`. Both pass these fixtures.

- **Canonical bytes** (RFC 8785's subset): UTF-8; object keys sorted by their UTF-8 bytes; no insignificant whitespace; strings, integers (within 2^53),
  booleans, null, arrays and objects only. A fraction or an exponent is refused: money and quantities are decimal strings. Strings escape only the quote,
  the backslash and control characters (`\b \t \n \f \r`, others `\u00xx` in lower case).
- **Digest**: `payload_sha256` is the lower-case hex SHA-256 of the payload's canonical bytes.
- **Signature**: Ed25519 over `"fyniq-link-v1\n"` and the canonical bytes of the header (every field but the signature, the digest included), base64url
  without padding. The receiving side verifies with the link key it was given when the link was approved, never one that comes with a message.
- **Checks, in order**: shape; source and target organisation and source system; epoch (earlier and later are both refused); signing time within 5 minutes;
  signature; payload digest. A refusal has a stable code (`PPL-01` to `PPL-06`) and never repeats the payload.

`canonical.json` holds hand-worked canonicalisation vectors (input, canonical bytes, SHA-256) and inputs that must be refused. `envelope.json` is a signed
envelope with its public key: its private key is derived at test time from the SHA-256 of a phrase, so no private key is committed. Fyniq's test owns it
(`go test ./internal/domain/peoplelink -run SignedFixture -update`); Nemura's must verify it and reproduce its signature.

## Object versions, what each event carries and how Nemura builds it (Nemura's side, built in `p1-nemura-fyniq-link` 2.2)

Every event names an object and the object's **version**, which rises by one for each change; Fyniq applies a version only if it is newer than the one it holds, per part (the person's record, the hold and the bank details move apart). Nemura queues an event in the transaction that made the change and builds its payload from the source rows **when it sends it**, so a late event of an older version carries the newest figures and nothing is lost; no payload is in an outbox row, a task or a log. Nemura keeps a durable log of the identities it queued (`nemura.link_event`: event ID, sequence, type, object, version, and whether it was sent, failed or refused), which link health, retry and gap repair read.

| Object | Version is | Queued when |
|---|---|---|
| `person` | a counter per person (`nemura.link_object_version`): one more for each change of the record while the link is active, because the record is updated in place | a person is added; their record or their job (department, manager) changes; the link is activated (every person, as a snapshot) |
| `bank_details` | the ordinal of the person's **confirmed** salary-account changes; `bank_detail_id` is the ID of the confirmed change that made that version. The number, its masked form and its fingerprint never leave Nemura | the employee confirms an approved change (NM-PPL-04); at activation (each person's current version) |
| `tax_hold` | the hold's own version | a hold is put on or released; at activation (each person's latest version) |

`people.person.upserted` carries the person's **display name** (the preferred name, else the legal name), work email (sent as no email when it is not shaped as Fyniq accepts), `started_on`, `left_on`, `status` (`candidate` before the start day, `active`, `ended` once the leaving date has come), `line_manager_person_id` and, as `department_id` and `cost_centre_id`, **Fyniq's own dimension values**, found by the job's department name (case and surrounding spaces ignored) in the departments Fyniq last told Nemura of (`people.department.changed`); a name Fyniq does not have is `null`. A person who is no longer in Nemura is sent as a tombstone (`deleted: true`). Never an NRIC or FIN, date of birth, salary, classification, leaving reason, medical, disciplinary or review data.

`people.department.changed` (Fyniq to Nemura; `link.events.write`) carries `mapping_version` (the version of Fyniq's payroll account mapping, `getPayrollMapping`) and the departments Fyniq has in it: `departments` (a list of `department_id`, `name` and `cost_centre_id`). Its object is the mapping (`payroll_mapping`) and its object version is the mapping version. Nemura keeps each version and uses the newest: it is the version a pay run is calculated against (`PPL-16`), and the departments are the ones a run's totals are sent by.

### How Nemura builds `payroll.run.approved` (Nemura's side, built in `p1-nemura-fyniq-link` 3.1)

While the link is active a run is approved with proof, and the plain approval is closed (while the link is suspended or ending, no approval is made at all). The approver reads **the approval envelope** (`GET /v1/payroll/runs/{id}/approval-envelope`), made from the run's own rows: `departments` is the gross pay of the people whose job department, on the last day of the wage month, matches a department Fyniq told Nemura of (a person with pay and no department, or with one Fyniq lacks, is a reason the envelope cannot be signed, in plain words); `deductions` groups the run's deduction items by kind; `mapping_version` is the newest Fyniq told Nemura of; `supersedes` is the highest earlier approved or superseded version of the run (0 for the first); `preparer` and `approver` are the member's sign-in identity (issuer and subject: the earliest-linked identity of the member, never an email). Sealed, it is canonical JSON with its departments by ID and its deductions by kind, and its SHA-256 is the passkey's challenge as the 32 raw bytes. The approver signs it with a passkey **enrolled in Fyniq** (the shared relying party, Nemura's own origin, user verification required, no `allowCredentials`: a discoverable credential), and approves with `POST /v1/payroll/runs/{id}/linked-approval`. Before it keeps the answer Nemura checks that it says `webauthn.get` over exactly that digest at its origin for the relying party, with the person present and verified (it cannot check the signature: Fyniq alone holds the passkey). The envelope's canonical bytes and the assertion are kept together, once, in `nemura.link_run_approval`, and `payroll.run.approved` is queued in the same transaction; the sender posts those bytes and nothing rebuilt. A refusal that Fyniq's side can put right (`PPL-19`, a ledger code such as a closed period) is sent again as a new event with the same proof on request; a refusal of the figures or the proof (`PPL-11` to `PPL-18`) needs a new calculation and a new approval. **A payroll approver therefore needs a passkey enrolled in Fyniq** (sign in to Fyniq once; no Fyniq role is granted): Nemura's approval page says so beforehand and `PPL-19` after.

### Gap repair, the link's health and a restore (Nemura's side, built in `p1-nemura-fyniq-link` 2.3, 5.1, 5.2)

- **Replay.** Fyniq's inbox, finding a gap in the sequence numbers it received or a version it never applied, sends `link.replay.requested` (Fyniq to Nemura; the link's one event scope, `link.events.write`) with `from_sequence` and `to_sequence`, a range of at most 10,000 of Nemura's sequence numbers. Nemura answers by queueing the **current state** of each person the range names (their record, bank-detail version and hold) again, **at the version they have now**, never an old payload: Fyniq ignores a version it holds and applies one it missed. A backwards range, one from 0 or one too wide is refused. Fyniq's detection and request are Fyniq's side (left); Nemura's people team can send the same repair from the link's page (`POST /v1/link/repairs`), and what failed or was refused is queued again.
- **Reading the link.** Nemura shows its own people the link's state by identity only (`GET /v1/link/health`, `GET /v1/link/events?cursor=`): events by state, the oldest waiting, the last sent, the checkpoint (the highest sequence queued, sent and received) and what failed, with Fyniq's code; never a payload. **Fyniq does not read these:** a read for Fyniq needs a read scope on a link client, which widens a grant and is the owner's decision (recommended: `link.sync.read` on the read-only client of the ask at release, answering identities only). Until then Fyniq's own inbox is its record of what it received.
- **A restore.** After Nemura's database is restored, two named operators run `nemura link-restore <organisation id> "<name>, <name>"`: it records the checkpoint as the restored database knows it, suspends the link and lists each run approved with proof whose answer the restored database lacks (`nemura.link_restore`, `link_restore_run`). Nothing is sent or paid while it is open (the sender waits, a run is neither sent again nor repaired). An Owner or Administrator with a fresh passkey decides each run against Fyniq's own record (`POST /v1/link/restore/decisions`): posted, with Fyniq's reference, or not posted, whose approval is then sent again with the proof it was approved with. When the last is decided the restore is reconciled, the link resumes and the current state is sent again. A run's **receipt** (`GET /v1/payroll/runs/{id}/receipt`) is what two databases are reconciled by: the proof kept for each version (digest, mapping version, time), each sending and where it stands, Fyniq's answers and the payment instructions and outcomes, with no figure, name or account. *Left, owner to confirm:* a bank-detail or hold version that Fyniq saw before the restore and Nemura lost cannot be told apart from a new one with the same number (both are ordinals Nemura derives), so after a restore the people team re-confirms any bank-detail change made since the restore point; the alternative is an object epoch carried in every version, which changes the envelope.

### One sign-in and what ends a session (built in `p1-nemura-fyniq-link` 4.2, 4.3)

- **`identity.session.revoked`** (Fyniq to Nemura; `link.events.write`) carries `issuer`, `subject` (the identity the shared sign-in knows the person by: never an email) and `reason` (`signed_out` or `removed`); its object is `identity`, its version 1 (it is not version-ordered: a repeat of the same event ID does nothing). Nemura ends the sessions of the members of the linked organisation whose directory identity is that one, each audited by the session and the reason and never who, and ends nothing else: no member, role or other organisation's session. Fyniq queues it when a member is removed. The other direction (Nemura to Fyniq) is not built: it needs a new capability for Nemura's client and is the owner's decision.
- **The switch** is each app's own session answer (`workspace.finance_url` in Nemura's `getSession`, `workspace.people_url` in Fyniq's): a door and never a grant. The destination's own sign-in and roles decide what is behind it, and a role in one app grants nothing in the other (an identity is matched by issuer, subject and tenant, exactly).

A refusal that asking again cannot change (`PPL-*`, `LEDGER-*`) finishes the event as refused with Fyniq's code; an answer that might pass (a 5xx, a timeout, a rate limit, a token that did not work) is tried again, up to eight times.

## What Nemura sends and where (Fyniq's side, built in `p1-payroll-link`)

Every call carries the envelope (`{"header": {...}, "signature": "...", "payload": {...}}`) with the link client's bearer token (client-credentials with
`private_key_jwt`, audience Fyniq's API, scopes below). `signed_at` is within 5 minutes; `epoch` is the link's current epoch; `source_organisation` is the
Nemura organisation and `target_organisation` the Fyniq one; `event_id` is unique (a repeat answers `duplicate`); `object_version` rises per object.

| Operation | Event type | Scope | Payload (only these fields; a field not listed is refused with `PPL-09`) |
|---|---|---|---|
| `POST /v1/people-link/projection-events` | `people.person.upserted` | `people.projection.write` | `person_id`, `display_name`, `work_email`, `department_id` and `cost_centre_id` (Fyniq dimension value IDs), `started_on`, `left_on` (YYYY-MM-DD), `status`, `line_manager_person_id`, `deleted` (a tombstone) |
| same | `people.bank_details.changed` | `people.projection.write` | `person_id`, `bank_detail_id` (never the number); `object_version` is the details' version |
| `POST /v1/people-link/hold-events` | `people.tax_hold.changed` | `people.holds.write` | `person_id`, `hold_id`, `effective_from`, `status` (`held` or `released`), `evidence_ref` (required for a release); `object_version` is the hold's version |
| `POST /v1/people-link/payroll-runs` | `payroll.run.approved` | `payroll.journal.submit` | `envelope` (the approval envelope, below), `envelope_sha256`, `approval` (`authenticator_data`, `client_data`, `signature`, base64url) |

**The approval envelope** (PRD-002 NM-INT-06) is a JSON object with `organisation` (the Nemura organisation), `source_system` (`nemura`), `run_id`, `version`,
`supersedes`, `wage_month` (`YYYY-MM`), `departments` (`department_id`, `gross`), `employee_cpf`, `employer_cpf`, `sdl`, `shg`, `deductions` (`kind`,
`amount`), `reimbursements`, `pack_version`, `policy_version`, `mapping_version` (Fyniq's current, from `getPayrollMapping`), `preparer` and `approver`
(`issuer`, `subject`) and `approver_right` (`payroll.approve`). Money is a decimal string in the base currency. `envelope_sha256` is the lower-case hex SHA-256
of its canonical bytes. **The approver's passkey signs that digest as the WebAuthn challenge (the 32 raw bytes)**, at Nemura's origin, with the shared
relying-party ID; Fyniq verifies it against a passkey the approver enrolled in Fyniq (matched by issuer and subject), so every payroll approver signs in to
Fyniq once and enrols one. Fyniq posts the figures of this envelope and nothing else. The refusals are `PPL-11` to `PPL-19` (`contracts/openapi/fyniq.v1.yaml`,
`submitPayrollRun`); a ledger refusal (a locked period, an unmapped department) comes back with its own code.

## The payment lines (`payroll.payments.ready`; Fyniq's side is `receivePayrollPayments`, p1-payroll-link task 6.1)

`POST /v1/people-link/payroll-payments`, scope `payroll.payments.submit`, the link's envelope as above with `object_type` `payroll_run`, `object_id` the run's ID and **`object_version` the lines' version** (1, then higher for a revision).
Send it **after** Fyniq answered `posted` or `unchanged` for the run's version: Fyniq accepts lines only for the version it posted last, for the people of its projection, and for no more than the run's net pay on 2100.
The payload (only these fields; a field not listed is refused with `PPL-01`):

| Field | Meaning |
|---|---|
| `run_id`, `run_version` | the run and the version Fyniq posted last |
| `pay_date` | `YYYY-MM-DD`, the day Nemura intends the money to arrive (Fyniq's Payroll administrator confirms or changes it when preparing the batch) |
| `preparer`, `approver` | `{issuer, subject}` of the run's preparer and approver: they must be the ones the run was posted under (`PPL-23`) |
| `lines[]` | at most 500: `line_id` (Nemura's ID of the payment line, stable across revisions), `person_id`, `payee_name`, `bank_bic` (8 or 11 characters), `account_name`, `account_number` (5 to 34 characters), `amount` (dollars and cents, above zero), `bank_detail_id`, `bank_detail_version`, `bank_detail_digest` (lower-case hex SHA-256) |

- Fyniq seals each line's payee, bank, account name and number on arrival; none of them is in an audit event, log, outbox row or error. Its answer names a batch number and counts, never a person.
- A repeat of the event ID answers `duplicate`; a submission at a lines version no higher than the batch's answers `unchanged`; a higher one **revises**: each line that changed (amount or bank-detail record, version or digest) is a new version of that `line_id`, the batch's version rises, and an approval of the old one is stale (`revised`). A line that already went in a release is never changed (`PPL-27`).
- Send a revised line when a person's bank details changed (`people.bank_details.changed` raises the projection's version; Fyniq blocks the line whose version is older until the new line is approved: FR-PPL-14).
- Refusals: `PPL-23` (lines not well formed, or another preparer or approver), `PPL-24` (Fyniq has not posted that run version, or a newer one is posted), `PPL-25` (more than the net pay), `PPL-26` (a person not in the projection: send the people events first), `PPL-27` (a released line, or a cancelled batch), `PPL-28` (an unknown outcome of the person in this run: resolve it first).
- Fyniq answers back with `payroll.payments.status` (the vocabulary below) when a line is released and when the bank feed or a person with evidence records what became of it (p1-payroll-link task 6.2).

## The inbox for `payroll.payments.status` (Nemura's side; the table and the function exist, the inbox is `p1-nemura-fyniq-link` 2.3)

PRD-002 NM-PAY-10 is the vocabulary. **Do not add a second table or a second list of states.** Nemura keeps a person's payment in a pay run as append-only events in
`nemura.pay_payment_event` (migration `000031_pay_payments.sql`), keyed on the **run and the person, not on a version**, so that an unknown outcome still blocks after a corrected
version. The inbox writes through one function, which the manual recording path (`POST /v1/payroll/runs/{id}/payments`) uses too:

```go
nemura.RecordPaymentState(ctx, tx, org, at, actor, nemura.PaymentInput{
    Run: runID, Person: personID, State: "paid", // the event's `state`
    Instruction: instructionID, Amount: "3963.21", PaidOn: date, // the event's instruction ID, amount (a decimal string) and date
    Source: "fyniq", // never "manual" from the inbox: a manual record needs By and a document of the person as Evidence
})
```

- **Actor**: the link's service identity (`audit.Actor{Type: "service", ID: "link"}`); the audit event holds the run, person, state, source and instruction ID, never the amount.
- **Role**: the table's `INSERT` is granted to `nemura_api`, so the inbox's HTTP handler runs as the API; widen nothing without the owners (`go test ./internal/adapters/postgres/nemura -update` shows the grant diff).
- **States**: `scheduled`, `released`, `sent`, `accepted`, `paid`, `rejected`, `returned`, `unknown` (`internal/nemura/payroll/payment.go`; the same list is in the database trigger, and a test checks all 64 pairs agree).
- **Legal moves** (the first event of a payment may be any state): scheduled to released, sent, accepted, paid, rejected or unknown; released to sent, accepted, paid, rejected or unknown; sent to accepted,
  paid, rejected, returned or unknown; accepted to paid, rejected, returned or unknown; paid to returned; rejected or returned to scheduled (a new payment); **unknown to paid, rejected or returned only**:
  an unknown outcome blocks a new payment (`scheduled`) of that person in that run until it is resolved. An illegal move or a repeat of the same state is refused with `NM_PAYROLL_CONFLICT`
  (the database says `NM175`); the inbox should answer a repeat of the same Fyniq `event_id` as `duplicate` before it gets here.
- **Refusals before the insert** (`NM_PAYROLL_INVALID`): an unknown state; no instruction ID; an amount that is not dollars and cents; a date in the future for paid, rejected or returned; a run whose
  latest version nobody approved (`NM_PAYROLL_CONFLICT`); a person owed nothing in the run's payable version.
- **"Paid" comes only from the bank feed matching the debit (Fyniq's event) or a person recording the outcome with evidence**: a manual record has `recorded_by` and `evidence_id` (a document of the
  person paid) or the database refuses it (`pay_payment_event_source`). Nothing in Nemura marks a payment paid for a file or a journal.
- **What reads it**: the run's payment state (`approved`, `paid in part`, `paid`), N.1 #14 (rejected, returned or unknown, ✖) and #9 (the payslip is due within 3 working days of the confirmed paid date), and the payslip's date of payment.
- `PaymentBlocked(ctx, tx, run, person)` says whether an unknown outcome blocks a person; the payment file (`p1-nemura-payroll` 6.1) and the `payroll.payments.ready` sender must call it and leave that person out.

## The tax-clearance hold (Nemura's side; the sender is `p1-nemura-fyniq-link` 2.2, 3.3)

Nemura is the hold authority (PRD-002 NM-INT-08). The table and the functions exist (`nemura.tax_hold`, migration `000032_final_pay_tax_hold.sql`); **the sender must read this table, not add another**. A hold is a series of
versions written once and never changed, one row each: `hold_id` (the id of its first version, stable for the hold), `version` (1, 2), `status` (`held` or `released`), `effective_from`, `evidence_id` (a
document of the person; the database requires it for a release), `reason`, `recorded_by`, `recorded_at`. `nemura.TaxHoldOf(ctx, tx, person)` reads the latest version; `PutTaxHold` and `ReleaseTaxHold`
write (also behind `GET`/`POST /v1/people/{id}/tax-hold`). The `people.tax_hold.changed` payload (`person_id`, `hold_id`, `effective_from`, `status`, `evidence_ref`; `object_version` is `version`) is built
from a row. A new hold starts as `held` at version 1; only the next version releases it. While the latest version is `held`, Nemura's own run holds every dollar due to the person (`pay_run_line.held`, a
disbursement of zero), so the person has no payment to send; Fyniq's side checks the hold again when it releases a payment (FR-PPL-13). Not modelled yet: "released after paying IRAS's directive" as a status of
its own (NM-INT-08 lists it): today both releases are `released` with the evidence of what happened.
