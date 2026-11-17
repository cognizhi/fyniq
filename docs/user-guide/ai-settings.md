---
key: settings.ai
title: Turn the AI off, and set what it may spend
mode: expert
roles: [owner, administrator, controller, auditor, dpo]
requirements: [AI-13, AI-77, AI-86]
---

# Turn the AI off, and set what it may spend

## What it is

**Settings → AI** (Expert mode) holds the AI switch and the monthly budgets, and shows the providers and model routes
at a glance. **AI usage and budgets** shows what the assistant and the agents used and cost each month. The Owner, the
Administrator, the Controller, the Auditor and the Compliance officer can see them.

## The AI switch

- While the switch is **off**, nothing is sent to any AI provider. It is off until someone turns it on.
- **Ask to turn it on** asks another member who approves settings, such as an Owner, to approve it; it turns on once
  they do. Connections can still be set up and tested while it is off.
- **Turn all AI off now** is the Owner's: it takes effect at once, with no second approver. New requests stop, and a
  reply being written stops within seconds. Accounting, checks and deadlines carry on. Anyone who may change settings
  can also turn it off under **Settings → Organisation → Features**.

## Budgets

- A budget is a monthly limit for the organisation, an agent, a connection or a person: in the base currency, or in
  tokens for the organisation or a person. A token budget also limits a subscription, whose calls cost nothing per
  token.
- Before each call, Fyniq sets aside the most it could cost, and the most tokens it could use, against every budget
  that applies. A call that does not fit is not started.
- At 80% of a budget the Owners are alerted, once a month. At 100% no new call starts until the month ends or the Owner
  raises the budget. Probes, evals and retries count too.
- Only the Owner adds, changes or removes a budget: **Add a budget**, or **Change** beside one. Leave the amount empty
  to remove it.

## Usage and budgets

- **AI usage and budgets** shows a month at a time: what was spent, the tokens, the requests and the fallbacks, then
  the use by agent, by connection and model, and by person, each beside its budget.
- Costs are exact, in the base currency at each day's exchange rate. A subscription shows its quota used and when it
  resets.
- Providers invoice the company as a supplier: enter the invoice as a bill, and compare it with the usage here.
