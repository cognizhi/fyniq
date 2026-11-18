# UAT: the DPO's walkthrough

- **Role:** DPO
- **Person:** Dee Protection, holding only this role
- **Proves:** QA-UI-08 and FR-UX-40: §7.1's row "What each role sees" for the role
- **Organisation:** the role scripts' organisation (README, step 5)

## Steps

| # | Who | Screen | Do | What to see | Evidence to keep |
|---|---|---|---|---|---|
| 1 | Dee | `compliance-center` | Sign in as Dee Protection (README, step 4). | Dee lands on the Compliance Center. | Screenshot of the landing page |
| 2 | Dee | `app-shell` | Read the navigation in Basic, then in Expert. | Only Compliance (the Compliance Center, the audit trail, data requests) and Settings (to view). Not shown: transactions; payroll masked. | Screenshot of the navigation in each mode |
| 3 | Dee | `compliance-center` | Read Home. | Data requests due, breaches and retention holds. | Screenshot of Home |
| 4 | Dee | `inbox` | Open the Inbox. | Data requests and breaches waiting. | Screenshot of the Inbox |
| 5 | Dee | `data-protection` | Open Compliance › Data protection. | The data inventory, DPIAs, requests and breaches. | Screenshot of Data protection |
| 6 | Dee | `not-part-of-access` | Type `http://localhost:8080/sales/invoices` (Sales) in the address bar. Then, in a new tab of the same window, open `http://localhost:8080/api/v1/invoices`. | The "not part of your access" page: no name or figure from Sales, who to ask, and a way back. The API refuses the same read: 403, with a problem coded `API-AUTHZ-01`, and no data. | Screenshot of the page and of the API's answer |

## Sign-off

| Who | Name | Date | Result |
|---|---|---|---|
| Founder | | | |
| Founder | | | |
| Accountant | | | |
