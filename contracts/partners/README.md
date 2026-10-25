# contracts/partners

The partners' OpenAPI documents, for local mode (PRD-001 §12.8): Prism mocks each one at contract level in the
`partners` Compose profile, so a connector is built and tested against its partner's contract before the partner's
sandbox, which staging uses.

| File | Partner | Mocked by | Added with |
|---|---|---|---|
| `example-bank.yaml` | A made-up bank: the template | `partner-example-bank` on port 4010 | `p1-connector-framework` |

Each connector that calls a partner adds, in the same change:

1. the partner's published OpenAPI document here, as the partner publishes it (its source URL and the date it was
   retrieved in the document's `info.description`);
2. a service in `deploy/local/compose.yaml` under the `partners` profile, as `partner-example-bank` is, on a port of its
   own;
3. a row in the table above.

Start the mocks with `docker compose -f deploy/local/compose.yaml --profile partners up`; `make up` starts without them.
