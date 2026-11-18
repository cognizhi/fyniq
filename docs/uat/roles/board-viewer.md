# UAT: the Board viewer's walkthrough

- **Role:** Board viewer
- **Person:** Bo Board, holding only this role
- **Proves:** QA-UI-08 and FR-UX-40: §7.1's row "What each role sees" for the role
- **Organisation:** the role scripts' organisation (README, step 5)

§7.1 lands the Board viewer on the board pack, which is P2 (`board-pack`): at launch, record where Bo lands.

## Steps

| # | Who | Screen | Do | What to see | Evidence to keep |
|---|---|---|---|---|---|
| 1 | Bo | `reports-index` | Sign in as Bo Board (README, step 4). | Bo lands on Reports, cut to the board pack. | Screenshot of the landing page |
| 2 | Bo | `app-shell` | Read the navigation in Basic, then in Expert. | Only Reports (the board pack) and Equity (his own holding; all if granted). Not shown: transactions. Settings holds only their own profile, notifications and appearance. | Screenshot of the navigation in each mode |
| 3 | Bo | `reports-index` | Read Home. | The board pack's reports, and no transaction. | Screenshot of Home |
| 4 | Bo | `cap-table` | Open Equity. | His own holding only, unless the Owner granted the whole cap table. | Screenshot of Equity |
| 5 | Bo | `not-part-of-access` | Type `http://localhost:8080/sales/invoices` (Sales) in the address bar. Then, in a new tab of the same window, open `http://localhost:8080/api/v1/invoices`. | The "not part of your access" page: no name or figure from Sales, who to ask, and a way back. The API refuses the same read: 403, with a problem coded `API-AUTHZ-01`, and no data. | Screenshot of the page and of the API's answer |

## Sign-off

| Who | Name | Date | Result |
|---|---|---|---|
| Founder | | | |
| Founder | | | |
| Accountant | | | |
