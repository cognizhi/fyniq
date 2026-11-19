---
key: nemura.letters.templates
title: Set up letter types and templates
mode: expert
roles: [hr_officer, hr_admin, owner]
requirements: [NM-LTR-01, NM-LTR-02]
---

# Set up letter types and templates

## What it is

**Letter templates** is where the company sets its letterhead, chooses which letters it issues and writes each letter's template. A
template is blocks of text with merge fields from the record, such as the person's name or start date. There is no free code. A
template is a draft until the HR administrator approves it, and then it is a numbered version that never changes.

## Before you start

- The HR administrator or the Owner sets the letterhead and the letter types, and approves templates. The HR officer writes drafts.
- Letters that show salary, and the key employment terms, wait until Nemura holds the person's pay and terms.

## Steps

1. Under **Letterhead**, enter the company name, UEN and address, then select **Save the letterhead**.
2. Under **Letter types**, tick **Issued** for each letter the company issues, set the working days it takes, and select **Save**.
3. Choose a letter type under **Choose a letter type**. The page lists the merge fields that type may use.
4. Write the blocks: a letterhead, a heading, paragraphs and a signature block, using fields written like {{person.legal_name}}.
   Select **Save the draft**. Nemura checks every block and field and reports every problem at once.
5. The HR administrator or the Owner, not the author, selects **Approve and publish the draft**. It becomes the next version, and
   the version before is superseded.

## Common problems

- **A field is not one the type may show.** Remove it, or choose a type that may show it.
- **A template needs a signature block.** Add one at the end.
- **You cannot approve your own draft.** Another HR administrator or the Owner approves it.
- **A published version cannot be edited.** Write a new draft and publish it as the next version.

## Related

- [Check, sign and issue letters](letters-issue.md)
- [Ask for a letter](letters-request.md)
