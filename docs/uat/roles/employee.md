# UAT: the Employee's walkthrough

- **Role:** Employee
- **Person:** Eve Employee, holding only this role
- **Proves:** QA-UI-08 and FR-UX-40: §7.1's row "What each role sees" for the role
- **Organisation:** the role scripts' organisation (README, step 5)

## Steps

| # | Who | Screen | Do | What to see | Evidence to keep |
|---|---|---|---|---|---|
| 1 | Eve | `personal-claims` | Sign in as Eve Employee (README, step 4). | Eve lands on the personal workspace, on My claims (FR-UX-39): "What the company owes you", "Your claims" and the expense policy in short. | Screenshot of the landing page |
| 2 | Eve | `app-shell` | Read the navigation in Basic, then in Expert. | Only My claims; My time when Projects is on; My shares and options; Help. Money I put in shows only to a director. Not shown: any company figure, and any other person's record. Settings holds only their own profile, notifications and appearance. | Screenshot of the navigation in each mode |
| 3 | Eve | `claim-editor` | Start a new claim from a receipt and cancel it. | The claim editor, with the expense policy's limits; nothing about anyone else's claims. | Screenshot of the editor |
| 4 | Eve | `not-part-of-access` | Type `http://localhost:8080/reports` (Reports) in the address bar. Then, in a new tab of the same window, open `http://localhost:8080/api/v1/invoices`. | The "not part of your access" page: no name or figure from Reports, who to ask, and a way back. The API refuses the same read: 403, with a problem coded `API-AUTHZ-01`, and no data. | Screenshot of the page and of the API's answer |

## Sign-off

| Who | Name | Date | Result |
|---|---|---|---|
| Founder | | | |
| Founder | | | |
| Accountant | | | |
