# UAT: the Administrator's walkthrough

- **Role:** Administrator
- **Person:** Ada Admin, holding only this role
- **Proves:** QA-UI-08 and FR-UX-40: §7.1's row "What each role sees" for the role
- **Organisation:** the role scripts' organisation (README, step 5)

## Steps

| # | Who | Screen | Do | What to see | Evidence to keep |
|---|---|---|---|---|---|
| 1 | Ada | `organisation-settings` | Sign in as Ada Admin (README, step 4). | Ada lands on Settings. | Screenshot of the landing page |
| 2 | Ada | `app-shell` | Read the navigation in Basic, then in Expert. | Only Settings, Inbox (role and integration changes), Reports and Compliance (view). Not shown: posting or approving transactions (SOD-6): no Sales, Purchases, Banking or Accounting. | Screenshot of the navigation in each mode |
| 3 | Ada | `users-roles` | Read Home. | Settings: users and roles waiting, and the health of integrations and connections. | Screenshot of Home |
| 4 | Ada | `inbox` | Open the Inbox. | Only role and integration changes. | Screenshot of the Inbox |
| 5 | Ada | `not-part-of-access` | Type `http://localhost:8080/sales/invoices` (Sales) in the address bar. Then, in a new tab of the same window, open `http://localhost:8080/api/v1/invoices`. | The "not part of your access" page: no name or figure from Sales, who to ask, and a way back. The API refuses the same read: 403, with a problem coded `API-AUTHZ-01`, and no data. | Screenshot of the page and of the API's answer |

## Sign-off

| Who | Name | Date | Result |
|---|---|---|---|
| Founder | | | |
| Founder | | | |
| Accountant | | | |
