# Design

## D1. Creating a payment link is a write at the provider, so it gets a port of its own

`ports.PaymentProvider` has a method-set test and its adapters send GET only (FR-INT-27; threat model T7). That stays.
Creating a payment request is the one write Model A needs (FR-AR-13: "an invoice carries a payment link"). Options:

| Option | For | Against |
|---|---|---|
| A. Add `CreatePaymentRequest` to `PaymentProvider` | One port | Breaks the read-only test and the claim in every manifest; the sweep and worker would then hold a writer |
| B. A separate `ports.PaymentLinks` with the one method, its own capability kind `payment-link`, its own HTTP client that sends only `POST /v1/payment-requests` (and refuses any other method, as `getOnly` does) | Read-only port and its tests untouched; the grant is visible in the manifest; only the API's command path holds it | One more capability to document |
| C. A link made by hand at HitPay and pasted | No new write | Not automatic; the amount can be wrong |

Decision: **B** (`[owner to confirm]`). Rules: the amount and currency come from the invoice's open balance read on the
server in the same transaction, never from the client; one live link per invoice (a unique partial index); an invoice
that is draft, void, paid, or in a foreign currency the provider account cannot collect is refused (PSP-09); a link
expires at the invoice's due date plus 30 days, whichever the setting says; the worker never holds the capability (it is
declared `used_by: api`); the capability's trigger is a human authority (a person with Invoices/Post) or the system rule
"the sending of an invoice", never the AI (never-list unchanged, no tool added).

## D2. The webhook names, the GET decides

The `payment_request` completed event names a payment request; the worker fetches it with the existing read-only client
(`GET /v1/payment-requests/{id}`) and mirrors its payments in `payment_link_payment`, unique per provider payment ID.
Nothing is read from the payload. Anything that does not fit parks with a warning for a person: a payment of a different
amount or currency than the link, a payment on a link whose invoice is void or already paid, a second payment, an unknown
status. A refund is not a link event (FR-PSP-04 is the route).

## D3. The automatic leg is auto-reconcile's, with one new exact case

A charge line on the provider's clearing account whose reference is a mirrored link payment, equal to the invoice's open
balance, with the invoice issued and unpaid, is an exact match: `banking.AutoReconcile` records the receipt (Dr clearing
gross, Cr AR) through the existing receipt command, within the existing per-line cap, open periods and undo. The fee and
the payout are the existing provider legs. This replaces nothing: the P1 exact match by number and contact stays, and
beats nothing the link does not certify. No posting rule changes, so there is no oracle half; the differential suite gets
a provider-link scenario only if a rule changes (none does).

## D4. Host-to-host

IDEAL Connect delivers files over SFTP (MT940 daily, MT942 intraday), PGP-wrapped [verify with DBS]. `ports.FileDrop`
lists and reads files of a folder; the local adapter is a directory (local mode and tests); the SFTP adapter needs a
dependency (`github.com/pkg/sftp`), SSH egress and a static IP, so it waits `[live-verify]`. Each statement file runs
through the existing import (continuity, duplicates by the bank's reference). MT942 lines are *signals*: they become
pending signals that speed matching and are closed by the MT940 line, or expire (`bank.signal_expiry_days`).

## D5. Bank APIs

RAPID and UOB API Services have no public specification. Writing adapter code or cassettes from third-party summaries
is barred (rev 0.156, P23 step 3). The change builds the provider-neutral half (feeds, signals, the file drop) and
records, per bank, the document to obtain and the check to run `[live-verify]`.

## D6. Cards

A card is a money account; the feed is the connector source of any money account (`bankfeed.feeds` does not look at the
kind), so a card feed needs no new mechanism; the card issuer's connector is the outside part (Airwallex cards appear in
its wallet transactions, already read). Built: receipt matching (a captured document of the same amount, near the date,
suggested to the cardholder, never attached by itself) and the reminder (a daily job queues one Inbox item per cardholder
listing lines older than `claims.receipt_reminder_days` (default 3) with no receipt; none for a line already completed).

## D7. Sequencing

Docs first, then Model B (p1-payment-providers 4.1 to 4.4), then D1 to D3, D6, D4.
