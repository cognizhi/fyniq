---
key: settings.webhooks
title: Send events to another system with webhooks
mode: expert
roles: [owner, admin]
requirements: [FR-INT-12]
status: draft            # written with the webhooks of p2-api-third-parties; the screen follows the API
---

# Send events to another system with webhooks

## Find it

- Webhooks are off until an Owner turns on **Webhooks and events** under **Settings › Organisation › Features**, with a second approver. Turning it off takes effect at once and stops every
  delivery.
- An Administrator adds and runs endpoints under **Settings › Integrations › Webhooks**, and needs a passkey or authenticator code each time. Controllers, Auditors and the DPO can see them.

## What is sent

- A webhook is a message Fyniq sends to a web address of yours when something happens, such as a document posted or a period closed. It carries **IDs and times, never names, amounts or
  titles**: your system reads the details through the API with its own rights.
- You choose the event types for each endpoint by name; there is no "everything". Security events, personal-data and payee events, a person's own reminders and investor updates are never sent.
- Each message is a CloudEvents 1.0 message signed as [Standard Webhooks](https://www.standardwebhooks.com/): the headers `webhook-id`, `webhook-timestamp` and `webhook-signature`. The id stays the
  same on every try, so your system can ignore a message it has already handled. Reject a timestamp more than five minutes from your own clock.

## Add an endpoint

1. Choose **Add endpoint**, give it a name, an https address on a public host, and the event types.
2. Copy the **signing secret** now. Fyniq keeps it sealed and cannot show it again; roll it to get a new one.
3. Choose **Verify**. Fyniq sends one signed message, `fyniq.webhook.verify.v1`, whose data holds a challenge; your endpoint answers with a 2xx and the challenge in its body. Only then is the endpoint
   **active**. Until it is, it receives nothing.

Fyniq never sends to a private, loopback or link-local address, never follows a redirect, and gives your endpoint 10 seconds to answer.

## When your endpoint fails

- Any answer other than 2xx, and a connection that fails, is tried again: after 5 seconds, 5 minutes, 30 minutes, 2 hours, 5 hours, 10 hours, then every 12 hours, for three days.
- After three days the delivery is **dead**. **Dead letters** lists them; **Replay** tries one again at once, with the same id.
- Ten deliveries failing in a row **pause** the endpoint and an Administrator is told. Resume a paused endpoint after you have fixed it.
- An endpoint that answers **410 Gone** is **disabled**.
- Every try is logged with its time, the answer's status and why it failed.

## Change an endpoint

- **Pause** and **Disable** stop deliveries at once. A new address makes the endpoint **pending** again until it is verified.
- **Roll the secret** gives a new secret and keeps the old one signing for 24 hours alongside it, so each message carries two signatures while you change over.

## Rules to know

- Only a person signed in to Fyniq creates or changes an endpoint; no API token, integration client or AI app can.
- Every change is in the audit trail, never the secret.
