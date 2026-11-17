# UAT: the Tax agent's walkthrough

- **Role:** Tax agent
- **Person:** Tad Tax, holding only this role
- **Proves:** QA-UI-08 and FR-UX-40: §7.1's row "What each role sees" for the role
- **Organisation:** the role scripts' organisation (README, step 5)

## Steps

| # | Who | Screen | Do | What to see | Evidence to keep |
|---|---|---|---|---|---|
| 1 | Tad | `home` | Sign in as Tad Tax (README, step 4). | Tad lands on Home. | Screenshot of the landing page |
| 2 | Tad | `app-shell` | Read the navigation in Basic, then in Expert. | Only as the Accountant's, plus the GST and tax working papers. Not shown: user administration. | Screenshot of the navigation in each mode |
| 3 | Tad | `home` | Read Home. | The full Home. | Screenshot of Home |
| 4 | Tad | `inbox` | Open the Inbox. | Requests the Tax agent decides, as the Accountant's. | Screenshot of the Inbox |
| 5 | Tad | `gst-returns` | Open Compliance › GST. | The registration monitor and its forecasts. | Screenshot of GST |
| 6 | Tad | `not-part-of-access` | Type `http://localhost:8080/settings/members` (Settings › Members and roles) in the address bar. Then, in a new tab of the same window, open `http://localhost:8080/api/v1/members`. | The "not part of your access" page: no name or figure from Settings › Members and roles, who to ask, and a way back. The API refuses the same read: 403, with a problem coded `API-AUTHZ-01`, and no data. | Screenshot of the page and of the API's answer |

## Sign-off

| Who | Name | Date | Result |
|---|---|---|---|
| Founder | | | |
| Founder | | | |
| Accountant | | | |
