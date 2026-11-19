# @fyniq/sdk

The TypeScript SDK of the Fyniq API (PRD-001 FR-INT-17): a client typed from the `/v1` contract, with the
conventions of §8.18.1 built in. It is generated from `contracts/openapi/fyniq.v1.yaml`; never hand-write a client.
The web app takes its types from it. The Go and Python SDKs are P2, and so is publishing to a registry
(`private` stays `true` until then; `pnpm pack` makes the tarball that would be published).

```ts
import { createFyniqClient, paginate, unwrap, etagOf, previewHeaders } from "@fyniq/sdk";

const api = createFyniqClient({ baseUrl: "https://fyniq.example", token: () => process.env.FYNIQ_TOKEN! });

// Every item of a list, following the cursor (FR-INT-10).
for await (const c of paginate((cursor) =>
  api.GET("/v1/integration-clients", { params: { query: { cursor } } }).then(unwrap))) {
  console.log(c.name);
}

// A refusal throws a FyniqError with the API's stable code and its plain-words fix (FR-INT-07).
try {
  unwrap(await api.GET("/v1/integration-clients/{integration_client_id}", { params: { path: { integration_client_id: id } } }));
} catch (e) { /* e.code, e.fix, e.fields */ }
```

What it does for you:

| Rule | Built in |
|---|---|
| FR-INT-08 | an `Idempotency-Key` on every write that has none, kept across retries |
| FR-INT-16 | a 429, or a read's 503, is retried after its `Retry-After` (up to `maxRetries`, 2; a wait over `maxWaitMs`, 10 s, is returned to you); `rateLimitOf(response)` reads `RateLimit` |
| FR-INT-07 | `unwrap(result)` throws a `FyniqError` (`status`, `code`, `fix`, `fields`) |
| FR-INT-09 | `etagOf(response)` for `If-Match` |
| FR-INT-14 | `previewHeaders` (`Prefer: return=preview`) and `wasPreview(response)` |
| FR-INT-10 | `paginate(page)` walks `next_cursor` |

The token is yours: an integration client's or a personal access token's (FR-INT-02). It is sent to the base URL only.

After a change to the API: `make openapi` regenerates the contract and `src/schema.d.ts`; `make api-check` fails while
`src/schema.d.ts` is out of date.
