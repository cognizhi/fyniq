---
key: nemura.letters.templates
title: Set up letter types and templates
mode: expert
roles: [hr_officer, hr_admin, owner]
requirements: [NM-LTR-04, NM-LTR-01, NM-LTR-02]
---

# Set up letter types and templates

## What it is

**Letter templates** is where the company sets its letterhead, chooses which letters it issues and writes each letter's template. A
template is blocks of text with merge fields from the record, such as the person's name or start date. There is no free code. A
template is a draft until the HR administrator approves it, and then it is a numbered version that never changes.

## Before you start

- The HR administrator or the Owner sets the letterhead and the letter types, and approves templates. The HR officer writes drafts.
- Letters that show salary wait until Nemura holds the person's pay. The key employment terms use their own standard text and the terms recorded on the person: see [Record a person's key employment terms and issue their KETs](letters-kets.md).

## Steps

1. Under **Letterhead**, enter the company name, UEN and address, then select **Save the letterhead**.
2. Under **Letter types**, tick **Issued** for each letter the company issues, set the working days it takes, and select **Save**.
3. Choose a letter type under **Choose a letter type**. The page lists the merge fields that type may use.
4. Write the blocks: a letterhead, a heading, paragraphs and a signature block, using fields written like {{person.legal_name}}.
   Select **Save the draft**. Nemura checks every block and field and reports every problem at once.
5. The HR administrator or the Owner, not the author, selects **Approve and publish the draft**. It becomes the next version, and
   the version before is superseded.

## Issue a standard letter without waiting

The Owner can let a standard letter, such as an employment confirmation, be issued at once. Under **Automatic issue**, choose the letter type, the most letters a month and
the day the delegation ends, within twelve months, and select **Confirm with my passkey and delegate**. From then on a request for that letter by an employee who is not
serving notice is checked and signed in the Owner's name as soon as it is made, and the letter shows under the person's requests. Select **Revoke automatic issue** to end it.

- A letter that shows pay is never issued this way: the salary certificate, the pay-change letter and the key employment terms still go through the people team and the signatory.
- A change to the template ends the delegation: publish the new version, then delegate again.
- The Owner's own letter, a request from someone serving notice, and a request over the monthly limit wait for the people team as usual.
- The HR administrator sees the delegations and how many letters each has issued this month, but cannot make or revoke one.

## Common problems

- **A field is not one the type may show.** Remove it, or choose a type that may show it.
- **A template needs a signature block.** Add one at the end.
- **You cannot approve your own draft.** Another HR administrator or the Owner approves it.
- **A published version cannot be edited.** Write a new draft and publish it as the next version.

## Related

- [Check, sign and issue letters](letters-issue.md)
- [Ask for a letter](letters-request.md)
