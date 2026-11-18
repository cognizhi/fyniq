# UAT: the AR specialist's walkthrough

- **Role:** AR specialist
- **Person:** Ari Receivables, holding only this role
- **Proves:** QA-UI-08 and FR-UX-40: §7.1's row "What each role sees" for the role
- **Organisation:** the role scripts' organisation (README, step 5)

## Steps

| # | Who | Screen | Do | What to see | Evidence to keep |
|---|---|---|---|---|---|
| 1 | Ari | `home` | Sign in as Ari Receivables (README, step 4). | Ari lands on Home. | Screenshot of the landing page |
| 2 | Ari | `app-shell` | Read the navigation in Basic, then in Expert. | Only Home, Inbox, Sales, Contacts (customers), Banking (view, to apply receipts), Projects (revenue and hours) and Reports (sales). Not shown: Purchases, Payroll, Accounting, Planning, Equity and Compliance. Settings holds only their own profile, notifications and appearance. | Screenshot of the navigation in each mode |
| 3 | Ari | `home` | Read Home. | Overdue invoices, receipts to apply and quotes waiting. | Screenshot of Home |
| 4 | Ari | `inbox` | Open the Inbox. | Only requests about sales: invoices, credit notes and receipts. | Screenshot of the Inbox |
| 5 | Ari | `contacts-list` | Open Contacts. | Customers only: no vendor and no bank details. | Screenshot of Contacts |
| 6 | Ari | `not-part-of-access` | Type `http://localhost:8080/purchases/bills` (Purchases) in the address bar. Then, in a new tab of the same window, open `http://localhost:8080/api/v1/bills`. | The "not part of your access" page: no name or figure from Purchases, who to ask, and a way back. The API refuses the same read: 403, with a problem coded `API-AUTHZ-01`, and no data. | Screenshot of the page and of the API's answer |

## Sign-off

| Who | Name | Date | Result |
|---|---|---|---|
| Founder | | | |
| Founder | | | |
| Accountant | | | |
