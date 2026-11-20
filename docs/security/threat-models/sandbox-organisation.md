# Threat model: the sandbox organisation

| | |
|---|---|
| Feature and requirement IDs | FR-INT-18 (sandbox), FR-ADM-07 (Super admin scope, dual control), FR-INT-12 (webhooks), FR-INT-21 (email), AI-70 (model connections); p2-api-third-parties task 6.1; rev 0.617 |
| Author, reviewer, date | Claude Code for the owner, 21 Nov 2026; second review waived before production (AGENTS.md), held with `needs-human-review` |
| Data and its class | Only the synthetic sample company (DEV-04): no personal data, no real amounts, no bank numbers |
| Who acts | A Super admin makes the organisation (with two Super admins, the other approves); its Owner (the creator) makes integration clients and tokens as in any organisation; integrators call the API with them |

## 1. What we are building

The owner's decision of 21 Nov 2026: the sandbox is a flagged organisation of the same installation, enabled per deployment (`FYNIQ_SANDBOX=on`, off by default). Trust boundaries: the Super admin to the API (step-up, dual control); the API to the database (the registry no role can write); every outbound path, which must stay inside Fyniq for this organisation.

## 2. What can go wrong

| # | Threat | Control | Test |
|---|---|---|---|
| T1 | A real organisation is turned into a sandbox one (its data then treated as test data), or a sandbox into a real one (a way to put test data among real books) | The flag is a registry row written only by the creation function in the statement that makes the organisation; a trigger refuses a row for an organisation that existed before the transaction and every update or delete, for the table's owner too (FY960) | `TestAttackASandboxOrganisationIsMadeNewFlaggedForGoodAndNeverFromARealOne` |
| T2 | Someone other than a Super admin, or one Super admin alone where two exist, makes one | `require_super_admin` (FY085); with two Super admins the other's approved, unapplied installation change of step `sandbox` naming this organisation (FY213); the requester cannot approve; the API needs step-up | the same, `TestASuperAdminMakesASandboxOrganisationWhereTheDeploymentAllowsIt` |
| T3 | A deployment that did not ask for sandboxes gets them | The API refuses (API-SANDBOX-01) unless the operator's `FYNIQ_SANDBOX=on`; no setting an organisation or a Super admin can change | the API test |
| T4 | Real email, payment-provider, bank, InvoiceNow, AI or webhook traffic leaves for a sandbox organisation | `connector.Runtime` refuses every adapter built for a sandbox organisation (ErrSandbox) and gives its email a sink; the outbox runs only an allow-list of kinds for it, default-deny, so a new outbound kind is blocked until listed; InvoiceNow sends, refreshes and polls nothing; the webhook sender reaches only the operator's allow-list (empty by default), at creation, at verification and at delivery; the API refuses to install a connector. The flag is read in each piece of work's own unit of work, never from a payload or a cache that something could set | `TestAttackASandboxOrganisationsOutboundWorkIsNotRun`, `TestAttackASandboxOrganisationGetsNoOutboundAdapter`, `TestASandboxOrganisationsWebhookReachesOnlyAnAllowedHost` |
| T5 | A sandbox client or token reaches the real organisation, or the reverse | Tenancy: every row has `organisation_id` and RLS; a token is bound to its organisation; another member's organisation is "not found" in the switcher | `TestASuperAdminMakesASandboxOrganisationWhereTheDeploymentAllowsIt` (the Owner of the real organisation cannot switch into it) |
| T6 | The sandbox is used to open a practice organisation or to copy real data in | A practice organisation is refused inside a sandbox; the sample company is loaded only into a sandbox that has no chart; no operation copies data between organisations | the API test |

## 3. Left

`[live-verify]` a run of the Go and Python SDKs against a deployed sandbox (needs a deployment). The IRAS filer has no operation that calls it yet: when one does, it must ask `Sandboxes().Is` first (the rule for every new outbound call). A sandbox's AI chat runs through the AI service's `Runtime.Model`, which refuses; a scripted model for sandboxes is not built.
