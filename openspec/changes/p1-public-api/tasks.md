# Tasks

## 1. Conventions claimed and enforced

- [ ] 1.1 Extend the Spectral ruleset so that a state-changing operation without `Idempotency-Key`, a versioned resource without `ETag` / `If-Match`, or a list without a cursor fails `make api-check`. Planted bad operations in a test fixture fail it. Tests `// req: FR-INT-07`, `// req: FR-INT-08`, `// req: FR-INT-09` and `// req: FR-INT-10` on the P1 operations; every invalid field is reported at once
- [ ] 1.2 Versioning: `Deprecation` and `Sunset` headers on deprecated operations, and `oasdiff` blocking a breaking change inside `/v1`. Test `// req: FR-INT-06`
- [ ] 1.3 Extend the private-endpoint archtest to every route of the web app. Test `// req: FR-INT-01`: the same command refuses the same input through the UI path and through the API

## 2. New conventions

- [ ] 2.1 Long and bulk operations: an operation resource through the queue port, and per-item results. Test `// req: FR-INT-11`
- [ ] 2.2 Preview before commit with `Prefer: return=preview`, run in a rolled-back transaction. Test `// req: FR-INT-14`: the database is unchanged and the impact equals that of the committed command
- [ ] 2.3 Rate limits per client and per organisation, with `RateLimit` / `RateLimit-Policy` and `Retry-After` on 429. Test `// req: FR-INT-16`

## 3. Clients and contracts

- [ ] 3.1 Client classes for integration clients and personal access tokens, issued by Hydra, with per-client limits and audit; personal access tokens are agent-class by default. Test `// req: FR-INT-02` (P1 part)
- [ ] 3.2 JSON Schema for every resource, the published API reference, `llms.txt` and the agent guide, all generated from the contract. Test `// req: FR-INT-13`
- [ ] 3.3 Package the TypeScript client as the SDK. Test `// req: FR-INT-17` (P1 part)
- [ ] 3.4 Create `contracts/events/` with a schema per event version. A test fails on an emitted event type without a schema or a version (`// req: platform/release-and-versioning Event schema versions`)
- [ ] 3.5 Finish with `make check` and `make test`
