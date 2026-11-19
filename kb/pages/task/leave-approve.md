---
key: leave.approve
type: task
title: Approve or refuse leave in Nemura
summary: Who sees and decides leave requests in Nemura, how to approve or refuse with a reason, and why nobody decides their own
sources:
  - docs/user-guide/nemura/leave-approve.md sha256:19053276bd71b744e50157ed049d7e6f6096ff638118f8df5f62c5397495771e
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

## When it looks wrong

- A request missing from the list belongs to someone who does not report to the viewer.
- A decision is made once; to change it the person cancels the request or the people team corrects it.
- Another approver decides one's own request.

(src: docs/user-guide/nemura/leave-approve.md#common-problems)

## Related

- How a person asks: [[leave.apply]]; the balance: [[nemura.my-leave]]. (src: docs/user-guide/nemura/leave-approve.md#related)
