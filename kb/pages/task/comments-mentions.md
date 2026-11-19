---
key: records.comments
type: task
title: Comment on a record and mention a colleague
summary: Leave a note on an invoice, bill, journal, claim, quote, order or contact, and mention a colleague who may see it
sources:
  - docs/user-guide/comments-and-mentions.md sha256:2c5d35ec6f0facd77877d4333cc2c1f15aedb2feae0677e62bc7afe24d4303c5
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@2
compiled_at: 2026-11-20
aliases:
  - comments
  - mention a colleague
  - at mention
  - leave a note on a bill
  - tag someone
---

# Comment on a record and mention a colleague

## What a comment is

- A note left on a record: an invoice, a credit note, a bill, a vendor credit, a journal, an expense claim, a quote, a sales order, a purchase order or a contact.
- Colleagues who may see the record read it there. Type **@** and the start of a name to mention a colleague, and Fyniq tells them.

(src: docs/user-guide/comments-and-mentions.md#what-it-is)

## Who sees it and who can be mentioned

- You read and write comments on a record only if you may see the record; a record you may not see is not found, as if it did not exist.
- The names offered after **@** are the members who may see this record, and only them: a colleague who may not open it is never offered and cannot be mentioned.
- An Approver who acts for some departments sees the records of those departments, so they are offered and told for those only ([[members.roles]]).

(src: docs/user-guide/comments-and-mentions.md#who-sees-a-comment-and-who-you-can-mention)

## Add a comment

1. Open the record and find **Comments**: on a contact it is the **Activity** tab; on a bill, an invoice or a journal, select **Show comments**.
2. Write in **Add a comment**, up to 2,000 characters.
3. To mention someone, type **@** and the start of a name, choose with the arrow keys and **Enter**, and go on writing.
4. Select **Add comment**: it is kept on the record and each person mentioned is told. You are not told of your own.

(src: docs/user-guide/comments-and-mentions.md#steps)

## Good to know

- A comment is never changed or removed: answer a mistake with another comment. The record's history keeps who wrote what and when.
- Only the people you mention are told, and only if they may see the record.
- A mention that cannot be made is refused with the same message as a name that is no member's.
- Mentions are notifications of the type **Someone mentioned you in a comment**, in the daily digest by email unless you choose otherwise under **Notifications**.

(src: docs/user-guide/comments-and-mentions.md#good-to-know)
