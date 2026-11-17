# UAT: the Line manager's walkthrough

- **Role:** Line manager (relationship, §6.2)
- **Person:** Liam Manager, holding only this role
- **Proves:** QA-UI-08 and FR-UX-40: §7.1's row "What each role sees" for the role
- **Organisation:** the role scripts' organisation (README, step 5)

The line manager is a relationship, not a role: Liam holds the Employee's role, and is the line manager of Eve. The
relationship comes from the people projection (FR-PPL-06) with Nemura, or the payroll provider's system. Until one is
connected, record step 3 as "not available yet" in the result.

## Steps

| # | Who | Screen | Do | What to see | Evidence to keep |
|---|---|---|---|---|---|
| 1 | Liam | `personal-claims` | Sign in as Liam Manager (README, step 4). | Liam lands on the personal workspace, on My claims. | Screenshot of the landing page |
| 2 | Liam | `app-shell` | Read the navigation in Basic, then in Expert. | Only the Employee's items, plus To approve. Not shown: any company figure; another person's claim, except while it waits for Liam. Settings holds only their own profile, notifications and appearance. | Screenshot of the navigation in each mode |
| 3 | Liam | `personal-approvals` | Open To approve once Eve has submitted a claim. | Eve's claim, and only while it waits for Liam; once decided it leaves the list. No other person's claim. | Screenshot of the Inbox |
| 4 | Liam | `not-part-of-access` | Type `http://localhost:8080/purchases/bills` (Purchases) in the address bar. Then, in a new tab of the same window, open `http://localhost:8080/api/v1/bills`. | The "not part of your access" page: no name or figure from Purchases, who to ask, and a way back. The API refuses the same read: 403, with a problem coded `API-AUTHZ-01`, and no data. | Screenshot of the page and of the API's answer |

## Sign-off

| Who | Name | Date | Result |
|---|---|---|---|
| Founder | | | |
| Founder | | | |
| Accountant | | | |
