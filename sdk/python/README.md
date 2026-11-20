# Fyniq Python SDK

The Python client of the Fyniq `/v1` API (PRD-001 FR-INT-17): the operations a client's scopes can reach, generated from `contracts/openapi/fyniq.v1.yaml` by `go run ./cmd/sdkgen`
(`fyniq/operations_gen.py`), with the conventions of §8.18.1 built in (`fyniq/client.py`). Standard library only.

```python
from fyniq import Client, Problem

api = Client("https://fyniq.example", token=lambda: os.environ["FYNIQ_TOKEN"])

r = api.query_metrics({"measure": "revenue", "from": "2026-10", "to": "2026-10"})
try:
    api.create_journal(journal, preview=True)        # checks and saves nothing
except Problem as p:
    print(p.code, p.fix)                              # the stable code and the plain-words fix

for item in api.paginate(api.list_journals):          # follows next_cursor
    ...
```

A write takes an Idempotency-Key the client makes once and keeps across retries (FR-INT-08); a 429, or a read's 503, is retried after its `Retry-After` when that is short (FR-INT-16); `Response.etag` goes back in `if_match` (FR-INT-09).
Money is a decimal string: parse it with `decimal.Decimal`, never `float` (ENG-10). There is a method only for an operation a client's scopes can reach.
After a change to the API: `make openapi` regenerates it; `make api-check` fails while it is stale; `make sdk-test` runs its tests.
