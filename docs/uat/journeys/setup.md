# UAT: set-up

- **Journey:** set-up (QA-E2E-01)
- **People:** Ann Owner (Owner), then Bea Bookkeeper (Bookkeeper), whom Ann invites
- **Proves:** FR-UX-10 (the set-up checklist), FR-ORG-01 (the profile), FR-ADM-01 (invitations, roles, the Owner
  alone until a second member joins)
- **Organisation:** `make uat-organisation NAME="UAT Set-up Pte Ltd" PEOPLE="Ann Owner:owner"`; Ann signs in and
  accepts her invitation (README, step 4)

The Owner of a new company with the sample books follows Home's set-up checklist: gives herself the Controller's role
to keep the books, enters the legal name and UEN, and invites a Bookkeeper, who joins with the code she gives them.

## Steps

| # | Who | Screen | Do | What to see | Evidence to keep |
|---|---|---|---|---|---|
| 1 | Ann | `home` | Open Home. | "Set up the company", with "Give yourself the roles you work in", "Enter the company's legal name and UEN" and "Invite the other founders" each marked To do. | Screenshot of the checklist |
| 2 | Ann | `users-roles` | Select "Give yourself the roles you work in". Switch the experience mode to Expert. Select "Change roles: Ann Owner", choose Controller in "Role to add", then "Add role". | Settings › Members and roles. Ann's roles list Owner and Controller at once: she is the only member, so no one else approves it. | Screenshot of Ann's roles |
| 3 | Ann | `organisation-settings` | From Home's checklist, select "Enter the company's legal name and UEN", then "Edit the profile". Enter the legal name "UAT Set-up Pte. Ltd." and the UEN "202612345K", then "Save the profile". | Settings › Organisation. The Profile shows the legal name and UEN as entered. | Screenshot of the profile |
| 4 | Ann | `users-roles` | Open Settings › Members and roles, select "Invite", enter the name "Bea Bookkeeper", tick Bookkeeper, then "Create invitation". | "Give them this code" shows the code once. The pending invitations list Bea as Bookkeeper. | Screenshot of the pending invitation (not of the code) |
| 5 | Bea | `sign-in` | In a window of her own, sign in on the local sign-in page as `uat-bea`, "Bea Bookkeeper". On "Joining by invitation", enter the code and her name, then "Accept the invitation". | Bea lands on Home in UAT Set-up Pte Ltd. Her navigation has no Settings administration, Planning, Equity or Compliance Center (§7.1). | Screenshot of Bea's Home |
| 6 | Ann | `users-roles` | Reload Members and roles. | People lists Ann (Owner, Controller) and Bea (Bookkeeper). From now on a role change waits for a second approver (FR-ADM-01). | Screenshot of People |
| 7 | Ann | `home` | Open Home. | The three steps are Done. The others ("Set when the books start", "Open the first financial year", "Load the chart of accounts" and so on) show their state: the sample company loaded its chart and periods. | Screenshot of the checklist |

## Sign-off

| Who | Name | Date | Result |
|---|---|---|---|
| Founder | | | |
| Founder | | | |
| Accountant | | | |
