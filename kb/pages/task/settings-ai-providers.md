---
key: settings.ai-providers
type: task
title: Connect the AI to a model provider, and see which data may go where
summary: Settings › AI › AI providers: the connections, their status, billing and key, which data may go where, and adding one with a second approver
sources:
  - docs/user-guide/ai-providers.md sha256:d84f13e914c62d97d5fbfacdb47d20061a1e3f7875af5b633cab8406cd549b22
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
---

# Connect the AI to a model provider, and see which data may go where

## What it is

- **Settings → AI → AI providers** (Expert mode) lists where the assistant and the agents send their model calls: each connection to a model provider, with its sign-in, billing and the data it may receive; each agent then picks from them on its route. Every connection starts off. The AI's switch and budgets: [[settings.ai]].

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

- **Usage-based**: each request's cost from the provider's usage figures and its price. **Subscription**: when the quota runs out the agent moves to its route's next step or stops, never switching to paying per token by itself.
- A person's own plan is not offered yet; a consumer chat plan meant for the provider's own apps, or a reused chat session or browser cookie, is not supported.

(src: docs/user-guide/ai-providers.md#how-a-connection-is-paid-for)
