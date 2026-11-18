# UAT: the Accountant's walkthrough

- **Role:** Accountant
- **Person:** Cal Accountant, holding only this role
- **Proves:** QA-UI-08 and FR-UX-40: §7.1's row "What each role sees" for the role
- **Organisation:** the role scripts' organisation (README, step 5)

## Steps

| # | Who | Screen | Do | What to see | Evidence to keep |
|---|---|---|---|---|---|
| 1 | Cal | `home` | Sign in as Cal Accountant (README, step 4). | Cal lands on Home. | Screenshot of the landing page |
| 2 | Cal | `app-shell` | Read the navigation in Basic, then in Expert. | Only every finance area; Payroll masked; Compliance to view. Not shown: Settings administration; per-person pay unless granted. | Screenshot of the navigation in each mode |
| 3 | Cal | `home` | Read Home. | The full Home: cash, runway, books health, to-dos, deadlines. | Screenshot of Home |
| 4 | Cal | `inbox` | Open the Inbox. | Requests the Accountant decides (issuing, posting), and the accountant's review. | Screenshot of the Inbox |
| 5 | Cal | `compliance-center` | Open Compliance. | To read: no change to a control, a policy or a register. | Screenshot of Compliance |
| 6 | Cal | `not-part-of-access` | Type `http://localhost:8080/settings/members` (Settings › Members and roles) in the address bar. Then, in a new tab of the same window, open `http://localhost:8080/api/v1/members`. | The "not part of your access" page: no name or figure from Settings › Members and roles, who to ask, and a way back. The API refuses the same read: 403, with a problem coded `API-AUTHZ-01`, and no data. | Screenshot of the page and of the API's answer |

## Sign-off

| Who | Name | Date | Result |
|---|---|---|---|
| Founder | | | |
| Founder | | | |
| Accountant | | | |
