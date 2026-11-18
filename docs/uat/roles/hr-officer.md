# UAT: the HR officer's walkthrough

- **Role:** HR officer (Nemura only)
- **Person:** Hal People, who works in Nemura and holds no role in Fyniq
- **Proves:** QA-UI-08 and FR-UX-40: §7.1's row "What each role sees" for the role; FR-PPL-02 (the HR officer's
  workspace is Nemura's People home)
- **Organisation:** the role scripts' organisation (README, step 5)

The HR officer is a role of Nemura, not of Fyniq: no invitation to Fyniq grants it, and Fyniq shows such a person
nothing. Nemura arrives with its own P1, after Fyniq's launch; until then, run steps 1 to 3, and record step 4 as "not
available yet" in the result.

## Steps

| # | Who | Screen | Do | What to see | Evidence to keep |
|---|---|---|---|---|---|
| 1 | Ann | `users-roles` | As the Owner, open Settings › Members and roles and select "Invite". | The roles offered do not include HR officer. | Screenshot of the roles offered |
| 2 | Hal | `home` | In a window of his own, sign in on the local sign-in page as `uat-hal`, "Hal People", with no invitation. | "Signed in. You are not a member of an organisation yet." No area, no name and no figure of any organisation. | Screenshot of the page |
| 3 | Hal | `not-part-of-access` | Type `http://localhost:8080/settings/members` in the address bar. Then, in a new tab of the same window, open `http://localhost:8080/api/v1/approvals`. | No organisation's page opens. The API refuses the read with a problem, and returns no data. | Screenshot of the page and of the API's answer |
| 4 | Hal | `nm-people-home` | With Nemura: sign in to Nemura as an HR officer, and use the Finance \| People switch. | Nemura's People home. The switch offers Finance only to a person who holds a Fyniq role. | Screenshot of the People home |

## Sign-off

| Who | Name | Date | Result |
|---|---|---|---|
| Founder | | | |
| Founder | | | |
| Accountant | | | |
