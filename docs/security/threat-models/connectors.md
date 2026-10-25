# Threat model: connectors, their secrets and their traffic

| | |
|---|---|
| Feature and requirement IDs | The connector framework: manifests and the catalogue (FR-INT-25), Settings › Integrations with write-only credentials, tests, health, enable and disable and the failover order (FR-INT-20), the lifecycle with dual control (FR-INT-26), the connector rules (FR-INT-27), the egress guard for connector traffic (SEC-36, QA-SEC-04); the email providers moved in as installation-wide connectors and pause as the one single-person step (the owner's decisions of 25 Oct 2026, PRD-001 rev 0.139; FR-INT-21, FR-INT-22) |
| Author, reviewer, date | Claude Code for the owner, 25 Oct 2026; second review waived before production (AGENTS.md), held with `needs-human-review` if wanted |
| Data and its class | The providers' keys and webhook secrets (secrets); connector configuration (endpoints, sending domains, DNS records: internal); the email that flows through the connectors (personal and confidential business data, as `email-delivery.md` lists); the audit trail of connector changes (internal, append-only) |
| Who acts | The Super admin (installation-wide connectors; FR-ADM-07); the Owner and the Administrator (an organisation's connectors; Appendix D "Organisation integrations"); the API (configuration, webhooks, bootstrap); the worker (sends through the connectors); the jobs (rotation clean-up, the nightly domain check); the providers (webhooks from the internet); the deployment's operators (OpenTofu, environment) |

## 1. What we are building

Every outside service is a connector: an adapter compiled into the release with a manifest that declares
what it does, which secrets it needs, the data it sends out and the destinations it may reach. A Super admin
installs, configures, tests, enables, pauses, disables and uninstalls the installation's connectors, and
orders the email ones, in Settings › Integrations. An Owner or Administrator does the same for an
organisation's (bank data, from `p1-bank-import-reconcile`). A secret typed into the interface goes
straight to Secret Manager as a new version. The database keeps only a reference pinned to that version,
and no answer ever returns the value.

Changes wait for a second person: the other Super admin for installation connectors (once there are two),
and the approval engine (FM-1) for an organisation's. Pause is the exception: one person with step-up pauses
at once. The worker reads the enabled connectors at the start of every send. The API reads a connector's
webhook secret on every request. On first start, the API turns the deployment's email settings into the
email connectors, with references to the secrets the deployment names.

Trust boundaries: browser ↔ API (lifecycle, configuration, secrets in); API ↔ Secret Manager (put, disable,
destroy versions); API and worker ↔ database (RLS on organisation connectors; global tables written only
through Super-admin functions); worker ↔ provider (send, over the egress guard); provider ↔ API (webhooks,
from the internet); release ↔ installation (a new manifest arriving with a deployment).

## 2. What can go wrong (STRIDE per boundary)

| # | Boundary or step | Threat (STRIDE) | Impact | Likelihood |
|---|---|---|---|---|
| T1 | Browser → API | Elevation: a stolen Super admin session replaces the email provider's key with the attacker's, or points the connector at the attacker's account | Every email the platform sends, including sign-in and security mail, goes through the attacker | Medium: the reason this surface is new |
| T2 | Browser → API | Elevation: one Super admin (or one Owner) changes a connector alone where dual control applies, or approves their own change | Dual control bypassed | Medium |
| T3 | Browser → API | Tampering / replay: a pending change is approved after it was edited, or an approval is replayed against another change | The approver signs something other than what runs | Low |
| T4 | API → database | Elevation: an organisation's Administrator reads or changes another organisation's connector, or an installation connector | Cross-tenant control of an outside service | Low |
| T5 | API, logs, audit, browser | Information disclosure: a secret value is returned by an answer, written to the database, a log, the audit trail, an error or the browser's storage, or shown again in the form | Keys leaked; mail or bank data reachable by whoever reads them | Medium: many places a value could leak |
| T6 | Configuration → worker | Spoofing / SSRF: a configured endpoint or SMTP host points at the metadata service, loopback, a private address or an internal service, directly, by redirect or by a DNS name that changes between check and connect | The service account's token or internal services reached from a connector | Medium: SMTP's host is typed by a person |
| T7 | Connector test → browser | Information disclosure: the test action echoes what the destination answered | An internal service's response (or a provider's account data) shown in the browser | Medium without a control |
| T8 | Release → installation | Elevation: a release ships a manifest that sends a new data class, asks a new scope or reaches a new destination, and the running connector starts doing it without anyone consenting | Data leaves for a new place without the Owner's decision (FR-INT-26) | Low: first-party code, but easy to miss |
| T9 | Worker ↔ database | Repudiation / tampering: a disabled or paused connector still sends because the worker cached its state, or a disable drops queued or in-flight mail silently | Mail sent after an emergency stop, or lost without anyone knowing (G4-096) | Medium |
| T10 | Browser → API | Denial of service: one person pauses everyone's email, or a pending change never decided leaves a connector stuck | Email stops until someone acts | Medium: deliberate, since pause is single-person |
| T11 | Provider → API | Spoofing: a webhook for a disabled or uninstalled connector, or signed with a rotated-out secret, is accepted | Forged bounces suppress customers' addresses | Low |
| T12 | API → Secret Manager | Elevation: the API's service account can read or write any secret in the project, not only the connectors' | A bug or a compromised API reaches the database password or the setup code | Medium without least privilege |
| T13 | Deployment → API | Tampering: the first-start bootstrap reads the deployment wrongly, runs twice, or overwrites what a Super admin set later | Email stops, or a Super admin's change is undone by a restart | Medium |
| T14 | Connector → domain | Elevation: a connector writes through a private path, or work started by the AI or an MCP client reaches a connector without the human authority behind it | The never-list bypassed through a connector (FR-INT-27, AI-66) | Low |
| T15 | Database | Tampering: a connector's history (installed, enabled, key replaced) is rewritten or deleted to hide a change | Lost evidence of who changed an outside service | Low |
| T16 | Secret rotation | Denial of service / disclosure: a rotation breaks webhooks mid-flight, or the old version is never destroyed | Lost events, or a leaked old key still valid | Medium |

SOD: an installation change is requested by one Super admin and approved by the other; an organisation's by
its Administrator or Owner and approved under FM-1. No money moves through a connector in this change. RLS:
`organisation_connector` has `organisation_id` and `FORCE ROW LEVEL SECURITY`, and the installation tables are
written only through functions that check an active Super admin. The AI and MCP have no route to any
operation here.

## 3. What we do about it

| Threat | Control | Test that proves it (`// req:` and `// asvs:`) |
|---|---|---|
| T1 | Replacing a secret, configuring, enabling, resuming, ordering and every other step except pause needs step-up in the last 5 minutes and, once there are two Super admins, the other one's approval; every change notifies every Super admin; the audit trail names the requester, the approver and the secret's reference; configured endpoints must match the manifest's consented destinations, so a key cannot be pointed at an arbitrary host | `TestConnectorsThroughTheAPI` (a stale step-up and a person who is not a Super admin refused; the founder told of the other's pause, of each decision, and, alone, of each change), `TestInstallationConnectorsGoThroughTheLifecycle`, `TestWideningManifestRunsUnderTheOldConsent` (a host the manifest did not consent to is refused) (`// req: FR-INT-20`, `// req: FR-ADM-07`, `// asvs: V2.3.5`) |
| T2 | The decider must be a different active Super admin (installation) or hold the approve right under the FM-1 policy (organisation); the database refuses a decision by the requester; with one Super admin, a change applies at once and is marked "single Super admin" in the trail | `TestInstallationConnectorsKeepTheirLifecycle` and `TestInstallationConnectorsGoThroughTheLifecycle` (deciding or approving one's own change refused; one Super admin applies at once), `TestOrganisationConnectorsThroughTheAPI` (an Administrator asks, an Owner approves) (`// asvs: V2.3.5`) |
| T3 | Each pending change is an RFC 8785 envelope with its SHA-256, which the database recomputes; the decision names the envelope's hash; a change decided, expired or replaced cannot be decided again | `TestInstallationConnectorsKeepTheirLifecycle` (another envelope's hash, deciding twice, applying twice or after a newer version, deciding an expired change, and reopening a decided one are each refused), `TestConnectorsThroughTheAPI` (a wrong hash) (`// asvs: V2.3.3`) |
| T4 | Organisation connectors are tenant rows under FORCE RLS; installation tables are global, written only through Super-admin functions (an organisation role is refused); the API checks the Super admin or the Appendix D row before the store does | `TestOrganisationConnectorsKeepTheirLifecycle` (another organisation sees none of its connectors or secrets), `TestConnectorsThroughTheAPI` (CodeNotSuperAdmin), `TestTenantKeysIndexesAndForeignKeys`, `TestEachRoleHoldsExactlyItsGrants` and the matrix rows (`// req: INV-06`, `// asvs: V8.4.1`) |
| T5 | Secret fields are `writeOnly` in the manifest's schema and the API contract; values go to `SecretStore.Put` and nowhere else; each installed connector keeps its manifest's secret names and the database refuses a configuration that holds one (`*_connector_no_secret`); answers carry only "set" and "replaced at"; the audit trail and logs carry the reference, never the value (SEC-15); the web form never shows a stored value | `TestConnectorsThroughTheAPI` and `TestOrganisationConnectorsThroughTheAPI` (a key is never answered, logged or in the trail), the `*_connector_no_secret` attacks in `TestInstallationConnectorsKeepTheirLifecycle` and `TestOrganisationConnectorsKeepTheirLifecycle`, `TestBootstrapFromTheDeployment` (references, never values), the web form's write-only field in `Integrations.test.tsx` (`// req: SEC-15`, `// asvs: V13.3.1`) |
| T6 | Connector traffic goes through `internal/egress`: it resolves the name, refuses loopback, private, link-local, unique-local and metadata addresses and the platform's internal names, dials the address it checked, follows no redirect, and limits time and response size; the local flavour allows loopback for its substitutes only | `TestEgressRefusals` (metadata, loopback, private and internal names), `TestEgressDialsTheAddressItChecked` (a name that changes between check and connect), `TestEgressFollowsNoRedirectAndBoundsTheAnswer` (`// req: SEC-36`, `// req: QA-SEC-04`, `// asvs: V1.3.6`, `// asvs: V15.3.2`) |
| T7 | A test action returns pass or fail and a reason Fyniq wrote, never the destination's body | `TestTestActionNeverReturnsTheBody` (`// req: SEC-36`, `// asvs: V15.3.1`) |
| T8 | The installation keeps the manifest it consented to; a widening release runs under the old consent (destinations and data classes) until the dual-controlled consent step; the catalogue shows the diff; the release checklist lists widening manifests | `TestWideningManifestRunsUnderTheOldConsent` (the new host refused until consent; the diff shows what the release asks), `TestPlanFollowsTheLifecycle` (the consent step) (`// req: FR-INT-26`, `// asvs: V13.2.4`) |
| T9 | Enabled is read in each job's own transaction at its start, never cached; a paused or disabled connector is skipped, and with none enabled the job is held, not dropped; an attempt already handed over is reconciled with its provider; disable cancels only what the manifest marks cancellable (email: nothing) | `TestEmailGoesThroughTheConnectorsEnabledAtEachSend` (a paused primary skipped, every connector off holds the message, an earlier attempt reconciled), `TestTheRuntimeReadsTheConnectorsAtEachPieceOfWork`, `TestTheMailOfAPieceOfWorkIsTheEnabledConnectorsInOrder`, `TestOrganisationConnectorsGoThroughTheLifecycle` (disable cancels only what the manifest allows) (`// req: FR-INT-20`, `// req: QA-SEC-04`, `// asvs: V2.3.2`) |
| T10 | Pause is audited and notifies the other Super admins (through the relay into their organisations) or the organisation's Owners and Administrators at once; the failover provider keeps sending while it is enabled; resume needs the second person; a pending change expires after 7 days | `TestConnectorsThroughTheAPI` (pause at once with two Super admins, resume waits, the others told), `TestOrganisationConnectorsThroughTheAPI` (pause at once, the Owner told, the one who paused not), `TestEmailGoesThroughTheConnectorsEnabledAtEachSend` (the failover sends), the expiry in `TestInstallationConnectorsKeepTheirLifecycle` (`// req: FR-INT-26`) |
| T11 | Webhooks read the connector at each request: accepted while it is enabled or paused, refused once disabled or uninstalled; during a rotation's overlap both versions verify, then only the new one | `TestTheConnectorsDecideTheWebhookKeysProvidersAndRecords` (no key once disabled; both keys in a rotation's overlap), `TestWebhookKeysAreReadForEachRequest` (`ErrOff` answers 404) (`// req: FR-INT-21`, `// asvs: V13.3.4`) |
| T12 | OpenTofu creates one secret per installation connector secret; the API's account may add, disable and destroy versions of those only, and the API and worker may read them; no project-wide Secret Manager role; organisation secrets live in a dedicated secrets project per environment, where the API's account is the only manager (the owner's decision of 25 Oct 2026, PRD-001 rev 0.140; `p1-bank-import-reconcile` task 8.1) | The environment module's `tofu test` run `connector_secrets_follow_their_manifests`, the infra rules in `make infra-check`, `TestSecretManagerConformance` (`// req: SEC-12`, `// asvs: V13.3.2`) |
| T13 | The bootstrap runs under an advisory lock only when no outbound-email connector was ever installed, so it runs once and never overwrites a later change; it stores references to the secrets the deployment names, never values; until it succeeds, the worker keeps the deployment's settings | `TestBootstrapFromTheDeployment` (runs once, waits before setup, nothing more reaches Secret Manager), `TestEmailGoesThroughTheConnectorsEnabledAtEachSend` (the deployment's providers send until the bootstrap has run) (`// req: FR-INT-21`) |
| T14 | Connectors call the same domain commands as the interface; each capability names its trigger (a human authority, a system rule or nobody) and the runtime refuses work whose trigger does not match; a connector approves nothing; the never-list test covers connectors | `TestTriggerMustMatchTheManifest` (never on an AI proposal alone), `TestConnectorWorkIsAuditedAsItsOwnPrincipal`; the never-list test arrives with the AI tool surface (`// req: FR-INT-27`, `// req: AI-66`) |
| T15 | Connector rows change only through the state machine and their history is append-only; uninstalled rows are kept; every step is audited in the same transaction, in the installation's trail or the organisation's | `TestInstallationConnectorsKeepTheirLifecycle` and `TestOrganisationConnectorsKeepTheirLifecycle` (deleting, renaming, re-pointing a secret version, reopening or deleting a change refused; every step's event), `TestPlanFollowsTheLifecycle` (`// req: AUD-01`, `// asvs: V16.3.3`) |
| T16 | A replaced secret is a new version; the old one stays enabled for the manifest's overlap and is then disabled and destroyed by a job; a rejected or expired change destroys its version | `TestTheConnectorsDecideTheWebhookKeysProvidersAndRecords`, `TestInstallationConnectorsGoThroughTheLifecycle` (a rejected key destroyed; a replaced key kept for its overlap, then destroyed), `TestRefusedRequestsLoseTheirNewKeys`, `TestAfterRollbackRunsOnlyWhenNothingCommits` (`// req: FR-INT-26`, `// asvs: V13.3.4`) |

## 4. What remains

- Rate limits and the circuit breaker are per instance: N instances allow N times a manifest's limit. The
  providers enforce their own limits; shared limits come if a connector needs them.
- Google does not document that a `resource.name` condition limits `secrets.create`, so organisation
  secrets do not rely on one: they live in a dedicated secrets project (PRD-001 rev 0.140), built with the first
  organisation connector (`p1-bank-import-reconcile`).
- A Super admin with step-up can still replace the email key alone while they are the only Super admin
  (FR-ADM-07). The setup wizard asks for a second Super admin; until then, every connector change sends them a
  security notice and is in the installation's trail.
- A Super admin hears of connector changes only in an organisation they belong to: one who joined by
  invitation and belongs to none sees the changes in Settings › Integrations, not in a notification centre.
- The never-list test (T14) arrives with the AI tool surface; until then no AI or MCP route reaches a
  connector operation, and `TestTriggerMustMatchTheManifest` refuses work on an AI proposal alone.
- Webhooks registered by hand in a provider's dashboard cannot be removed by Fyniq on uninstall; the
  connector lists them for the operator to remove.
- SEC-36 for every other outbound request (OIDC discovery, generic webhooks) and SEC-37 private endpoints
  stay with `p1-security-hardening`.
