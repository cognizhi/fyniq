# Design

## Context

- **Email is wired by hand today:**
  - `internal/wiring/mail.go` reads `FYNIQ_EMAIL_PROVIDERS` (order), each provider's key and endpoint, the
    platform sender, the sending domains and their DNS records, and the webhook secrets from the environment;
  - Cloud Run mounts each key from Secret Manager as an environment variable
    (`infra/gcp/modules/environment/secrets.tf`, `service_secrets` in `infra/README.md`);
  - the worker builds `[]ports.Email` once at start (`cmd/worker/main.go`), and the API lists the providers'
    names in `getEmailSettings`.
- **Ports and adapters (ADR-T8):**
  - `ports.Email` (Send, Status) and `ports.InboundMail` are the capability interfaces that exist;
  - `internal/adapters/smtp`, `zeptomail` and `resend` implement them over plain HTTP and SMTP;
  - Google Cloud is called over REST from `internal/adapters/gcp` with the service's own token (`rest.go`).
- **Secrets are read-only:**
  - `ports.Secrets.Get(name)` is the only secrets port: `.env` locally (`internal/adapters/local/secrets.go`);
  - production has no Secret Manager adapter, because secrets arrive as environment variables;
  - OpenTofu creates each secret and never its value.
- **Dual control:**
  - the approval engine (`internal/domain/approval`, FM-1, signed envelopes, FR-APR-10) covers an
    organisation's changes, keyed by object type and operation;
  - Super admin dual control (FR-ADM-07) is not built: an installation has at most two Super admins "until
    dual control arrives" (`internal/api/superadmins.go`);
  - step-up (a second factor in the last 5 minutes) is checked by the API (`approvals.go`).
- **Audit:**
  - the audit actor has the types user, service, integration, scheduler and ai
    (`internal/ports/storage/audit.go`);
  - installation-wide changes are recorded in the installation's trail (`Installation.InstallationOrganisation`).
- **Background work:** sends go through the outbox and Cloud Tasks (ADR-T7); a send is reconciled before any
  retry or failover (FR-INT-21, G4-073).
- **The Appendix D row** "Organisation integrations": Owner A (with a second approver), Administrator C.
- **No egress client yet:** SEC-36 and QA-SEC-04 are planned for `p1-security-hardening`.

## Goals / Non-Goals

**Goals:**
- One way to plug in, configure, test, switch and audit an outside service, proven on the three email
  providers that already work, with no gap in sending while they move over.
- A disabled or paused connector stops new work at once, on every instance, without a deployment.
- No secret value in the database, the API's answers, the audit trail, the logs or the browser after it is
  typed.
- An organisation-level connector (bank data next) needs only its manifest, its adapter and its tests.

**Non-Goals:**
- Third-party and out-of-process connectors (FR-INT-28, P3) and in-process plug-ins (ADR-T8 "not a
  commitment").
- The re-ingestion preview (FR-INT-27's P2 part).
- Sign-in providers (FR-ADM-08), OAuth clients and MCP access: they keep their own switches (FR-INT-20's list).
  The catalogue shows sign-in as managed in its own settings.
- Messaging, ticketing, Peppol and document-AI connectors: their own changes.
- SEC-36 for every outbound request (OIDC discovery, generic webhooks) and SEC-37 private endpoints:
  `p1-security-hardening`. This change guards connector traffic only.
- Super admin dual control for anything other than connectors. The mechanism is general, so announcements and
  Super admin changes can adopt it later.

## Decisions

- **First-party connectors are compiled in, each with a JSON manifest.**
  - `connectors/<id>/manifest.json` is embedded with `go:embed`. A small Go file in the same directory binds
    the manifest's capabilities to an adapter in `internal/adapters/<provider>/`, so cloud and vendor code
    stays where NFR-PORT-03 puts it.
  - A JSON Schema for manifests (`contracts/connectors/manifest.schema.json`) validates every manifest in
    `make check` and at start; the schema is the format FR-INT-28's third parties will use.
  - The catalogue is the set compiled into the release.
  - *Alternatives:*
    - Go structs as manifests: not reviewable as data, and no published format.
    - YAML: a second format for the same JSON Schema.
    - Loading connectors at runtime: the plug-in ADR-T8 does not commit to.
- **The manifest declares more than FR-INT-25's list, per capability:**
  - who may trigger it (a human authority, a system rule or nobody, FR-INT-27);
  - what pause and disable do to queued and in-flight work (hold, cancel or reconcile);
  - how each secret rotates (an overlap in hours, during which both versions are accepted);
  - the parser version;
  - the destinations (host patterns, or "configured" for a host the person types, such as SMTP's);
  - rate limits (requests a minute) and circuit-breaker thresholds.
  - The email manifests say queued sends are **held**, never cancelled: a message a person asked to send is
    not dropped. Their trigger is the system rule "a send the outbox holds".
- **Data model:**
  - `fyniq.installation_connector` (global, in `globalTables`) and `fyniq.organisation_connector` (tenant,
    PK `(organisation_id, id)`, FORCE RLS) hold, per installed connector:
    - its manifest id;
    - the consented manifest's version and SHA-256;
    - its state (`installed`, `configured`, `tested`, `enabled`, `paused`, `disabled`, `uninstalled`);
    - its configuration (non-secret, validated against the manifest's schema);
    - its secret references (name → store reference, pinned to a version);
    - its position in each capability's order;
    - the last test result (pass or fail and the reason only);
    - its health, and a version for `ETag`.
  - A trigger enforces the state machine (new FY codes). An uninstalled row is kept and never reused; a new
    install is a new row. Every change is audited in the same transaction (AUD-01).
  - *Alternative:* one table with a nullable `organisation_id`. Rejected: RLS on a nullable tenant key is the
    kind of exception §11.4 forbids.
- **Enabled is read at the start of each piece of work, never cached (FR-INT-20, G4-096):**
  - `connector.Runtime.For(ctx, capability)` reads the enabled installations, in order, in the job's own
    transaction, then resolves their secrets;
  - the email job calls it per attempt, and the webhook handlers per request;
  - a paused or disabled connector is skipped, so failover moves to the next enabled one.
  - With none enabled, a held job is re-queued with a delay, and the delay uses none of its attempts.
  - An attempt already handed to a provider is reconciled through that provider's `Status`, as today, even if
    the connector was paused since (in-flight work finishes or is reconciled).
  - Webhooks are accepted while the connector is enabled or paused (they report on work already done) and
    refused once it is disabled or uninstalled.
- **Secrets are references, values live in Secret Manager:**
  - a new port, `ports.SecretStore`:
    - `Put(name, value)` returns a reference pinned to the new version;
    - `Get(ref)`;
    - `Disable(ref)` and `Destroy(ref)`;
  - production: a Secret Manager adapter over REST in `internal/adapters/gcp` (addVersion, access, disable,
    destroy) under the service's own account;
  - locally: a file store in `.data/secrets/` (0600, git-ignored) in `internal/adapters/local`, in the local
    flavour only (DEV-07).
  - Values are cached in memory by their pinned reference only. A version never changes, so a cache cannot
    serve a stale secret: replacing a secret changes the reference.
  - **Installation connectors** write to secrets OpenTofu creates, one per manifest secret
    (`connector-<id>-<secret>`). The API's account may add, disable and destroy versions of those secrets
    only, and the worker and API may read them.
  - **Organisation connectors** create their secret at install (`org-<org>-connector-<id>-<secret>`) under a
    custom role whose IAM condition limits `resource.name` to that prefix. `[verify]` that Secret Manager
    honours a `resource.name` condition on `secrets.create`; if it does not, organisation secrets are created
    by a narrowly scoped job instead. No organisation connector ships in this change, so this is proven with
    the first one (`p1-bank-import-reconcile`).
  - *Alternatives:*
    - KMS-encrypted values in the database: against FR-INT-20's wording and the owner's "never copied into the
      database".
    - Environment variables as today: they change only with a deployment.
- **Rotation without downtime:**
  - a replaced secret is a new version referenced by the pending change;
  - on approval, the connector switches to it;
  - the old version stays enabled for the manifest's overlap (webhook keys: 24 hours, during which both
    verify), and is then disabled and destroyed by a job;
  - a rejected or expired change destroys its new version.
- **Dual control, two routes:**
  - **Organisation connectors** use the approval engine: object type `connector` (its key is the
    installation's id), operation `connector.change`, whose change names the step (install, configure,
    replace a secret, enable, pause, resume, disable, uninstall, order, consent). The FM-1 policy is Owner and
    a second approver, as Appendix D's row says.
  - **Installation connectors** get `fyniq.installation_change`, a global table that is the Super admins'
    counterpart:
    - the change and an RFC 8785 envelope with its SHA-256 (the approval engine's canonical form, reused);
    - the requester and their step-up;
    - the decider, who must be a different active Super admin with step-up;
    - a status and a 7-day expiry.
  - With one active Super admin, a change applies at once with step-up, is audited, and is marked "single
    Super admin" in the trail (FR-ADM-07: dual control "once a second Super admin exists").
  - **Pause is the one exception** (the owner's decision of 25 Oct 2026, taken interactively). It applies at
    once for one person (Super admin, Owner or Administrator) with step-up, is audited, and notifies the other
    Super admin or the Owners. Pause stops new work and holds the queue, so a leaked key is contained in
    seconds and nothing is lost. Resume, disable (which cancels queued work and revokes tokens) and every
    other step wait for the second person.
  - *Alternative:* make Super admins approvers in the installation organisation's approval engine. Rejected:
    Super admin is not an organisation role (FR-ADM-07), and the installation organisation's members would
    become approvers of installation changes.
- **Upgrades never widen what a running connector does:**
  - the installation keeps the manifest it consented to;
  - when a release ships a newer manifest, the catalogue shows the diff;
  - a version with no new scopes, data classes, destinations or webhooks runs under the existing consent too;
    the catalogue shows it as an upgrade that asks nothing new, and the record moves to it through the consent
    step like any other upgrade. *Changed while building (task 5.4):* the service no longer records such an
    upgrade itself on its first run, which would need a database function that skips the Super admin check;
  - a widening version keeps running under the old consent: the egress guard allows only the consented
    destinations, and the adapter receives the consented data classes and leaves what needs more turned off,
    until the Owner (or the Super admin) consents through the dual-controlled `consent` step.
  - *Alternative:* pause a widening connector at deployment. Rejected: a release could stop email without
    anyone choosing to.
- **Connector principal, rate limits, circuit breaker (FR-INT-27):**
  - work a connector does is audited as `integration:connector/<installation id>`, on behalf of the person
    or rule that triggered it;
  - rate limits and the breaker are per instance (token bucket; open after the manifest's consecutive
    transport failures, half-open after its cool-down). An open breaker counts as "not accepted" only for
    sends never handed over, so failover stays safe.
  - *Alternative:* shared limits in PostgreSQL. Not needed while the providers enforce their own limits and
    the instance count is small; noted as a risk.
- **Egress guard for connector traffic (the connector part of SEC-36; QA-SEC-04):**
  - `internal/egress` gives connectors an `http.Client` and an SMTP dialer that resolve the name and refuse
    loopback, private, link-local, unique-local and metadata addresses and the platform's internal names;
  - it dials the checked address, follows no redirect, and limits time and response size;
  - a URL in a connector's configuration must match a consented destination; "configured" destinations
    (SMTP) rely on the guard alone;
  - the local flavour allows loopback for its substitutes (Mailpit, Prism) only;
  - a test action returns pass or fail with a reason, never the provider's body.
  - `p1-security-hardening` moves every other outbound request onto it.
- **Bootstrap from the deployment (the owner's decision of 25 Oct 2026):**
  - on start, the API takes an advisory lock, and if no outbound-email connector was ever installed, it
    installs the providers `FYNIQ_EMAIL_PROVIDERS` names, enabled, in that order;
  - their configuration comes from the same variables as today;
  - their secrets come as references from `FYNIQ_CONNECTOR_SECRETS`: JSON of `<connector>.<secret>` →
    Secret Manager secret name, pinned to its latest version at bootstrap, never the values;
  - the bootstrap is audited by the service actor as "from the deployment's configuration";
  - afterwards those variables are ignored, and the API logs a warning once if they still differ;
  - the worker no longer builds providers at start.
  - Local mode bootstraps SMTP to Mailpit the same way.
  - *Alternative:* a one-off migration command. Rejected: it adds a deployment step that is easy to forget,
    and a fresh installation would have no email until someone ran it.
- **The capture domain, the platform sender and the sending domains:**
  - the capture domain stays a deployment setting (it is DNS that infra owns);
  - the platform sender and the sending domains stay the deployment's settings, shared by every email
    connector so failover sends from the same domains. *Changed while building (task 6.3):* they were to become
    installation settings the Super admin edits; they are bound to the deployment's DNS like the capture domain,
    and the owner's decision named the providers, their keys and their order, so they stay where they are;
  - each provider's DNS records are in its own configuration, and the nightly `email.domains` job reads them
    from there.
- **API (FR-INT-01, §8.18.1):**
  - `GET /v1/connector-catalogue`;
  - `/v1/installation/connectors` and `/v1/organisation/connectors`: list (cursor-paginated), get, install,
    configuration (`PUT` with `If-Match`), a write-only secret (`PUT …/secrets/{secret}`, `204`), test,
    enable, pause, resume, disable, uninstall, consent;
  - `PUT /v1/installation/connector-order/{capability}`;
  - `/v1/installation/changes/{id}` approve and reject;
  - every write takes `Idempotency-Key` and answers `202` with the pending change when a second person must
    decide;
  - RFC 9457 errors with new `CONNECTOR-nn` codes;
  - no answer ever carries a secret, only whether one is set and when it was replaced.
- **Audit events (AUD-01):**
  - `connector.installed`, `.configured`, `.secret_replaced` (the reference, never the value), `.tested`,
    `.enabled`, `.paused`, `.resumed`, `.disabled`, `.uninstalled`, `.upgraded`, `.consented` and
    `.order_changed`;
  - `installation_change.requested`, `.approved`, `.rejected` and `.expired`;
  - the approval engine's own events for organisation changes.
- **Screens:**
  - `connector-catalogue` and `connector-detail` as designed: Expert mode;
  - visible to Super admins (installation section) and to Owners and Administrators (organisation section);
  - the email tabs (senders, domain checks, failover order, suppressions) call the existing email operations.
- **Local partner mocks:**
  - a `partners` Compose profile runs Prism (pinned by digest) over every OpenAPI document in
    `contracts/partners/`;
  - `contracts/partners/README.md` says each connector adds its partner's document with the connector, so
    this change ships the profile and an empty-safe start, and bank import adds Wise and Airwallex.

## Risks / Trade-offs

- [A Super admin can now change email keys at runtime] → step-up, the second Super admin's approval once
  there are two, write-only fields, audit with the secret's reference, a notification to every Super admin,
  and the threat model's abuse cases tested.
- [The bootstrap misreads the deployment and email stops] → the bootstrap is idempotent and audited. A test
  boots from today's variables and sends through every provider's fake. Until the bootstrap succeeds, the
  worker keeps using the deployment's variables, so a failed bootstrap degrades to today's behaviour, never
  to no email.
- [Reading Secret Manager on each send adds latency and cost] → values are cached by their pinned version;
  only the enabled state is read each time, from PostgreSQL.
- [Per-instance rate limits allow N times the limit across N instances] → the providers enforce their own
  limits, and the manifest's limit is set per instance with headroom; shared limits come if a connector
  needs them.
- [The IAM condition on secret creation may not work as expected] → `[verify]` before the first organisation
  connector, with a narrowly scoped job as the fallback. Installation secrets need no runtime creation.
- [A release widens a connector's manifest] → it runs under the old consent until someone consents; the
  release checklist lists widening manifests.
- [Two Super admins disagree, or one is away, and a change waits] → pause never waits. Every other change
  expires in 7 days, so nothing hangs forever.
- [One person pauses everyone's email] → it is audited and notified, the failover provider keeps sending if
  it is still enabled, and resuming takes a second person, so a pause done in error is visible and reversed
  the same day.

## Migration Plan

1. Deploy with the new tables and code. The API's bootstrap installs the email connectors from the
   deployment's variables, and the worker reads them from the next send.
2. Operators add `FYNIQ_CONNECTOR_SECRETS` before the deployment (Upgrade notes), and OpenTofu grants the new
   Secret Manager roles.
3. After a successful bootstrap (the catalogue shows the providers enabled), operators may remove
   `FYNIQ_EMAIL_PROVIDERS`, the keys, URLs and DNS variables from `service_env` and `service_secrets`.
   Leaving them does nothing.
4. Rollback: the previous release still reads the variables, so rolling back before step 3 restores today's
   behaviour; the new tables are left in place (expand and contract).

## Open Questions

- Which Super admin notification channel carries "a connector was paused by one person" when the
  other is away: the notification centre today, with email as a fallback once the email connectors
  themselves may be the thing that is paused. The notification design does not change either way.
