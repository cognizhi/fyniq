---
key: settings.integrations
type: task
title: Install, configure, test and run connectors, such as the email providers
summary: Connectors and the catalogue: install, configure, secrets, test, enable, pause, upgrade, approve; email senders; Stripe and HitPay payment providers
sources:
  - docs/user-guide/integrations.md sha256:2e2a6e4563e1eabdae17a6f4797987976e9333086247992fe164c552d172e965
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@2
compiled_at: 2026-11-19
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
  - Stripe
  - HitPay
  - payment provider
  - payout
  - dispute
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
- If the provider says when a key or certificate expires, give that day in **Expires on**: it is kept with that value,
  never changes, and Books health warns 14 days before it ([[checks.overview]]). Give a new date with each new value.

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

## Payment providers (Stripe and HitPay)

- The Stripe and HitPay connectors read your provider account into Banking, and never charge, refund, retry or pay out.
  For Stripe, use a restricted key that can only read, and give Fyniq the signing secret of the webhook that points to the
  connector's address. Add a money account of kind **Payment gateway** for each currency, named by the currency, with the
  connector as its data source.
- The provider's balance transactions come in as the account's lines, a charge and its fee on lines of their own. Every
  hour Fyniq also reads the provider's payouts and balance, going back four days to make up for a webhook that never came.
- **Reconcile now** posts a fee to the fee account (5040), a dispute's money to the disputes account (1055 is the usual
  one) and a payout as a transfer to the bank, which the bank's own line completes, within the S$500 limit for a single
  line: a larger payout waits for you, with what it settles listed. A charge waits for its invoice. Fees carry no claimable
  GST until the provider's monthly tax invoice is matched.
- Someone with a right on bank connections names the bank for payouts, the fee account and the disputes account; this is the payment providers API, and Payouts (Banking, Expert mode, [[banking.payouts]]) shows the result.
- Nothing is posted for a lost dispute by itself: its amount stays in the disputes account. A person with a right on journals asks for a draft journal that writes it off to the dispute losses account, once per dispute, and posts it like any other journal. Up to S$100 that is all; above it the person confirms that the invoice's revenue stays as invoiced and says why. When the invoice is known, the person credits it instead: the credit note is issued under Sales, then its credit is settled against the dispute on Payouts (Banking, Expert mode), clearing against the money the dispute holds, once per dispute and in the company's currency; a journal never touches accounts receivable by hand. What stays held, the dispute fee, is written off with a draft journal. The credit note's GST is for the accountant to confirm.
- A person who has dealt with a set-aside event marks it done, with a reason.
- Checks: a settled payout with what it settles comes to nothing in the provider's account; what the books hold equals what
  the provider holds for you; a sweep that fell behind shows with the other bank syncs. A webhook of an unknown type is kept
  and set aside for a person, and posts nothing.

(src: docs/user-guide/integrations.md#payment-providers-stripe-and-hitpay)

## Tasks from other systems

- A ticketing system or another connected system makes tasks, each with its own reference and a link back, which show in the Inbox under Tasks from other systems, apart from the compliance tasks.
- The system that made a task finishes it, and a finished task stays finished. An Owner or an Administrator can also mark one done or cancel it in the Inbox, for good; ask the system for a new one rather than reopening.
- A task never approves anything, marks no obligation met and changes no record; do not put personal data or amounts in one.

(src: docs/user-guide/integrations.md#tasks-from-other-systems)

