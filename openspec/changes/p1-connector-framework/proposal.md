# Proposal

## Why

Fyniq already talks to outside services: ZeptoMail, Resend and SMTP send its email, and Resend receives its
email-in. But each one is wired by hand into the deployment's environment. Changing a provider, its key or
the failover order needs a deployment. Nothing records which data each one sends out or where, and there is no
switch to stop one at once. Bank import (`p1-bank-import-reconcile`: DBS reports, Wise, Airwallex), payment
providers (`p1-payment-providers`: Stripe, HitPay) and AI providers (`p1-ai-providers`) each need such a
service, so the way to plug one in has to exist before they start
(`docs/prd/plans/p1-p3-build-plan-2026-10-16.md`, wave 3).

The owner decided on 25 Oct 2026, interactively, that **the email providers move into the framework** as
installation-wide connectors. The deployment's current settings become their first configuration, with
secrets left in Secret Manager as references. After that, the Super admin manages them in the interface with
step-up, dual control and audit. That proves the framework on adapters that already work, and FR-INT-20 and
FR-INT-21 are met in one way.

## What Changes

- **Connectors and their manifest (FR-INT-25):**
  - every outside service is a connector: an adapter compiled into the release with a manifest declaring its
    id, name, publisher and version;
  - the manifest also declares the capabilities it implements, a configuration schema (JSON Schema), the
    secrets it needs, its provider and Fyniq scopes, the data classes it sends out, its processing regions
    and destinations, the webhooks it registers, a health check and a test action, its local substitute and
    the minimum platform version;
  - per capability, the manifest also declares who may trigger the capability, what happens to queued and
    in-flight work, how secrets rotate and the parser version.
  - Each release ships its **connector catalogue**; `make check` refuses a manifest that does not validate.
- **The framework (FR-INT-20):**
  - one capability interface per kind of service, with interchangeable adapters;
  - Settings › Integrations, where the Super admin manages installation-wide connectors and the Owner or
    Administrator manages organisation-level ones;
  - credentials are write-only and kept in Secret Manager, with only a reference in the database;
  - test the connection, see its health, enable or disable it, and set a failover order;
  - **whether a connector is enabled is checked when each piece of work starts, never from a cached flag**;
  - every change is audited, and dual-controlled: FM-1 through the approval engine for an organisation's
    connectors, and a second Super admin for the installation's once one exists (FR-ADM-07).
- **The lifecycle (FR-INT-26):**
  - install → configure → test → enable → upgrade → pause → disable → uninstall, each step audited and
    dual-controlled, **except pause** (the owner's decision of 25 Oct 2026). One person with step-up can pause
    at once, so a leaked key is contained in seconds; resuming needs the second person;
  - pause holds the queue; disable also cancels the work the manifest marks cancellable and revokes the
    tokens Fyniq issued for the connector;
  - uninstall also deletes the connector's secrets and removes or lists its provider-side webhooks, and its
    history stays;
  - an upgrade shows the manifest diff, and new scopes, data classes or destinations need consent again
    before the connector runs.
- **The rules (FR-INT-27):**
  - a connector writes only through the domain commands the interface uses;
  - each capability names who may trigger it, and a connector approves nothing;
  - each connector runs as its own principal (an `integration` audit actor), with the manifest's rate limits
    and a circuit breaker;
  - when it fails, core work goes on; for email, that means the failover provider.
  - The re-ingestion preview is P2 and stays out.
- **The email providers move in (the owner's decision of 25 Oct 2026; FR-INT-21, FR-INT-22):**
  - **ZeptoMail**, **Resend** (outbound, and inbound for the email-in addresses) and **SMTP** become
    installation-wide connectors;
  - on first start, the deployment's current settings become their configuration: the order, each
    provider's endpoint, the sending domains and their DNS records, the platform's sender, and the secrets as
    references named by the deployment;
  - the worker and the API then read the connectors on each send and each webhook, so the Super admin can
    change a key, the order or a provider's state without a deployment.
  - The capture domain stays the deployment's (it is DNS, not a provider).
  - This supersedes in part rev 0.102's "the deployment's operators set the providers … never through the
    API".
- **Outbound connector traffic** goes to the manifest's declared destinations only. A configured host (SMTP)
  is checked by an egress guard: loopback, private, link-local and metadata addresses are refused, the
  connection uses the checked address, and no redirect is followed. This is the connector part of SEC-36;
  `p1-security-hardening` widens it to every outbound request.
- **Local mode (§12.8, "Local edge, email, messaging and partner substitutes"):**
  - a local secret store under `.data/` that the interface can write to;
  - Mailpit stays the local SMTP connector;
  - a `partners` Compose profile runs Prism mocks from `contracts/partners/`, for the bank and payment
    connectors that follow.
- **The screens** `connector-catalogue` (`/settings/integrations`) and `connector-detail`
  (`/settings/integrations/:connector`), as designed in `docs/design/screens/`, with the email connectors'
  senders, domain checks, failover order and suppression list on the detail.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None (`skip_specs: true`). This change builds:
- `integrations/connectors`: FR-INT-20, FR-INT-25, FR-INT-26 and FR-INT-27 (its P1 part);
- `platform/local-mode`: "Local edge, email, messaging and partner substitutes" (the partner mocks; Mailpit
  is built);
- and moves FR-INT-21's and FR-INT-22's provider configuration (`communication/email-and-notifications`,
  built by `p1-templates-email`) into the framework.

The owner's decisions of 25 Oct 2026 change two requirements:
- FR-INT-21's text: rev 0.102's "never through the API";
- FR-INT-26's "every step … dual-controlled": pause becomes the exception.

PRD-001 rev 0.139 records both. FR-INT-21 in `communication/email-and-notifications` and FR-INT-26 in
`integrations/connectors` each gain a scenario in the same pull request.

## Impact

- **Database:**
  - global tables for the installation's connectors and their changes awaiting a second Super admin;
  - tenant tables for an organisation's connectors (RLS, FORCE);
  - a new approval object type, `connector`;
  - audit events for every lifecycle step.
- **Go:**
  - `internal/connector` (the manifest, the catalogue, the lifecycle and the runtime: enabled checks, rate
    limits, circuit breaker, the egress guard);
  - `connectors/` (one directory per first-party connector: its manifest and its adapter, wrapping
    `internal/adapters/smtp`, `zeptomail` and `resend`);
  - a writable secrets port, `ports.SecretStore`, with a Secret Manager adapter over REST in
    `internal/adapters/gcp` and a file adapter in `internal/adapters/local`;
  - `internal/wiring/mail.go` becomes the first-start bootstrap.
- **API:** the connector catalogue, installed connectors, their lifecycle operations, configuration, secrets
  (write-only), the connection test, health and the failover order. These go in
  `contracts/openapi/fyniq.v1.yaml` and the generated web client.
- **Web:** Settings › Integrations, both screens, the registry, help keys and the user-guide page
  `docs/user-guide/integrations.md`, compiled into `kb/`.
- **Infra:**
  - the API's service account may add secret versions only to the connectors' secrets, and the worker and
    API may read them (`infra/gcp/modules/environment/secrets.tf`);
  - the services get the secrets' names, not their values, for the bootstrap.
  - Upgrade notes tell operators which environment variables stop being read after the first start.
- **Security:**
  - a threat model before code, `docs/security/threat-models/connectors.md` (SEC-20);
  - runtime changes to email secrets are a new surface, guarded by step-up, dual control and the audit trail.
- **Depends on** (all built): the approval engine, Super admins and step-up, the outbox and Cloud Tasks,
  outbound and inbound email (`p1-templates-email`), and the installation's trail.
- **Unblocks:** `p1-bank-import-reconcile`, `p1-payment-providers`, `p1-ai-providers`.
