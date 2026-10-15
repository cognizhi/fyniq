# contracts/openapi

`fyniq.v1.yaml` is the contract of the public API (`/v1`, PRD-001 §8.18): OpenAPI 3.1, exactly as the
API serves it at `/v1/openapi.json`. huma generates it from the registered operations
(`internal/api`), and `make test` fails while the code and this file differ, so every change to the
API shows up here and is reviewed in its pull request. Regenerate it with `make openapi`; never edit
it by hand. release-please keeps `info.version` at the platform version.

What every operation documents (NFR-API-01, checked by `TestEveryOperationIsDocumented`):

| Field | What |
|---|---|
| `summary`, `description` | The purpose, the side effects and the approval policy that may apply |
| `x-fyniq-permission` | The PRD-001 Appendix D capability and the right it needs (`C`, `A`, `P`, `R`, `V`), from the policy table in `internal/authz` |
| `x-fyniq-error-codes` | Every stable code it can return; each error response names its codes |
| examples | On every parameter and on every leaf of the request and response bodies |

The conventions (§8.18.1): problem details with stable codes (FR-INT-07), `Idempotency-Key` on every
`POST` (FR-INT-08), `ETag` and `If-Match` on versioned records (FR-INT-09), cursor lists and money
as decimal strings with a currency (FR-INT-10). The `info.description` of the document states them for
integrators.
