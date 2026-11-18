# UAT: the Controller's walkthrough

- **Role:** Controller
- **Person:** Con Controller, holding only this role
- **Proves:** QA-UI-08 and FR-UX-40: §7.1's row "What each role sees" for the role
- **Organisation:** the role scripts' organisation (README, step 5)

## Steps

| # | Who | Screen | Do | What to see | Evidence to keep |
|---|---|---|---|---|---|
| 1 | Con | `home` | Sign in as Con Controller (README, step 4). | Con lands on Home. | Screenshot of the landing page |
| 2 | Con | `app-shell` | Read the navigation in Basic, then in Expert. | Only every finance area, and Settings: approval policies and statutory parameters. Not shown: per-person pay unless granted (payroll is after go-live, rev 0.25), and data protection. | Screenshot of the navigation in each mode |
| 3 | Con | `home` | Read Home. | The full Home. | Screenshot of Home |
| 4 | Con | `inbox` | Open the Inbox. | Requests the Controller approves: invoices above the threshold, journals, period reopenings. | Screenshot of the Inbox |
| 5 | Con | `approval-policies` | Open Settings › Approval policies, then Statutory parameter packs. | Both open; a change to either waits for a second approver. | Screenshot of each |
| 6 | Con | `not-part-of-access` | Type `http://localhost:8080/compliance/data-protection` (Data protection) in the address bar. Then, in a new tab of the same window, open `http://localhost:8080/api/v1/compliance/dpias`. | The "not part of your access" page: no name or figure from Data protection, who to ask, and a way back. The API refuses the same read: 403, with a problem coded `API-AUTHZ-01`, and no data. | Screenshot of the page and of the API's answer |

## Sign-off

| Who | Name | Date | Result |
|---|---|---|---|
| Founder | | | |
| Founder | | | |
| Accountant | | | |
