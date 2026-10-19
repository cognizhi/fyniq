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

The contract lint enforces them, so a new operation gets them without a reviewer spotting a missing
header (`functions/apiConventions.js`, rule `fyniq-api-conventions`):

| Convention | Refused when |
|---|---|
| Deprecation (FR-INT-06) | A deprecated operation lacks `x-fyniq-deprecated-on`, `x-sunset` at least six months later, `x-fyniq-deprecation-note`, or the `Deprecation` and `Sunset` headers on its responses. Use `deprecate()` in `internal/api`; `oasdiff` also refuses a sunset less than 181 days away and a removal before the sunset |
| Idempotency (FR-INT-08) | A `POST` takes no `Idempotency-Key` header |
| Concurrency (FR-INT-09) | A command changes a record you can read (the record's own path, or an action under it) without `If-Match`, 412 and 428, or the record's read returns no `ETag` |
| Lists (FR-INT-10) | A list (named `list…`, or a page of `items`) lacks `cursor`, `page_size` and `next_cursor` |
| Preview (FR-INT-14) | A write takes no `Prefer` header, or has no 200 response with `Preference-Applied`. Embed `PreferHeader` and pass it to `write()`: the command then runs in a preview, which checks everything the commit would, including the database's deferred checks, and rolls back |

Bulk operations (FR-INT-11) take up to 250 items and answer with one result per item, in order: use
`bulk()` in `internal/api`, which runs each item in a unit of work of its own and points a refused
item's field paths into the request (`body.items[3]…`).

An operation opts out of one only with `x-fyniq-exempt` naming the convention and the reason (rule
`fyniq-exemption-reasons`); `TestConventionExemptions` lists every exemption, so a new one is reviewed.
Today the six commands whose response carries a one-time secret take no `Idempotency-Key` (FR-INT-08,
rev 0.27) and no preview, which would confirm a one-time code without spending it (FR-INT-14, rev 0.28). `testdata/planted.yaml` holds an operation that breaks each rule;
`TestTheContractLintRefusesPlantedOperations` checks that Spectral refuses every one.

Who uses it, and what guards it:

| | |
|---|---|
| The web app | `apps/web/src/api/schema.d.ts` is generated from this file (openapi-typescript) and used through `openapi-fetch` in `src/api/client.ts`. `make openapi` regenerates both; `make api-check` fails while the client is stale; `internal/archtest` refuses hand-written network calls elsewhere in the app (QA-API-03) |
| Breaking changes | `oasdiff breaking` compares this file with the last release's (the latest `v*` tag); an error-level change fails `make test` (QA-API-02, FR-INT-06). Inside `/v1` changes are additive only; a breaking change needs `/v2` |
