# Proposal

## Why

People ask their company for the same few things again and again: a letter for a bank or a landlord, a salary certificate, a change of address. HR answers
each by hand, and a letter that a bank trusts has to be exact, signed by someone with the authority, and kept as it was sent. Employees also need a home of
their own in Nemura: their balances, their requests and where each stands, their details, and, after they leave, their own documents for as long as the law
makes the company keep them. Nemura has the people record, leave and the packs (`p1-nemura-records-packs`, `p1-nemura-leave-time`); it has no letters, no
templates, no key employment terms (KETs) and no self-service pages beyond leave. This change builds them without a pay run: payslips, the final pay and
IR21 need `p1-nemura-payroll`, and Fyniq accounts for joiners need `p1-nemura-fyniq-link`.

Timing (PRD-002 question 12, decided 16 Oct 2026): built after Fyniq's go-live and before the first paid person; none of it is a launch gate.

## What Changes

- **Letter types and templates** (NM-LTR-01, 02): types with who may request, the fields shown (salary only when asked for), who signs and how fast it is
  due; templates of text blocks and merge fields from a typed catalogue, versioned, a published version immutable and needing the HR administrator's approval.
- **Issue in two steps and what an issued letter proves** (NM-LTR-03, 07): the request fills the template; an HR officer checks it; the authorised signatory
  signs with a passkey; the exact PDF is hashed and kept as evidence with the template version, the data, the signatory, their authority and the approval the
  passkey signed; the employee gets it in My requests and by an expiring link.
- **KETs** (NM-LTR-06): generated from the person's terms and the people record, due within 14 days of the start, missing ones ✖ on Checks (N.1 #1).
- **Self-service** (NM-ESS-01, 03, 04, 06): My home, My details (contact details directly; legal name, NRIC or FIN and residency with evidence and HR approval),
  requests with a type, a status, a due date and the reply, and a leaver's narrow account for their own documents until the retention period ends.
- **Standard letters without waiting and the joiner's and leaver's tasks** (NM-LTR-04, NM-ONB-02, 03): issuing a pre-approved type automatically under a
  signatory's delegation; the task to invite a joiner to Fyniq and the offboarding dates and leave settlement, without the pay.
- **Screens:** `nm-letters`, `nm-letter-template`, `nm-request-letter`, `nm-my-home`, `nm-my-requests`, `nm-onboarding`, `nm-leaver-home`.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None (`skip_specs: true`). It builds requirements already in the main specs: `people/letters` (NM-LTR-01 to 04, 06, 07), `people/self-service` (NM-ESS-01 to 06) and
`people/onboarding` (NM-ONB-02, 03).

## Impact

- **Code:** `internal/nemura/{letters,kets,selfservice}`; a renderer port with a local adapter; `internal/adapters/postgres/nemura`; `db/nemura/migrations/` from the
  next number; `contracts/openapi/nemura/`; `apps/nemura-web`; a pack version with the KETs content from MOM.
- **Data:** letter types, template versions, requests, issued letters (evidence), delegations, terms, detail changes, HR questions; the data inventory and the
  retention schedule gain each record type.
- **Docs:** user-guide and knowledge-base pages for each screen.
- **Needs the owner:** none blocking; design O1 and O2 are decided defaults recorded for the owner to confirm. It depends on `p1-nemura-records-packs`.
