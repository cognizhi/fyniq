---
key: payroll
title: Data protection impact assessment of the payroll link
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
controls: [CTL-DP-02, CTL-DP-04, CTL-DP-05]
---

# Data protection impact assessment of the payroll link

## The processing

The company runs payroll in its people system (Nemura, or another it links) and brings each approved pay run into
Fyniq to post it to the books and to pay it. Fyniq receives, for each pay run, the totals the books need and, to pay
people, each person's net pay and the bank account it goes to.

## Personal data and how it flows

- **Into Fyniq:** each employee's name, net pay and bank account, with the pay run's totals by department and the
  statutory contributions due.
- **Not into Fyniq:** NRIC or FIN numbers, addresses, salary breakdowns, leave and medical data. They stay in the
  people system. [counsel to confirm] Fyniq needs none of them to keep the books or pay.
- **Out of Fyniq:** the bank file of the payment batch, to the company's bank in Singapore.

## Necessity and proportionality

The books must record payroll costs and liabilities, and people must be paid. Fyniq takes only what that needs. Totals
post to the ledger; each person's net pay and bank account are used to pay them and kept as the record of payment.

## Risks to the people concerned

- A person's pay or bank account seen by someone who should not see it.
- A payment sent to the wrong account, or a bank account changed by a fraudster.
- Pay data kept longer than the law requires.
- [counsel to confirm] A leak of a name with pay or bank details is a notifiable breach (prescribed data).

## Measures

- Bank account numbers sealed under the deployment's key, shown masked, and proved by fingerprint in the audit trail
  (SEC-11, AUD-02).
- Pay is shown only to the Payroll administrator, the payment releaser and those granted it (Appendix D's "Pay runs,
  employee data"); everyone else sees it masked.
- A payment batch is released by someone other than whoever prepared it, and a bank change is approved by another
  (CTL-FIN-08).
- Personal values in the audit trail are shredded per person and financial year once their retention ends (§11.8).

## Residual risk and decision

[counsel to confirm] With these measures, the residual risk to employees is low. The company may proceed once counsel
has confirmed the retention approach (open question 39).

## Approval and review

The Data Protection Officer approves this assessment before payroll data is loaded, and reviews it when the processing
changes and at least once a year.
