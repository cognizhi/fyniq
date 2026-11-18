---
key: settings.ai-routes
type: task
title: Choose the model each AI agent runs on, and change an agent's route
summary: Settings › AI › Model routes: each agent's steps, qualification and cost, what needs attention, presets A, B and C, failover, and changing a route
sources:
  - docs/user-guide/ai-routes.md sha256:38aca45efd24ef948bd17dfd1b6f6b49bafbce0f1ef5249ae85c0d435c0af6eb
data_class: general
taint: trusted
status: published
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-11-14
aliases:
  - model route
  - which model does the assistant use
  - AI fallback
  - change the AI model
  - region preset
  - Singapore only AI
---

# Choose the model each AI agent runs on, and change an agent's route

## What it is

- **Settings → AI → Model routes** (Expert mode) shows each AI agent's route: the connection and model it tries first, its fallbacks, and the classes of data it may send. An agent without a route does not run. Connections and models: [[settings.ai-providers]].

(src: docs/user-guide/ai-routes.md#what-it-is)

## The agents and their steps

- Steps in order, **First choice** then **Fallback 1** and on, each with its connection and model, **Qualified**, **To qualify again** or **Not qualified** for that agent, and why it is not serving now.
- Each agent's cost this month in the base currency; a route change waiting for the Owner or a second approver shows at the top with a link ([[approvals.decide]]).

(src: docs/user-guide/ai-routes.md#the-agents-and-their-steps)

## Needs attention

- The AI routes check lists: a route falling back more often than the day's threshold, a moving alias not qualified again this week, a budget or quota within 20% of its limit, a model retiring within 30 days or no longer listed, and a connection with a stored key whose model list has not been read for 3 days ([[checks.overview]]).

(src: docs/user-guide/ai-routes.md#needs-attention)

## Presets

- **A** Singapore only (the default, always for personal data and payroll); **B** the provider's global endpoint; **C** by class of data. A preset fills a route from the connections and models you have; you still review and save it.

(src: docs/user-guide/ai-routes.md#presets)

## How failover works

- A call moves to the next step only if that step may receive every class of data in the call and has what the call needs; otherwise it stops with a plain message and an error code. A model serves an agent only while qualified for it.

(src: docs/user-guide/ai-routes.md#how-failover-works)

## Change an agent's route

- Select the agent's name; tick the classes of data it may send (personal data and payroll only to steps with evidenced Singapore processing).
- Up to five steps, each a connection that is on and a model on its allow-list, with a reasoning effort and caching; **Up**, **Down**, **Remove**, **Fill from preset**.
- **Check it** checks without saving. **Save the route** saves a narrowing change (reordering, removing a step or class, effort, caching) at once; a new connection, model or class waits for the Owner or a second approver and needs every step's model qualified.
- **Versions** lists every version, who made it and when (see also [[settings.ai-activity]]). The daily fallback threshold is in Organisation settings ([[settings.organisation]]); the budget in AI settings ([[settings.ai]]).

(src: docs/user-guide/ai-routes.md#change-an-agents-route)
