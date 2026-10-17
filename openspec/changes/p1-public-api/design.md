# Design

## Context

- huma serves the contract, and `make test` fails while the code, the contract and the generated
  client differ.
- Problem details, `Idempotency-Key` (24 hours per client; the clock fix of #52), `ETag` / `If-Match`
  and cursor lists are middleware with tests tagged FR-INT-07…10.
- `oasdiff` runs against the last release.
- Hydra (Apache-2.0, ADR-T3) issues opaque tokens with introspection. Open question 40 chose option
  (b): no Enterprise Licence.

## Goals / Non-Goals

**Goals:**
- A later change adds an operation and gets every convention for free, with a test that fails if it
  opts out.

**Non-Goals:**
- Third-party access, webhooks (FR-INT-12) and the Go and Python SDKs (P2).
- MCP itself (wave 6).

## Decisions

- **Conventions enforced by a contract lint, not by review.** A Spectral ruleset, already run by
  `make api-check`, gains rules:
  - every state-changing `POST` declares `Idempotency-Key`;
  - every versioned resource returns `ETag`, and its updates require `If-Match`;
  - every list is cursor-paginated;
  - every write declares the preview preference.

  A new endpoint that misses one fails `make test`.
- **Preview runs the real command in a transaction that is rolled back.**
  - The same domain command runs, the ledger impact is collected from `post_entry()`'s plan, and the
    transaction is always rolled back. Preview and commit therefore cannot disagree (FR-INT-14, the
    same simulation as proposals, §9.5).
  - Alternative considered: a separate dry-run code path. Rejected because the two paths would
    drift.
- **Operations for long work go through the outbox.**
  - `202 Accepted` creates an operation row and a job through the queue port (ADR-T7). The client
    polls `GET /v1/operations/{id}`.
  - Bulk endpoints return per-item results, and an item never fails silently.
- **Rate limits per client and per organisation**, counted in PostgreSQL. A token bucket sits in the
  idempotency table's neighbourhood, with no Redis, keeping to scale to zero (ADR-T7). Default limits
  come from settings; headers follow the IETF draft's current names [verify at build time].
- **`contracts/events/`** holds one JSON Schema per event version. A test checks that every outbox
  event type the code emits has its schema and a version suffix.
- **Ports and adapters (ADR-T8):** storage and queue; the OAuth port for client classes. No new
  adapter.
- **Audit events (AUD-01):** client creation, scope and expiry changes, and token revocation.
  Previews write no audit event and no outbox row.

## Risks / Trade-offs

- [The `RateLimit` header draft changes again before P2] → Keep the header names in one place and
  keep `Retry-After` as the stable signal.
- [Preview holds row locks while it runs] → Previews take the same lock order (ENG-47) with a short
  statement timeout, and never hold locks across requests.
