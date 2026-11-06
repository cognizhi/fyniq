---
key: accounting.reviews
title: The accountant's quarterly review
mode: expert
roles: [accountant, owner, controller, auditor, tax_agent]
requirements: [FM-7, AUD-04, FR-GL-01, FR-GL-08]
status: draft            # written with p1-approval-engine task 3.4; the accountant confirms it
---

# The accountant's quarterly review

## What it is

In Founder mode the external accountant reviews the books at least once a quarter. Each review is a
record of one closed month:

- the month's close it covers, with that close's snapshot;
- the accountant's queries, each with management's answer;
- any adjusting journals the review raised;
- the accountant's sign-off.

The month is normally the quarter's last, because its close snapshot is the trial balance at the
quarter end. **Accounting → Accountant reviews** lists each quarter with its review.

## Open a review

1. Close the month first ([Periods and the close](periods-and-close.md)).
2. The Accountant opens **Accounting → Accountant reviews**, chooses the closed month and selects
   **Open a review**.

A month's close is reviewed once. The Accountant role can be given with an end date, so the
accountant's access ends after the review; it reads the full audit trail while it lasts.

## Raise and answer queries

- The Accountant writes each question and selects **Raise query**.
- An Owner or the Controller writes the answer and selects **Answer**. Each query is answered once;
  for more, the Accountant raises another query.
- The Accountant never answers a query, and management never raises one.

## Adjusting journals

- The Accountant links each adjusting journal the review raised with **Link journal**: a draft, or
  one posted already.
- Each posts as any journal does: above the self-approval threshold, another person approves it
  ([Journals](journals.md)).
- A journal dated in a closed month needs the month reopened first, which another founder approves.
  Reopening supersedes this review.

## Sign off

Once every query has its answer, the Accountant writes what they sign off, selects **Sign off** and
confirms it's them with a passkey or authenticator app. A signed-off review changes no more.

## When a month reopens

Reopening a reviewed month marks its review **Superseded**. It is kept as it was. Once the month is
closed again, the Accountant opens a new review of it.

## Every quarter

- A quarter is reviewed when a signed-off review covers its last month.
- In Founder mode, a quarter with no signed-off review 60 days after it ends shows a warning on
  **Checks**. Open, answer and sign off the review of its last month to clear it.

## Common problems

- **The month is not closed**: close it first; the review covers its close snapshot.
- **The close has a review already**: open that review instead.
- **A query has no answer yet**: an Owner or the Controller answers each query before the sign-off.
- **The review is signed off or superseded**: it changes no more; open a review of the month as it is
  closed now.
