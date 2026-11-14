---
key: settings.ai-providers
title: Connect the AI to a model provider
mode: expert
roles: [owner, administrator, controller, auditor, dpo]
requirements: [AI-71, AI-72, AI-73, AI-75, AI-79]
---

# Connect the AI to a model provider

## What it is

**Settings → AI → AI providers** (Expert mode) lists where the assistant and the agents send their model calls: each
connection to a model provider, with its sign-in, billing and the data it may receive. Each agent then picks from them
on its route. Every connection starts off.

## Connections

- Each connection shows its provider and the one address its calls go to, whether it is **On** or **Off**, how it is
  paid for, whether its key is set (a key is never shown again), and what it cost this month.
- Adding, changing or turning on a connection, and allowing one of the models its provider lists, needs a second
  approver. A request waiting for one shows at the top, with a link to it.

## Which data may go where

- Know-how and business figures may go to any connection that is on.
- Personal data and payroll go only where processing in Singapore is evidenced, in the connection's data handling.
- Restricted data goes nowhere. NRIC and FIN numbers, full bank account numbers and secrets never leave Fyniq.
- The card also lists the connections the company's policy adds or takes away for each class.

## Add a connection

- Choose a provider, or one of its presets, such as a provider reached through the OpenAI-compatible adapter or a
  model your company hosts, and select **Set up**.
- Give it a name, choose how it signs in, and paste its API key if it uses one: the key goes straight to the secret
  store and is never shown again, logged or sent to a model.
- Choose how it is paid for: **usage-based**, charged per token, or a **subscription**, a fixed fee with a quota that
  resets each day or month. Fill in the provider's settings, or leave one empty for its default.
- **Qualify new models automatically**, off unless you turn it on, runs each model the provider lists for the first
  time through the evals of every agent whose route uses the connection. Adding a model to the allow-list or a route
  stays a person's decision.
- **Ask to add it** sends the request for approval. Once approved, the connection is added, off; its models are read
  from the provider, and it is turned on separately, also with a second approver.

## How a connection is paid for

- **Usage-based**: Fyniq works out each request's cost from the provider's own usage figures and its price.
- **Subscription**: when the quota runs out, the agent moves to the next step of its route or stops; it never
  switches to paying per token on its own.
- **A person's own plan** is not offered yet. Signing in with a consumer chat plan meant for the provider's own apps,
  or reusing a chat session or browser cookie, is not supported.
