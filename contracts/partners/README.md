# contracts/partners

The partners' OpenAPI documents, for local mode (PRD-001 §12.8): Prism mocks each one at contract level in the
`partners` Compose profile, so a connector is built and tested against its partner's contract before the partner's
sandbox, which staging uses.

| File | Partner | Mocked by | Added with |
|---|---|---|---|
| `example-bank.yaml` | A made-up bank: the template | `partner-example-bank` on port 4010 | `p1-connector-framework` |
| `wise.yaml` | Wise: an extract of its published Platform API (2026Q4), the two read-only operations the `wise` connector calls, retrieved 26 Oct 2026 | `partner-wise` on port 4011 | `p1-bank-import-reconcile` |
| `airwallex.yaml` | Airwallex: written from its API reference pages, retrieved 26 Oct 2026 (Airwallex publishes no OpenAPI document), the three operations the `airwallex` connector calls | `partner-airwallex` on port 4012 | `p1-bank-import-reconcile` |
| `stripe.yaml` | Stripe: a hand-made extract of the read operations the `stripe` connector calls, from the API reference (retrieved 19 Nov 2026) at API version 2025-09-30.clover | `partner-stripe` on port 4013 | `p1-payment-providers` |
| `slack.yaml` | Slack: `chat.postMessage`, the one operation the `slack` connector calls, from docs.slack.dev (retrieved 21 Nov 2026) | `partner-slack` on port 4015 | `p2-api-third-parties` |
| `hitpay.yaml` | HitPay: a hand-made extract of the read operations the `hitpay` connector calls, from its reference pages (retrieved 19 Nov 2026) | `partner-hitpay` on port 4014 | `p1-payment-providers` |

Each connector that calls a partner adds, in the same change:

1. the partner's published OpenAPI document here, as the partner publishes it (its source URL and the date it was
   retrieved in the document's `info.description`);
2. a service in `deploy/local/compose.yaml` under the `partners` profile, as `partner-example-bank` is, on a port of its
   own;
3. a row in the table above.

Start the mocks with `docker compose -f deploy/local/compose.yaml --profile partners up`; `make up` starts without them.
