---
key: nemura.letters.issue
title: Check, sign and issue letters
mode: expert
roles: [hr_officer, hr_admin, owner]
requirements: [NM-LTR-03, NM-LTR-07]
---

# Check, sign and issue letters

## What it is

**Letters** lists every request for a letter, the oldest due first. A letter is issued in two steps: an HR officer checks it,
then the signatory signs it with a passkey. Once issued it is kept as evidence that nothing can change.

## Before you start

- You need to be the HR officer, the HR administrator or the Owner. Only the signatory (the Owner) signs.
- The signatory needs a registered passkey: signing asks for a passkey confirmation each time.

## Steps

1. Open **Letters**. Under each request you see who asked, for which letter, what it is for and when it is due.
2. As the HR officer, select **Check**. Nemura fills the template, draws the PDF, keeps it and gives it a reference. You cannot check
   your own letter. To refuse instead, write a reason in the box beside the buttons and select **Refuse**.
3. As the signatory, select **Read the letter** to open the exact PDF, compare the hash shown, then select **Sign with my passkey**
   and confirm with the passkey. The letter is issued once.
4. The person finds the letter under their requests and can download it or make a link that works for seven days.

## What an issued letter proves

The exact PDF is hashed and kept with the template version, the data, the signatory, their authority and what the passkey
confirmed. Whenever the letter is read, its bytes are checked against that hash, and a changed byte is never served. A passkey
approval is the platform's proof of who approved, not a signature inside the PDF.

## Common problems

- **The hash you confirmed is not the checked letter's.** Open the letter again and confirm the hash shown.
- **The stored letter no longer matches its hash.** It is not signed or shown; the change is for the security team.
- **The passkey was not confirmed.** Nothing is signed; try again within five minutes of the confirmation.

## Related

- [Ask for a letter](letters-request.md)
- [Letter templates](letters-templates.md)
