# Threat model: payment-provider connectors (Stripe, HitPay)

| | |
|---|---|
| Feature and requirement IDs | Payment-provider connectors and signed webhooks (FR-PSP-01), Model B (FR-PSP-03, FR-PSP-07), refunds and disputes (FR-PSP-04), fees and their GST (FR-PSP-05), payout reconciliation (FR-PSP-06); `p1-payment-providers` (PRD-001 open questions 23 and 45, rev 0.25) |
| Author, reviewer, date | Claude Code for the owner, 19 Nov 2026; second review waived before production (AGENTS.md), held with `needs-human-review` if wanted |
| Data and its class | Provider API keys and webhook secrets (secrets); raw webhook payloads (a customer's name, email and address: personal data, kept sealed, SEC-11); balance transactions, invoices, subscriptions (confidential business data); the postings they lead to (ledger, append-only) |
| Who acts | The Owner and Administrator (install, configure and enable a connector under dual control, as every organisation connector, `connector.change`); the worker (fetch and post, as the connector's own principal); Stripe and HitPay (webhooks from the internet); nobody at Fyniq can charge, refund, retry or pay out at a provider: the connector is read-only |

## 1. What we are building

A provider connector does two read-only things: it feeds the provider's balance transactions into a money account as the
statement lines of the provider's clearing account (the bank-data capability, the existing sync), and it takes the
provider's signed webhooks. A verified webhook is stored once per event ID, as received (its payload sealed), and queued;
the worker fetches the object it names from the provider and hands the fetched object, never the payload, to the rule of
its kind, which posts through the commands that already exist, once (a posting identity keyed on the provider object and
the action).

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
| T10 | Posting | Elevation: the connector posts into a locked period, beyond what a person could, or a person or the AI uses the connector to post | Closed books change; the never-list is bypassed | Low |
| T11 | Unknown events | Tampering: an event type the parser does not know is acted on | Unreviewed postings | Low |
| T12 | API ↔ database | Information disclosure: one organisation reads another's events | Cross-tenant disclosure | Low |
| T13 | Webhook endpoint | Spoofing: one organisation's connector ID used to post an event into another's | Cross-tenant tampering | Low |

SOD and money movement: nothing in this feature moves money. The postings are the same rules a person's categorising
posts, made by the connector's own principal, audited with the provider's event ID, and bounded by exact provider IDs;
the S$500 auto-reconcile cap still stands for matching a payout to its bank line. The AI and MCP have no route to any
operation here (the never-list is unchanged: no tool is added).

## 3. What we do about it

| Threat | Control | Test that proves it (`// req:` and `// asvs:`) |
|---|---|---|
| T1 | The binding verifies the raw bytes against the connector's own secret before anything is stored: Stripe's `v1` HMAC-SHA256 over `t.body`, HitPay's HMAC-SHA256 over the body; a failure is 401 with no row | `TestAWebhookIsAcceptedOnlyWithASignatureOfItsRawBodyAndAFreshTimestamp` (Stripe), `TestAWebhookIsAcceptedOnlyWithASignatureOfItsRawBody` (HitPay), `TestProviderWebhooksAreStoredOnceAndWorkedFromTheFetchedObject` (`// req: FR-PSP-01`, `// asvs: V4.1.5`) |
| T2 | Stripe's timestamp must be within five minutes of the platform's clock; every event is deduplicated on its event ID (a unique key per connector, answering 2xx and changing nothing); HitPay, which signs no timestamp and sends no ID, is deduplicated on a digest of its type, object and body; posting identity (design D4) makes a late redelivery post nothing | the same tests; `TestEventsGetAnIDFromTheirBody` (HitPay); the posting-identity tests of task 2.2 |
| T3 | A 512 KiB cap on the body (413); the outbox queues and retries the work with backoff, one row per event; the connector's rate limit and breaker bound reads | `TestProviderWebhooksAreStoredOnceAndWorkedFromTheFetchedObject` (a body too large) |
| T4 | The worker fetches the object the event names and posts from the fetch, never the payload; the payload is stored as evidence only | `TestProviderWebhooksAreStoredOnceAndWorkedFromTheFetchedObject` (the rule is handed the fetched object, the provider is asked once) |
| T5 | Postings are keyed on the provider object and the action, not the event; events never order anything; the daily sweep fetches what was missed | tasks 2.2 and 2.4: G4-040, G4-107 |
| T6 | Stripe's integer minor units are converted by the currency's scale; HitPay's numbers are read as exact text; a number with more decimals than the currency allows is refused; both adapters are held free of floats | `TestMinorUnitsAreConvertedWithTheCurrencysScale`, `TestNoFloatsInTheAdapter` (both), `TestChargesFeesAndPayoutsComeAsStatementLines` (`// req: ENG-10`) |
| T7 | The port has no method that writes and the clients send only GET (the round-tripper refuses any other method before it leaves); the manifest names no write scope and asks for a restricted key; the egress guard limits hosts; HitPay's lack of a read-only key is a residual risk (§4) | `TestTheAdapterIsReadOnly` (Stripe), `TestObjectsBalanceAndReadOnly` (HitPay), `TestThePartnerContractNamesEveryOperationTheAdapterCalls` (both) (`// req: FR-INT-27`) |
| T8 | Object IDs that contain a path, query or space are refused; requests go through the egress guard to the manifest's hosts only | `TestObjectsAreFetchedByIDWithTheirLinks` (an ID that is a path is refused) |
| T9 | The payload is sealed (`ports.Cipher`, bound to its row) and read only by a person's review; no log line, audit event or error carries it (the audit records the SHA-256 and the type); errors carry the status only | `TestProviderWebhooksAreStoredOnceAndWorkedFromTheFetchedObject` (not stored in the clear; does not open against another row) |
| T10 | The connector posts only through the existing commands, as its own principal, into open periods (a locked one parks the event); `psp_event` is evidence that is never edited (FY620) and moves forward only (FY621) | the database guard tests; task 2.2 |
| T11 | An event type the parser does not know is stored and parked for a person; it posts nothing | `TestProviderWebhooksAreStoredOnceAndWorkedFromTheFetchedObject` (UC-PSP-01) |
| T12 | `organisation_id` and FORCE RLS on every table; the grants of `psp_event` | `TestEveryTenantTableForcesRowLevelSecurity`, `TestEachRoleHoldsExactlyItsGrants` |
| T13 | The connector ID in the path is looked up before any tenant is set and must belong to the named connector; the event is stored under that connector's organisation only | `TestProviderWebhooksAreStoredOnceAndWorkedFromTheFetchedObject` (another connector's name, an unknown ID) |

## 4. What remains

- HitPay documents no read-only API key: the key can do more than Fyniq does with it. The manifest says to keep it in this
  connector only, and the Owner accepts the risk in writing before enabling it `[live-verify]`.
- The fee fields of a HitPay charge, whether a refunded charge carries its refund, and the balance's reserve are
  `[verify]` against a live sandbox; check C.2 #42 shows a wrong fee formula.
- Stripe's webhook secrets roll with up to seven days of overlap; the connector holds the current one, and a roll is
  done by setting the new secret in Fyniq first, then rolling at Stripe `[live-verify]`.
- HitPay's replay protection rests on deduplication alone (no timestamp is signed).
