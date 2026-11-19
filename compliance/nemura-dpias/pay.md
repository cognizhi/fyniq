---
key: nemura-pay
title: Data protection impact assessment of pay processing in Nemura
draft: fyniq-dpia-draft@1
sources:
  - title: PDPC, Guide to Data Protection Impact Assessments
    url: https://www.pdpc.gov.sg/-/media/Files/PDPC/PDF-Files/Other-Guides/DPIA/Guide-to-Data-Protection-Impact-Assessments-14-Sep-2021.pdf
    retrieved: 2026-11-09
  - title: PDPC, Advisory Guidelines on the PDPA for Selected Topics (employment)
    url: https://www.pdpc.gov.sg/-/media/files/pdpc/pdf-files/advisory-guidelines/ag-on-selected-topics/advisory-guidelines-on-the-pdpa-for-selected-topics-(revised-may-2024).pdf
    retrieved: 2026-11-09
  - title: PDPC, Advisory Guidelines for NRIC Numbers
    url: https://www.pdpc.gov.sg/-/media/files/pdpc/pdf-files/advisory-guidelines/advisory-guidelines-for-nric-numbers---310818.pdf
    retrieved: 2026-11-09
  - title: MOM, Itemised pay slips
    url: https://www.mom.gov.sg/employment-practices/salary/itemised-payslips
    retrieved: 2026-11-20
  - title: MOM, Paying salary
    url: https://www.mom.gov.sg/employment-practices/salary/paying-salary
    retrieved: 2026-11-20
controls: [CTL-DP-02, CTL-DP-04, CTL-DP-05, CTL-NM-01]
---

# Data protection impact assessment of pay processing in Nemura

This is the assessment for the Data Protection Officer to review and approve before pay data is loaded into Nemura (PRD-002 NM-SEC-01; the data inventory row is `pay-records` in `compliance/nemura-data-inventory.yaml`).
It covers what Nemura itself does with pay. The part where an approved pay run goes to Fyniq to be posted and paid is assessed in `compliance/dpias/payroll.md`. It is a draft written by the builders from what the code does:
every `[counsel to confirm]` is a point for the DPO or counsel to decide, not a finding.

## The processing

The Payroll administrator records each person's pay terms (pay group, monthly basic salary, allowances, fixed deductions, the self-help group fund and a permanent resident's joint election) and the one-off items of a
month. Nemura calculates each person's month (CPF, SDL, the fund, deductions), a person other than the preparer approves it with a passkey, and the people paid receive an itemised payslip. For a leaver it prepares the
final pay and, for a foreign employee ceasing work, holds all monies due under a tax-clearance hold until IRAS gives clearance. Standalone, it produces a journal file and a bank payment file. Each person's payment has a state
(scheduled to paid, rejected, returned or unknown), recorded from Fyniq's status events or by a person with evidence.

## Personal data and how it flows

- **In Nemura's database, in Singapore:** pay terms and pay history; one-off items; the year to date of CPF wages; each run's lines with the working of each figure; overrides with their reasons; each person's payslips (the data
  and the PDF); the state of each payment (amount, date, instruction ID); tax-clearance holds with their evidence; the manifests of the files made (counts, totals and a SHA-256, never the files).
- **Sealed, shown masked:** bank account numbers, opened only for a payment file by the Payroll administrator or the Owner after a fresh passkey, each read audited. NRIC or FIN numbers are not used by pay and never appear in a payslip.
- **Out of Nemura:** the payment file, once, to the person who asked for it (it is not kept); the approved run's figures to Fyniq when linked (`p1-nemura-fyniq-link`); a payslip to the person it is for, in My pay or as a PDF.
- **To an AI model:** none. The assistant explains a payslip line from its rule; no pay figure, no bank account and no NRIC reaches a model, and own-pay explanations are off until an Owner and a second approver can turn them on
  (NM-AI-06).
- **Not kept:** the payment file, the question a person asks the assistant, and any amount or account in an audit event or a log.

## Necessity and proportionality

MOM requires an itemised payslip with twelve items, salary within 7 days of the end of the salary period and final salary by the case. CPF, SDL and the self-help funds must be calculated from the pay. Every figure of a run
carries its working and its source because the person paid and the approver must be able to check it. Nemura holds no more than that needs: a bank account is held only to pay, and only the person proposes it (with an approver and
the person confirming it). Pay records are kept for the employment and for one year after the person leaves, which is what MOM requires of employment records [counsel to confirm the longer tax and CPF periods].

## Risks to the people concerned

- A person's pay seen by someone who should not see it: a line manager, an HR officer, another employee, a model.
- A payment sent to the wrong account, or a bank account changed by a fraudster.
- A pay run approved by the person who prepared it, or changed after it was approved.
- A payslip, a payment file or an export read by the wrong person, or kept where it should not be.
- A person ceasing work whose monies are paid before tax clearance, or held when they should not be.
- Pay data kept longer than the law requires, or kept in the audit trail.
- [counsel to confirm] A leak of a name with pay or bank details is a notifiable breach.

## Measures

- Pay is for the Payroll administrator and the Owner alone; everyone else gets "no such record" (NM-SEC-02). A person reads only their own payslips; another's is as if it did not exist (tested through the API and the database).
- Row-level security with `organisation_id` on every pay table; the assistant's database role can insert a proposal and nothing else and reads none of it; the executor holds no grant on a pay table (grants golden).
- A version of a run is read-only once written, with a SHA-256 of its figures; the approver is another person (a constraint, not only the API) confirming that digest with a passkey in the last five minutes; a correction is the
  next version (NM-PAY-09, FR-PAY-07).
- A bank account is sealed under the deployment's key; a change is proposed by the person, approved by another and confirmed by the person with a passkey (NM-PPL-04); a payment file needs a fresh passkey, opens each account
  with an audited read, and keeps only a manifest.
- Payment states are written once and only by legal moves; an unknown outcome blocks a new payment; "paid" needs a bank event or a person's evidence, never a file (NM-PAY-10).
- A tax-clearance hold is written once as versions and holds every dollar due until it is released with evidence (FR-PAY-10).
- A payslip is written once with the SHA-256 of its PDF and is never served if its bytes no longer match.
- No audit event, log or outbox row holds an amount, a bank account or the text of a draft; the audit trail holds the fact and a fingerprint (SEC-15, AUD-01).
- Pay data is retained with the employment record and, when the retention ends, its personal values in the audit trail are shredded per person (PRD-002 NM-SEC-05, the retention rules).

## Residual risk and decision

[counsel to confirm] With these measures the residual risk to employees is low. The company may proceed once counsel has confirmed the retention periods (employment records, CPF and tax) and the DPO has approved this
assessment. Until a real model route is evidenced in Singapore and a second approver exists, no pay figure is sent to a model and own-pay explanations stay off.

## Approval and review

The Data Protection Officer approves this assessment before pay data is loaded into Nemura, and reviews it when the processing changes (the Fyniq link, a provider connector, a real model route) and at least once a year.
