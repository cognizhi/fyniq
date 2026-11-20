---
key: equity.exit-waterfall
type: task
title: Model an exit
summary: A sandbox for what an exit value would pay each share class and holder under the recorded preferences, next to a plain split, and what it refuses
sources:
  - docs/user-guide/exit-waterfall.md sha256:0a60d67beda7c5ce730a35256cd9ac8f5ef7f8d68a790892ec1ede7f10f8949b
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-sonnet-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@2
compiled_at: 2026-11-20
aliases:
  - exit waterfall
  - liquidation preference
  - what would investors get if we sell
  - preference stack
  - waterfall model
  - sale of the company
---

# Model an exit

## Find it

- In Expert mode, **Equity** has **Exit waterfall**: a sandbox that reads the register and the classes' recorded preferences and posts and saves nothing.

(src: docs/user-guide/exit-waterfall.md#find-it)

## What it shows

- Enter the exit value, what the shareholders receive after debts and sale costs.
- Under the preferences, each preference class takes its preference first, in the order of its seniority, and classes of one seniority share in proportion to their preferences; what is left is shared by shares among the ordinary shares and the classes that converted or participate.
- As converted, every class is converted, no preference, and each share receives the same.

(src: docs/user-guide/exit-waterfall.md#what-it-shows)

## What it models

- A preference of a multiple of the money invested that does not participate: the class takes the preference, or converts, whichever pays it more.
- A participating preference with no cap, and seniority between classes.
- The money invested in a class is what its share issues paid up to the day, shared among its holders by shares; a holder who bought shares from another holder has the class's average price, which the accountant confirms.

(src: docs/user-guide/exit-waterfall.md#what-it-models)

## What it refuses

- A class with a cap on its participation and a class with a cumulative dividend are refused, naming the class, and never approximated; the company's lawyer or accountant confirms them first.
- Anti-dilution protection is listed and no adjustment is applied, because none is recorded.
- Until a class with a liquidation preference is recorded, there is nothing to model.

(src: docs/user-guide/exit-waterfall.md#what-it-refuses)

## Related

- Recording a class and its rights, and the cap table: [[equity.register]].

(src: docs/user-guide/exit-waterfall.md#related)
