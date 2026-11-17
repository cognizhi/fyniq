# UAT: the AP specialist's walkthrough

- **Role:** AP specialist
- **Person:** Pat Payables, holding only this role
- **Proves:** QA-UI-08 and FR-UX-40: §7.1's row "What each role sees" for the role
- **Organisation:** the role scripts' organisation (README, step 5)

## Steps

| # | Who | Screen | Do | What to see | Evidence to keep |
|---|---|---|---|---|---|
| 1 | Pat | `home` | Sign in as Pat Payables (README, step 4). | Pat lands on Home. | Screenshot of the landing page |
| 2 | Pat | `app-shell` | Read the navigation in Basic, then in Expert. | Only Home, Inbox, Purchases (bills, purchase orders, payment batches, claims), Contacts (vendors), Banking (view) and Reports (purchases). Not shown: Sales, Payroll, Accounting, Planning, Equity and Compliance. Settings holds only their own profile, notifications and appearance. | Screenshot of the navigation in each mode |
| 3 | Pat | `home` | Read Home. | Bills due, batches to prepare, claims to pay and bank-detail changes waiting. | Screenshot of Home |
| 4 | Pat | `inbox` | Open the Inbox. | Only requests about purchases, claims and vendors. | Screenshot of the Inbox |
| 5 | Pat | `bank-details-change` | Open a vendor and start a change of their bank details. | The change waits for a second approver: Pat alone cannot make it take effect (FM-1). | Screenshot of the waiting change |
| 6 | Pat | `not-part-of-access` | Type `http://localhost:8080/sales/invoices` (Sales) in the address bar. Then, in a new tab of the same window, open `http://localhost:8080/api/v1/invoices`. | The "not part of your access" page: no name or figure from Sales, who to ask, and a way back. The API refuses the same read: 403, with a problem coded `API-AUTHZ-01`, and no data. | Screenshot of the page and of the API's answer |

## Sign-off

| Who | Name | Date | Result |
|---|---|---|---|
| Founder | | | |
| Founder | | | |
| Accountant | | | |
