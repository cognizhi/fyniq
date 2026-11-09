---
key: compliance.reviews
title: Sign off the recurring reviews
mode: basic
roles: [owner, admin, controller, accountant, auditor, dpo]
requirements: [CMP-03, FR-CTL-06, SEC-06, FM-3, CTL-DP-05, CMP-PD-04, CMP-PD-05, CMP-PD-06, CMP-PD-08, CTL-DP-06, CTL-DP-07]
---

# Sign off the recurring reviews

## What it is

**Reviews and attestations** (from **Compliance**) lists the reviews people sign off again and again, overdue ones
first. Each shows the role that signs it off, how often it falls due, what is waiting and by when, and when it was last
signed off and by whom. In Expert mode it also shows what each sign-off keeps as evidence, and the controls whose status
reads it.

The Owner, the Administrator, the Controller, the Accountant, the Auditor and the Compliance officer see it.

## The reviews

| Review | Who signs off | When it is overdue | Where |
|---|---|---|---|
| Review log (Founder mode only) | Another founder | 14 days after the action was logged | Review log |
| Access review | An Owner | 14 days after the quarter ends | Settings › Access review |
| Accountant's review (Founder mode only) | The Accountant | 60 days after the quarter ends | Accounting › Accountant reviews |
| Close sign-off | Someone who did not prepare the close | 15 days after the month ends | Accounting › Periods and close |
| Break-glass review | Another Owner, or the Accountant from its due date | 7 days after the break-glass ends | Break-glass |
| Policy acknowledgement | Every member, for themselves | 30 days after the version is published, or after they joined | This page |
| Risk review | The Compliance officer | After the risk's review date | Compliance › Risks and policies |
| Data inventory review | The Compliance officer | 14 days after the quarter ends | Compliance › Data protection |
| Impact assessment | The Compliance officer | As soon as a processing that needs one goes on without an approved one | Compliance › Data protection |
| Access or correction request | The Compliance officer | 30 days after it was received, or the day its written extension names | Compliance › Data protection |
| Breach | The Compliance officer | 30 days after the company became aware of it, then 3 calendar days after it was decided notifiable | Compliance › Data protection |
| Breach tabletop exercise | The Compliance officer | 6 months after the last exercise | Compliance › Data protection |
| Overseas transfers | The Compliance officer | A flow abroad waits until it is assessed; a transfer is overdue after its next review day | Compliance › Data protection |

A review that waits within its time is **Due**; once its time has passed it is **Overdue**, and more than 14 days past
its due date it is also marked **Over 14 days**. **Signed off** means nothing waits. **Nothing due** means nothing has
fallen due yet, and **Not needed** means the team mode does not call for it: Standard and Enterprise modes approve before
the action, so they need neither the review log nor the accountant's quarterly review.

## Sign off a review

Select a review to open the page where it is signed off, and sign it off there; this page shows the result. Policy
acknowledgements are the exception: the policies waiting for you are listed at the top of this page, to read and accept.

The reviews your roles sign off that are due or overdue also appear in your **Inbox**, under **Reviews to sign off**.

## How it affects the controls

Some controls read a review: the access review proves the quarterly access review control, the close sign-off the
monthly close, the accountant's review the review of journals, and the review log and the break-glass reviews the
Founder-mode controls. When a review falls overdue, its control shows as overdue on **Compliance** › **Controls** and its
owner is told.
