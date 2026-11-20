---
key: nemura.verify-letter
title: Verify a letter
mode: basic
roles: [employee, line_manager, hr_officer, hr_admin, payroll_admin, administrator, owner]
requirements: [NM-LTR-05]
---

# Verify a letter

## What it is

Every letter Nemura issues carries a reference and a verification code, printed in its footer. A bank, an embassy or anyone else can enter both on the verification page, with no sign-in, to learn whether the company issued the letter and it still stands.

## Verify a letter

1. Open the verification page and enter the reference and the code exactly as printed. Capital letters, dashes and spaces do not matter.
2. Select **Verify**. You are told whether the letter is **Valid** or **Withdrawn**, what kind of letter it is, the day it was issued and the company.

The page shows nothing of the person or the letter's words. A wrong code, an unknown reference and a letter that is not issued yet all give the same answer: nothing matches. Only a few tries are allowed for one reference in an hour, and all look-ups together are limited, so a code cannot be guessed. The company's record shows each look-up of a real letter.

## Compare a document instead

If you hold the letter as a file, choose **Compare a document** and select the file. Your browser works out the file's fingerprint (its SHA-256) and sends only that: the file never leaves your device. You get the same answer as with the reference and code, and a document that is not an issued letter, or has been changed by even one byte, is not found.

## Withdraw the verification of your own letter

The person a letter is about can withdraw its verification, in **My requests**, under **My letters**, with **Withdraw verification**. Anyone who then looks it up is told it is withdrawn, and any link you shared stops showing facts. This never withdraws the letter itself, which stays as it was issued.

## Share what your letter says about you

The check above proves a letter is genuine and shows nothing of you. If a bank or an embassy needs to see what the letter says about you, you decide that, letter by letter.

1. In **My requests**, under **My letters**, find the issued letter, choose how many days the link should work (1 to 30; 7 is the default) and select **Share what it says about me**.
2. Copy the link shown. It is shown once; Nemura keeps only a fingerprint of it.
3. Send the link to the recipient. It opens the verification page and shows the letter's facts about you, such as your name, job title and start date, with the letter's kind, day and company. It never shows what the letter is for.

Revoke a link whenever you like with **Revoke** beside it in the list of your links: it stops at once. Revoking a link never withdraws the letter or its verification. A link also stops when its days run out, and shows no facts if you withdraw the letter's verification.

## Common problems

- **Nothing matches.** Check the reference and the code against the letter, exactly as printed; the code is in the footer.
- **Too many tries.** Wait a while and try again.
- **The link shows nothing.** It may have expired or been revoked, or the person withdrew the letter's verification: ask them for a new link.
