# Design

## Context

Fyniq has the approval engine (signed approval records, passkeys, envelopes), the payment batch (payments-out), the posting engine and its oracle, the
outbox, integration clients and the `PeopleAndHR` and `PayRuns` permission rows. It has no payroll, no projection and no link. Nemura (own database,
own migrations in `db/nemura/migrations`) has an outbox, API clients, passkeys and an approval primitive, and imports nothing of Fyniq (archtest: the
shared rules, not shared code). Everything here is built and tested locally; real IRAS, CPF Board and bank behaviour is `[live-verify]` or not touched.

## Goals / Non-Goals

**Goals:** the contract and its fixtures; link states, epoch and dual-controlled linking; a durable inbox; the projection; verified posting of a run and
its corrections; salary payments with holds and bank-detail versions; the go-live gate and payroll checks for a company without Nemura.
**Non-Goals:** the payroll engine, CPF, SDL, SHG, payslips and KETs (`p1-nemura-payroll`); IR8A, IR21 filings and the CPF file (`p2-payroll-outputs`);
Nemura's side of the link (`p1-nemura-fyniq-link`); a real provider's connector (`p1-nemura-assistant-external`).

## Decisions

- **D1. Two credentials on every call.** Nemura authenticates to Fyniq as an API client with Appendix E's scopes only (`people.projection.write`, `payroll.journal.submit`, `payroll.payments.submit`, `people.availability.write`,
  `people.holds.write` and `people.holds.read`; none matches `fyniq.read|write.<area>`, so no ordinary client can hold one), and every event or call carries the envelope's signature with the link key. A stolen token alone
  cannot sign an envelope; a stolen link key alone has no token. The scopes are held only by the one client the link registers (dual-approved, D11) and
  are unreachable by personal access tokens, ordinary integration clients, agent-class clients and MCP (attack tests for each).
  *Alternative:* the token only (rejected: the spec requires the link signature and epoch, FR-PPL-09).
- **D2. One canonical serialisation.** The envelope's payload is hashed as canonical JSON (RFC 8785 subset: UTF-8, object keys sorted by code point, no
  insignificant whitespace, integers and strings only; money and quantities are decimal strings, never numbers). `payload_sha256` is the SHA-256 of that
  text; the signature is Ed25519 over `"fyniq-link-v1\n" + canonical(header)` where the header holds every envelope field but the signature, and the
  payload digest. The rule and signed fixtures live in `contracts/peoplelink/` (generated at test time from a test key, never a committed private key);
  Fyniq's `internal/domain/peoplelink` and Nemura's `internal/nemura/link` each implement it and each test the fixtures, as the boundary forbids shared code.
- **D3. Who verifies the approver (security posture; owner to confirm).** The approval record's WebAuthn assertion is checked with `passkey.Reverify`
  against a public key Fyniq already holds for the person matched by issuer and subject: the passkeys the approver enrolled **in Fyniq** (one
  relying-party ID, so one authenticator works for both apps, but each app keeps its own registry rows). The challenge is the envelope's SHA-256. The key
  never comes from the submission or from a Nemura event, so a compromised Nemura holding its token and link key still cannot forge an approver's
  authority. *Cost:* every Nemura payroll approver needs a Fyniq identity with an enrolled passkey (they sign in through the shared sign-in anyway; no Fyniq
  role is granted). *Alternative (A):* Nemura publishes its credentials' public keys through the link (no second enrolment; a compromised Nemura could
  register a key it controls and approve anything). Recommended: B. Recorded as owner-to-confirm in the PRD revision.
- **D4. The link principal.** A request authenticated as the link client becomes a principal of class `link` with `Scoped` and the link scopes; it is
  organisation-bound, never acts for a member, and reaches only the people-link operations. The operations' authorisation checks the scope and that the
  client is the link's current client at the link's current epoch.
- **D5. The inbox keeps identities, not payloads.** `people_link_inbox` has the event ID, type, object, version, payload SHA-256, state (applied, ignored
  as older, refused with a code) and time. A pay or bank payload is processed in the receiving transaction and never stored there, in the outbox, in a task
  or in a log; the posting and the batch are the record.
- **D6. Refusals are audited outside the rolled-back transaction.** A refused run does nothing, but the refusal (code, run, version, reason, never the
  payload) is audited in its own short transaction after the rollback, as the existing refusal audits are (design D6 of p1-approval-engine); the run-level
  checks and the unused-envelope claim share the posting's transaction, so a replay races to one winner.
- **D7. One posting source, version by version.** `payroll_run` (rule `payroll_run@1`), identity `(organisation, source system, run, wage month)` and a
  version; a version posts the **difference** between its full totals (per department, account and pay item) and the sum already posted for the run, so
  out-of-order retries converge. The entry's lines are per department (salary expense and dimensions), employer CPF (6020), SDL (6030), 2100 net pay, 2110
  (employee and employer CPF, SDL, SHG), advances recovered to 1250 and claims paid with salary to 2150; no per-person line. A closed wage month posts in
  the current open month with the wage month recorded on the entry. The Python oracle computes the same entries from PRD-001 §10.2 and B.3, written from
  the rules; the delta arithmetic is in the oracle too.
- **D8. A mapping version, no more.** FR-PPL-09 needs Fyniq's current account-mapping version. The department → salary account mapping (FR-EMP-02) is
  `p1-nemura-payroll`'s row in the plan; this change builds the minimum, a versioned `payroll_mapping` (department value → salary account, a version that
  changes on any edit), without tagging FR-EMP-02.
- **D9. A salary batch is a payment batch.** *(Amended 21 Nov 2026, built as a sibling of the payment batch, not a kind of it [owner to confirm]: the payment batch's document payload, versions, audit values, approval diff and envelope print each payee and amount, which FR-PPL-17 forbids; it is released once (FY239), so a part of it could not go while a held person waits; its items are bills and claims. The salary batch keeps its sealed lines in tables of its own (migration 000912) and reuses the approval engine, the release under a step-up and the payment files' writers.)* Kind `salary`, lines sealed (`fieldseal`: payee, account, amount) and visible only with the salary-payments
  right; others see total, count and state. Each line keeps its bank-detail record ID, version and digest inside the batch's approval envelope, so a newer
  version blocks the line until re-approval. "Paid" comes from the bank feed or recorded evidence only.
- **D10. The hold check is a port.** `HoldAuthority` answers hold status and version per person within 5 seconds (a fake with a timeout in tests, the link
  call in the adapter); no answer, or an older version than the projection's newest hold event, leaves the person's lines waiting.
- **D11. The second approver of NM-ARC-04 (owner to confirm).** "An Owner of both turns on with a second approver" is read as: linking, unlinking and
  registering or revoking the link's client need the Owner and a second approver (golden rule 6, the existing approvals); a standalone Nemura keeps the
  Owner's single passkey-confirmed registration of its API clients, and the second approver applies to Nemura's API clients from the day the link exists
  (built in `p1-nemura-fyniq-link`). *Alternative:* a second approver on every Nemura API client always (rejected: a team of one could not register one).
- **D12. Without Nemura.** A provider's journal or a file import is a draft journal under Journals (the existing approval), idempotent per period and
  version with a preview; the payroll go-live gate (FR-PAY-15) records, as evidence, how that month's statutory outputs are met and blocks approval for
  payment while an arrangement is missing.

- **D13. Two origins, one relying party.** A passkey assertion names the origin it was made at in its client data. Nemura's web app is served from its own origin (`people.<domain>`), so the link records that origin (`people_link.nemura_origin`, asked for with the link and approved with it) and Fyniq checks a Nemura approval against it (`passkey.ReverifyAt`), while the relying-party ID is the one both apps share. An approval made at any other origin is refused (`PPL-15`).
- **D14. The approver's right is the envelope's statement.** Fyniq cannot read Nemura's roles, so that the approver "held the payroll-approval right when they signed" is the approval envelope's own field, covered by the passkey's signature over its digest and by the link's signature over the message. A compromised Nemura that holds the link key can claim the right for a person who has a Fyniq passkey, but cannot make that person sign; a person who has no passkey enrolled in Fyniq cannot approve at all (`PPL-19`). Recorded owner-to-confirm with D3.

## Risks / Trade-offs

- A pay payload must never reach a log, an outbox row or a task: archtest-style greps and a redaction test on the inbox and the logs.
- The signature scheme is ours: it is checked by negative tests (altered payload, altered header, wrong key, old epoch, 5-minute window, replay).
- The differential run can disagree on the first scenario: the oracle is written first, from the PRD, not from the Go.

## Open questions

- The SDL difference (PRD-001 question 27) stays with the accountant: the posting takes totals from Nemura as given and posts SDL as stated.
- Whether untaken-leave accrual drafts (FR-PPL-07, P2 part) arrive through `payroll.journal.submit`: built as a draft journal source only if time allows.

- **D15. Partial release and the lines' versions (21 Nov 2026) [owner to confirm].** A release pays the lines free to go and leaves the others waiting (held, new bank details, an unknown outcome), each in a later release with its own file; a line goes in one release. A revised line (a new `payments.ready` at a higher lines version) is a new version of the line, never an update, and moves the digest the approval binds, so the batch is approved again (FR-PPL-14). The approval's envelope holds the total and a digest of the lines, never a name or an account; only the roles that may create or approve on the pay-run row see lines; the tenant export carries the batch header only. No break-glass reaches a salary. The payment file is downloaded by the Payment releaser who may release it, stepped up and audited (owner to confirm).
