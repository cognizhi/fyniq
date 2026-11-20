# Proposal

## Why

Nemura's first phase (P1) runs the company's people records, leave, payroll, self-service, letters and the link to Fyniq. P2 adds the parts of
people work that come before and after a payslip: hiring, performance, onboarding, letter verification, grievances, and the rest of mixed use
with an external payroll. The plan's row for this change (`docs/prd/plans/p1-p3-build-plan-2026-10-16.md`) names 20 requirements, three screens
(`nm-hiring`, `nm-goals`, `nm-verify-letter`), no outside account and no sandbox: everything here can be built and tested locally.

## What Changes

- **Letter verification** (NM-LTR-05): a public page and a read-only operation where a recipient enters an issued letter's reference and the code
  printed on it and sees only that it is genuine, its date and its type (never the facts); the employee can revoke it.
- **Grievances** (NM-SEC-06): a confidential channel with an inquiry record, a written outcome and restricted access; one about the default
  handler goes to another authorised person, and the person complained about cannot see it.
- **Hiring** (NM-REC-01 to 06, NM-AI-05, `people.draft_job_post`, `people.draft_interview_kit`): requisitions, job posts with fair-wording checks,
  a pipeline with consent and retention (N.1 #11), offers from a template, and the hire into a person record and an onboarding plan.
- **Onboarding plans** (NM-ONB-01), **goals and KPIs** (NM-PRF-01, 02), **check-ins** (NM-PRF-03), **review cycles** (NM-PRF-04, `people.draft_review_summary`)
  and **pay outcomes** (NM-PRF-05, through the existing Payroll-administrator-prepares, Owner-approves path).
- **Mixed use and external pay documents** (NM-EXT-04, 05): pay documents imported from an external payroll kept with their source and as-of date.

## Capabilities

### Modified Capabilities
- `people/recruitment`, `people/performance`, `people/onboarding`, `people/letters`, `people/security`, `people/external-systems`, `people/assistant`

## Impact

Nemura migrations after the highest on `dev/integration`; no Fyniq migration; no posting change. The assistant gains three draft task types
and never screens, ranks or scores a person (NM-AI-05), each tested by the red-team suite. New Appendix B rows (Hiring and Performance exist as P2
rows already). Candidate data is personal data of people who are not employees: the data inventory, retention (6 months by default) and the
erasure of NM-SEC-08 apply to it from the first migration.
