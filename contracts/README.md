# contracts

Machine-readable contracts, the source of truth for every interface (golden rule 4, PRD-001 §12.4).

| Directory | What | Arrives with |
|---|---|---|
| `openapi/` | The OpenAPI 3.1 contract of `/v1`; huma's output must equal it in CI, and the web client is generated from it | p0-api-conventions-authz |
| `schemas/` | A JSON Schema 2020-12 per resource of `/v1`, generated with the contract by `make openapi` (FR-INT-13); the API reference, `llms.txt` and the agent guide are in `docs/api/` | p1-public-api (today) |
| `audit/` | The audit event format (AUD-01) and how to verify a chain and its anchors outside Fyniq (§11.5) | p0-audit-chain (today) |
| `events/` | The event catalogue (`fyniq.<object>.<event>.v<N>`): a JSON Schema per event version in `schemas/` (IDs and the time only, never a title or a detail), and `asyncapi.yaml`, the AsyncAPI 3 document that lists them, both written by `go test ./internal/notify -run TestTheEventCatalogue -update` for a new event type. The test fails on an event the code emits without a schema or a version, and on a schema for an event nothing emits; a breaking change is a new version (a new file). The CloudEvents envelope and webhook delivery are P2 (FR-INT-12) | p1-public-api (today) |
| `ai/` | The AI's contracts: the read-tool registry, one `tools/<name>.json` per tool in MCP's tool shape with its version, where it runs, its scope and its limits; `tools.lock.json` pins each version's hash, and `ai.go` embeds the registry for `internal/aitool`. Prompts and the proposal catalogue follow (p1-ai-assistant, p1-ai-proposals) | p1-ai-assistant (today) |
| `connectors/` | The connector manifest format (`manifest.schema.json`, FR-INT-25); each first-party connector's manifest is in `connectors/<id>/manifest.json` and validates against it in `make test` | p1-connector-framework (today) |
| `partners/` | Partners' OpenAPI documents (banks, payment providers, the Peppol access point), each mocked by Prism in the local `partners` profile | p1-connector-framework (today) |
