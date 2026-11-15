---
key: settings.data
title: See where your data lives, and take a full export
mode: expert
roles: [owner, administrator, dpo]
requirements: [FR-MIG-04, AUD-05]
---

# See where your data lives, and take a full export

## What it is

**Settings → Data and export** shows where the organisation's data lives and how long it is kept, and gives a full
copy of everything it keeps in Fyniq. It is in Expert mode, for the Owner, the Administrator and the data protection
officer.

## Where your data lives

- **Data region**: every record is kept and processed in Google Cloud's Singapore region. The platform sets it, so
  it is shown as fixed.
- **Records kept for**, the purge after retention and the other retention settings are shown as they stand. Change
  them in **Settings → Organisation**, under **Data region and retention**
  ([Organisation settings](organisation-settings.md)); a retention can be lengthened, never shortened.

## What a full export holds

One ZIP file with everything the organisation keeps in Fyniq:

- each table of its records as a CSV file, amounts as plain decimals and dates as YYYY-MM-DD;
- every version of every document (invoices, bills, journals and the others);
- the evidence and attachments, and the payment files released to the bank, as their own files;
- the audit trail, one event per line, with the hashes that let anyone check nothing was changed;
- a manifest with each file's SHA-256, and a README that says how to check them.

It holds the vendors' bank account numbers in clear. Keep the file encrypted and share it only with whoever must
have it. Secrets, keys and sign-in tokens are never in it; the manifest lists what was left out and why.

## Ask for a full export

An Administrator selects **Ask for a full export** and confirms it's them. Nothing is prepared yet: an Owner other
than the person who asked approves it in **Approvals** ([Approve or reject a request](approvals.md)). Once
approved, it is prepared in the background and the person who asked is told when it is ready. Each export is listed
here: awaiting approval, being prepared, ready to save, failed with why, or expired.

## Save it

Only the person who asked can save the export, for 24 hours after it is ready; after that the file is deleted and
the export can be asked for again. **Save** makes a link that works for 5 minutes and saves the ZIP. Each link is
recorded in the audit trail with who made it. Check the file against its SHA-256, shown with the export in Expert
mode.
