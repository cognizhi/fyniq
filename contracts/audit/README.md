# The audit trail format (PRD-001 §11.5)

Anyone with the events and the anchors can verify an organisation's trail without Fyniq's code.

## Events

Each event's **body** is a JSON object in the format `fyniq-audit-event@1`
([`audit-event.v1.schema.json`](audit-event.v1.schema.json), AUD-01). The body is stored exactly as
written and never changed. The database adds three values per event:

| Field | Meaning |
|---|---|
| `seq` | The event's position in its organisation's chain: 1, 2, 3, … with no gaps |
| `prev_hash` | The `hash` of the event before it; for the first event, 32 zero bytes |
| `hash` | `SHA-256(prev_hash ‖ body)`: the 32 bytes of `prev_hash` followed by the body's UTF-8 bytes |

To verify a chain: start with 32 zero bytes, and for each event in `seq` order check that its
`prev_hash` equals the running value, compute `SHA-256(running ‖ body)`, check it equals `hash`, and
carry it forward. A gap in `seq`, a broken link or a different hash means the trail was altered.

## Anchors

Every hour a job signs each organisation's chain head and writes it, once, to a retention-locked
bucket (`audit-anchors/<organisation>/<seq, 20 digits>.json`; CTL-AUD-02):

```json
{"anchor": {"schema": "fyniq-audit-anchor@1", "organisation_id": "…", "seq": 42,
            "head_hash": "<hex>", "anchored_at": "<RFC 3339>", "key_id": "…"},
 "signature": "<base64>"}
```

The signature covers the compact JSON of `anchor` as written (Go's `encoding/json`, fields in the
order above). In production the key is a Cloud KMS asymmetric key whose public key verifies offline;
locally it is an Ed25519 key under `.data/keys/`. A chain rewritten from its first event still
disagrees with an anchor written before the rewrite, so verification checks every anchor against the
recomputed hash at its `seq` (INV-12, AUD-03).

Values are masked before they reach a body (SEC-15): secrets and tokens are replaced by
`[redacted]`; NRIC/FIN and bank or card numbers keep their last four characters.
