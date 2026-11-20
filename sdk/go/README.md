# Fyniq Go SDK

The Go client of the Fyniq `/v1` API (PRD-001 FR-INT-17): the operations a client's scopes can reach, generated from `contracts/openapi/fyniq.v1.yaml` by `go run ./cmd/sdkgen`
(`operations_gen.go`), with the conventions of §8.18.1 built in (`client.go`). Its own module, standard library only.

```go
c := &fyniq.Client{BaseURL: "https://fyniq.example", Token: func(ctx context.Context) (string, error) { return os.Getenv("FYNIQ_TOKEN"), nil }}

// An operation is a method; path arguments come first, then the body, then options.
r, err := c.QueryMetrics(ctx, map[string]any{"measure": "revenue", "from": "2026-10", "to": "2026-10"})
var p *fyniq.Problem
if errors.As(err, &p) { fmt.Println(p.Code, p.Fix) } // a refusal: the stable code and the plain-words fix

// A write takes an Idempotency-Key the client makes once and keeps across retries; a preview checks and saves nothing.
_, err = c.CreateJournal(ctx, journal, fyniq.WithPreview())

// A list, followed by its cursor.
err = fyniq.Paginate(ctx, func(ctx context.Context, cursor string) (*fyniq.Response, error) {
    if cursor == "" { return c.ListJournals(ctx) }
    return c.ListJournals(ctx, fyniq.WithQuery("cursor", cursor))
}, func(item json.RawMessage) error { return nil })
```

Money is a decimal string in the JSON: decode it into a string or a decimal type, never `float64` (ENG-10). The token is yours (an integration client's or a personal access token's, FR-INT-02) and goes to the base URL only.
There is a method only for an operation a client's scopes can reach; a person's operations (approvals, users, the audit trail, webhook endpoints) are in the web app.
After a change to the API: `make openapi` regenerates it; `make api-check` fails while it is stale; `make sdk-test` runs its tests.
