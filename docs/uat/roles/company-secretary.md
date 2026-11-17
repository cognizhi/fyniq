# UAT: the Company secretary's walkthrough

- **Role:** Company secretary
- **Person:** Cos Secretary, holding only this role
- **Proves:** QA-UI-08 and FR-UX-40: §7.1's row "What each role sees" for the role
- **Organisation:** the role scripts' organisation (README, step 5)

## Steps

| # | Who | Screen | Do | What to see | Evidence to keep |
|---|---|---|---|---|---|
| 1 | Cos | `statutory-calendar` | Sign in as Cos Secretary (README, step 4). | Cos lands on Compliance. | Screenshot of the landing page |
| 2 | Cos | `app-shell` | Read the navigation in Basic, then in Expert. | Only Compliance (statutory calendar, registers, the AI company secretary) and Equity (registers). Not shown: transactions, payroll and bank details. Settings holds only their own profile, notifications and appearance. | Screenshot of the navigation in each mode |
| 3 | Cos | `statutory-calendar` | Read Home. | Statutory deadlines and register updates. | Screenshot of Home |
| 4 | Cos | `inbox` | Open the Inbox. | The company secretary's drafts and tasks waiting for a person. | Screenshot of the Inbox |
| 5 | Cos | `company-secretary` | Open Compliance › company secretary, then the registers. | The AI company secretary's runs and drafts, and the registers to read. | Screenshot of each |
| 6 | Cos | `not-part-of-access` | Type `http://localhost:8080/banking` (Banking) in the address bar. Then, in a new tab of the same window, open `http://localhost:8080/api/v1/bank-lines`. | The "not part of your access" page: no name or figure from Banking, who to ask, and a way back. The API refuses the same read: 403, with a problem coded `API-AUTHZ-01`, and no data. | Screenshot of the page and of the API's answer |

## Sign-off

| Who | Name | Date | Result |
|---|---|---|---|
| Founder | | | |
| Founder | | | |
| Accountant | | | |
