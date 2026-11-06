---
key: accounting.reviews
type: task
title: The accountant's quarterly review of a closed month
summary: The accountant reviews a closed month each quarter: queries management answers, adjusting journals and a sign-off; unreviewed after 60 days, it warns
sources:
  - docs/user-guide/accountant-review.md sha256:03f14d3240029b43214d05fab50849ced33da2650f8b19d72f5b87e186bc204e
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-11-06
aliases:
  - accountant review
  - quarterly review
  - external accountant
  - review queries
  - adjusting journal
  - sign off a review
  - superseded review
---

# The accountant's quarterly review of a closed month

## What a review is

- In Founder mode the external accountant reviews the books at least once a quarter; each review records one closed month: the month's close with its snapshot, the accountant's queries with management's answers, any adjusting journals and the accountant's sign-off.
- The month is normally the quarter's last, because its close snapshot is the trial balance at the quarter end.
- **Accounting → Accountant reviews** lists each quarter with its review.

(src: docs/user-guide/accountant-review.md#what-it-is)

## Open a review

- Close the month first ([[accounting.periods]]); then the Accountant chooses the closed month and selects **Open a review**. A month's close is reviewed once.
- The Accountant role can be given with an end date, so the accountant's access ends after the review; it reads the full audit trail while it lasts.

(src: docs/user-guide/accountant-review.md#open-a-review)

## Queries and answers

- The Accountant writes each question and selects **Raise query**; an Owner or the Controller writes the answer and selects **Answer**.
- Each query is answered once; for more, the Accountant raises another. The Accountant never answers a query, and management never raises one.

(src: docs/user-guide/accountant-review.md#raise-and-answer-queries)

## Adjusting journals

- The Accountant links each adjusting journal with **Link journal**: a draft or one posted already.
- Each posts as any journal does: above the self-approval threshold, another person approves it ([[accounting.journals]]).
- A journal dated in a closed month needs the month reopened first, which another founder approves; reopening supersedes the review.

(src: docs/user-guide/accountant-review.md#adjusting-journals)

## Sign-off, reopening and the quarter

- Once every query has its answer, the Accountant writes what they sign off, selects **Sign off** and confirms it's them; a signed-off review changes no more.
- Reopening a reviewed month marks its review **Superseded** and keeps it as it was; once closed again, the Accountant opens a new review.
- A quarter is reviewed when a signed-off review covers its last month; in Founder mode one with none 60 days after it ends warns on **Checks**.

(src: docs/user-guide/accountant-review.md#sign-off)
(src: docs/user-guide/accountant-review.md#when-a-month-reopens)
(src: docs/user-guide/accountant-review.md#every-quarter)

## Common problems

- **The month is not closed**: close it first; the review covers its close snapshot.
- **The close has a review already**: open that review instead.
- **A query has no answer yet**: an Owner or the Controller answers each query before the sign-off.
- **The review is signed off or superseded**: it changes no more; open a review of the month as it is closed now.

(src: docs/user-guide/accountant-review.md#common-problems)
