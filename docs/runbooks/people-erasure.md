# Runbook: erasing a former employee's data in Nemura, and after a restore

**Requirements:** PRD-002 NM-SEC-08, NM-INT-13, NM-SEC-05; PRD-001 FR-PPL-19. **Who:** an Owner or an HR administrator asks, a second Owner or HR
administrator approves (each with a passkey confirmed in the last five minutes); the deployment's operators run the restore step.

## When it may be asked for

Only for a person who has left, once **both** periods have run: the employment records' (MOM: for ex-employees, "last two years, to be kept for one
year after the employee leaves employment", https://www.mom.gov.sg/employment-practices/employment-records, read 21 Nov 2026) and, when the person was
paid through Nemura, the payroll records' (IRAS: five years from the relevant Year of Assessment, so the pay of year Y to the end of Y+5,
https://www.iras.gov.sg/taxes/corporate-income-tax/basics-of-corporate-income-tax/record-keeping-requirements, read 21 Nov 2026).
**[accountant to confirm]** the reach for IR8A and CPF records; the CPF Board's own page was not read. A legal hold on the person, or an access request
of theirs not yet answered, refuses it.

## What it does, and what it does not

1. The request and the second person's approval are orders (`POST /v1/erasures`, `POST /v1/erasures/{id}/decision`), audited by the person's ID, never the name.
2. The worker files a **signed entry** in the register (a write-once store outside Nemura's database: identities and dates, never a name or a value), then
   in one transaction empties the person's names, work email, phone and reason for leaving, their own contact details, emergency contacts, NRIC or FIN
   and salary account, and the member's display name, tells Fyniq with a tombstone (which clears the name and email Fyniq held), and deletes the
   person's files (documents, payslip and letter PDFs; a file under retention is tried again when it lapses).
3. From 21 Nov 2026 the itemised figures of each **new payslip** are sealed under a key of the person's own (`person_key`); the erasure destroys the key, so those figures can never be read again. `reapply_erasure` destroys a key a restore brought back.
4. **No append-only record is changed and no guard function is loosened.** What sits in plaintext in append-only rows (issued letters, payslip figures,
   pay run lines, leave and sick notes, key employment terms, dependants, classification, assistant drafts) stays, tied to the person's opaque ID.
   `internal/nemura/erasure.Classes` lists every class of the data inventory with what an erasure does to it; the owner decides how the append-only
   ones are shredded (`docs/security/p2-grants-for-human-review.md`).

## After a restore of Nemura's database

Run **before the restored copy opens**, with the migrate credential, for each organisation:

```bash
make people-erasure-reapply ORG=<organisation id>        # locally; in the cloud: the nemura image's `erasure-reapply` command
```

Every entry of the register that verifies is applied again (a field erased since the backup is emptied again, its files deleted, the tombstone queued); one
that does not verify, or is filed under another organisation or person, is reported and not applied. Running it again finds nothing more. Then run the
link's restore step (`nemura link-restore`), which holds the link until its runs are reconciled; its repair sends every person's current state, an erased
person as a tombstone. After a restore of **Fyniq** alone, Nemura's repair (the link page, "Send the current state to Fyniq again") sends the tombstones again
and Fyniq's version rule applies them over the restored names.

`[live-verify]` in the cloud: the register is its own bucket with an object retention lock and the signing key a Cloud KMS key; the local register is a
directory under `NEMURA_DATA_DIR/erasure-register` and `keys/erasure-register`.
