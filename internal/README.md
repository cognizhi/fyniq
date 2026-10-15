# internal

The Go code shared by the commands; nothing outside this repository can import it.

| Package | What |
|---|---|
| `domain/` | The business logic, depending only on `ports/`: `money/` (exact decimals with a currency, ENG-10…14, ENG-44), `rounding/` (the named policies of §10.4), `ledger/` (the pure posting engine, ENG-01), `journal/` (the manual journal: draft, post, reverse), `balance/` (the balance service, the only reader of balances, ENG-04) |
| `ports/` | The interfaces the domain needs (Clock, Secrets, BlobStore, Queue, …; `storage/`, the storage port and unit of work), and `porttest/`, the conformance suites every adapter passes (NFR-PORT-04) |
| `adapters/<provider>/` | The implementations: `local/` (build tag `local` only, DEV-07), `gcp/` and `postgres/` (the storage adapter: the only place SQL, pgx and sqlc may appear, NFR-PORT-01; see `db/README.md`); the only place a cloud SDK may be imported (NFR-PORT-03) |
| `runtime/` | Build flavour and environment checks at start-up |
| `clock/` | The only reader of the wall clock (DEV-05) |
| `app/` | Starts a service or tool; wires adapters |
| `httpserver/` | The HTTP shell: health, readiness, version |
| `seed/` | The sample-company generator |
| `archtest/` | The architecture rules as tests |
| `version/` | The build's version, commit and time |
