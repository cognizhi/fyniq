# Proposal

## Why

The roadmap puts the control registry and its CI checks in P0 (§18, Compliance), so that every control
that can be checked automatically produces evidence from the first merge, not after an audit asks for
it (PRD-001 Appendix F).

## What Changes

- `compliance/controls/`: one file per control from Appendix F (F.1–F.8), with its mapping keys, owner,
  frequency, check and evidence location, validated by a schema.
- A runner that executes the automated checks at their frequency (CI on every change; nightly and daily
  through the Scheduler tick) and stores each result as evidence.
- The CI-frequency checks P0 can already run: protected branches and two-person review (CTL-CHG-01, once
  the founder switches branch protection on), security scans (CTL-CHG-03), signed images
  (CTL-CHG-04), hardened images within budget (CTL-CHG-08), no local adapters in staging or production
  (CTL-CHG-09), versions only from release-please (CTL-CHG-10), pinned actions (CTL-CHG-11) and tenant
  isolation (CTL-IAM-05).
- The secure development lifecycle scaffold: OWASP ASVS 5.0 IDs on security tests and a threat-model
  template per feature with money movement or personal data (SEC-20).
- A check that logs carry no secrets, tokens, full NRIC or bank numbers (SEC-15), and that local adapters
  never reach staging or production (SEC-16).

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None (`skip_specs: true`). It builds the P0 parts of `compliance/technology-controls` and
`compliance/financial-controls` (Appendix F), SEC-20 (`security/application-security`), and SEC-15 and
SEC-16 checks (`security/data-protection`).

## Impact

- `compliance/` (registry, schema, runner), `make check` (registry validation), CI and the tick (runner).
- No screens: the Compliance Center that shows the evidence is P1.
