# <Date>: <what users saw, in one line>

The root-cause record of a calculation defect or an incident (PRD-001 QA-REL-01; the fyniq-bug-fix skill, §7).
Copy this file to `docs/postmortems/YYYY-MM-DD-<slug>.md` within 5 working days of the fix. Blameless; facts only;
synthetic or masked figures, never a customer's data.

## Summary

What was wrong, for whom, and since when, in two or three sentences. The fix's pull request and release.

## Timeline

| When (SGT) | What happened |
|---|---|
| | Introduced (commit, release) |
| | Found (by whom or which check) |
| | Regression test written, failing |
| | Fix merged; released |
| | Data corrected (journal or reversal numbers) |

## Impact

- Organisations and periods affected, and how they were found.
- Figures that were wrong, before and after (masked or synthetic).
- Filed returns or sent documents affected (GST F5, CPF, invoices), and what the accountant decided.

## Root cause

The defect itself: the rule, the code path and the input that reached it.

## Why the checks did not catch it

For each, say why it missed this class of defect:
- the oracle (`oracle/`) and the differential suite;
- the properties and the invariants (INV-*);
- the golden sets and the pinned cases;
- the nightly verifications.

## What now catches it

- The regression test that failed before the fix (file and test name, with its `req:` tag).
- The new property, invariant, fixture, pinned case or check that would find the same class of defect, and the
  proof that it fails on the old code.

## Data correction

The approved journals or reversals, with the bug reference in their reason. Never an edit of posted history.

## Follow-ups

| Action | Owner | Due |
|---|---|---|
| | | |
