# Threat model: payment-provider connectors (Stripe, HitPay)

| | |
|---|---|
| Feature and requirement IDs | Payment-provider connectors and signed webhooks (FR-PSP-01), Model B (FR-PSP-03, FR-PSP-07), refunds and disputes (FR-PSP-04), fees and their GST (FR-PSP-05), payout reconciliation (FR-PSP-06); `p1-payment-providers` (PRD-001 open questions 23 and 45, rev 0.25) |
| Author, reviewer, date | Claude Code for the owner, 19 Nov 2026; second review waived before production (AGENTS.md), held with `needs-human-review` if wanted |
| Data and its class | Provider API keys and webhook secrets (secrets); raw webhook payloads (a customer's name, email and address: personal data, kept sealed, SEC-11); balance transactions, invoices, subscriptions (confidential business data); the postings they lead to (ledger, append-only) |
| Who acts | The Owner and Administrator (install, configure and enable a connector under dual control, as every organisation connector, `connector.change`); the worker (fetch and mirror, as the connector's own principal, posting nothing); auto-reconcile in the API (posts a provider's own lines); Stripe and HitPay (webhooks from the internet); nobody at Fyniq can charge, refund, retry or pay out at a provider: the connector is read-only |

## 1. What we are building

A provider connector does two read-only things: it feeds the provider's balance transactions into a money account as the
statement lines of the provider's clearing account (the bank-data capability, the existing sync), and it takes the
provider's signed webhooks. A verified webhook is stored once per event ID, as received (its payload sealed), and queued;
the worker fetches the object it names from the provider and mirrors it (never the payload), and auto-reconcile in the API
posts the provider's own lines through the commands that already exist, once each (a line is reconciled once).

Trust boundaries: provider ↔ API (webhooks, from the internet); worker ↔ provider (reads, over the egress guard);
API and worker ↔ database (RLS on every table); API ↔ secrets project (keys).

## 2. What can go wrong (STRIDE per boundary)

| # | Boundary or step | Threat (STRIDE) | Impact | Likelihood |
|---|---|---|---|---|
| T1 | Provider → API | Spoofing: a forged webhook | Fake charges or invoices in the books | Medium: the endpoint is public |
| T2 | Provider → API | Replay: a captured genuine webhook sent again, now or days later | A charge or fee posted twice | Medium |
| T3 | Provider → API | Denial of service: a huge body, or a storm of valid events | The API or the worker stalls | Medium |
| T4 | Event → posting | Tampering: the event's payload says one amount and the provider's object another, or the payload is crafted to carry data into the books | A wrong posting that looks right | Medium |
| T5 | Event → posting | Tampering: events out of order, repeated, or missing for days (Stripe retries for three days) | A leg posted twice, or never | High: Stripe does not order events |
| T6 | Parser → books | Tampering: a float or a wrong currency scale (JPY, BHD) changes an amount; HitPay's JSON number read as a float | Wrong money | Medium |
| T7 | Worker → provider | Elevation: a key that can write (charge, refund, pay out) is used by Fyniq, or by an attacker who reaches the worker | Funds moved | Medium: HitPay documents no read-only key |
| T8 | Worker → provider | SSRF: an object ID or a configured host aimed at an internal address | Internal services reached | Low |
| T9 | Store | Information disclosure: raw payloads, with customers' names and emails, in a log, the audit trail, a response or an AI tool | Personal data leaked | Medium |
| T10 | Posting | Elevation: provider data makes auto-reconcile post into a locked period, beyond its caps, or to accounts a person did not choose; or a person or the AI uses the connector to post | Closed books change; the never-list is bypassed | Low |
| T11 | Unknown events | Tampering: an event type the parser does not know is acted on | Unreviewed postings | Low |
| T12 | API ↔ database | Information disclosure: one organisation reads another's events | Cross-tenant disclosure | Low |
| T13 | Webhook endpoint | Spoofing: one organisation's connector ID used to post an event into another's | Cross-tenant tampering | Low |
| T14 | Credit settlement → books | Tampering and elevation: a credit settled against a dispute twice, one larger than the dispute, a credit note that is not issued, or a person without the right to post on Invoices moving accounts receivable | Customer balances and disputes clearing wrong, or accounts receivable changed without the approval an invoice change has | Low: it is a person's request, never an event's |
| T15 | Payment link → provider | Elevation: the link capability becomes a way to charge, refund or pay out (a second write at the provider), or the AI or MCP asks for links | Funds moved or customers sent to a wrong page (p2-bank-feeds-payments, rev 0.510) | Low |
| T16 | API → link | Tampering: a client sets the amount, the invoice or the currency of a link; a link made for a draft, void or paid invoice, twice for one invoice, or in a currency no provider account collects | A customer pays the wrong amount, or a payment lands where no one reconciles it | Medium |
| T17 | Charge → receipt | Tampering: a charge is matched to an invoice it does not settle (another amount, a second payment, a cancelled link, a voided or paid invoice), or the same charge is receipted twice | Customer balances wrong, revenue and cash overstated | Medium |
| T18 | Provider answer → customers | Spoofing and disclosure: the provider's answer carries a non-https URL or a path that is shown to customers; its error body is kept or shown | A phishing or plain-text link on an invoice; provider words in the audit trail | Low |

SOD and money movement: nothing in this feature moves money. The worker only mirrors what the provider reports and posts
nothing (it holds no right to make a document). Postings are made by auto-reconcile in the API, the same system principal and
rules as any exact auto-reconciliation: the same posting rules a person's categorising uses, audited, within the S$500 per-line
cap, into open periods, undone like any auto-reconciliation, and bounded by the provider's own reference on the line. Setting
a provider account up needs a right on "Bank data… connections" and chooses only internal accounts. The AI and MCP have no
route to any operation here (the never-list is unchanged: no tool is added).

## 3. What we do about it

| Threat | Control | Test that proves it (`// req:` and `// asvs:`) |
|---|---|---|
| T1 | The binding verifies the raw bytes against the connector's own secret before anything is stored: Stripe's `v1` HMAC-SHA256 over `t.body`, HitPay's HMAC-SHA256 over the body; a failure is 401 with no row | `TestAWebhookIsAcceptedOnlyWithASignatureOfItsRawBodyAndAFreshTimestamp` (Stripe), `TestAWebhookIsAcceptedOnlyWithASignatureOfItsRawBody` (HitPay), `TestProviderWebhooksAreStoredOnceAndWorkedFromTheFetchedObject` (`// req: FR-PSP-01`, `// asvs: V4.1.5`) |
| T2 | Stripe's timestamp must be within five minutes of the platform's clock; every event is deduplicated on its event ID (a unique key per connector, answering 2xx and changing nothing); HitPay, which signs no timestamp and sends no ID, is deduplicated on a digest of its type, object and body; a line is reconciled once and import keeps each reference once (design D4), so a late redelivery posts nothing | the same tests; `TestEventsGetAnIDFromTheirBody` (HitPay); `TestAProviderAccountIsSweptAndItsLinesReconciledOnce` (a second sync that sends every line again adds none and marks none a possible duplicate) |
| T3 | A 512 KiB cap on the body (413); the outbox queues and retries the work with backoff, one row per event; the connector's rate limit and breaker bound reads | `TestProviderWebhooksAreStoredOnceAndWorkedFromTheFetchedObject` (a body too large) |
| T4 | The worker fetches the object the event names and posts from the fetch, never the payload; the payload is stored as evidence only | `TestProviderWebhooksAreStoredOnceAndWorkedFromTheFetchedObject` (the rule is handed the fetched object, the provider is asked once) |
| T5 | Money legs are keyed on the provider's reference on a line, not on the event; events never order anything; the hourly sweep reads back four days and records what was missed by ID, once | `TestAProviderAccountIsSweptAndItsLinesReconciledOnce` (G4-107's four-day outage is not simulated: `[live-verify]`) |
| T6 | Stripe's integer minor units are converted by the currency's scale; HitPay's numbers are read as exact text; a number with more decimals than the currency allows is refused; both adapters are held free of floats | `TestMinorUnitsAreConvertedWithTheCurrencysScale`, `TestNoFloatsInTheAdapter` (both), `TestChargesFeesAndPayoutsComeAsStatementLines` (`// req: ENG-10`) |
| T7 | The port has no method that writes and the clients send only GET (the round-tripper refuses any other method before it leaves); the manifest names no write scope and asks for a restricted key; the egress guard limits hosts; HitPay's lack of a read-only key is a residual risk (§4) | `TestTheAdapterIsReadOnly` (Stripe), `TestObjectsBalanceAndReadOnly` (HitPay), `TestThePartnerContractNamesEveryOperationTheAdapterCalls` (both) (`// req: FR-INT-27`) |
| T8 | Object IDs that contain a path, query or space are refused; requests go through the egress guard to the manifest's hosts only | `TestObjectsAreFetchedByIDWithTheirLinks` (an ID that is a path is refused) |
| T9 | The payload is sealed (`ports.Cipher`, bound to its row) and read only by a person's review; no log line, audit event or error carries it (the audit records the SHA-256 and the type); errors carry the status only | `TestProviderWebhooksAreStoredOnceAndWorkedFromTheFetchedObject` (not stored in the clear; does not open against another row) |
| T10 | Postings are auto-reconcile's, through the existing commands, within its per-line cap, into open periods (a locked period refuses the line, which waits); the accounts are the ones set up under "Bank data… connections"; `psp_event` and the mirror are never edited (FY620, FY622, FY623) and `psp_event` moves forward only (FY621) | `TestAProvidersRecordsAreKeptAsTheProviderReportedThem`, `TestAProviderAccountIsSweptAndItsLinesReconciledOnce` (the posting is exact, once, undoable, and the worker's sweep posts nothing), `TestAPaymentProvidersAccountIsSetUpThroughTheAPI` |
| T11 | An event type the parser does not know is stored and parked for a person; it posts nothing | `TestProviderWebhooksAreStoredOnceAndWorkedFromTheFetchedObject` (UC-PSP-01) |
| T12 | `organisation_id` and FORCE RLS on every table; the grants of `psp_event` | `TestEveryTenantTableForcesRowLevelSecurity`, `TestEachRoleHoldsExactlyItsGrants` |
| T13 | The connector ID in the path is looked up before any tenant is set and must belong to the named connector; the event is stored under that connector's organisation only | `TestProviderWebhooksAreStoredOnceAndWorkedFromTheFetchedObject` (another connector's name, an unknown ID) |
| T14 | `settleDisputeCredit` needs the right to post on Invoices (the policy of a customer refund) and reads the dispute and the credit from the books, never from the request; the posting is the refund document's own under `dispute_settlement@1` (Dr 1100 · Cr disputes clearing, base currency only), so every refusal of that rule holds; the credit may not exceed the disputed amount and must be an issued credit note; `psp_dispute_credit` is unique per dispute, guarded against change or delete (FY625), and written in the same transaction as the refund; the API role cannot update it | `TestALostDisputeIsSettledAgainstACustomerCredit`, `TestPostDisputeSettlement`, the authorisation matrix row `settleDisputeCredit`, the differential golden `payment-providers.json` (the oracle's half) |
| T15 | A port of its own: `ports.PaymentLinks` has one method, `CreatePaymentRequest`, and `ports.PaymentProvider` stays read-only; the HitPay link client sends `POST /v1/payment-requests` and refuses any other method or path before it leaves; the capability `payment-link` is a system rule that a person's command alone triggers (an AI proposal's work is refused by `checkTrigger`), no handler outside the API calls it (a test reads the sources), and no AI tool or MCP scope exists for it | `TestThePaymentLinksPortHasOneMethod`, `TestTheLinkClientSendsNothingButAPaymentRequest`, `TestThePartnerContractNamesTheOnePaymentRequestWrite` |
| T16 | The request has no amount field: the amount and currency are the invoice's open balance read in the same unit of work (`paymentlink.OpenInvoice`); a draft, void, paid or credit-note document, a second live link (also a unique partial index in the database), and a currency no provider account of the connector collects are each refused before the provider is called; a preview asks the provider nothing | `TestAPaymentLinkIsMadeForTheOpenBalanceAndSettlesTheInvoiceOnce`, `TestAPaymentLinkIsRecordedOnceLiveAndItsPaymentsByTheWorkerOnly` |
| T17 | The worker only mirrors the charge as a payment of a link (`payment_link_payment`, unique per provider charge, written by the worker alone); auto-reconcile takes a line to the invoice only when the link is live, the currency and the amount equal what the provider reported and what the invoice has open, through the existing receipt command (cap, open periods, undo); every other case is left in the queue as `link_mismatch` | `TestAPaymentLinkIsMadeForTheOpenBalanceAndSettlesTheInvoiceOnce` (a replay, another amount), `TestAPaymentLinkIsRecordedOnceLiveAndItsPaymentsByTheWorkerOnly` |
| T18 | The answer's URL must be https (checked in the adapter and by a CHECK in the table), its ID is refused if it holds a path or space, a refusal is an error with the status only (no body), and the customer-facing page is the provider's | `TestTheLinkClientSendsNothingButAPaymentRequest`, `TestARefusedPaymentRequestLeavesNoLinkAndShowsNoProviderWords` |

## 4. What remains

- HitPay documents no read-only API key: the key can do more than Fyniq does with it. The manifest says to keep it in this
  connector only, and the Owner accepts the risk in writing before enabling it `[live-verify]`.
- The fee fields of a HitPay charge, whether a refunded charge carries its refund, and the balance's reserve are
  `[verify]` against a live sandbox; check C.2 #42 shows a wrong fee formula.
- Stripe's webhook secrets roll with up to seven days of overlap; the connector holds the current one, and a roll is
  done by setting the new secret in Fyniq first, then rolling at Stripe `[live-verify]`.
- HitPay's replay protection rests on deduplication alone (no timestamp is signed).
- A payment link is a write at the provider (the one write of the HitPay connector, rev 0.510). HitPay's key could always do more
  than read, so the new grant is the connector's use of it, not the key's power; listed `needs-human-review` and
  `[owner to confirm]`. Fyniq cannot cancel a payment request at the provider (no cancel call is made), so a cancelled link's
  page stays payable until it expires, and a payment on it waits for a person `[live-verify]`.
- Whether HitPay's charge carries the payment request's ID as `payment_request_id`, and that a charge's ID is the reference of its
  statement line, are `[live-verify]` against the sandbox; until then the exact match by number and contact (rev 0.305) stands.
