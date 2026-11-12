# The audit trail format (PRD-001 §11.5)

Anyone with the events and the anchors can verify an organisation's trail without Fyniq's code.

## Events

Each event's **body** is a JSON object in the format its `schema` names (AUD-01):
- `fyniq-audit-event@3` ([`audit-event.v3.schema.json`](audit-event.v3.schema.json)) for events written since PRD-001
  rev 0.244;
- `fyniq-audit-event@2` ([`audit-event.v2.schema.json`](audit-event.v2.schema.json)) from rev 0.230;
- `fyniq-audit-event@1` ([`audit-event.v1.schema.json`](audit-event.v1.schema.json)) before.

The body is stored exactly as written and never changed. The database adds three values per event:

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

Values are masked before they reach a body (SEC-15): secrets and tokens are replaced by `[redacted]`, and NRIC/FIN
numbers found in text keep their last four characters.

From version 2, a **sensitive value** (AUD-02, proof only) never reaches a body. An identifier (NRIC/FIN, a bank, card
or PayNow number) or a person's pay is replaced by its proof:

```json
{"fingerprint": "fp1:<HMAC-SHA256 in unpadded base64url>", "hint": "******6789"}
```

The fingerprint is the organisation's keyed hash of the value (SEC-11: Cloud KMS `macSign`; a key file locally),
equal for equal values. So two proofs show whether a value changed, and whether it changed back, but not what it is.
An identifier's proof keeps its last four characters as `hint`; a pay figure's has none. Version 1 events kept an
identifier's last four characters only.

## Personal values (version 3)

From version 3 (§11.8), an event about a **person** keeps that person's own values sealed: a member (by their user), a
person invited or asking to join before they are a member, and a contact who is a director or is marked as a person. The
value of each field named as personal (a name, an email address, a phone number, an address, at any depth) is replaced
by

```json
{"pk": "<the person key's ID>", "ct": "<unpadded standard base64>"}
```

`ct` is AES-256-GCM under the person's data key for the event's financial year: the 12-byte nonce, the ciphertext and its
16-byte tag. Its associated data is `fyniq-audit-personal@1:<organisation>:<event>:<field>`, where a nested value's
field is its path, such as `payee.name` or `emails[1]`, so a sealed value copied to another event or field does not open.
The data key is wrapped by the deployment's key-encryption key (Cloud KMS; a key file locally) and kept in
`fyniq.person_key`, one per person and financial year. When the records' retention for that year ends, the DPO approves
the shredding of its keys: the values no longer open, while the events, their hashes and the anchors stay as they were,
so the chain still verifies. A value is sealed after masking (SEC-15) and is never masked again.

## Security events' details

From version 2, a **security event** (AUD-06) keeps the IP address and user agent it came from beside the chain, in
`fyniq.audit_request_detail`, not in its body. Security events are:
- sign-ins and sign-outs (`session.started`, `session.ended`);
- changes to how a person signs in, and recoveries;
- a Super admin's installation events;
- every reveal of a full identifier and every export.

Their body's `request` keeps its `id` and `session`, leaves `ip` and `user_agent` empty, and carries `detail`:
`fp1:` and the organisation's keyed fingerprint of the JSON pair `["<ip>", "<user agent>"]`. While the details exist,
verification checks them against that fingerprint, so a changed address is caught.

After the security-log retention (`retention.security_events_days`, 400 days by default and at least 30), the erasure
job deletes them. The event stays, so the chain still verifies.

### The erasure register

Before each erasure the job writes one signed entry, once, to `audit-erasures/<organisation>/<cutoff>.json`:

```json
{"erasure": {"schema": "fyniq-audit-erasure@1", "organisation_id": "…", "cutoff": "<RFC 3339>", "count": 3,
             "written_at": "<RFC 3339>", "key_id": "…"},
 "signature": "<base64>"}
```

The signature covers the compact JSON of `erasure` as written, with the anchors' key. Every detail of an event that
occurred before `cutoff` is erased. A database restored from a backup taken before an entry re-applies the register
before it opens (AUD-09, G4-070), so an erased address never comes back.

## Close anchors

A period's close records a **snapshot** of the books at the period end (AUD-04), and a reopening records one after.
From PRD-001 rev 0.234 the snapshot is `fyniq-close-snapshot@2`: canonical JSON (RFC 8785) holding:
- the trial balance;
- every control account's subledger totals, by source type and currency;
- the audit chain's head (`chain.seq`, `chain.hash`).

All three are read under the ledger lock at the close's ledger revision. Its SHA-256 is stored with it.

Every hour the anchoring job signs each close and reopening not yet anchored, and writes it once to
`audit-close-anchors/<organisation>/<close|reopen>-<record id>.json`:

```json
{"anchor": {"schema": "fyniq-close-anchor@1", "organisation_id": "…", "kind": "close", "record_id": "…",
            "period_id": "…", "ledger_revision": 42, "snapshot_sha256": "<hex>", "chain_seq": 118,
            "chain_hash": "<hex>", "anchored_at": "<RFC 3339>", "key_id": "…"},
 "signature": "<base64>"}
```

The signature covers the compact JSON of `anchor`, with the chain anchors' key. Verification checks each close
anchor's signature, that the stored snapshot still hashes to `snapshot_sha256` at its revision, and that the chain's
event `chain_seq` still has `chain_hash`.

## Purge boundaries

An organisation may switch on the purge after retention (`retention.purge_after_retention`, off by default, dual
control; open question 38). The purge then removes the trail's leading run of events whose records' retention has
passed: the end of each event's financial year plus `retention.records_years`, and never less than 7 years by the
database's clock.

Before any event goes, a **boundary** is signed and written once to `audit-boundaries/<organisation>/<seq, 20
digits>.json`, and recorded in `fyniq.audit_boundary`:

```json
{"boundary": {"schema": "fyniq-audit-boundary@1", "organisation_id": "…", "last_seq": 8, "last_hash": "<hex>",
              "last_occurred_at": "<RFC 3339>", "approval": "retention.purge_after_retention v1",
              "counts": {"events": 8, "security_events": 1}, "written_at": "<RFC 3339>", "key_id": "…"},
 "signature": "<base64>"}
```

To verify a purged trail, start from the latest boundary rather than from 32 zero bytes. The first remaining event's
`prev_hash` must equal the boundary's `last_hash`, and its `seq` must be `last_seq + 1`. Anchors at or behind
`last_seq` name purged events and are not checked. A check of a range that reaches back into the purged days is
**explained** by the boundary: never clean, never failed.
