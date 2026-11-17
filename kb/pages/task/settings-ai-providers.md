---
key: settings.ai-providers
type: task
title: Connect the AI to a model provider, and see which data may go where
summary: Settings › AI › AI providers: connections, which data may go where, adding one; a connection's page: on and off, key, models, qualification, data handling
sources:
  - docs/user-guide/ai-providers.md sha256:a0b69d0592d8d22e729a1302ce191faf723335d50214afe1aa0cf04b2a8dcafa
data_class: general
taint: trusted
status: published
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-11-14
aliases:
  - add an AI provider
  - connect OpenAI
  - connect Claude
  - model provider
  - API key for the AI
  - which data goes to the AI
  - AI subscription quota
  - allow a model
  - qualify a model
  - replace the AI key
  - turn off an AI provider
  - where prompts are processed
---

# Connect the AI to a model provider, and see which data may go where

## What it is

- **Settings → AI → AI providers** (Expert mode) lists where the assistant and the agents send their model calls: each connection to a model provider, with its sign-in, billing and the data it may receive; each agent then picks from them on its route ([[settings.ai-routes]]). Every connection starts off. The AI's switch and budgets: [[settings.ai]].

(src: docs/user-guide/ai-providers.md#what-it-is)

## Connections

- Each shows its provider and the one address its calls go to, **On** or **Off**, how it is paid for, whether its key is set (never shown again), and this month's cost.
- Adding, changing or turning one on, and allowing a model its provider lists, needs a second approver ([[approvals.decide]]); a waiting request shows at the top with a link.

(src: docs/user-guide/ai-providers.md#connections)

## Which data may go where

- Know-how and business figures go to any connection that is on; personal data and payroll only where processing in Singapore is evidenced in the connection's data handling; restricted data nowhere.
- NRIC and FIN numbers, full bank account numbers and secrets never leave Fyniq; the card lists what the company's policy adds or takes away for each class.

(src: docs/user-guide/ai-providers.md#which-data-may-go-where)

## Add a connection

- Choose a provider or one of its presets and select **Set up**; name it, choose how it signs in, and paste its API key if it uses one (straight to the secret store, never shown again, logged or sent to a model).
- Choose **usage-based** or a **subscription** with a quota that resets each day or month, and fill in the provider's settings, or leave one empty for its default.
- **Qualify new models automatically** (off unless turned on) runs each newly listed model through the evals of every agent whose route uses the connection; adding a model to the allow-list or a route stays a person's decision.
- **Ask to add it** sends it for approval; once approved it is added, off, its models are read from the provider, and turning it on needs a second approver too.

(src: docs/user-guide/ai-providers.md#add-a-connection)

## How a connection is paid for

- **Usage-based**: each request's cost from the provider's usage figures and its price. **Subscription**: when the quota runs out the agent moves to its route's next step or stops, never switching to paying per token by itself. Models that draw at another rate are listed as `model = rate` with the plan's published terms; others draw 1.
- A person's own plan is not offered yet; a consumer chat plan meant for the provider's own apps, or a reused chat session or browser cookie, is not supported.

(src: docs/user-guide/ai-providers.md#how-a-connection-is-paid-for)

## A connection's own page

- Select a connection's name to open it. **Overview** shows how it signs in, when its key was set, the address its calls go to, its billing and settings, and the agents whose routes name it.
- **Change** asks, with a second approver, to change its name, settings or **Qualify new models automatically**; until approved the connection stays as it is.

(src: docs/user-guide/ai-providers.md#a-connections-own-page)

## Turn it on or off

- **Ask to turn it on** needs a second approver, and is refused until the connection has its API key (or a hosted server's private endpoint), a model list read whole, and data handling declared with evidence and its data inventory row.
- **Turn it off** takes effect at once with no second approver; each agent moves to its route's next step.

(src: docs/user-guide/ai-providers.md#turn-it-on-or-off)

## Replace the key

- **Replace the key** takes the new API key once, straight to the secret store, never shown again. The old key works until a second approver approves, then for the provider's overlap; a rejected or expired request destroys the new key.

(src: docs/user-guide/ai-providers.md#replace-the-key)

## Its models

- **Models** shows the provider's own list, when it was last read, and **Read it again**; a model the provider stops listing leaves the connection and every route by itself. A list not read for 3 days makes the AI routes check warn ([[checks.overview]]); one not read whole for 14 days carries no personal data or payroll until it is.
- **Probe** sends one small test call. **Ask to allow it** needs a passing probe and a second approver; names come only from the provider's list. **Remove** takes a model off at once.
- Each allowed model shows its price and its source; the Owner can **Confirm it** or **Decline it** when a new price waits for review, **Enter a price** per million input and output tokens (with a second approver) when no source prices it, and **Withdraw your price** at once.

(src: docs/user-guide/ai-providers.md#its-models)

## Qualify a model

- **Qualify** runs an agent's evals against the model and shows how many cases passed; it serves that agent only while its latest run passed.
- **To qualify again** follows a different reported version, or a week after a moving alias was qualified; until then it carries no personal data or payroll.

(src: docs/user-guide/ai-providers.md#qualify-a-model)

## Where its prompts go

- **Data handling** says, per model, endpoint and feature, where the provider processes and keeps what it is sent, whether it trains on it, and the official page or contract showing it; unclear evidence never counts as Singapore.
- **Change** asks, with a second approver, to change the declaration, its data inventory row and its transfer assessment.

(src: docs/user-guide/ai-providers.md#where-its-prompts-go)
