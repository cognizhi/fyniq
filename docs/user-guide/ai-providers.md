---
key: settings.ai-providers
title: Connect the AI to a model provider
mode: expert
roles: [owner, administrator, controller, auditor, dpo]
requirements: [AI-71, AI-72, AI-73, AI-75, AI-78, AI-79, AI-80, AI-81, AI-82, AI-88]
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

## A connection's own page

- Select a connection's name in the list to open its page. **Overview** shows how it signs in, when its key was set,
  the one address its calls go to, its billing and settings, and the agents whose routes name it.
- **Change** asks to change its name, settings or **Qualify new models automatically**. The change waits for a second
  approver; until then the connection stays as it is.

## Turn it on or off

- **Ask to turn it on** needs a second approver. It is refused until the connection has its API key (or, for a server
  your company hosts, its private endpoint registered), the provider's model list has been read whole, and its data
  handling is declared with evidence and its data inventory row.
- **Turn it off** takes effect at once, with no second approver: calls stop going to it, and each agent moves to the
  next step of its route.

## Replace the key

- **Replace the key** takes the new API key once. It goes straight to the secret store and is never shown again. The
  old key keeps working until a second approver approves the new one, then for the provider's overlap; if the request
  is rejected or expires, the new key is destroyed.

## Its models

- **Models** shows the provider's own list: when it was last read, and **Read it again**. Fyniq keeps no list of its
  own, so a model the provider stops listing leaves the connection and every route by itself. A list not read for 3
  days makes the AI routes check warn; one not read whole for 14 days carries no personal data or payroll until it is.
- **Probe** sends one small test call to a model and records whether it answered.
- **Ask to allow it** needs a passing probe and a second approver. Model names only ever come from the provider's
  list; you cannot type one.
- **Remove** takes a model off the allow-list at once.
- Each allowed model shows its price and where the price came from. When a new price waits for the Owner's review,
  the Owner can **Confirm it** or **Decline it**.
- When no source prices a model, or its source's price should not apply, the Owner can **Enter a price** per million
  input and output tokens in the provider's currency; it applies once a second approver approves it. **Withdraw your
  price** takes it away at once.

## Qualify a model

- **Qualify** runs an agent's evals against the model on this connection and shows how many cases passed. A model
  serves an agent only while its latest run for that agent passed.
- A model is shown **To qualify again** when the provider reports a different version, or a week after a moving alias
  was qualified. Until it is qualified again it carries no personal data or payroll.

## Where its prompts go

- **Data handling** says, per model, endpoint and feature, where the provider processes what it is sent, how long it
  keeps it, whether it trains on it, and the official page or contract that shows it.
- Evidence that is unclear never counts as processing in Singapore.
- **Change** asks to change the declaration, its data inventory row and its transfer assessment, with a second approver.
