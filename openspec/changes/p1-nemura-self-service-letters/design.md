# Design

## Context

Nemura holds the people record with restricted fields and documents, the pack, leave and the approvals primitive with passkey step-up. It stores files in the blob
store port, renders nothing yet and sends no mail (a notice is queued and logged until notifications). Letters are the first thing Nemura produces that leaves
the company, so they must be exact, attributable and kept.

## Goals / Non-Goals

**Goals:** letters issued in two steps with the evidence the PRD names; templates a company can change under approval; KETs on time; an employee's own home and
requests; a leaver's narrow account.

**Non-Goals:** payslips, IR8A and final pay (`p1-nemura-payroll`); Fyniq accounts and the one sign-in (`-fyniq-link`); the verification page for banks (NM-LTR-05, P2);
the people assistant's drafts (`-assistant-external`); hiring and onboarding plans (P2).

## Decisions

- **D1. Letter types are rows with a catalogue behind them.** The catalogue in code names each type of NM-LTR-01 with its default fields, signer and due days; a
  company's `letter_type` row switches it on, narrows who may request it and sets its due working days. A type never shows salary unless the type says so and the
  person asked for it. Evaluative statements (a reference, a performance remark) are never a field: a letter says facts of the record.
- **D2. Templates are text blocks and merge fields from a typed catalogue, with no free code.** A block is a paragraph, a heading, a signature block or a
  letterhead; a field is `person.legal_name`, `job.title`, `employment.start`, `pass.type`, `company.name`, `company.uen` and so on, each with a format. A template is
  a draft until published; a published version is immutable, numbered, and needs the HR administrator's approval (the approvals primitive); rollback publishes an
  earlier version as a new one. **Departure from NM-LTR-02**, which says to use Fyniq's template engine and editor: Nemura runs standalone and the editor is a Fyniq
  screen, so P1 ships Nemura's own text editor over the same ideas (typed fields, versioned, no free code); sharing Fyniq's editor arrives with the link. Recorded
  for a PRD revision with the first slice.
- **D3. Rendering is a port.** `Renderer.Render(template version, data) -> PDF bytes`. The production adapter calls the platform's private render service (the same
  sandbox and self-hosted fonts that Fyniq's documents use) and is a `[live-verify]` item; the local adapter writes a plain text PDF in Latin letters with a
  deterministic layout, flags in the letter's footer that it is a local rendering, and refuses a character it cannot draw, so a local letter is never silently wrong.
  The hash is of the bytes issued, whichever adapter made them.
- **D4. Issue in two steps, and the passkey signs the hash.** A request fills a template version with the person's data and the requester's reason and snapshots the
  data. An HR officer checks it (and may refuse with a reason). The signatory (an Owner or a person the Owner gave the signing right, NM-ROLE-04) opens it, sees the
  exact rendered bytes' hash, and confirms with a fresh passkey: the approval envelope names the letter, its hash and the template version. The issued letter is
  written once: the bytes in the blob store, a `letter_issued` row with the SHA-256, the template version, the data snapshot, the signatory and their authority (a
  signing role or a delegation) and the envelope. Nothing about it changes afterwards: a replacement or a withdrawal is a new row naming the old (NM-LTR-07). The
  letter says how a recipient can check it; the verification page itself is P2.
- **D5. Delivery is an expiring link and the employee's page.** The employee sees the letter in My requests; an email carries a link that delivers the file for
  seven days from a one-time token kept as a hash, and the page never shows more than the letter's own facts. Email goes out as a queued notice, logged until
  notifications deliver it.
- **D6. Automatic issue is a delegation.** The signatory creates a `letter_delegation`: the letter type, the approved template version, the fields it may show
  (never salary or an evaluative statement), who may request it (active employees not serving notice), a monthly limit and an end date at most twelve months away.
  They can revoke it; a change of the template ends it; each automatic letter keeps its exact bytes and who asked. The people assistant never uses this path.
- **D7. KETs come from the person's terms.** A `person_terms` record, effective-dated, holds what the Employment Act's key employment terms need that the people
  record does not (main duties, working arrangement, the salary period, the basic salary and fixed allowances as restricted fields, overtime, medical benefits,
  probation, the notice period, the place of work); the KETs letter is generated from it and the record, issued as any letter and due within 14 days of the start
  for an employee the Employment Act covers on a contract of two weeks or longer. Missing KETs are ✖ on Checks (N.1 #1), with a reminder in the 3 days before. The 17
  items are read from MOM's tripartite guidelines into `sg-people@2026.6` (`ket_items`, each with when it applies: always, only for a fixed-term contract, only if the
  overtime payment period differs), with the days to issue them; the standard text lists them in MOM's order. The pay terms are the Payroll administrator's and the
  Owner's to write and read (O3); an item that does not apply says so in words; a fact not recorded is named when the request is made, never guessed.
- **D8. Self-service is the person's own scope.** My home reads the leave balance, the next public holiday, the open requests and the tiles; My details lets a person
  change contact details, emergency contacts and address directly, and change legal name, NRIC or FIN and residency only through a request with evidence that HR
  approves (the bank account keeps its own flow, NM-PPL-04). Requests are one list of letter requests, flexible work, details changes and general HR questions, each
  with a type, a status ("received, with HR, ready"), a due date per type and the reply.
- **D9. A leaver keeps a narrow account.** When a person leaves, their member is deactivated and a `leaver` access is created that signs in with a verified email or
  a passkey and reads only their own documents (letters, KETs; payslips and IR8A when payroll exists) until the retention period ends; a lost document is re-issued on
  request, as a new letter row. Email links only deliver a document.
- **D10. Honest scope.** The joiner's Fyniq invitation is a task on People home for someone with the right to invite in Fyniq; the link raises and closes it. The
  offboarding dates (notice given, last day, the retrenchment notice date) and the leave settlement are recorded here; final pay and IR21 belong to payroll.

## Owner decisions

Decided 20 Nov 2026 under the owner's standing instruction (the owner is away: take the recommended option for small and medium choices, record why).

### O1. Which template engine in P1

| Option | Pros | Cons |
|---|---|---|
| **A. Nemura's own text templates now; Fyniq's editor with the link** | Nemura stays standalone (ADR-N1); small, no new shared code | Two editors for a while |
| B. Share Fyniq's template engine now | One editor | Couples the images; the editor is a Fyniq screen; widens the shared list |

**Decided: A.** Standalone is the architecture's first rule, and letters are text.

### O2. How the local build renders a letter

| Option | Pros | Cons |
|---|---|---|
| **A. A plain Latin text PDF, refusing what it cannot draw** | Works offline; no extra binary; deterministic | Not for names in other scripts: those need the platform's service |
| B. Bundle Typst and fonts in the local image | Same output as production | A binary and fonts in a lean image; heavier local mode |

**Decided: A.** Production uses the platform's service; the local limit is stated in the footer and the refusal.

### O3. Who writes the pay terms of the KETs

| Option | Pros | Cons |
|---|---|---|
| **A. The Payroll administrator and the Owner write and read the pay terms; the people team writes the rest** | Follows Appendix B's Pay row (HR roles have none); the audit event never holds pay; the people team cannot see pay by writing the KETs | The HR officer cannot finish the KETs alone: the request names the missing pay terms and payroll records them |
| B. The HR officer writes everything | One person finishes the KETs | Gives HR the pay, which Appendix B withholds |
| C. Pay terms come only from payroll (P1 later) | One source of pay | KETs would wait for `p1-nemura-payroll`, past the 14 days of the first hire |

**Decided: A.** It keeps Appendix B intact and lets the first hire's KETs be issued before payroll exists; when payroll arrives, its pay record becomes the source and this
form is read-only for a person it holds.

### O4. How a change of legal name, residency or NRIC or FIN is requested and what the request holds

| Option | Pros | Cons |
|---|---|---|
| **A. The request carries the evidence as a document and a note; it never holds an NRIC or FIN. Another HR member approves; a name or residency applies on approval, an NRIC or FIN is recorded by HR through the sealed flow afterwards** | The NRIC never rests anywhere but the sealed store (SEC-15); the evidence is on the person's file with its retention; one rule for the three kinds | An NRIC change takes two steps (approve, then record) |
| B. The request holds a sealed proposed NRIC or FIN and applies it on approval | One step | A second sealed store and key handling for a value that may never be approved |
| C. Only HR changes these, on a paper request | Nothing to build | Breaks NM-ESS-03 |

**Decided: A.** Due days: a change is decided within 5 working days and a question answered within 3 (code constants; letters keep their type's days and flexible work the
pack's months). The audit event of a direct change names the fields that changed and never their values (data minimisation, PDPA); a request's words stay in its row.

## Risks / Trade-offs

- [A letter is edited after issue] -> the hash of the exact bytes is kept in a row nothing can change; a replacement is a new row.
- [A signatory signs what they did not see] -> the passkey confirms the hash of the rendered bytes shown on that screen, and the envelope names it.
- [A salary leaks into a letter that must not show it] -> the type's field list is the only way a field enters; a field outside it is refused when the template is
  published, and again when the letter is filled.
- [KETs are late for a joiner] -> N.1 #1 is ✖ from the start date's fourteenth day and warns before it.

## Migration plan

Additive migrations from the next number; no data to migrate. The pack's next version adds the KETs content beside the earlier ones.
