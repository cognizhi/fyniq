# Security engineering (SEC-20)

Fyniq's secure development lifecycle follows **OWASP ASVS 5.0**: Level 2 as the baseline, Level 3 for
business logic, authentication, authorisation and logging on money and approval flows (PRD-001 §13.4,
SEC-20).

## Tag every security test with the ASVS requirements it verifies

A security test is a test whose `// req:` names a security requirement: `SEC-*` (except SEC-20 itself),
`QA-SEC-*`, `QA-MCP-*`, `CTL-IAM-*`, or the invariant attack suites (`QA-ENG-05`). It also carries an
`// asvs:` line with one or more ASVS 5.0 requirement IDs:

```go
// req: SEC-02
// req: QA-ENG-05
// asvs: V8.4.1
func TestAttackCrossTenantReadsAndWritesFail(t *testing.T) {
```

`internal/archtest` (`TestSecurityTestsCarryASVSIDs`) fails on a security test without an ID, and on an
ID that ASVS 5.0 does not have. The valid IDs are in [`asvs-5.0.tsv`](asvs-5.0.tsv): the requirement
index of ASVS 5.0.0 (tag `v5.0.0_release`), with each requirement's level, chapter and section. Pick the
most specific requirement the test proves. The ones in use so far:

| ID | Section | Tests |
|---|---|---|
| V2.2.1, V2.2.3 | Input validation | amounts off scale or NaN; accounts that cannot take postings |
| V2.3.1–V2.3.3 | Business logic security | document statuses that cannot rewind; closed periods; entries that commit whole or not at all |
| V2.3.5 | Business logic security: multi-user approval | a connector's step needs a second person: the other Super admin's change, signed over its envelope, or an Owner's approval; a bank rule applies by itself, and a bank reconciliation is signed off, only by another person |
| V1.5.1 | Safe deserialization | statement files: camt.053's XML read strictly, with no DTD or external entity, its size bounded before it is read (nesting by Go's decoder); every reader fuzzed |
| V4.1.5 | Generic web service security: per-message signatures | bank providers' webhooks verified over their raw body before anything is read (Wise: RSA with its published key; Airwallex: HMAC over the timestamp and body, within five minutes), and only queue a pull [verify the requirement's wording against ASVS 5.0.0's text] |
| V1.3.6 | Sanitization: server-side request forgery | the egress guard refuses metadata, loopback, private and internal addresses, re-checks the address it dials, and follows no redirect |
| V5.2.1–V5.2.3, V5.3.2, V5.4.3 | File handling | evidence uploads: size, type from content, archive limits and macros, file names as data, the malware scan |
| V6.3.3 | General authentication security | every session passed a second factor, or only reads itself and steps up |
| V6.5.1, V6.7.2 | Multi-factor and cryptographic authentication | passkey challenges: random, bound to the session, used once; answers checked and counters kept |
| V8.1.1 | Authorisation documentation | the policy table equals Appendix D |
| V8.2.2 | Object-level authorisation | a record's evidence needs the right to view the record too |
| V11.2.2, V11.3.2, V11.3.3 | Cryptography: agility, approved modes, authenticated encryption | sealed fields: a fresh AES-256-GCM data key per value, bound to its record, wrapped by a key that names itself in a versioned layout; any change to a sealed value is caught |
| V14.2.4 | Sensitive data protection | vendors' account numbers sealed, never in a response, log or audit event but by their last four characters, revealed after step-up and audited, masked for a masked view |
| V14.2.7 | Data retention | retained evidence cannot be deleted or replaced before its retain-until |
| V16.2.1 | Logging content | evidence events carry the hashes, uploader, source and parent |
| V8.2.1, V8.2.3, V8.3.1 | Authorisation design and operation level | the policy and the matrix; masked fields |
| V8.4.1 | Multi-tenant | every cross-tenant attack, RLS on every table |
| V13.2.2, V13.3.2 | Least privilege for backend accounts and secrets | the database roles' grants; workflow tokens |
| V13.2.4 | Backend communication allow list | a connector reaches only the destinations its consented manifest names |
| V13.3.1, V13.3.4 | Secret management and rotation | connector keys only in Secret Manager by reference, never answered, logged or audited; a replaced key valid for its overlap, then destroyed |
| V13.4.2, V15.2.3 | No debug or extra functionality in production | a release build refuses local adapters; hardened images |
| V15.2.1, V15.2.4 | Dependencies | the CI scans; actions pinned by SHA |
| V15.3.1, V15.3.2 | Returning only the required fields; no redirects followed | masked responses; a connector test returns Fyniq's reason, never the body; outbound requests follow no redirect |
| V15.4.1, V15.4.2 | Concurrency | the race suites |
| V16.2.5, V16.3.2, V16.3.3, V16.4.2 | Security logging | masking in logs and the audit trail; refusals logged; every change audited; the hash chain |

Note: PRD-001 Appendix F maps CTL-API-01 to "ASVS V13". In ASVS 5.0, V13 is Configuration and API and
web service requirements are V4, so that cell probably uses ASVS 4.0's numbering [verify with the PRD
owner].

## Cryptographic inventory

| What | Algorithm | Key | Where |
|---|---|---|---|
| Sealed fields (SEC-11): vendor bank account numbers | AES-256-GCM, a fresh data key per value, bound to its record | Data keys wrapped by the Cloud KMS key `fields` (rotated every 90 days); locally `.data/keys/fields.key` | `internal/fieldseal`, `gcp.KMSFields` |
| Fingerprints of sealed fields (FR-CT-04, AUD-02) | HMAC-SHA256 within the organisation | The Cloud KMS MAC key `fingerprints`, version pinned; locally `.data/keys/fields.key` | `internal/fieldseal`, `gcp.KMSFields` |
| Audit anchors (CTL-AUD-02) | ECDSA P-256 (KMS `EC_SIGN_P256_SHA256`); locally Ed25519 | The anchors' KMS key; locally `.data/keys/audit-anchor.ed25519` | `internal/audit`, `gcp.KMS` |
| Approval envelopes (FR-APR-03) | SHA-256 of RFC 8785 canonical JSON | None: a hash the signature covers | `internal/domain/approval` |

## Threat models

Every feature that moves money or handles personal data gets a threat model before it is built, from
[`threat-model-template.md`](threat-model-template.md), stored as `docs/security/threat-models/<feature>.md`
and reviewed with the feature (§16.3 definition of done, item 3).
