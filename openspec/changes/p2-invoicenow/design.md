# Design

Decisions are recorded on the recommended option, tagged `[owner to confirm]` where the owner did not decide.

## D1. A port, a local adapter, a contract fixture

`ports.AccessPoint` has four methods: `Lookup(participant)` (is the Peppol ID registered and which document types it accepts, the
SMP lookup), `Send(request)` (the PINT-SG bytes, the participant, the document UUID and an idempotency key; returns the
transmission ID), `Status(transmission)` (queued, delivered, acknowledged with IRAS's acknowledgement ID, or failed with reasons)
and `Poll(cursor)` (received documents). The local adapter is deterministic: a transmission reads queued, delivered and then acknowledged, one step for each status read,
a participant that starts `9999` is unknown, a body that the rules refuse fails, and a switch (`FYNIQ_LOCAL_AP_OUTAGE`) makes every
call fail with a transport error. `contracts/connectors/peppol-access-point.contract.json` holds the fixtures every adapter must answer the same way (run by
`porttest.AccessPointContract`); the manifest (capability `e-invoicing`) arrives with the real adapter, since it names a provider's
secrets and settings. The cloud adapter arrives with the access point's contract (Q2): `[live-verify]`.
Options: a hosted access point's API now (blocked on a choice and credentials); a Peppol AS4 access point of our own (a
certification we do not need). Recommended: the port and a stub.

## D2. The document is PINT-SG, built by Fyniq

Sources (retrieved 20 Nov 2026; PINT SG Billing v1.4.1, released 25 May 2026), `docs.peppol.eu/poac/sg/pint-sg/trn-invoice/`:
- the specification identifier `urn:peppol:pint:billing-1@sg-1` and the business process `urn:peppol:bis:billing`
  (rule `PEPPOL-EN16931-R004-SG`);
- the endpoint scheme `0195`, "Singapore UEN identifier" (the EAS list); the identifier takes the prefix `SGUEN`, so the UEN
  201912345A is the participant `0195:SGUEN201912345A`, as the contacts' Peppol ID form says (CT-08) `[verify]` with the access point;
- the GST categories `SR, SRCA-S, SRCA-C, SROVR-RS, SROVR-LVG, SRRC, SRLVG, ZR, ES33, ESN33, DS, OS, NA, NG` (the `GST-cat` list);
- the rules `BR-105-GST-SG` (a seller GST identifier when the category is SR, SRCA-S, SRCA-C, ZR, SRRC, SROVR-RS, SROVR-LVG, SRLVG or NA),
  `BR-106` and `BR-107` (seller and buyer address line 1 and post code), `BR-108` (a UUID, BT-SG-003), `BR-109` (UUID format),
  `BR-111` (a credit note gives its reason), `BR-112` (the seller's legal registration identifier), `BR-113` (the accounting
  currency SGD when the invoice currency is not SGD, and not given when it is) and `BR-13-GST-SG` / `BR-14-GST-SG`, `BR-45` to `BR-48-GST-SG`.
Not fetched and so `[verify]` against an access point's validator: the business process value `urn:peppol:bis:billing` (the page
says only that a business process type is mandatory, IBR-076), the tax scheme identifier `GST` of the tax categories, the credit note
type code 381 (380 and 383 are in the page's invoice type code list), and the values `HUR` and `C62` for units. The golden document
`internal/domain/invoicenow/testdata/invoice-sgd.xml` is made by `Build` and so proves the element order read from the page, not
the values above; no official PINT-SG example file was found. Fyniq checks the rules above itself before queuing, so a document the network would refuse never leaves; the complete Schematron run is the
access point's and is `[live-verify]`. A line's GST category comes from the line's GST code through a mapping table of the
`sg-gst` pack's codes to the categories (a code with no mapping blocks sending, with the code named).

## D3. One UUID for a document, a version for each send

A document gets a stable `document_uuid` the first time it is queued. A correction of the same invoice is a resubmission: the same
UUID, the next `version`. A credit or debit note is its own document with its own UUID and a reference to the invoice
(FR-TAX-10, G4-032).

## D4. Submissions and exclusions

`invoicenow_submission` holds one row for each (document, version): `kind` (`peppol`, `extracted_sale`, `extracted_purchase`,
`aggregated`), `direction`, `status` (`queued`, `sent`, `acknowledged`, `failed`), the transmission ID, IRAS's acknowledgement ID
and the last error. Its history is `invoicenow_event`, append-only. `invoicenow_exclusion` records a document left out with a
`reason` from a fixed list (reverse charge, import permit, purchase from a supplier not registered for GST, blocked input tax,
out of scope, other with a text) and the person who recorded it. A document is **in scope** when it is a posted sale or purchase
with a GST code the return reports, in a return period; the population is read, never stored.

## D5. The gate and the contingency

`Gate(from, to)` returns every in-scope document with neither an acknowledged submission nor an exclusion, and a result `ready` or
`blocked`. The filing of an F5 (`p1-gst` section 7) must call it. `invoicenow_contingency` records a gap filing: the period, the
reason, the outage evidence (FR-DOC-05) and two different members, the one who requested it and the one who approved it (the
database refuses the same person twice, and the approver holds a different role from the requester's list below). Unsent items
stay queued and are resubmitted when service returns (G4-033).

## D6. When the requirement applies

An organisation setting `invoicenow.applies_from` (a date, blank until the tax agent confirms, question 2). With it blank the
gate is "not assessed". Sending a document is always allowed. Auto-send at issue is a setting, `invoicenow.auto_send`, off by default
[owner to confirm].

## D7. Roles

No new Appendix D row: InvoiceNow is part of the GST return, so its operations use the **GST return, filing** capability: view for
the Owner, the Finance controller, the Accountant, the Tax agent and the Auditor; send, resubmit, exclude and request a
contingency with the create right (the Accountant and the Tax agent: the Owner and the Finance controller hold approve only, not
create); approve a contingency with the approve right (the Owner or the Finance controller, so never the same role, and the database
refuses the same person who holds both roles). Sending an invoice stays with the invoice's own right. [owner to confirm]

## D8. Received documents

`Poll` stores each received document as an evidence file and creates a draft bill (FR-AP-05) through the same capture
pipeline's draft command, flagged "received through InvoiceNow", with the sender's participant ID matched to a contact by its
Peppol ID or left unmatched. A duplicate (the same sender and invoice number) is refused with its reason.
