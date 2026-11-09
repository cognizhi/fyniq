---
key: incident-response
title: Incident response policy
draft: fyniq-policy-draft@1
sources:
  - title: PDPC, Guide on Managing and Notifying Data Breaches under the PDPA
    url: https://www.pdpc.gov.sg/-/media/files/pdpc/pdf-files/other-guides/guide-on-managing-and-notifying-data-breaches-under-the-pdpa-15-mar-2021.pdf
    retrieved: 2026-11-09
  - title: PDPC, Report your organisation's data breach
    url: https://www.pdpc.gov.sg/organisations/e-services/report-your-organisations-data-breach
    retrieved: 2026-11-09
  - title: CSA, Cyber Essentials
    url: https://www.csa.gov.sg/our-programmes/support-for-enterprises/sg-cyber-safe-programme/cybersecurity-certification-for-organisations/cyber-essentials/
    retrieved: 2026-11-09
controls: [CTL-DP-06, CTL-AUD-02, CTL-AUD-05]
---

# Incident response policy

## Purpose

This policy says what we do when something goes wrong: a lost device, a compromised account, a mistake that exposes
data, or an attack on our systems.

## Scope

Every security incident and every suspected breach of personal data, whoever notices it.

## What we do

The PDPC's guide sets out four steps, contain, assess, report and evaluate, and we follow them:

1. **Contain.** Whoever notices tells the Owner and the Compliance officer at once. We stop the harm: sign the account
   out, revoke its access, pause the integration, change the secret.
2. **Assess.** We work out what happened, what data was involved and whose, and whether the breach is notifiable.
   [counsel to confirm] We finish this assessment within 30 calendar days of having credible grounds to believe a
   breach happened. A breach is notifiable when it is likely to result in significant harm to the people affected, or
   affects 500 or more people.
3. **Report.** [counsel to confirm] We notify the PDPC within 3 calendar days of deciding that a breach is notifiable,
   and the people affected as soon as practicable, at the same time as or after the PDPC unless an exception applies.
4. **Evaluate.** Afterwards we write down what happened and what we changed so it does not happen again.

We keep the evidence: we never change the audit trail or the logs of an incident.

## What Fyniq enforces

- The audit trail is hash-chained and anchored, so a change to it shows (CTL-AUD-02), and every export and reveal of
  sensitive data is recorded (CTL-AUD-05).
- The breach timers and the twice-yearly exercise of this policy are tracked (CTL-DP-06).

## Roles

The Compliance officer leads the assessment and the notifications; the Owner decides and informs the company. We
exercise this policy twice a year, and review it after every incident.
