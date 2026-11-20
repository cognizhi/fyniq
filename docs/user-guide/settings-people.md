---
key: settings.people
title: People and HR: where payroll lives and how its outputs are met
mode: expert
roles: [owner, administrator, payroll_administrator, controller, accountant, auditor]
requirements: [FR-PPL-01, FR-PPL-08, FR-PAY-15, FR-PPL-05]
status: draft            # written with p1-payroll-link; reviewed with the People and HR screen
---

# People and HR: where payroll lives and how its outputs are met

## What it is

**Settings › People and HR** says where people records, payroll, leave and letters live: in Nemura, in an external system, or nowhere (payroll is posted by manual journal). It shows the link's state and
what Fyniq received from Nemura, and records how each month's statutory payroll outputs are met.

## Where each feature lives

- Linking Nemura, changing where a feature lives and unlinking need the Owner and a second approver; they are asked for through the API today.
- While payroll lives in Nemura, a provider's payroll journal or a file import is not taken: the pay run posts through the link, so it is never posted from two sources.

## Link or unlink Nemura

- **Link Nemura** asks to link this organisation with what Nemura's own link page shows once an Owner there has proposed the link: its organisation ID, its web address, the two client IDs, its link key and its client key, and a date within a year when the client expires. Nothing is linked by asking: the Owner and a second approver approve it here, and another Owner or Administrator approves Nemura's half there.
- **Ask to unlink Nemura** gives a reason and needs the same two approvers. Work in flight settles first: salary payments not yet paid, rejected or returned, payments with an unknown outcome and events waiting for Nemura. While any is left the link stays **unlinking** and the page counts them; when nothing is left, **Finish unlinking** ends the link. Both sides keep their data, and a later link starts at a higher epoch.

## Link health

- While the link is active, **Link health** shows how it stands, by identity only: how many events Nemura has queued for Fyniq, the highest number Fyniq's inbox holds, and how many events wait to be delivered, fail or were refused. Nothing in it is a payload, a name or a figure.
- A **gap** is a run of event numbers that Nemura says it delivered and Fyniq never received. The page names the runs and offers **Ask Nemura to send them again**: Nemura then sends the current state of what those events were about, never an old payload, and Fyniq applies what it missed and ignores what it already holds. Nemura answers at new event numbers, so the numbers you asked for never arrive: the page then says you asked for them in the last day and does not ask again; after a day they show as missing again, in case Nemura never answered. Asking twice with nothing new missing asks for nothing. If Nemura's log is longer than one look reads, the page looks at the newest part and says the oldest was left out.
- If Nemura's log cannot be read, the page says why (the link is not active, Nemura did not answer, or the link was made before the log could be read and is approved again to allow it) and shows no number it does not know. **Check again** looks once more.
- It needs People and HR's right.

## People who approve pay runs in Nemura

- A pay run approved in Nemura while the apps are linked is signed with **a passkey enrolled in Fyniq**. Nemura tells Fyniq, signed, which sign-in accounts approve pay runs there (an account and a yes or no, never a role name or an email).
- A person Nemura says approves pay runs, and who has no passkey in Fyniq, sees a reminder at the top of every page: **Add a passkey to approve pay runs in Nemura**. It gives them no role in Fyniq. A person with no sign-in in Fyniq at all needs an invitation first.
- Fyniq also tells Nemura, for each sign-in account, whether it holds a role in the finance workspace and whether it has a passkey, so Nemura's **Finance | People** switch and its warning before an approval are right. Both are hints, never grants.
- The **Finance | People** switch in Fyniq's header shows only to a person who holds a role in the finance workspace here and whom Nemura says is an active member there. It opens Nemura with the sign-in provider the person used, so they are not asked to choose one again.

## How the month's payroll outputs are met

- Before a pay run that pays a person can be approved for payment, five things are recorded as evidence: itemised payslips, key employment terms, CPF, SDL and SHG submission and payment, and where they apply
  the Auto-Inclusion Scheme and IR21.
- Each is met by a named payroll provider, a documented manual process, Nemura (payslips and key employment terms only, while the link is active), or not applicable with the reason (the Auto-Inclusion Scheme and IR21 only).
- Every method but Nemura's needs a file of evidence. An arrangement holds from the month you choose until a later one replaces it; it is never changed.
- While one is missing, the salary batch cannot be asked for approval and says which.

## A provider's payroll journal

- A provider's journal, or a file import, by account and department, becomes a **draft journal** under **Accounting › Journals**; a Controller or Accountant approves it, because the provider's own approval cannot be checked.
- It is taken once for each provider, pay period and version; a later version drafts only the difference from the version before, once the earlier journal is posted.
- A file may not name a person: pay is kept by account and department.
