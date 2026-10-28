# Design

## Context

`internal/domain/paymentfile` already has one writer per format behind `Writer`. Release writes the file inside its
unit of work, seals it, and records the writer's name and version (`payment-list@1`; `p1-payments-out` design D6
and D7). Today a package variable chooses the writer. This change chooses it from the batch's money account.

## Decisions

### D1 The setting lives on the money account

`money_account.payment_file`: text, `payment-list` by default, CHECK against the formats that have been built.
Release reads it when it writes the file, so a batch released before a change keeps its file. The setting is changed
by whoever may change the money account, with an audit event recording the old and new value. Alternatives were a
setting per batch, which invites the wrong file for the account, and one setting for the whole organisation, which
breaks for an organisation with both DBS and Wise.

### D2 A bank's file is offered only for an account at that bank

The API refuses a format the account's provider doesn't take. The provider is known from the account's statement
connector when it has one (Wise, Airwallex; FR-BNK-15). Otherwise it is the institution, picked from a list for a
bank account rather than typed. How existing free-text institutions map to that list is decided in task 1.1.

### D3 Wise's batch payment file (`wise-batch@1`)

A CSV with the columns of Wise's template for paying to bank accounts, for SGD to SGD, as Wise's template builder
writes them. The amount is in SGD, so the payee receives the batch's amount. Each row's reference is the batch's,
cut to Wise's limit, and Wise picks the local route, so a Wise account's batch has no FAST or GIRO choice [verify].
Cells are neutralised against formula injection, as in the payment list. The columns are pinned by a test against
the owner's blank template. A real upload to Wise is the `[live-verify]` step, since the template is all the spec
there is.

### D4 DBS IDEAL's UFF (`dbs-uff@2.2`)

Written from "DBS UFF Version 2.2": its header, payment and trailer records for FAST and GIRO, from the batch's
method. Each field is pinned to the specification's own example, and a test upload to IDEAL is the `[live-verify]`
step. Nothing is taken from unofficial copies of the layout (rev 0.156).

### D5 Airwallex gives the payment list

An Airwallex account's setting offers only the payment list until the payout API (P2). The help text says why.

## Risks

- **A provider changes its template.** The writer is versioned (`wise-batch@1`). A changed template is a new
  version, and released files keep the version they were written with.
- **The wrong file for the account.** D2 refuses it, and the bank refuses an upload it can't read before anything
  is paid.
- **More places full account numbers go.** Every format is written and sealed the same way as the list (design D7
  of `p1-payments-out`). The threat model `docs/security/threat-models/payment-batches.md` gains the new formats.

## Open questions

- Wise's SGD bank columns and the reference's length limit: from the blank template.
- DBS UFF 2.2's record layout: from the specification, once IDEAL is set up.
