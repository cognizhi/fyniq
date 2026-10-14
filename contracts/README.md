# contracts

Machine-readable contracts, the source of truth for every interface (golden rule 4, PRD-001 §12.4).

| Directory | What | Arrives with |
|---|---|---|
| `openapi/` | The OpenAPI 3.1 contract of `/v1`; huma's output must equal it in CI, and the web client is generated from it | p0-api-conventions-authz |
| `asyncapi/` | Events (`fyniq.<object>.<event>.v<N>`) | P1 |
| `ai-tools/` | AI and MCP tool schemas | P1 |
| `partners/` | Partner API mocks (Peppol access point, banks) for local mode | P1 |
