---
key: nemura.assistant
title: Turn on the people assistant and decide its drafts
mode: expert
roles: [hr_officer, hr_admin, owner]
requirements: [NM-AI-01, NM-AI-04, NM-AI-02]
---

# Turn on the people assistant and decide its drafts

## What it is

The people assistant drafts letters and replies for the people team to edit and approve. It is off until the Owner turns it on, and it never decides anything: it never approves or refuses leave or a request, changes pay, a bank account, a role or a record, issues or signs a letter, moves a candidate, decides a hiring, a rating, a promotion or a termination, or reads another person's restricted data. Every draft is a proposal that a named person decides. A draft that nobody decides in 72 hours expires and cannot be approved.

## Before you start

- Only the Owner changes the settings, with a passkey confirmed in the last five minutes: see [Sign in to Nemura](sign-in.md).
- The assistant needs a model route. Until a model in Singapore is evidenced for your company, the only route is the scripted model of local mode.

## Steps

1. Open **Settings, The people assistant**. The assistant is off, with no route and no kind of draft turned on.
2. As the Owner, turn the assistant on, choose the route and tick each kind of draft it may make: a draft letter, or a draft reply to a flexible-work request. Select **Save with my passkey**.
3. When the assistant drafts something, it appears under **Drafts waiting for a person** with the person it was drafted for. Read it, then select **Approve the draft** or give a reason and select **Do not use it**.

## What happens next

- To ask for a draft, open a letter request or a flexible-work request and select **Draft a reply with the assistant**. The assistant writes an acknowledgement or fills a letter's fields from the record, and never states a decision. The words in the request are only quoted, never followed.
- Approving keeps the draft exactly as you saw it: only that text is used, and only after you approve. An approved draft is attached beside the request, where you select **Use the approved draft**; you still decide, sign and send. The audit trail records who asked for the draft and who decided it.
- The audit trail says that a draft was proposed and approved, with a fingerprint of the text, never the text itself.
- Own-pay explanations, which would send a person's own payslip figures to a model, stay off until the Owner and a second approver can turn them on, which arrives when Nemura is linked to Fyniq. Until then the assistant explains a payslip line from its rule and the page shows the figures.

## Common problems

- **The assistant is off.** The Owner turns it on, and each kind of draft, in the settings.
- **A draft cannot be approved.** It waited more than 72 hours: ask the assistant again.
- **I cannot decide a draft.** You are not the HR officer, the HR administrator or the Owner.

## Related

- [Request a letter](letters-request.md)
- [Issue letters](letters-issue.md)
