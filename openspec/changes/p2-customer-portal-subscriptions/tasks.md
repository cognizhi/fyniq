# Tasks

Migrations 001070 to 001099. Cloud and real-account checks are tagged `[live-verify]`.

## 1. The customer portal (FR-AR-14)

- [x] 1.1 Migration (`portal_link`, its registry and lookup), storage port, domain `portal` with `Scope`, the staff operations to create, list and revoke a link, the public operations to read the summary, an invoice's PDF and a statement.
  - Tests: the token is stored only as a hash and shown once; expired, revoked and unknown tokens answer one 404; a link reads only its contact's documents (another contact's invoice ID in the same organisation, another organisation's); every other principal's token on a portal route and the portal token on every other route is refused; rate limit; the token never in a log. `// req: FR-AR-14`, `// asvs: V2`, `V3`, `V4`, `V13`
- [x] 1.2 The portal page and the customer's Portal card for staff, help keys, guide page, kb page, design fragment (`customer-portal`).
- [x] 1.3 Threat model, PDPA notes, PRD revision, spec scenarios, stamp.

- Built (rev 0.520): migration 001070 (`portal_link`, its registry `portal_link_token` and `portal_link_route`, FORCE RLS, a guard trigger FYP10 and FYP11, no grant to the worker), `internal/domain/portal` (`Scope`, `Read`, `InvoiceOf`, `Statement`: every read takes the customer from the link), `internal/api/portal.go` (`createPortalLink`, `getContactPortalLinks`, `revokePortalLink`; the public `getPortal`, `getPortalInvoicePDF`, `getPortalStatement`), the page `CustomerPortal` at `/portal` (token from the fragment, sent in `Fyniq-Portal`), the customer's `PortalLinksCard`, help keys, guide sections (`contacts.manage`, `sales.invoices`) and kb pages, the design inventory, the threat model `customer-portal`. Tests: `TestAPortalOpensItsCustomersDocumentsOnly` (cross-customer, unknown, malformed, revoked, expired, other principals both ways, the log), `TestAPortalLinkIsMadeByThoseWhoMayPostAndForCustomersOnly`, `TestAPortalLinkIsRateLimitedPerLink`, `matrixPortal`, the web tests. Needs-human-review: a new external principal and the access design (D1).
- **Left:** emailing the link from Fyniq (the maker sends it); a paid-invoice history (the statement shows it); a staff-side "view as customer".

## 2. Disputes and promise-to-pay (FR-AR-08, P2)

- [x] 2.1 `invoice_dispute`, its operations, the reminder pause and the ageing split. `// req: FR-AR-08`
  - Built (rev 0.521): migration 001072, `internal/domain/disputes` (`Make`, `Resolve`, `Pause`, `Disputed`), `salesreminder.Due` skips a paused invoice, `getReceivables` gains `disputed` per customer, `internal/api/invoice_disputes.go`, the invoice's `DisputeCard`, help keys, the guide section and kb page. Tests: `TestADisputeOrPromisePausesRemindersAndSplitsTheAgeing`, `TestADisputeOrPromiseIsOfAnInvoiceAndOnlyEnds`, `matrixInvoiceDisputes`, `disputeCard.test.tsx`.

## 3. Subscription contracts (FR-AR-15)

- [x] 3.1 Contracts and dated amendments, the derived schedule and contracted MRR, the `subscriptions` screen's own-contract half.
  - Built (rev 0.566): migration 001079, `internal/domain/subscription`, `internal/adapters/postgres/subscription.go`, `internal/api/subscriptions.go` (`createSubscriptionContract`, `listSubscriptionContracts`, `getSubscriptionContract`, `amendSubscriptionContract`, `getContractedMRR`), `SubscriptionContracts` on `/sales/subscriptions`, help keys, guide, kb. Tests: `TestAContractsMonthlyValueFollowsItsAmendmentsFromTheirDays`, `TestTheScheduleIsTheValueInForceOnEachMonthsLastDay` (worked by hand), `TestASubscriptionContractNeverChangesAndItsAmendmentsOnlyAccumulate`, `TestASubscriptionContractIsAmendedFromDatedDaysAndItsMRRIsDerived`, `matrixSubscriptions`, the NaN attacks, `subscriptions.test.tsx`. Records only, no posting; the contracted MRR has an oracle half (`oracle/src/oracle/subscription.py`, `oracle/tests/test_subscription.py`) and hand-worked cases in `test/differential/subscriptions/` that `TestTheSubscriptionCasesConformAndTheEngineReproducesEachCheck` reproduces on the Go side (design D3).
  - **Left:** evidence on an amendment (FR-DOC-05), the day-weighted revenue schedule feeding deferred revenue (with an oracle half), a base-currency total, the contract on the customer portal, the KPI page's contracted MRR tile (FR-KPI-04).

## 4. Revenue over time for fixed-fee projects (FR-PRJ-11)

- [ ] 4.1 Election, cost-to-cost progress, contract asset and liability, a monthly journal proposed for approval, with the oracle's half and a differential scenario `[accountant to confirm]`.
  - **First half built (rev 0.571):** `RND-RECOGNISE` (`rounding.Recognised`), `internal/domain/projectrevenue` (`Work`), the oracle's twin `oracle/src/oracle/project_revenue.py`, hand-worked cases in `test/differential/project-revenue/` (11, checked against Fractions) reproduced by `TestTheProjectRevenueCasesConformAndTheEngineReproducesEach` and `oracle/tests/test_project_revenue.py` with Hypothesis properties. Design: a milestone invoice already credits revenue, so the journal is a true-up of the ledger's revenue for the project to what is earned; contract asset = earned less invoiced when positive, liability when negative.
  - **Left:** the election and the estimated total cost on a fixed-fee project (migration, API, screen), the cost to date read from the ledger by project (`projectreport.LedgerByProject`), the settings naming the contract asset and liability accounts, the monthly proposal as an ordinary draft journal carrying the project through `requestJournalPost`, a per-month record so a month is proposed once, help, guide, kb and the design fragment.
