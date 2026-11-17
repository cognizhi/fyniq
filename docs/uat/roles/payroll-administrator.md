# UAT: the Payroll administrator's walkthrough

- **Role:** Payroll administrator
- **Person:** Paz Payroll, holding only this role
- **Proves:** QA-UI-08 and FR-UX-40: §7.1's row "What each role sees" for the role
- **Organisation:** the role scripts' organisation (README, step 5)

Payroll is after go-live, with Nemura (rev 0.25): until it is connected the Payroll area holds no pay run. Record what
the area shows.

## Steps

| # | Who | Screen | Do | What to see | Evidence to keep |
|---|---|---|---|---|---|
| 1 | Paz | `home` | Sign in as Paz Payroll (README, step 4). | Paz lands on Home; with Nemura, the Finance \| People switch. | Screenshot of the landing page |
| 2 | Paz | `app-shell` | Read the navigation in Basic, then in Expert. | Only Home, Inbox, Payroll (pay runs received or imported, their postings, salary batches), Projects (reports) and Reports (payroll). Not shown: Sales, Purchases beyond salary batches, Banking detail, Accounting and Equity. Settings holds only their own profile, notifications and appearance. | Screenshot of the navigation in each mode |
| 3 | Paz | `home` | Read Home. | The next pay run, salary batches, payroll postings and CPF due. | Screenshot of Home |
| 4 | Paz | `inbox` | Open the Inbox. | Only payroll requests: pay runs and salary batches. | Screenshot of the Inbox |
| 5 | Paz | `payroll-overview` | Open Payroll. | Payroll in the books: pay runs received or imported, their postings and salary batches; no payslip (FR-PPL-01). | Screenshot of Payroll |
| 6 | Paz | `not-part-of-access` | Type `http://localhost:8080/sales/invoices` (Sales) in the address bar. Then, in a new tab of the same window, open `http://localhost:8080/api/v1/invoices`. | The "not part of your access" page: no name or figure from Sales, who to ask, and a way back. The API refuses the same read: 403, with a problem coded `API-AUTHZ-01`, and no data. | Screenshot of the page and of the API's answer |

## Sign-off

| Who | Name | Date | Result |
|---|---|---|---|
| Founder | | | |
| Founder | | | |
| Accountant | | | |
