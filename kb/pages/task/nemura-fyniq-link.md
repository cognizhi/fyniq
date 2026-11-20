---
key: nemura.fyniq-link
type: task
title: Link Nemura to Fyniq and keep the link healthy
summary: How an Owner and a second approver link Nemura to Fyniq, what is sent each way, how to read the link's health and repair a gap, and what to do after a restore
sources:
  - docs/user-guide/nemura/fyniq-link.md sha256:22e123416fed80408a1df2f4ad1cb53a5c0b470d65ca765b5598b085737ff999
data_class: personal
taint: trusted
status: published
human_edited: false
compiled_by: claude-sonnet-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@2
compiled_at: 2026-11-21
aliases:
  - link to Fyniq
  - Finance People switch
  - link health
  - restore the link
---

# Link Nemura to Fyniq and keep the link healthy

## What it is

- Linking makes Nemura and Fyniq work together for one company. Nemura sends Fyniq the people it needs for the books (names, work emails, departments, managers and dates), the version of each person's bank details (never the number), tax-clearance holds and approved pay runs, each signed so Fyniq can tell it is Nemura's and unaltered. Fyniq sends back its departments and what became of each pay run. Never sent: NRIC or FIN, medical, disciplinary or review data. (src: docs/user-guide/nemura/fyniq-link.md#what-it-is)

## Steps

1. An Owner opens **Fyniq link** under settings, enters the Fyniq organisation's ID and starts the link with a passkey.
2. Give Fyniq what the page shows (Nemura's organisation ID, its envelope key, the IDs of the two clients and Nemura's client key): none of it is secret.
3. In Fyniq, an Owner and a second approver approve the link; Fyniq then shows its half.
4. Another Owner or an Administrator, not the one who started it, pastes Fyniq's half and finishes the link with a passkey. It is active, and everyone already in Nemura goes to Fyniq with it. The page also lists the API clients registered before the link for the two approvers to review.

(src: docs/user-guide/nemura/fyniq-link.md#link-the-two)

## The link's health

- The page shows what waits, what was not delivered yet, what was sent and what Fyniq refused, with Fyniq's code and the object and version it is about, never the content. A sync failing for 24 hours warns here and on Checks. To repair a gap, send the current state again: each person's present record, bank-detail version and hold, and what failed; Fyniq ignores a version it holds, so repeating is safe. (src: docs/user-guide/nemura/fyniq-link.md#read-the-links-health)
- The Payroll administrator and the Owner are also told, once for each oldest stuck event: after an hour while an approved pay run still waits for Fyniq's posting, and after 24 hours at any time. Nothing is told for a link that is not active. (src: docs/user-guide/nemura/fyniq-link.md#read-the-links-health)

## After a restore

- After a restore of Nemura's database the link is held and nothing is sent or paid. The page lists each pay run approved with proof whose answer the restored database lacks; check each in Fyniq and choose that Fyniq posted it (with its reference) or did not (the approval is then sent again). When the last is decided the link resumes and the current state is sent again. (src: docs/user-guide/nemura/fyniq-link.md#after-a-restore-of-nemuras-database)

## The Finance and People switch

- While linked, a person who may cross sees **Finance | People** in the header: it opens the other app with the same account and each app keeps its own last page. It never changes what you may do: each app has its own roles, a role in one grants nothing in the other, and an employee who only claims sees no switch. Nemura shows it to Owners, Administrators and Payroll administrators. Signing out of Fyniq, or being removed from it, signs the person out of Nemura as well; signing out of Nemura does not yet sign you out of Fyniq. (src: docs/user-guide/nemura/fyniq-link.md#the-finance-and-people-switch)

## When it looks wrong

- A refusal from Fyniq carries a code, never the content: correct what it names, then send the current state again. A pay run's refusal is handled on the run (see [[nemura.payroll-approval]]). A held link means an unreconciled restore. (src: docs/user-guide/nemura/fyniq-link.md#common-problems)
