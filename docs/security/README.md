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
| V5.2.1–V5.2.3, V5.3.2, V5.4.3 | File handling | evidence uploads: size, type from content, archive limits and macros, file names as data, the malware scan |
| V6.3.3 | General authentication security | every session passed a second factor, or only reads itself and steps up |
| V6.5.1, V6.7.2 | Multi-factor and cryptographic authentication | passkey challenges: random, bound to the session, used once; answers checked and counters kept |
| V8.1.1 | Authorisation documentation | the policy table equals Appendix D |
| V8.2.2 | Object-level authorisation | a record's evidence needs the right to view the record too |
| V14.2.7 | Data retention | retained evidence cannot be deleted or replaced before its retain-until |
| V16.2.1 | Logging content | evidence events carry the hashes, uploader, source and parent |
| V8.2.1, V8.2.3, V8.3.1 | Authorisation design and operation level | the policy and the matrix; masked fields |
| V8.4.1 | Multi-tenant | every cross-tenant attack, RLS on every table |
| V13.2.2, V13.3.2 | Least privilege for backend accounts and secrets | the database roles' grants; workflow tokens |
| V13.4.2, V15.2.3 | No debug or extra functionality in production | a release build refuses local adapters; hardened images |
| V15.2.1, V15.2.4 | Dependencies | the CI scans; actions pinned by SHA |
| V15.3.1 | Returning only the required fields | masked responses |
| V15.4.1, V15.4.2 | Concurrency | the race suites |
| V16.2.5, V16.3.2, V16.3.3, V16.4.2 | Security logging | masking in logs and the audit trail; refusals logged; every change audited; the hash chain |

Note: PRD-001 Appendix F maps CTL-API-01 to "ASVS V13". In ASVS 5.0, V13 is Configuration and API and
web service requirements are V4, so that cell probably uses ASVS 4.0's numbering [verify with the PRD
owner].

## Threat models

Every feature that moves money or handles personal data gets a threat model before it is built, from
[`threat-model-template.md`](threat-model-template.md), stored as `docs/security/threat-models/<feature>.md`
and reviewed with the feature (§16.3 definition of done, item 3).
