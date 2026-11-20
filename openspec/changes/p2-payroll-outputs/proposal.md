# Proposal

## Why

A company that pays staff must pay CPF contributions by the 14th of the next month, and give IRAS each employee's employment income
for the year (IR8A, with Appendix 8A and 8B where they apply) by 1 March under the Auto-Inclusion Scheme (AIS). Nemura runs payroll when it
is on (PRD-002 NM-PAY-07), so these outputs belong to Nemura. Fyniq never produces them (FR-PAY-08 is superseded for Fyniq: Nemura
issues payslips, built in `p1-nemura-payroll`).

## What Changes

- **The CPF submission file** (FR-PAY-09, NM-PAY-13): the CPF Board's EZPay (FTP) file, from the approved run's employee-months, in
  the layout the Board publishes (effective 16 January 2025), as a written-once export with a manifest.
- **IR8A and the Auto-Inclusion Scheme** (FR-PAY-10, NM-PAY-14): the year's employment income by employee from the paid runs, as the
  AIS-API 2.0 JSON payload IRAS publishes, checked against IRAS's data-item rules before it is kept; a port and a local stub for the
  submission (CorpPass and APEX, which need IRAS onboarding: `[live-verify]`).
- **Payslips:** nothing to build: Nemura issues them (NM-PAY-04, P1). **IR21 filing** needs IRAS's IR21 API specification, which
  is behind IRAS's sign-in: **left**, `[verify]`.

## Capabilities

### Modified Capabilities
- `payroll/pay-runs` (FR-PAY-08, FR-PAY-09, FR-PAY-10), `people/payroll` (NM-PAY-07, NM-PAY-13, NM-PAY-14)

## Impact

Nemura migration after the highest on `dev/integration`; no change to Fyniq's database. No posting changes.
Outside needs: IRAS's APEX onboarding and a CorpPass account for the AIS submission; the CPF EZPay account is the employer's.
