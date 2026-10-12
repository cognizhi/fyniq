# Page override · Banking: categorise and match

Overrides MASTER.md for the categorise queue (FR-BNK-06/07, FR-UX-05/07).

## Basic mode: one line at a time
- A card per bank line: date, description (mono, untrusted text shown as plain text),
  amount, and **suggestions** in order: a matching invoice or bill, a rule hit, an AI suggestion
  (AI badge).
- One click accepts; "Change" opens a short form (category, contact, GST code). "Skip" moves on.
- Progress: "12 of 40 done".

## Expert mode: the grid
- AG Grid: date, description, amount, suggested category, contact, GST code, dimensions,
  status chip; range selection, paste, Ctrl+D fill-down, bulk accept of the selection.
- **Split view** for matching: the bank line on the left, candidate documents on the right,
  with amount difference and tolerance shown.
- Rules editor in a side-peek panel with a back-test result ("would have matched 23 lines").

## Both modes
- AI suggestions never auto-apply; accepting a batch of AI suggestions creates a tier-1 proposal
  that the user approves.
- Duplicates and mirrored transfers are flagged with a ⚠ chip and an explanation.
