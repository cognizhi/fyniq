---
key: equity.exit-waterfall
title: Model an exit
mode: expert
roles: [owner, controller, accountant]
requirements: [FR-EQ-29]
status: draft            # written with the exit waterfall sandbox (P2); the structures it models are confirmed with the company's lawyer or accountant
---

# Model an exit

## Find it

In Expert mode, open **Equity** and choose **Exit waterfall**. It is a sandbox: it reads the register and the classes' recorded preferences and posts and saves nothing.

## What it shows

Enter an **exit value**: what the shareholders receive after debts and sale costs. The page shows what each share class and each holder would receive in two ways.

- **Under the preferences**: each preference class takes its preference first, in the order of its seniority, and the classes of one seniority share in proportion to their preferences. What is left is shared by shares among the ordinary shares and the classes that converted or participate.
- **As converted**: every class converted, no preference: each share receives the same.

## What it models

- A preference of a multiple of the money invested, which does not participate: the class takes the preference, or converts and takes its share as if ordinary, whichever pays it more.
- A participating preference with no cap: the class takes its preference and then shares the rest as converted.
- Seniority between classes, and classes of one seniority paid together.

The money invested in a class is what its share issues paid up to the day, shared among its holders by shares. A holder who bought shares from another holder has the class's average price. The accountant confirms this.

## Over a round that is not recorded

When a round scenario exists (**Equity › Model a round**), choose it under **Over a round that is not recorded** and the waterfall is read as if the round had happened. Its new shares become a class of their own, "New round (not recorded)", each investor's money being what its shares cost, next to every class as the register has it the day before the round: the round moves no one's shares, and a pool top-up grants none.

The terms of that class are not recorded anywhere, so you give them: the multiple its preference pays first (1 by default), whether it then shares the rest as converted (no by default), and its seniority (1, paid first, by default). The page says the terms are the ones you asked for. A round that converts SAFEs or notes is refused, naming why: which class their conversions would take is not recorded. The scenario is only read.

## What it refuses

It refuses a class with a **cap** on its participation and a class with a **cumulative dividend**, and says which class. It never approximates them. The company's lawyer or accountant confirms them first. Anti-dilution protection is listed, and no adjustment is applied, because none is recorded.

Until a class with a liquidation preference is recorded, there is nothing to model, and the page says so.

## Related

- Recording a class and its rights: [Share classes and holders](equity-register.md). The cap table: [Equity register](equity-register.md).
