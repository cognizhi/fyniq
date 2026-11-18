# UAT: the Approver's walkthrough

- **Role:** Approver: budget holder or project manager
- **Person:** Ava Approver, holding only this role
- **Proves:** QA-UI-08 and FR-UX-40: §7.1's row "What each role sees" for the role
- **Organisation:** the role scripts' organisation (README, step 5)

Ava approves for one department of the sample company: give her the department's scope in Settings › Members and roles
first `[verify: where a role's department scope is set]`.

## Steps

| # | Who | Screen | Do | What to see | Evidence to keep |
|---|---|---|---|---|---|
| 1 | Ava | `department-home` | Sign in as Ava Approver (README, step 4). | Ava lands on Home: My department. | Screenshot of the landing page |
| 2 | Ava | `app-shell` | Read the navigation in Basic, then in Expert. | Only Home, Inbox, Projects (her projects), Reports (her department), Planning (her department's budget) and the personal items. Not shown: company cash, runway and bank; other departments; Sales, Purchases and Contacts (her approvals arrive in the Inbox); bank details; Payroll, Accounting, Equity, Compliance and Settings administration. | Screenshot of the navigation in each mode |
| 3 | Ava | `department-home` | Read Home. | Her department's budget against actual, spend this month by supplier and category, approvals waiting with her limits, her projects' hours and margin at role-average rates. Payroll cost only as a department total covering at least 3 people besides her (FR-PAY-13). | Screenshot of Home |
| 4 | Ava | `inbox` | Open the Inbox. | Only approvals within her department and her limits, each opening the document it approves. | Screenshot of the Inbox |
| 5 | Ava | `not-part-of-access` | Type `http://localhost:8080/banking` (Banking) in the address bar. Then, in a new tab of the same window, open `http://localhost:8080/api/v1/cash-position`. | The "not part of your access" page: no name or figure from Banking, who to ask, and a way back. The API refuses the same read: 403, with a problem coded `API-AUTHZ-01`, and no data. | Screenshot of the page and of the API's answer |

## Sign-off

| Who | Name | Date | Result |
|---|---|---|---|
| Founder | | | |
| Founder | | | |
| Accountant | | | |
