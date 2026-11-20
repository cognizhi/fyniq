---
key: settings.webhooks
type: task
title: Send events to another system with webhooks
summary: Turning webhooks on, adding and verifying an endpoint, what a message carries, retries and dead letters, pausing, rolling the secret
sources:
  - docs/user-guide/webhooks.md sha256:6371c3e04170d3f9c12467bcaf33555d80501c06d74a5e5e36118ad0264b5e45
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-sonnet-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@2
compiled_at: 2026-11-21
aliases:
  - webhooks
  - webhook endpoint
  - send events to another system
  - event notifications to my own software
  - dead letter
  - replay a webhook
  - roll the webhook secret
---

# Send events to another system with webhooks

## Find it

- Webhooks are off until an Owner turns on **Webhooks and events** under Settings › Organisation › Features, with a second approver; turning it off takes effect at once and stops every delivery.
- An Administrator adds and runs endpoints under Settings › Integrations › Webhooks, with a passkey or authenticator code each time. Controllers, Auditors and the DPO can see them.

(src: docs/user-guide/webhooks.md#find-it)

## What is sent

- A message carries IDs and times, never names, amounts or titles: your system reads the details through the API with its own rights.
- Endpoints choose event types by name, never "everything". Security events, personal-data and payee events, a person's own reminders and investor updates are never sent.
- Each message is a CloudEvents 1.0 message signed as Standard Webhooks (`webhook-id`, `webhook-timestamp`, `webhook-signature`); the id stays the same on every try so a message already handled can be ignored, and a timestamp more than five minutes from the receiver's clock should be rejected.

(src: docs/user-guide/webhooks.md#what-is-sent)

## Add an endpoint

1. **Add endpoint**: a name, an https address on a public host, and the event types.
2. Copy the signing secret now: Fyniq keeps it sealed and cannot show it again; roll it for a new one.
3. **Verify**: Fyniq sends one signed message, `fyniq.webhook.verify.v1`, with a challenge; the endpoint answers 2xx with the challenge in its body and becomes **active**. Until then it receives nothing.

Fyniq never sends to a private, loopback or link-local address, never follows a redirect, and gives the endpoint 10 seconds to answer.

(src: docs/user-guide/webhooks.md#add-an-endpoint)

## When the endpoint fails

- A non-2xx answer or a failed connection is tried again after 5 seconds, 5 minutes, 30 minutes, 2 hours, 5 hours, 10 hours, then every 12 hours, for three days.
- After three days a delivery is **dead**; **Dead letters** lists them and **Replay** tries one again at once with the same id.
- Ten deliveries failing in a row pause the endpoint and an Administrator is told; an endpoint answering 410 Gone is disabled. Every try is logged.

(src: docs/user-guide/webhooks.md#when-your-endpoint-fails)

## Change an endpoint

- **Pause** and **Disable** stop deliveries at once; a new address makes the endpoint pending again until it is verified.
- **Roll the secret** keeps the old secret signing for 24 hours beside the new one, so each message carries two signatures meanwhile.
- Only a person signed in to Fyniq creates or changes an endpoint, never an API token, integration client or AI app; every change is in the audit trail, never the secret.

(src: docs/user-guide/webhooks.md#change-an-endpoint)
