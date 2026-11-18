# UAT: the Payment releaser's walkthrough

- **Role:** Payment releaser
- **Person:** Rae Releaser, holding only this role
- **Proves:** QA-UI-08 and FR-UX-40: §7.1's row "What each role sees" for the role
- **Organisation:** the role scripts' organisation (README, step 5)

## Steps

| # | Who | Screen | Do | What to see | Evidence to keep |
|---|---|---|---|---|---|
| 1 | Rae | `home` | Sign in as Rae Releaser (README, step 4). | Rae lands on Home. | Screenshot of the landing page |
| 2 | Rae | `app-shell` | Read the navigation in Basic, then in Expert. | Only Home, Inbox, Purchases (payment batches to release), Banking and Reports. Not shown: creating or approving payments, and the bills themselves; salary lines show only as totals (FR-PPL-17). Settings holds only their own profile, notifications and appearance. | Screenshot of the navigation in each mode |
| 3 | Rae | `home` | Read Home. | Batches waiting for release, bank balances, and payments rejected or with an unknown outcome. | Screenshot of Home |
| 4 | Rae | `inbox` | Open the Inbox. | Only approved batches waiting for release. | Screenshot of the Inbox |
| 5 | Rae | `payment-batches` | Open Purchases › Payment batches. | Batches to release: no way to create a batch or approve one. | Screenshot of the batches |
| 6 | Rae | `not-part-of-access` | Type `http://localhost:8080/purchases/bills` (the bills) in the address bar. Then, in a new tab of the same window, open `http://localhost:8080/api/v1/bills`. | The "not part of your access" page: no name or figure from the bills, who to ask, and a way back. The API refuses the same read: 403, with a problem coded `API-AUTHZ-01`, and no data. | Screenshot of the page and of the API's answer |

## Sign-off

| Who | Name | Date | Result |
|---|---|---|---|
| Founder | | | |
| Founder | | | |
| Accountant | | | |
