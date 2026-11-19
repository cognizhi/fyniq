---
key: leave.report-sick
type: task
title: Report sick leave in Nemura
summary: How an employee tells the company they are sick, the shared sick-leave ceiling, the medical certificate and who sees it, and the 48-hour rule
sources:
  - docs/user-guide/nemura/leave-sick.md sha256:bac1cf28fd815c6837d47d67e27d41eace4c6dfe7c3387df8295b66a2388280c
data_class: general
taint: trusted
status: published
human_edited: false
compiled_by: claude-sonnet-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@2
compiled_at: 2026-11-20
aliases:
  - sick leave
  - medical certificate
  - MC
  - hospitalisation leave
  - outpatient sick leave
---

# Report sick leave in Nemura

## How sick leave works

- Sick leave is not applied for: **Tell us you're sick** tells the company, and nobody approves it. The people team verifies the medical certificate; only the HR officer and the HR administrator see it, and a manager sees only that the person is **Away**.
- Outpatient and hospitalisation leave share one limit: outpatient days count towards the hospitalisation days. (src: docs/user-guide/nemura/leave-sick.md#what-it-is)

## Singapore's sick-leave days

- Under the Employment Act, paid sick leave starts after 3 months' service: 5 days outpatient and 15 hospitalisation at 3 months, 8 and 30 at 4 months, 11 and 45 at 5 months, and 14 and 60 from 6 months; the 60 include the 14. Nemura applies the limit in force on each day of absence, and a company may give more.
- It must be certified by a registered medical practitioner. The values were read from MOM's page and have not yet been checked by a second person.

(src: docs/user-guide/nemura/leave-sick.md#before-you-start)

## Steps

1. Open **Tell us you're sick** and choose **Outpatient** or **Hospitalisation**.
2. Give the **First day** and the **Last day**, and a note if wanted.
3. Select **Tell the company**: Nemura counts the working days, takes the paid days from the limit and shows how many were paid and how many were beyond it.
4. On return, give the certificate under **Give the medical certificate**: who issued it, and a photo or the e-MC file.

(src: docs/user-guide/nemura/leave-sick.md#steps)

## The 48-hour rule

- Tell the employer, or try to, within 48 hours of being away, and give the certificate on return. A later notice is accepted and marked late. (src: docs/user-guide/nemura/leave-sick.md#the-48-hour-rule)

## Common problems

- **Days shown as beyond the limit** are not paid sick leave; the people team decides how they are handled.
- **Nothing paid in the first 3 months**: paid sick leave starts after 3 months' service.
- **Leave already on those days**: the people team corrects it, or the days are chosen again.
- **A certificate is given once.**

(src: docs/user-guide/nemura/leave-sick.md#common-problems)

## Related

- Other leave: [[leave.apply]]; approving it: [[leave.approve]]. (src: docs/user-guide/nemura/leave-sick.md#related)
