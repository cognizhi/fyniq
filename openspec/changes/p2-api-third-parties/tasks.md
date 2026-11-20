# Tasks

Decisions are in design.md. Cloud and real-account checks are tagged `[live-verify]`. Migrations 001151 to 001199.

## 1. Webhooks and events (FR-INT-12)

- [x] 1.1 Migration 001151 (`webhook_endpoint`, `webhook_delivery`, `webhook_attempt`, FORCE RLS, endpoint changes audited, secrets sealed under the field key and bound to the endpoint), the storage port `storage.Webhooks` and its adapter, the signer (`internal/webhooks`).
  - Built: `TestTheSignatureIsStandardWebhooksV1` (the spec's format; the vector is the reference libraries' `[verify]`), `TestSecretsAreRandomAndShowOnlyTheirLastFour`, `TestWebhookRecordsKeepTheirRulesInTheDatabase`.
- [x] 1.2 The fan-out queues a delivery per active endpoint in the notification's transaction (`notify.Fanout`, `webhooks.QueueFor`); the job `webhooks.deliver` sends through the third-party egress guard, signing afresh at every attempt.
  - Built: `TestAWebhookEndpointIsVerifiedBeforeItReceivesASignedEvent` (no title in the body, a pending endpoint receives nothing, switching the feature off stops everything), `TestADeliveryNeverReachesAPrivateAddressOrFollowsARedirect` (a non-local guard). Decision D1 amended in building: no per-delivery outbox row; first and later attempts are driven by `next_attempt_at` on the tick (the outbox has no not-before time), so the first attempt waits at most one tick.
- [x] 1.3 Retries for three days, the dead-letter list and replay; ten failures in a row pause an endpoint; 410 disables it. `TestAFailedWebhookDeliveryIsRetriedThenDeadThenReplayed`, `TestTheRetriesBackOffForThreeDays`.
- [x] 1.4 The API (nine operations; `make openapi` run; the matrix rows `matrixWebhooks`; the one-time-secret exemptions listed in `TestConventionExemptions`). The setting `features.webhooks` (dual controlled, off at once; the event allow-list is 15 types, design D4 amended: no dual control per endpoint, `[owner to confirm]`).
- [ ] 1.5 The `webhooks` screen, help keys, design fragment (the inventory row exists). **Left:** the guide page `docs/user-guide/webhooks.md` and its knowledge-base page are built; the React screen is not.
- [x] 1.6 Threat model `docs/security/threat-models/webhooks.md`, the grants for human review, PRD revision 0.605, spec scenarios, the stamp.

## 2. Inbound tasks (FR-INT-24)

- [x] 2.1 Scope `fyniq.write.tasks`; `POST /v1/tasks`, `PATCH /v1/tasks/{id}` (own tasks of the client only).
  - Built (rev 0.607; the owner's decision of 21 Nov 2026, design D5 amended: a table of its own, `integration_task`, migration 001152, not a new source of `compliance_task`, so no client can reach a statutory or agent task by construction): a new Appendix D row "Tasks made by integrations" (Owner and Administrator C; Controller, Accountant and Auditor V; never the AI), the scope mapped to it, `createIntegrationTask`, `updateIntegrationTask` (own tasks, done or cancelled once), `getIntegrationTask`, `listIntegrationTasks`, the matrix rows, `TestAnIntegrationClientMakesAndUpdatesItsOwnTasks`, `TestNoOneButAScopedIntegrationClientTouchesAnIntegrationTask`, `TestATaskAnIntegrationMadeIsTheClientsOwn`, threat model `integration-tasks.md`, the grants for human review. **Left:** a person closing or editing a client's task in the Inbox; the Go and Python SDK methods exist from the contract.

## 3. Semantic metrics endpoint (FR-INT-15)

- [x] 3.1 Built in P1 (`queryMetrics`, `internal/api/metrics.go`); `TestAnIntegrationClientWithTheReportsScopeAsksTheSemanticLayer` proves an integration client or an agent with `fyniq.read.reports` reaches it and one without is refused.

## 4. SDKs (FR-INT-17)

- [x] 4.1 Built (rev 0.606): `cmd/sdkgen` writes `sdk/go/fyniq/operations_gen.go` and `sdk/python/fyniq/operations_gen.py` from the contract (the 337 operations a client's scopes can reach), the hand-written conventions are `client.go` and `client.py`, `make api-check` fails while they are stale and `make sdk-test` runs `TestAWriteKeepsItsIdempotencyKeyAcrossRetries`, `TestRefusalsAreProblemsAndListsArePaged`, `TestOnlyTheOperationsAClientsScopesReachAreHere` and the Python equivalents. Design D8 amended in building: no external generator (a download was not available, and a 1,100-operation typed client would be tens of megabytes), so the clients take and return JSON. **Left:** typed models, registry publication `[owner to confirm]`, a sandbox run.

## 5. Third-party MCP clients (AI-46)

- [ ] 5.1 Registration, dual-controlled approval, Hydra registration, access review and data inventory entries.

## 6. Sandbox (FR-INT-18)

- [ ] 6.1 A sandbox organisation from the sample company with its own credentials.

## 7. Ticketing and chat (FR-INT-23, FR-NTF-04, FR-INT-04)

- [ ] 7.1 Jira Cloud connector against a recorded contract; Linear, Zoho Desk, GitHub Issues the same way `[live-verify]`.
- [ ] 7.2 Slack and Zoho Cliq connectors; routing rules and their screen.
