# Threat model: chat routing (Slack)

| | |
|---|---|
| Feature and requirement IDs | FR-NTF-04 (Slack and Zoho Cliq), FR-NTF-05 (one event stream), FR-INT-26/27 (connector lifecycle and authority), FR-INT-18 (sandbox); p2-api-third-parties task 7.2; rev 0.621 |
| Author, reviewer, date | Claude Code for the owner, 21 Nov 2026; second review waived before production (AGENTS.md), held with `needs-human-review` |
| Data and its class | The label of a notification type and a link into the web app (internal, no personal data, no amount, no title); the bot token (a secret, in Secret Manager, never in the configuration) |
| Who acts | An Owner or Administrator installs and configures the connector (the connector lifecycle's approvals apply); the platform's fan-out queues, the worker posts |

## 1. What we are building

An organisation connects a private Slack app by its bot token and lists routing rules, each an event type to a channel. When an event is fanned out, one message per rule carries the type's label and a deep link. Trust boundaries: Fyniq to Slack (the egress guard, to `slack.com:443` only, one method and path); the people in the channel to Fyniq (a link opens the web app, where sign-in and step-up apply).

## 2. What can go wrong

| # | Threat | Control | Test |
|---|---|---|---|
| T1 | A message leaks an amount, a name or a title into a workspace outside Singapore | The text is the type's label and a link, built from the catalogue, never from the event's words; the standard level is not offered until the data inventory and the PDPA transfer assessment exist (the manifest's content enum has one value) | `TestChatRoutingRulesSendOnlyTheEventsTheyNameToTheirChannels` |
| T2 | A security, personal-data or payee event goes to chat | Only the types a webhook may carry (operational and approval events, IDs only) are routed; a rule for any other type is ignored | the same test (`security.alert` with a rule) |
| T3 | Chat becomes a way to approve, pay or change records | The port has one method, posting; the client refuses any other method or path; the capability's trigger is the platform's fan-out alone, so a person's request, an AI proposal and an approval are refused by `checkTrigger`; a message only opens the web app | `TestTheMessagingPortHasOneMethod`, `TestTheSlackClientSendsNothingButAMessage` |
| T4 | The token leaks, or goes to another host | The token is a connector secret read through the secret cache, never logged or stored in the configuration; the client is bound to `slack.com:443` by the connector's consent; a rotation keeps the old version 24 hours | the connector framework's tests |
| T5 | A message flood, or a retry storm after a refusal | The connector's rate limit and circuit breaker; a provider's wait holds the row for that long; a refusal fails the row, retried and then dead, which tells the Owner and Administrators once | the same test |
| T6 | A sandbox organisation, or a paused connector, posts | The queuer queues nothing for a sandbox; the outbox ends `chat.send` for one; the runtime refuses it; a paused or disabled connector holds the row | the sandbox tests |
| T7 | A rule makes another organisation's channel receive a message | Rules live in the organisation's own connector row (RLS); the queuer reads only the unit of work's organisation | the same test (another organisation's connector) |

## 3. Left

Zoho Cliq (`[verify]` its channel endpoint), direct messages, batching, the email fallback, the standard content level, a live workspace `[live-verify]`.
