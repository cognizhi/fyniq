---
key: settings.ai-routes
title: Choose the model each AI agent runs on
mode: expert
roles: [owner, administrator, controller, auditor, dpo]
requirements: [AI-26, AI-74, AI-76, AI-78, AI-80, AI-81]
---

# Choose the model each AI agent runs on

## What it is

**Settings → AI → Model routes** (Expert mode) shows, for each AI agent, the route it runs on: the connection and model
it tries first, the fallbacks it may use when the first cannot serve, and the classes of data it may send. An agent
without a route does not run.

## The agents and their steps

- Each agent shows what it does, the data it may send, and its steps in order: **First choice**, then **Fallback 1**
  and on. Each step shows its connection and model, whether the model is **Qualified** for that agent, **To qualify
  again** or **Not qualified**, and why it is not serving now, if it is not.
- The agent's cost this month is shown beside it, in the organisation's base currency.
- A route change waiting for the Owner or a second approver shows at the top, with a link to it.

## Needs attention

- The AI routes check lists what needs a person before it stops an agent: a route that fell back more often than the
  day's threshold, a moving model alias not qualified again this week, a budget or a subscription's quota within 20%
  of its limit, a model on a route retiring within 30 days or no longer listed, and a connection with a stored key
  whose model list has not been read for 3 days.

## Presets

- **A**: Singapore only. It is the default, and always used for personal data and payroll.
- **B**: the provider's global endpoint.
- **C**: by class of data.
- A preset fills a route from the connections and models you have; you still review and save it.

## How failover works

- When a step cannot serve, the call moves to the next step only if that step may receive every class of data in the
  call and has what the call needs. Otherwise it stops with a plain message and an error code.
- A model serves an agent only while it is qualified for that agent.

## Change an agent's route

- Select the agent's name to open its route. Tick the classes of data it may send on the route; personal data and
  payroll go only to steps whose processing in Singapore is evidenced.
- Add up to five steps. For each, choose a connection that is on and a model on its allow-list (names come only from
  the provider's list), a reasoning effort and whether to cache repeated instructions. Move steps with **Up** and
  **Down**, or **Remove** one. **Fill from preset** fills the steps from a preset.
- **Check it** checks the route without saving it. **Save the route** saves a change that only narrows where data goes
  (reordering, removing a step or a class, the effort or caching) at once. A new connection, model or class waits for
  the Owner or a second approver, and needs every step's model qualified for the agent.
- **Versions** lists every version of the route, who made it and when.
- The daily fallback threshold is in Organisation settings; the agent's budget is in AI settings.
