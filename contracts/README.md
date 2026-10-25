# contracts

Machine-readable contracts, the source of truth for every interface (golden rule 4, PRD-001 §12.4).

| Directory | What | Arrives with |
|---|---|---|
| `openapi/` | The OpenAPI 3.1 contract of `/v1`; huma's output must equal it in CI, and the web client is generated from it | p0-api-conventions-authz |
| `schemas/` | A JSON Schema 2020-12 per resource of `/v1`, generated with the contract by `make openapi` (FR-INT-13); the API reference, `llms.txt` and the agent guide are in `docs/api/` | p1-public-api (today) |
| `audit/` | The audit event format (AUD-01) and how to verify a chain and its anchors outside Fyniq (§11.5) | p0-audit-chain (today) |
| `asyncapi/` | Events (`fyniq.<object>.<event>.v<N>`) | P1 |
| `ai-tools/` | AI and MCP tool schemas | P1 |
| `connectors/` | The connector manifest format (`manifest.schema.json`, FR-INT-25); each first-party connector's manifest is in `connectors/<id>/manifest.json` and validates against it in `make test` | p1-connector-framework (today) |
| `partners/` | Partner API mocks (Peppol access point, banks) for local mode | P1 |
