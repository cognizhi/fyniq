# Proposal

## Why

InvoiceNow is Singapore's Peppol e-invoicing network. IRAS requires GST-registered businesses to submit the invoice data behind their
GST return through it, phased by company size from 1 April 2026 for new voluntary registrants to 2031 for the rest (PRD-001 §14.2).
Cognizhi is not GST-registered and will register only once InvoiceNow works (question 2, decided 16 Oct 2026). So this change is a
precondition of registration: Fyniq must send and receive PINT-SG documents, track each document to IRAS's acknowledgement ID, know
which documents feed the return and have each submitted or excluded with a reason, and refuse to file the return while any is missing
unless two people record a contingency. The change is built after `p1-gst`, whose F5 return waits for registration; the gate this
change builds is the function that F5's filing calls.

## What Changes

- **PINT-SG documents** (FR-AR-12, FR-AP-05): an invoice or a credit note is built as a PINT-SG UBL document (specification
  identifier `urn:peppol:pint:billing-1@sg-1`, UEN as endpoint scheme `0195`, the GST category codes of the `GST-cat` list) and checked
  against the published rules `BR-105-GST-SG` to `BR-113-GST-SG` and the totals rules before it is queued; a received invoice is read
  into a draft bill (never a posted one).
- **An access-point port** (`ports.AccessPoint`: look a participant up, send, read a status, poll received documents) with a local
  adapter that behaves like an access point (acknowledgements after a delay, an outage switch for G4-033) and a contract fixture
  set under `contracts/connectors/`; a real access point is wired later (`[live-verify]`).
- **The submission population and the filing gate** (FR-TAX-10, FR-TAX-11): every GST document of a return period has a submission
  (Peppol, solution-extracted, aggregated, credit or debit note, a resubmission under the same document UUID) or a recorded exclusion
  with its reason; the gate lists what is missing; filing with a gap needs the contingency, dual-approved with evidence.
- **API and screen:** the operations behind them with authorisation-matrix rows; the screen `invoicenow-submissions` (Expert).

## Capabilities

### Modified Capabilities
- `sales/receivables` (FR-AR-12), `purchases/payables` (FR-AP-05), `tax/gst-and-filings` (FR-TAX-10, FR-TAX-11)

## Impact

New tables (migration range 001000 to 001049), a port and its local adapter, `contracts/connectors/peppol-access-point.contract.json`, the API, a job
`invoicenow.send` and `invoicenow.poll` on the outbox. No posting rule changes: a submission posts nothing.
Outside needs: an accredited access point (Q2) and its sandbox credentials: every call that reaches it is `[live-verify]`.
