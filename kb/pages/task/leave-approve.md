---
key: leave.approve
type: task
title: Approve or refuse leave in Nemura
summary: Who sees and decides leave requests in Nemura, how to approve or refuse with a reason, and why nobody decides their own
sources:
  - docs/user-guide/nemura/leave-approve.md sha256:cc91c8e2fcfdd8db239474fa7b5092e46cd30d8a5559738a4c7ea19320aa8dbb
data_class: general
taint: trusted
status: published
human_edited: false
compiled_by: claude-sonnet-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@2
compiled_at: 2026-11-20
aliases:
  - approve leave
  - refuse leave
  - team leave
  - leave approval
---

# Approve or refuse leave in Nemura

## Who sees what

- **Team leave** lists the requests waiting for a decision and those decided lately.
- A line manager sees and decides on their reports' requests; the HR officer, the HR administrator and the Owner on anyone's; the Payroll administrator can read them. Nobody decides their own request.

(src: docs/user-guide/nemura/leave-approve.md#what-it-is)

## Steps

1. Open **Team leave**.
2. Under **Waiting for a decision**, read the person, dates, days and note.
3. Select **Approve**, or write the reason in the box and select **Refuse**; a refusal needs a reason.
4. The decision is audited, the person is told, and a refusal gives the reserved days back.

(src: docs/user-guide/nemura/leave-approve.md#steps)

## The month at a glance

- A row for each person in view and a column for each day: **A** away, **L** leave, **P** a request waiting, **S** sick leave, **H** a public holiday as that person observes it, and a dot for a rest day.
- A manager sees the type for their reports, the people team for everyone, and the others in the team show **Away** only; sick leave is shown as such only to the person and the HR officer and the HR administrator.

(src: docs/user-guide/nemura/leave-approve.md#the-month-at-a-glance)

## Verify medical certificates

- The HR officer and the HR administrator see **Certificates to verify** and select **Verify** once a certificate is checked: once, and never for their own. A manager cannot refuse a certified sick day and does not see the certificate. See [[leave.report-sick]].

(src: docs/user-guide/nemura/leave-approve.md#verify-medical-certificates)

## Answer flexible work requests

- The HR officer, the HR administrator and the Owner see each flexible work request waiting for its written reply, soonest deadline first: choose accepted, refused or an alternative, write the reply and select **Send reply**. A refusal or an alternative gives the business grounds; the reply is kept as the record, and nobody answers their own request. See [[nemura.requests]].

(src: docs/user-guide/nemura/leave-approve.md#answer-flexible-work-requests)

## Hours worked, sick days beyond the limit and the pay run

- **Hours waiting for approval**: **Approve**, or write a reason and **Refuse**; nobody approves their own hours; a day flagged beyond the monthly limit stays recorded and is paid, so HR applies for an overtime exemption or changes the working pattern.
- **Sick days beyond the paid limit** (HR officer, HR administrator): choose **Unpaid leave** or **Take from annual leave**, once.
- **What the next pay run takes from leave and time** lists unpaid leave, encashed annual leave and approved hours not yet used, each linked to its source.
- **Month-end leave accrual**: choose a month, **Draft the month** and **Approve the month** (not by the drafter); Nemura also drafts the previous month by itself while Leave is on; amounts and the journal come from payroll and the Fyniq link.

(src: docs/user-guide/nemura/leave-approve.md#hours-worked-sick-days-beyond-the-limit-and-the-pay-run)

## When it looks wrong

- A request missing from the list belongs to someone who does not report to the viewer.
- A decision is made once; to change it the person cancels the request or the people team corrects it.
- Another approver decides one's own request.

(src: docs/user-guide/nemura/leave-approve.md#common-problems)

## Related

- How a person asks: [[leave.apply]]; the balance: [[nemura.my-leave]]. (src: docs/user-guide/nemura/leave-approve.md#related)
