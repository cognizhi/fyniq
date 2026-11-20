---
key: nemura.fyniq-link
title: Link Nemura to Fyniq and keep the link healthy
mode: expert
roles: [owner, administrator]
requirements: [NM-ARC-04, NM-INT-01, NM-INT-04, NM-INT-10, NM-INT-11, NM-INT-13]
---

# Link Nemura to Fyniq and keep the link healthy

## What it is

Linking makes Nemura and Fyniq work together for one company. Nemura sends Fyniq the people it needs for the books (names, work emails, departments, managers and dates), the version of each person's bank details (never the number), tax-clearance holds and approved pay runs, each signed so that Fyniq can tell it is Nemura's and unaltered. Fyniq sends back its departments, what became of each pay run and, later, the state of each salary payment. Never sent: NRIC or FIN, medical, disciplinary or review data.

## Link the two

1. An **Owner** opens **Fyniq link** under settings, enters the Fyniq organisation's ID (shown on Fyniq's People and HR page) and selects **Start the link with my passkey**.
2. Give Fyniq what the page then shows: Nemura's organisation ID, its envelope key, the IDs of the two clients Fyniq will have here and Nemura's client key. None of it is secret.
3. In Fyniq, an Owner and a second approver approve the link. Fyniq then shows its half.
4. **Another Owner or an Administrator** (not the one who started it) opens the page, pastes Fyniq's half and selects **Finish the link with my passkey**. The link is active, and everyone already in Nemura goes to Fyniq with it.

The page also lists the API clients registered before the link: the two people who approve review them, and from then on a new one needs a second approver too.

## Read the link's health

**How the link is doing** shows what waits, what was not delivered yet (Nemura tries again), what was sent and what Fyniq refused for good, with Fyniq's code and the object and version it is about; never the content. A sync that has failed for 24 hours shows a warning here and on Checks. If a sequence number never arrived or something was refused, select **Send the current state to Fyniq again**: it sends each person's present record, bank-detail version and hold again and what failed. Fyniq ignores a version it already holds, so it is safe to repeat.

## After a restore of Nemura's database

When Nemura's database has been restored, the people who run the deployment record it, and the link is held: nothing is sent or paid. **After a restore** lists each pay run approved with proof whose answer the restored database does not hold, because Fyniq may have posted it after the restore point. For each, look at the run in Fyniq (its People and HR page shows what it posted) and choose **Fyniq posted it** with Fyniq's reference, or **Fyniq did not post it**: that approval is then sent again. When the last is decided the link resumes and the current state is sent again.

## The Finance and People switch

While Nemura is linked, a person who may cross sees **Finance | People** in the header. It opens the other app with the same account, and each app keeps its own last page: you come back to the page you left. It never changes what you may do: each app has its own roles, a role in one grants nothing in the other, and an employee who only claims sees no switch (their Claims tile opens the personal workspace in Fyniq). Nemura shows it to Owners, Administrators and Payroll administrators; Fyniq decides what is behind it.

Signing out of Fyniq, or being removed from it, signs the person out of Nemura as well; signing out of Nemura does not yet sign you out of Fyniq.

## Common problems

- **Fyniq refused an event.** The code says why; the content is never shown. Correct what it names, then send the current state again.
- **A pay run is refused.** See [Read and approve a pay run](payroll-approval.md): a refusal Fyniq's side can put right is sent again from the run.
- **The link is held.** A restore has not been reconciled: decide each run listed under **After a restore**.

## Related

- [Read and approve a pay run](payroll-approval.md)
- [Switch features on and off](features.md)
