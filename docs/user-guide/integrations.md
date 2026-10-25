---
key: settings.integrations
title: Integrations and connectors
mode: expert
roles: [owner, admin, controller, auditor, dpo, super_admin]
requirements: [FR-INT-20, FR-INT-21, FR-INT-25, FR-INT-26, FR-INT-27, SEC-36]
status: draft
---

# Integrations and connectors

## What it is

A connector links Fyniq to another service, such as an email provider. Each connector comes with a manifest:
who publishes it, what it sends out of Fyniq, where the provider processes it, and the only places it may reach.

- Open **Settings → Integrations** (Expert mode).
- **Installed connectors** lists each one with its status, its health, its secrets and when it last changed.
- **Connector catalogue** lists the connectors this release ships, by kind of service.
- Some connectors are **installation-wide**: the email providers (ZeptoMail, Resend and SMTP) send everyone's email.
  The Super admins manage them.
- Others belong to an organisation. An Administrator asks to install one, and an Owner approves it. This release
  ships no organisation connector yet; the bank connectors come later.
- The Owner, the Administrator, the Controller, the Auditor and the data protection officer can see the page.

## What a connector sends out

- **Sends out** names the kinds of data the manifest allows: for example personal data (names and email
  addresses) and business figures (invoice totals and PDFs). A connector sends nothing else.
- **Processed in** names where the provider processes it, such as the United States. "Where its server is" means
  a server you choose, such as your own mail server.
- **Destinations** are the only hosts it may reach. Every request goes through Fyniq's checked outbound client,
  which refuses any other host, an internal address and a redirect.

## Install a connector

1. In **Connector catalogue**, choose **Install** next to the connector.
2. Read its manifest in the dialog, then choose **Install** (or **Ask to install** for an organisation connector).
3. Confirm it's you with your passkey.

Installing records your consent to the manifest and leaves the connector off. With two Super admins, the other
one approves an installation connector first. An Owner who did not ask approves an organisation connector.

## A connector's page

Choose a connector's name to open its page.

- The **Lifecycle** shows how far it has come: **Install**, **Configure**, **Test**, **Enable**.
- **Health** and the **Manifest** are beside it.
- **Configuration** holds its settings and secrets.
- **Activity** shows its recent work. For an email provider, that is the messages it sent.
- **Audit** shows its history: every request about it, and who decided it.
- When a change waits for a second person, a banner says so. Nothing changes until they decide.

## Configure it and set its secrets

1. Open **Configuration**. The form comes from the connector's manifest. Each field says what it holds, and an
   empty field uses the default it names.
2. Choose **Save changes**. A second person approves the new configuration. A changed configuration is tested
   again before the connector is enabled.
3. Under **Secrets**, type each secret and choose **Set**, or **Replace** for one that is already set.

A secret goes straight to Secret Manager. Nobody can see it again: not you, not a Super admin, and not the page.
A replaced secret stays valid for a while, so work already in progress still passes; each secret says for how
long. If the change is rejected, the new value is destroyed.

## Test it

- Choose **Test connection**. It runs the connector's test action: an email provider sends one message to the
  organisation's own email address, from its profile.
- The result is **Passed** or **Failed**, with the reason. It never shows what the provider sent back.
- A test needs no second person. A passed test moves a configured connector to **Tested**.

## Health

- **OK**: it is enabled and its last test passed.
- **Failing**: its last test failed. The reason says what to fix; fix it, then test again.
- **Not tested**: it is not enabled, or has not been tested.

## Turn it on or off

- **Enable** asks to turn it on. It needs a passed test and every secret set.
- **Pause** stops new work at once and holds its queue. It takes effect without a second person, and the
  others are told. **Resume** asks to pick up where it stopped.
- **Disable** asks to turn it off. What happens to its queued work is in its manifest: held until it is enabled
  again, or cancelled. Work already handed to the provider finishes or is reconciled; it is never dropped.
- Every action asks for your passkey. Every step but a pause and a test waits for a second person.

## Uninstall it

- **Uninstall** asks to remove the connector for good. Once approved, its secrets are deleted, and its audit
  history stays.
- Some webhooks are set by a person in the provider's dashboard. The page then lists them; remove them there.

## Upgrade and consent

- When a release ships a newer manifest, **Upgrades** counts it, and the connector's page shows **Upgrade**.
- The diff marks what is new with a plus, and what goes away with a minus.
- When the new manifest asks for nothing new, **Consent to the upgrade** moves the connector to it.
- When it asks for more, such as a new data class, destination or scope, the connector keeps running under the
  old consent until you consent and a second person approves.

## Approve or reject a change

- With two Super admins, each change to an installation connector waits for the other. It is listed under
  **Installation changes waiting**, and on the connector's page.
- Choose **Approve**, or **Reject** with a reason. Either asks for your passkey.
- You can't approve your own change. A change you don't decide expires after a week.
- An organisation connector's request is approved in the **Inbox**, like any other approval.

## Email senders, domains and suppressions

An email provider's page also has **Senders and domain**.

- **Senders by purpose**: the address each kind of email comes from (notifications, invoices and quotes,
  reminders, security). **Change** sets an address on a sending domain, or goes back to the platform's sender.
- **Domain authentication**: each sending domain's DNS records and its DMARC policy. They are checked within the
  hour and every night. **Weak** means DMARC only reports: set it to quarantine.
- **Failover order**: the first provider sends. The next one sends only what the ones before it refused; a send
  with an unknown result is checked first, so nobody gets an email twice. **Move** changes a provider's place.
- **Suppression list**: addresses Fyniq doesn't send to, after a hard bounce or a spam complaint. Fix the
  contact's email first, then choose **Release**.

## Related

- [Approve a change](approvals.md)
- [Sign in to Fyniq](sign-in.md)
- [Notifications and banners](notifications.md)
