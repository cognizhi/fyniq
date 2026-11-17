# UAT: the Bookkeeper's walkthrough

- **Role:** Bookkeeper
- **Person:** Bea Bookkeeper, holding only this role
- **Proves:** QA-UI-08 and FR-UX-40: §7.1's row "What each role sees" for the role
- **Organisation:** the role scripts' organisation (README, step 5)

## Steps

| # | Who | Screen | Do | What to see | Evidence to keep |
|---|---|---|---|---|---|
| 1 | Bea | `home` | Sign in as Bea Bookkeeper (README, step 4). | Bea lands on Home. | Screenshot of the landing page |
| 2 | Bea | `app-shell` | Read the navigation in Basic, then in Expert. | Only Home, Inbox, Sales, Purchases, Contacts, Banking, Accounting (view) and Reports. Not shown: Payroll, Projects, Planning, Equity, the Compliance Center and Settings administration. Settings holds only their own profile, notifications and appearance. | Screenshot of the navigation in each mode |
| 3 | Bea | `home` | Read Home. | Books health, bank lines to categorise, documents to finish, deadlines and cash today. | Screenshot of Home |
| 4 | Bea | `inbox` | Open the Inbox. | Her own requests waiting for others, and nothing for her to approve: the Bookkeeper prepares, others issue and post. | Screenshot of the Inbox |
| 5 | Bea | `journals` | Open Accounting › Journals. | The journals, to read: no New journal, Edit or Reverse. | Screenshot of the journals |
| 6 | Bea | `not-part-of-access` | Type `http://localhost:8080/compliance/controls` (the Compliance Center) in the address bar. Then, in a new tab of the same window, open `http://localhost:8080/api/v1/compliance/controls`. | The "not part of your access" page: no name or figure from the Compliance Center, who to ask, and a way back. The API refuses the same read: 403, with a problem coded `API-AUTHZ-01`, and no data. | Screenshot of the page and of the API's answer |

## Sign-off

| Who | Name | Date | Result |
|---|---|---|---|
| Founder | | | |
| Founder | | | |
| Accountant | | | |
