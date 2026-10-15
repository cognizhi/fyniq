# Tasks

## 1. Contract and client

- [x] 1.1 Write `contracts/openapi/fyniq.v1.yaml` with the first endpoints (organisation, accounts read, manual journals) and serve it with huma; CI fails when the generated document differs from the contract; lint `// req: NFR-API-01` asserts a description, examples, permission and error codes on 100% of operations
- [ ] 1.2 Generate the TypeScript client into `apps/web/src/api` and forbid hand-written fetches in a lint rule; verify the web shell builds against it
- [ ] 1.3 Add `oasdiff` against the last release's contract; test `// req: QA-API-02` plants a breaking change and expects CI to fail

## 2. Conventions middleware

- [x] 2.1 Add RFC 9457 problem details with stable codes and field-level fixes; tests `// req: FR-INT-07` cover each error code the first endpoints return
- [x] 2.2 Add `Idempotency-Key` on writes with a request hash and a 24-hour store; test `// req: FR-INT-08` repeats a post (same result) and changes its body (422, as FR-INT-08 states; this task said 409)
- [x] 2.3 Add `ETag` and `If-Match` on versioned records; test `// req: FR-INT-09` updates with a stale ETag and expects 412
- [x] 2.4 Add cursor lists and the value formats (decimal strings for money); test `// req: FR-INT-10`

## 3. Authorisation

- [x] 3.1 Write the policy table and the handler wrapper that refuses an operation without a policy; test `// req: SEC-02` calls an unregistered operation and a cross-tenant ID and expects refusals
- [x] 3.2 Generate the matrix test (every role in §7.1 × every endpoint × allowed or denied, with field masking) from the policy table and Appendix D; `// req: QA-SEC-03` passes in `make test`, proving §17.3 exit item 2's half; then `make check`
