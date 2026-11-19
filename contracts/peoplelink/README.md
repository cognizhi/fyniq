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
