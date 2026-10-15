# contracts/openapi

`fyniq.v1.yaml` is the contract of the public API (`/v1`, PRD-001 §8.18): OpenAPI 3.1, exactly as the
API serves it at `/v1/openapi.json`. huma generates it from the registered operations
(`internal/api`), and `make test` fails while the code and this file differ, so every change to the
API shows up here and is reviewed in its pull request. Regenerate it with `make openapi`; never edit
it by hand. release-please keeps `info.version` at the platform version.

What every operation documents (NFR-API-01, checked by Spectral with `spectral.yaml` in `make api-check`
and by `TestEveryOperationIsDocumented`):

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

Who uses it, and what guards it:

| | |
|---|---|
| The web app | `apps/web/src/api/schema.d.ts` is generated from this file (openapi-typescript) and used through `openapi-fetch` in `src/api/client.ts`. `make openapi` regenerates both; `make api-check` fails while the client is stale; `internal/archtest` refuses hand-written network calls elsewhere in the app (QA-API-03) |
| Breaking changes | `oasdiff breaking` compares this file with the last release's (the latest `v*` tag); an error-level change fails `make test` (QA-API-02, FR-INT-06). Inside `/v1` changes are additive only; a breaking change needs `/v2` |
