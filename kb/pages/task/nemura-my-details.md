---
key: nemura.my-details
type: task
title: Change your own details in Nemura
summary: What a person changes at once, what needs evidence and approval, and what an NRIC or FIN change holds
sources:
  - docs/user-guide/nemura/my-details.md sha256:36320d485618d260098abcf778b6d6fc628dcddf4465311da088733ea7c63b67
data_class: personal
taint: trusted
status: published
human_edited: false
compiled_by: claude-sonnet-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@2
compiled_at: 2026-11-20
aliases:
  - change my address
  - emergency contact
  - change my name
  - update my phone
  - legal name change
  - NRIC change
---

# Change your own details in Nemura

## What it is

- **My details** keeps a person's phone, personal email, home address and up to three emergency contacts; these save at once.
- A change of **legal name**, **residency** or **NRIC or FIN** needs evidence and another person on the people team to approve it. The bank account has its own flow with a second approver and a passkey. (src: docs/user-guide/nemura/my-details.md#what-it-is)

## Steps

1. Change the phone, personal email, address or postal code and select **Save my details**.
2. Add up to three emergency contacts (name, relationship, phone) and select **Save my emergency contacts**.
3. To change a legal name, residency or NRIC or FIN, choose what changes, give the new name or residency, say what changed and why, and attach the document that shows it; a permanent resident gives the day permanent residence began.
4. Select **Send the request**; Nemura shows the day the people team decides it by.

(src: docs/user-guide/nemura/my-details.md#steps)

## What happens next

- The request shows under **Where my requests are** in [[nemura.requests]]. A member of the people team who is not the person reads the evidence and approves or refuses; a refusal comes with a reason, and a refused request can be sent again.
- Approving a legal name or residency changes the record from the day of the decision. For an NRIC or FIN, approving records that the evidence was checked, and the people team then records the number in the restricted fields; see [[nemura.requests-queue]].
- The audit trail records which details changed, never what they were. (src: docs/user-guide/nemura/my-details.md#what-happens-next)

## When it looks wrong

- Never type an NRIC or FIN number in the note: the request holds no number.
- The evidence is refused as too large: attach a file of up to 5 MB.
- Changed your mind: withdraw the request while nobody has decided it. (src: docs/user-guide/nemura/my-details.md#common-problems)
