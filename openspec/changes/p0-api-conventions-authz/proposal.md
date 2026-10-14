# Proposal

## Why

Every capability is an API operation before it is a screen, and the web app calls only the generated
client (golden rule 4). P0 lays the contract-first API, its conventions and the authorisation matrix
skeleton, so every P1 endpoint is born documented, authorised and tested (PRD-001 §8.18.1, §17.3
exit item 2).

## What Changes

- `contracts/openapi/fyniq.v1.yaml` as the source of truth, served by huma and checked against the code
  in CI; the TypeScript client generated from it for `apps/web`.
- The conventions as shared middleware, ready for the P1 endpoints: RFC 9457 problem details with
  stable codes (FR-INT-07), `Idempotency-Key` on writes (FR-INT-08), `ETag` / `If-Match` on versioned
  records (FR-INT-09), cursor lists and value formats (FR-INT-10). The requirements themselves are P1;
  P0 builds and tests the middleware.
- The first endpoints, each documented to NFR-API-01: the caller's organisation, accounts (read) and
  manual journals (draft, post, reverse, read).
- Authorisation in the API (role and attribute checks from one policy table) and in the database (RLS,
  from p0-database-tenancy-migrations) (SEC-02).
- The authorisation matrix test skeleton: every role × every endpoint × allowed or denied, generated
  from the policy table, including field masking (QA-SEC-03).
- `oasdiff` in CI, so a breaking change inside `/v1` fails (QA-API-02).

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None (`skip_specs: true`). It builds NFR-API-01 (`platform/performance-and-scale`), SEC-02 API half
(`security/identity-and-access`) and QA-SEC-03 and QA-API-02 (`quality/test-gates`), and the
middleware behind FR-INT-07…10 (`integrations/public-api`, P1).

## Impact

- `contracts/openapi/`, `cmd/api`, `internal/api/`, `internal/authz/`, `apps/web/src/api` (generated).
- Depends on p0-database-tenancy-migrations and p0-money-posting-engine.
- **Second human reviewer required** (AGENTS.md: tenancy and approvals touch the authorisation path).
