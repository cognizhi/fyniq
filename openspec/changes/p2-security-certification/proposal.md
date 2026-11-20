# Proposal

## Why

P1 built the controls as code, the Compliance Center, the PDPA processes and the evidence rules. The plan row for `p2-security-certification`
carries what they still lack: **retention and legal holds** (AUD-11: the purge after retention has no way to be told "not this"; the audit
trail's threat model says so, and `retention.NextYear` takes a list of held dates that nothing passes), **the annual independent penetration test
and continuous DAST** (SEC-25: DAST runs, but nothing records a penetration test, its findings or when the next is due), and **readiness for
CSA Cyber Trust and ISO/IEC 27001** (PRD §14.5 and §14.8 stage 1 and 2: the controls map to the frameworks, but nobody can see what is missing).

## What already exists (grep-verified; this change does not rebuild it)

| The prompt's item | Where it is |
|---|---|
| Controls as code, evidence, ASVS tagging | `compliance/controls/`, `compliance/registry.go`, `evidence.go`, `release-assets.json`; `make controls`; the `// asvs:` rule in `internal/archtest` |
| Compliance Center items | `/compliance/controls`, `/compliance/attestations` (CMP-01, CMP-03), risk register (CMP-06), policy library (CMP-07) |
| PDPA: DPIA, data inventory, breach timers, access requests, transfers, shredding | `p1-pdpa-retention`: `/compliance/data-protection`, `db/migrations/000302` to `000309`, CTL-DP-05 to CTL-DP-07 |
| Access reviews | the `access-review` attestation, `GET /v1/access-review` (CTL-API-02) |
| Vulnerability handling | `posture.findings` and CTL-RES-04 (Critical within 7 days, High within 30) |
| DAST | `make dast` in CI; `dast-staging.yml` nightly `[live-verify]` (CTL-CHG-07) |
| Supply chain | `make scan`, `make image-sign`, SLSA provenance, SBOM (SEC-21, SEC-22, SEC-26) |
| Stored files' legal hold | `ports.BlobStore.SetLegalHold` (local and Cloud Storage adapters); no caller yet |
| Evidence export per framework | **not here**: CMP-04 is `p2-mobile-approvals-automation` 9.2 |

## What Changes

- **Retention and legal holds** (AUD-11): a hold on a record, a contact or a financial year with a reason, placed and lifted by the DPO or an
  Owner, each audited. The audit trail's purge stops at the first event a hold covers; the ledger's purge refuses a year a hold covers; the
  database refuses both even if the job is wrong; the evidence files a hold covers keep a storage-level legal hold, including files attached
  later. The DPO's Compliance page lists the holds in force. New Appendix D row: Retention holds.
- **Security assessments** (SEC-25, new control CTL-CHG-12): a register of penetration tests and other assessments (who, independent or not,
  scope, dates, the report's SHA-256 and where it is kept) with each finding's severity, due date, fix and retest. The annual test falls due 12 months after the
  last independent one; an overdue Critical or High finding fails the control. The test itself is an outside firm's work `[live-verify]`.
  New Appendix D row: Security assessments.
- **Readiness by framework** (new CMP-08): per framework, controls by status, the gaps in plain words, and the ISO/IEC 27001:2022 Annex A clause
  numbers no control maps to. A readiness view, never a certificate.

## Capabilities

### Modified Capabilities
- `data/audit-trail` (AUD-11), `security/application-security` (SEC-25), `compliance/compliance-center` (CMP-08), `compliance/technology-controls`
  (CTL-CHG-12, CTL-AUD-07), `access/roles-and-segregation` (two Appendix D rows)

## Impact

AUD-11 adds a table and guards (a new migration), read functions the purge jobs call, a change to the two purges, one narrow grant
(`fyniq_api`: place a hold; lift it by the lift columns only), and a worker reconcile of storage holds. Every one is listed in
`docs/security/p2-grants-for-human-review.md` and is a needs-human-review change: it touches the audit trail and retention. SEC-25 adds two
tables and API operations. CMP-08 is a read over the registry. None is on the AI or MCP surface.
