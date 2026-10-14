# internal

The Go code shared by the commands; nothing outside this repository can import it.

| Package | What |
|---|---|
| `domain/` | The business logic (ledger, money, …): depends only on `ports/` |
| `ports/` | The interfaces the domain needs (Clock, Secrets, BlobStore, Queue, …), and `porttest/`, the conformance suites every adapter passes (NFR-PORT-04) |
| `adapters/<provider>/` | The implementations: `local/` (build tag `local` only, DEV-07) and `gcp/`; the only place a cloud SDK may be imported (NFR-PORT-03) |
| `runtime/` | Build flavour and environment checks at start-up |
| `clock/` | The only reader of the wall clock (DEV-05) |
| `app/` | Starts a service or tool; wires adapters |
| `httpserver/` | The HTTP shell: health, readiness, version |
| `seed/` | The sample-company generator |
| `archtest/` | The architecture rules as tests |
| `version/` | The build's version, commit and time |
