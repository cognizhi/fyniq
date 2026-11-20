# Tasks

Decisions are in design.md. Cloud and real-account checks are tagged `[live-verify]`. Migrations 001151 to 001199.

## 1. Webhooks and events (FR-INT-12)

- [ ] 1.1 Migration (`webhook_endpoint`, `webhook_delivery`, FORCE RLS, audited, secrets sealed), the storage port and adapter, the signer (`internal/webhooks`).
  - Tests: Standard Webhooks signature vectors; a tampered body, an old timestamp and a wrong secret fail; two signatures while a secret rolls. `// req: FR-INT-12`
- [ ] 1.2 The fan-out writes a delivery per matching endpoint in the notification's transaction; the delivery job sends through the egress guard.
  - Tests: an event of a rolled-back change has no delivery; a private address, a redirect and a slow answer are refused; a retry keeps the `webhook-id`. `// req: FR-INT-12`
- [ ] 1.3 Retries for three days, the dead-letter list and replay; an endpoint failing ten running is paused.
- [ ] 1.4 The API: create, list, change, pause, roll the secret, verify, list deliveries, replay; the matrix rows; the contract and `make openapi`.
- [ ] 1.5 The `webhooks` screen, help keys, guide page, kb page, design fragment.
- [ ] 1.6 Threat model, the grants for human review, PRD revision, spec scenarios.

## 2. Inbound tasks (FR-INT-24)

- [ ] 2.1 Scope `fyniq.write.tasks`; `POST /v1/tasks`, `PATCH /v1/tasks/{id}` (own tasks of the client only); source `integration`.

## 3. Semantic metrics endpoint (FR-INT-15)

- [x] 3.1 Built in P1 (`queryMetrics`, `internal/api/metrics.go`); the public reference and the agent guide name it; an integration client with `fyniq.read.reports` reaches it (verify).

## 4. SDKs (FR-INT-17)

- [ ] 4.1 `sdk/go` and `sdk/python` generated, with the conventions layer, and a staleness check in `make api-check`.

## 5. Third-party MCP clients (AI-46)

- [ ] 5.1 Registration, dual-controlled approval, Hydra registration, access review and data inventory entries.

## 6. Sandbox (FR-INT-18)

- [ ] 6.1 A sandbox organisation from the sample company with its own credentials.

## 7. Ticketing and chat (FR-INT-23, FR-NTF-04, FR-INT-04)

- [ ] 7.1 Jira Cloud connector against a recorded contract; Linear, Zoho Desk, GitHub Issues the same way `[live-verify]`.
- [ ] 7.2 Slack and Zoho Cliq connectors; routing rules and their screen.
