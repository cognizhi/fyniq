---
key: settings.integrations
type: task
title: Install, configure, test and run connectors, such as the email providers
summary: Connectors and the catalogue; install, configure, set secrets, test, enable, pause, upgrade, approve changes; email senders, domains, failover, suppressions
sources:
  - docs/user-guide/integrations.md sha256:e79fadc5c4ef308cccb9451270a1fdfca8337285e1718208593bc22ed7b37aa3
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-25
aliases:
  - connector
  - integration
  - email provider
  - ZeptoMail
  - Resend
  - SMTP
  - install a connector
  - test connection
  - pause a connector
  - connector secret
  - manifest
  - failover
  - suppression list
  - DMARC
---

# Install, configure, test and run connectors, such as the email providers

## What it is

- A connector links Fyniq to another service, such as an email provider. Its manifest says who publishes it, what it
  sends out of Fyniq, where the provider processes it, and the only places it may reach.
- **Settings → Integrations** (Expert mode): **Installed connectors** (status, health, secrets, last change) and the
  **Connector catalogue** this release ships, by kind of service.
- The email providers (ZeptoMail, Resend and SMTP) are installation-wide: the Super admins manage them. An
  organisation's own connector is asked for by an Administrator and approved by an Owner; none ships yet.
- The Owner, the Administrator, the Controller, the Auditor and the data protection officer can see the page.

(src: docs/user-guide/integrations.md#what-it-is)

## What a connector sends out

- **Sends out**: the kinds of data the manifest allows, such as personal data and business figures; nothing else.
- **Processed in**: where the provider processes it; "where its server is" means a server you choose.
- **Destinations**: the only hosts it may reach, through Fyniq's checked outbound client, which refuses any other host,
  an internal address and a redirect.

(src: docs/user-guide/integrations.md#what-a-connector-sends-out)

## Install a connector

- **Install** next to it in the catalogue, read its manifest, choose **Install** (or **Ask to install** for an
  organisation connector), and confirm it's you with your passkey.
- Installing records your consent and leaves it off. The other Super admin approves an installation connector; an
  Owner who did not ask approves an organisation connector.

(src: docs/user-guide/integrations.md#install-a-connector)

## A connector's page

- **Lifecycle**: **Install**, **Configure**, **Test**, **Enable**; with **Health** and the **Manifest** beside it.
- **Configuration**, **Activity** (an email provider's sent messages) and **Audit** (every request and who decided it).
- A banner says when a change waits for a second person; nothing changes until they decide.

(src: docs/user-guide/integrations.md#a-connectors-page)

## Configure it and set its secrets

- The form comes from the manifest; an empty field uses the default it names. **Save changes** waits for a second
  person, and a changed configuration is tested again before the connector is enabled.
- Under **Secrets**, **Set** (or **Replace**) sends the value straight to Secret Manager: nobody can see it again. A
  replaced secret stays valid for a while, as each secret says; a rejected change destroys the new value.

(src: docs/user-guide/integrations.md#configure-it-and-set-its-secrets)

## Test it

- **Test connection** runs the test action: an email provider sends one message to the organisation's own email
  address, from its profile. The result is **Passed** or **Failed** with the reason, never what the provider sent back.
- A test needs no second person; a passed test moves a configured connector to **Tested**.

(src: docs/user-guide/integrations.md#test-it)

## Health

- **OK**: enabled and its last test passed. **Failing**: its last test failed; fix what the reason says, then test
  again. **Not tested**: not enabled, or not tested.

(src: docs/user-guide/integrations.md#health)

## Turn it on or off

- **Enable** needs a passed test and every secret set. **Pause** stops new work at once and holds its queue, without
  a second person; the others are told. **Resume** picks up where it stopped.
- **Disable**: the manifest says whether queued work is held or cancelled; work already handed to the provider
  finishes or is reconciled, never dropped.
- Every action asks for your passkey; every step but a pause and a test waits for a second person.

(src: docs/user-guide/integrations.md#turn-it-on-or-off)

## Uninstall it

- **Uninstall** removes it for good once approved: its secrets are deleted and its audit history stays. Webhooks a
  person set in the provider's dashboard are listed on the page, to remove there.

(src: docs/user-guide/integrations.md#uninstall-it)

## Upgrade and consent

- A newer manifest shows under **Upgrades** and as **Upgrade** on the page, with a plus for what is new and a minus
  for what goes away.
- Asking nothing new, **Consent to the upgrade** moves it on. Asking more (a data class, destination or scope), it
  runs under the old consent until you consent and a second person approves.

(src: docs/user-guide/integrations.md#upgrade-and-consent)

## Approve or reject a change

- With two Super admins, each installation change waits under **Installation changes waiting** and on the
  connector's page: **Approve**, or **Reject** with a reason, each with your passkey.
- You can't approve your own change; an undecided one expires after a week. An organisation connector's request is
  approved in the **Inbox** ([[approvals.decide]]).

(src: docs/user-guide/integrations.md#approve-or-reject-a-change)

## Email senders, domains and suppressions

- **Senders by purpose** (notifications, invoices and quotes, reminders, security): **Change** sets an address on a
  sending domain, or the platform's sender.
- **Domain authentication**: DNS records and DMARC, checked within the hour and every night; **Weak** means DMARC
  only reports, so set it to quarantine.
- **Failover order**: the first sends, the next only what the ones before it refused; an unknown result is checked
  first, so nobody gets an email twice. **Move** changes a provider's place.
- **Suppression list**: addresses not sent to after a hard bounce or a spam complaint; fix the contact's email, then
  **Release**. How notifications reach people: [[settings.notifications]].

(src: docs/user-guide/integrations.md#email-senders-domains-and-suppressions)
