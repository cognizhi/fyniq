# UAT: the Auditor's walkthrough

- **Role:** Auditor
- **Person:** Aud Auditor, holding only this role
- **Proves:** QA-UI-08 and FR-UX-40: §7.1's row "What each role sees" for the role
- **Organisation:** the role scripts' organisation (README, step 5)

The Auditor's access is time-boxed (§7.1): set its end date, the engagement's, on Aud's role in Settings › Members and
roles `[verify]`.

## Steps

| # | Who | Screen | Do | What to see | Evidence to keep |
|---|---|---|---|---|---|
| 1 | Aud | `auditor-workspace` | Sign in as Aud Auditor (README, step 4). | Aud lands on the Auditor workspace, read-only, with the access's scope and expiry. | Screenshot of the landing page |
| 2 | Aud | `app-shell` | Read the navigation in Basic, then in Expert. | Only Reports, Accounting, evidence and the audit trail. Not shown: any change; payroll personal data unless granted. | Screenshot of the navigation in each mode |
| 3 | Aud | `auditor-workspace` | Read Home. | The engagement: requests, samples and the access's expiry. | Screenshot of Home |
| 4 | Aud | `journals` | Open Accounting › Journals, then Contacts. | Everything in scope to read, and no New, Edit, Post, Reverse or Delete anywhere. | Screenshot of each |
| 5 | Aud | `audit-trail` | Open the audit trail. | Every change with its actor, approver and reason, and the chain's verification. | Screenshot of the audit trail |

## Sign-off

| Who | Name | Date | Result |
|---|---|---|---|
| Founder | | | |
| Founder | | | |
| Accountant | | | |
