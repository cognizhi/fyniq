---
key: compliance.reviews
type: task
title: Sign off the recurring reviews
summary: Reviews and attestations - the seven recurring reviews, who signs each off, when each is overdue and where to sign it
sources:
  - docs/user-guide/compliance-reviews.md sha256:891abf3ca0c246ad12075400b870db1f1d295b51846622a2a53111a99e4f8412
data_class: general
taint: trusted
status: published
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-11-09
aliases:
  - attestations
  - recurring reviews
  - review overdue
  - reviews to sign off
  - who signs off the access review
  - over 14 days
---

# Sign off the recurring reviews

## What it is

- **Reviews and attestations** (from **Compliance**) lists the reviews signed off again and again, overdue first: the role that signs each off, how often it falls due, what waits and by when, and the last sign-off and who gave it.
- Expert mode adds what each sign-off keeps as evidence and the controls whose status reads it ([[compliance.controls]]).
- The Owner, the Administrator, the Controller, the Accountant, the Auditor and the Compliance officer see it.

(src: docs/user-guide/compliance-reviews.md#what-it-is)

## The reviews

- **Review log** (Founder mode only): another founder; overdue 14 days after the action was logged; signed in the review log ([[approvals.decide]]).
- **Access review**: an Owner; overdue 14 days after the quarter ends; signed on Settings › Access review ([[settings.access-review]]).
- **Accountant's review** (Founder mode only): the Accountant; overdue 60 days after the quarter ends; on Accounting › Accountant reviews ([[accounting.reviews]]).
- **Close sign-off**: someone who did not prepare the close; overdue 15 days after the month ends; on Accounting › Periods and close ([[accounting.periods]]).
- **Break-glass review**: another Owner, or the Accountant from its due date; overdue 7 days after the break-glass ends ([[settings.team-mode]]).
- **Policy acknowledgement**: every member for themselves; overdue 30 days after the version is published, or after they joined; on this page ([[compliance.policies]]).
- **Risk review**: the Compliance officer; overdue after the risk's review date; on Compliance › Risks and policies ([[compliance.risks]]).
- **Data inventory review**: the Compliance officer; overdue 14 days after the quarter ends; on Compliance › Data protection ([[compliance.data-protection]]).
- **Due** waits within its time; **Overdue** is past it, and more than 14 days past its due date it is also **Over 14 days**; **Signed off** means nothing waits; **Nothing due** means nothing has fallen due yet; **Not needed** means the team mode does not call for it (Standard and Enterprise modes approve before the action).

(src: docs/user-guide/compliance-reviews.md#the-reviews)

## Sign off a review

- Select a review to open the page where it is signed off; this page shows the result.
- Policy acknowledgements are the exception: the policies waiting for you are listed at the top of this page, to read and accept.
- The reviews your roles sign off that are due or overdue also appear in the **Inbox** under **Reviews to sign off**.

(src: docs/user-guide/compliance-reviews.md#sign-off-a-review)

## How it affects the controls

- The access review proves the quarterly access review control, the close sign-off the monthly close, the accountant's review the review of journals, and the review log and the break-glass reviews the Founder-mode controls.
- A review that falls overdue shows its control as overdue on **Compliance** › **Controls**, and its owner is told.

(src: docs/user-guide/compliance-reviews.md#how-it-affects-the-controls)
