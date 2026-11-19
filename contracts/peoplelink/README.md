# The link between Fyniq and Nemura: the envelope and its fixtures

PRD-001 FR-PPL-15 and PRD-002 NM-INT-10. Every event and call between the apps is an **envelope**: a header, the link key's signature over the
header, and the payload. The rule is decided once (`openspec/changes/p1-payroll-link/design.md` D2) and implemented twice, because Nemura imports
nothing of Fyniq's code (`internal/archtest`): `internal/domain/peoplelink` and `internal/nemura/link`. Both pass these fixtures.

- **Canonical bytes** (RFC 8785's subset): UTF-8; object keys sorted by their UTF-8 bytes; no insignificant whitespace; strings, integers (within 2^53),
  booleans, null, arrays and objects only. A fraction or an exponent is refused: money and quantities are decimal strings. Strings escape only the quote,
  the backslash and control characters (`\b \t \n \f \r`, others `\u00xx` in lower case).
- **Digest**: `payload_sha256` is the lower-case hex SHA-256 of the payload's canonical bytes.
- **Signature**: Ed25519 over `"fyniq-link-v1\n"` and the canonical bytes of the header (every field but the signature, the digest included), base64url
  without padding. The receiving side verifies with the link key it was given when the link was approved, never one that comes with a message.
- **Checks, in order**: shape; source and target organisation and source system; epoch (earlier and later are both refused); signing time within 5 minutes;
  signature; payload digest. A refusal has a stable code (`PPL-01` to `PPL-06`) and never repeats the payload.

`canonical.json` holds hand-worked canonicalisation vectors (input, canonical bytes, SHA-256) and inputs that must be refused. `envelope.json` is a signed
envelope with its public key: its private key is derived at test time from the SHA-256 of a phrase, so no private key is committed. Fyniq's test owns it
(`go test ./internal/domain/peoplelink -run SignedFixture -update`); Nemura's must verify it and reproduce its signature.
