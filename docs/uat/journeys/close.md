# UAT: close

- **Journey:** close (QA-E2E-01)
- **People:** Cal Accountant (Accountant), Ann Owner (Owner and Controller)
- **Proves:** FR-GL-08 (the period lock, the close and its reopening with approval), INV-03 (nothing posts in a closed
  period), FM-4 (the close attested by a founder who did not prepare it), the close checks of Appendix C
- **Organisation:** `make uat-organisation NAME="UAT Close Pte Ltd" PEOPLE="Cal Accountant:accountant;Ann Owner:owner,controller"`

The Accountant runs the month's close checks; another founder attests and closes the month. Nothing then posts in it,
by any path, until a reopening another founder approves.

## Steps

| # | Who | Screen | Do | What to see | Evidence to keep |
|---|---|---|---|---|---|
| 1 | Cal | `period-close` | Switch to Expert. Open Accounting › Periods and close; choose last month. | The month is Open. "Run the checks first. They record the ledger as they find it, so another founder can attest and close." | Screenshot of the period |
| 2 | Cal | `checks` | Select "Run the checks". | Either "The checks are clean: another founder can attest and close", or "The checks found something that blocks the close", each finding with its ✖ or ⚠ and where to fix it. | Screenshot of the checks |
| 3 | Cal | `checks` | If a check blocks: fix what it names (for the sample company, usually a bank line to categorise or a reconciliation to sign), then "Run the checks again". | "Nothing blocks the close." | Screenshot of the clean checks |
| 4 | Cal | `period-close` | Look for a way to attest the close. | Cal cannot: "Another founder attests: you ran the checks". | Screenshot |
| 5 | Ann | `period-close` | Open the same month, read "What you attest", and select "Attest and close". | "Books closed". The month is Closed: "The period is closed: nothing posts in it, by any path." A snapshot of the ledger is kept. | Screenshot of the closed month |
| 6 | Cal | `journal-editor` | Prepare a journal dated in the closed month (any two balancing lines) and select "Post". | It cannot post: Fyniq names the closed month and offers no way round it. | Screenshot of the refusal |
| 7 | Cal | `period-close` | Select "Ask to reopen", with the reason "UAT: check the reopening". | The request waits for another founder; "until then, the month stays closed". | Screenshot of the request |
| 8 | Ann | `inbox` | Open the reopening's request in the Inbox and approve it. | The month is Open again, with the snapshots before and after its reopening. | Screenshot of the reopened month |
| 9 | Cal, Ann | `period-close` | Run the checks and close the month again (steps 2 to 5). | Closed again; the period's history shows the close, the reopening with its reason and approver, and the new close. | Screenshot of the history |

## Sign-off

| Who | Name | Date | Result |
|---|---|---|---|
| Founder | | | |
| Founder | | | |
| Accountant | | | |
