# Tasks

## 1. Conventions claimed and enforced

- [x] 1.1 Extend the Spectral ruleset so that a state-changing operation without `Idempotency-Key`, a versioned resource without `ETag` / `If-Match`, or a list without a cursor fails `make api-check`. Planted bad operations in a test fixture fail it. Tests `// req: FR-INT-07`, `// req: FR-INT-08`, `// req: FR-INT-09` and `// req: FR-INT-10` on the P1 operations; every invalid field is reported at once
  (19 Oct 2026: `contracts/openapi/functions/apiConventions.js`, run by `make api-check`; an operation opts out of a
  convention only with `x-fyniq-exempt` and a reason, and `TestConventionExemptions` lists every exemption. The six
  commands whose response carries a one-time secret are exempt from Idempotency-Key, recorded in FR-INT-08 (rev 0.27).
  `GET /v1/sign-in-providers` is now cursor-paginated. `TestTheContractLintRefusesPlantedOperations` runs Spectral on
  `contracts/openapi/testdata/planted.yaml`)
- [x] 1.2 Versioning: `Deprecation` and `Sunset` headers on deprecated operations, and `oasdiff` blocking a breaking change inside `/v1`. Test `// req: FR-INT-06`
  (19 Oct 2026: `deprecate()` in `internal/api/conventions.go` sets the dates, `x-sunset` and the note; every response
  then carries `Deprecation`, `Sunset` and a `Link`, documented in the contract. The lint refuses less than six months'
  notice, and `oasdiff` runs with `--deprecation-days-stable 181`, so a sunset too soon or a removal before the sunset
  breaks `/v1`)
- [x] 1.3 Extend the private-endpoint archtest to every route of the web app. Test `// req: FR-INT-01`: the same command refuses the same input through the UI path and through the API
  (19 Oct 2026: the archtest already covers every file of each web app outside `src/api`, and the generated client's
  types refuse an operation the contract lacks; `TestTheWebAppAndTheAPIRefuseTheSameInput` sends the same invalid journal
  through the session and through a token and gets the same status, code and fields)

## 2. New conventions

- [ ] 2.1 Long and bulk operations: an operation resource through the queue port, and per-item results. Test `// req: FR-INT-11`
  (19 Oct 2026, the bulk half: `POST /v1/journals:bulk` saves up to 250 drafts, each in a unit of work of its own, and
  answers with a result per item in order; a refused item carries its problem with field paths into the request, and
  the Idempotency-Key and preview cover the whole request. FR-INT-11 allows 1,000, but 1,000 took 5.3 s locally and would
  pass 10 s at Cloud SQL's latency, so the cap is 250 (1.25 s); `TestBulkCreateJournals` measures it. **Left:** the
  operation resource (`202` and `GET /v1/operations/{id}`) arrives with the first operation that takes longer than 10 s,
  in its own change (likely `p1-reports`' asynchronous exports). Which service and database role runs a long write a
  person asked for is an open design point: the worker's and the executor's grants do not cover it, and widening them
  needs an ADR)
- [x] 2.2 Preview before commit with `Prefer: return=preview`, run in a rolled-back transaction. Test `// req: FR-INT-14`: the database is unchanged and the impact equals that of the committed command
  (19 Oct 2026: `storage.WithPreview` makes the outermost unit of work run the command, flush its audit events and
  `SET CONSTRAINTS ALL IMMEDIATE`, then roll back with `storage.ErrPreviewed`, so the deferred balance check runs; a
  conformance case in `internal/ports/porttest`. `write()` previews when `Prefer` asks for `return=preview`: status 200,
  `Preference-Applied`, no `ETag`, `Location` or claimed key. The contract lint refuses a write without `Prefer`.
  `TestPreviewBeforeCommit` posts after a preview and finds the same lines. FR-INT-14 now says what a preview answers and
  exempts the one-time-secret commands (rev 0.28))
- [x] 2.3 Rate limits per client and per organisation, with `RateLimit` / `RateLimit-Policy` and `Retry-After` on 429. Test `// req: FR-INT-16`
  (19 Oct 2026: migration 000017 adds `rate_limit_window`, a per-minute counter per organisation, client and kind, with no
  audit event; the API's `limit` middleware counts every authenticated request in an organisation, sends `RateLimit` and
  `RateLimit-Policy` (draft-ietf-httpapi-ratelimit-headers-11, still a draft on 19 Oct 2026) and refuses with 429
  `API-RATE-01` and `Retry-After` once the window is full; the hourly `ratelimit.purge` job deletes ended windows.
  Defaults are 600 reads and 120 writes a minute (`api.Deps.Limits`). **Left:** adjusting them per organisation waits for
  the organisation's settings (`p1-organisation-chart`); public operations are limited by IP at the load balancer (SEC-23))

## 3. Clients and contracts

- [ ] 3.1 Client classes for integration clients and personal access tokens, issued by Hydra, with per-client limits and audit; personal access tokens are agent-class by default. Test `// req: FR-INT-02` (P1 part)
- [x] 3.2 JSON Schema for every resource, the published API reference, `llms.txt` and the agent guide, all generated from the contract. Test `// req: FR-INT-13`
  (19 Oct 2026: `make openapi` also writes a JSON Schema 2020-12 per resource to `contracts/schemas/`, and
  `docs/api/reference.md` and `docs/api/llms.txt`, from the same document as the contract;
  `TestTheReferenceIsTheContract` fails while any is stale and checks the hand-written `docs/api/agent-guide.md` has
  its three parts. They are published in the public repository; serving them from an installation comes with
  third-party access in P2)
- [ ] 3.3 Package the TypeScript client as the SDK. Test `// req: FR-INT-17` (P1 part)
- [ ] 3.4 Create `contracts/events/` with a schema per event version. A test fails on an emitted event type without a schema or a version (`// req: platform/release-and-versioning Event schema versions`)
- [ ] 3.5 Finish with `make check` and `make test`
