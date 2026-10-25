# Tasks

Each slice is one pull request, merged when green. Tests carry `// req:` tags (QA-TRACE-01); every new
✖ or ⚠ message is triggered by a fixture (QA-RPT-02). The threat model comes before any code that touches
secrets or connector traffic (SEC-20).

## 1. Before code

- [x] 1.1 The threat model `docs/security/threat-models/connectors.md`:
  - assets: provider keys, webhook secrets, the email path;
  - actors: a Super admin, an Owner, an Administrator, an attacker with a stolen session, a malicious
    configured host;
  - abuse cases: replace a key alone, point a connector at the metadata service, read a secret back, replay a
    pending change, resume a paused connector alone, widen a manifest by release;
  - each case is mapped to its control and the test that proves it.
  - Verified by review in the pull request, before the first code slice merges.
  - Refs `// req: SEC-20`, FR-INT-20, FR-INT-26, SEC-36.
  (25 Oct 2026: `docs/security/threat-models/connectors.md`, T1 to T16, each mapped to its control and the test of
  tasks 3 to 7 that proves it)

## 2. Manifests and the catalogue

- [x] 2.1 The manifest format:
  - `contracts/connectors/manifest.schema.json` with every field of FR-INT-25 and the per-capability fields
    of `design.md` (trigger, queued and in-flight work, secret rotation overlap, parser version,
    destinations, rate limit, breaker);
  - `internal/connector` loads and validates embedded manifests, and refuses a manifest whose configuration
    schema is not valid JSON Schema or whose secrets, capabilities or destinations are malformed.
  - Tests `TestEveryManifestValidates`, `TestManifestRefusals`. `// req: FR-INT-25`
- [x] 2.2 The first-party manifests and bindings:
  - `connectors/smtp`, `connectors/zeptomail` and `connectors/resend` (outbound, and inbound for Resend), each
    a `manifest.json` and a binding to `internal/adapters/<provider>`;
  - destinations as the providers document them (SMTP "configured");
  - queued sends held on pause and disable, never cancelled;
  - their local substitutes (Mailpit for SMTP; fakes for ZeptoMail and Resend).
  - Tests: contract tests of each binding against its fake through the capability interface.
    `// req: FR-INT-20` (every adapter has a local substitute and contract tests), `// req: FR-INT-25`
- [x] 2.3 `make check` validates every manifest against the schema, and the catalogue lists exactly the
  connectors compiled in.
  - Tests `TestTheCatalogueIsTheCompiledSet`. `// req: FR-INT-25` (connector catalogue)
  (25 Oct 2026, tasks 2.1 to 2.3: `contracts/connectors/manifest.schema.json`; `internal/connector` (`Parse` refuses an
  unknown field and reports every problem at once, `Validate` checks a configuration and refuses a secret in it, the
  canonical `Hash`, the `Catalogue` that checks each binding builds exactly its manifest's capabilities, `LocalFlavour`);
  `connectors/smtp`, `zeptomail` and `resend` with their manifests and bindings, and `connectors.Catalogue`. The
  manifests validate in `make test`, not `make check`: CI's `make check` job has no Go toolchain, and the JSON Schema
  check is a Go test (`TestEveryManifestValidates`). Tests: `TestEveryManifestValidates`, `TestManifestRefusals` (the
  schema refuses what the platform refuses), `TestValidateConfiguration`, `TestManifestHashIsCanonical`,
  `TestTheCatalogueChecksItsBindings`, `TestTheCatalogueIsTheCompiledSet`, `TestTheEmailConnectorsSendThroughTheirFakes`,
  `TestResendReceivesThroughItsFake`, `TestTheSMTPConnectorSpeaksPlainTextOnlyInTheLocalFlavour`)

## 3. Secrets

- [x] 3.1 `ports.SecretStore` (Put returns a reference pinned to a version; Get, Disable, Destroy):
  - a shared contract suite in `internal/ports/porttest`;
  - the local file adapter under `.data/secrets/` (0600; local flavour only, DEV-07);
  - an in-memory cache keyed by the pinned reference.
  - Tests: the porttest suite on the local adapter, `TestReleaseBuildHasNoLocalSecretStore` (archtest).
    `// req: FR-INT-20` (credentials kept in Secret Manager, write-only), `// req: DEV-07`
- [x] 3.2 The Secret Manager adapter over REST in `internal/adapters/gcp` (addVersion, access, disable,
  destroy) with the service's own token.
  - Tests: the porttest suite against a fake Secret Manager server; no client library imported (archtest).
    `// req: FR-INT-20`, `// req: NFR-PORT-03`
- [x] 3.3 Infra:
  - one secret per installation connector secret (`connector-<id>-<secret>`);
  - the API's account may add, disable and destroy versions of those only, and the API and worker may read
    them;
  - the services get `FYNIQ_CONNECTOR_SECRETS` (names, never values);
  - `infra/README.md` updated.
  - Tests: the environment module's `tofu test`, and the infra rule check (no project-wide Secret Manager
    role) in `make infra-check`. `// req: SEC-12`
  - The organisation-secret IAM condition stays `[verify]` until `p1-bank-import-reconcile`'s first
    organisation connector (`design.md`).
  (25 Oct 2026, tasks 3.1 to 3.3: `ports.SecretStore` and `ports.SecretRef`, with `porttest.SecretStore`;
  `local.SecretFiles` (0600 files under `.data/secrets`, a tombstone so a version number is never reused; local flavour
  only, which `scripts/images.py` proves for every release image rather than a new archtest); `gcp.SecretManager` over
  REST (addVersion and access with CRC32C, the enabled versions listed for `Latest`, disable and destroy idempotent, a
  disabled or destroyed version told apart by its state), touching only the secrets `FYNIQ_CONNECTOR_SECRETS` names
  (and later organisation prefixes); `connector.SecretCache` by pinned reference for `SecretTTL`. Infra: the
  environment module reads `connectors/*/manifest.json`, makes each installation connector secret its own secret or the
  existing one `connector_secret_sources` names (so today's email keys are referenced, never copied), grants
  `secretAccessor` to the services each manifest lists and `secretVersionManager` to the api on those secrets only, and
  gives the api, worker and jobs `FYNIQ_CONNECTOR_SECRETS`. Tests: `TestSecretFilesConformance`,
  `TestSecretManagerConformance`, `TestSecretManagerChecksTheCRC`, `TestSecretCacheReadsEachVersionOncePerTTL`, the
  module's `connector_secrets_follow_their_manifests`, `make infra-check`)

## 4. The installed connector, its lifecycle and dual control

- [x] 4.1 Migrations and storage:
  - migrations: `installation_connector` (global, in `globalTables`), `organisation_connector` (tenant, FORCE
    RLS) and `installation_change` (global);
  - the state machine trigger and append-only history, with new FY codes in `db/README.md`;
  - the storage port and its postgres adapter, sqlc;
  - goldens updated (schema, grants, tenant).
  - Tests: attack tests for every illegal transition, a row of another organisation, an update of an
    uninstalled row, and a secret value in a configuration column (refused by a check on the schema's
    `writeOnly` keys). `// req: FR-INT-26`, `// req: INV-06`
  (25 Oct 2026: migrations 000141 and 000142: the global `installation_connector`, `installation_connector_secret` and
  `installation_change`, written only through SECURITY DEFINER functions that check an active Super admin and, with
  two, an approved change for every step but pause (`apply_installation_change`, FY213), the bootstrap only into an
  empty catalogue (FY214); the tenant `organisation_connector` and `organisation_connector_secret`; `connector_guard`
  (FY205 to FY207), the secret guards (FY208), the change guard and decision (FY209 to FY212); a configuration refused
  when it holds one of the manifest's secret names, which each row keeps; the approval object `connector`. The
  `storage.Connectors` port and its adapter, auditing every step in the same transaction. Tests:
  `TestInstallationConnectorsKeepTheirLifecycle`, `TestOrganisationConnectorsKeepTheirLifecycle`, the schema, grants and
  tenant goldens)
- [x] 4.2 The lifecycle in `internal/connector`:
  - install, configure (validated against the manifest's schema), replace a secret, test, enable, pause,
    resume, disable, uninstall, order, consent;
  - disable cancels what the manifest marks cancellable, and holds the rest;
  - uninstall destroys the secrets, and removes the provider-side webhooks where the manifest registers them
    or lists them for the operator;
  - audit events of `design.md`, each in the same transaction.
  - Tests `TestConnectorLifecycle`, `TestDisableCancelsOnlyWhatTheManifestAllows`,
    `TestUninstallDestroysSecretsAndKeepsHistory`, `TestEveryStepIsAudited`. `// req: FR-INT-26`,
    `// req: AUD-01`
  (25 Oct 2026: `connector.Plan`, the pure planner of every step with its plain-word refusals `CONNECTOR-01` to `-08`;
  `connector.Installations` (install, step, pause, test, approve, reject) and `connector.Organisations` (install, apply,
  put a secret); disable runs the `Canceller` of each capability whose manifest cancels its queued work; uninstall lists
  the webhooks a person removes by hand, and its secret versions are destroyed by `connector.Cleanup` (scheduled with
  task 4.4); a version put for a unit of work that rolls back is destroyed with it (`UnitOfWork.AfterRollback`, new).
  Tests: `TestPlanFollowsTheLifecycle`, `TestInstallationConnectorsGoThroughTheLifecycle`,
  `TestOrganisationConnectorsGoThroughTheLifecycle` (the canceller), `TestAfterRollbackRunsOnlyWhenNothingCommits`, and the
  adapter's audit checks in `TestInstallationConnectorsKeepTheirLifecycle`)
- [x] 4.3 Dual control:
  - organisation connectors through the approval engine: object `connector`, operation `connector.change`,
    the FM-1 policy of Appendix D's "Organisation integrations" row;
  - installation connectors through `installation_change`: an RFC 8785 envelope, a decider who is another
    active Super admin with step-up, a 7-day expiry;
  - with one Super admin, a change applies at once with step-up and is marked so;
  - **pause applies at once for one person with step-up and notifies the others** (the owner's decision of
    25 Oct 2026); resume and every other step wait for the second person.
  - Tests `TestOrganisationConnectorChangesNeedASecondApprover`, `TestInstallationChangesNeedTheOtherSuperAdmin`,
    `TestOneSuperAdminAppliesAtOnce`, `TestPauseAppliesAtOnceAndResumeWaits`, `TestPendingChangeCannotBeReplayed`.
    `// req: FR-INT-20` (changes audited and dual-controlled), `// req: FR-INT-26`, `// req: FR-ADM-07`
  (25 Oct 2026, the installation half: `connector.Installations` requests a change once there are two Super admins, for
  every step but pause and test, with an RFC 8785 envelope; `Approve` decides and applies in one unit of work and refuses a
  change asked of an older version; `Reject` destroys the new secret version a change carried. Proved by
  `TestInstallationConnectorsGoThroughTheLifecycle` and the database's `TestInstallationConnectorsKeepTheirLifecycle`.
  The organisation half, with task 7.1: the approval engine's object `connector` and operation `connector.change`
  (dependencies: the installed connector's version and state, or whether it is installed; Appendix D's "Organisation
  integrations" row; the founders' FM-1 policy), executed through `connector.Organisations` on the final approval.
  Proved by `TestOrganisationConnectorsThroughTheAPI` and `TestOrganisationConnectorsGoThroughTheLifecycle`)
- [x] 4.4 Secret rotation:
  - a replaced secret is a new version referenced by the pending change, and switched on approval;
  - the old version stays enabled for the manifest's overlap (both verify webhooks), then is disabled and
    destroyed by a job;
  - a rejected or expired change destroys its version.
  - Tests `TestRotationOverlapAcceptsBothWebhookKeys`, `TestRejectedChangeDestroysItsSecretVersion`.
    `// req: FR-INT-26` (secrets rotated without downtime)
  (25 Oct 2026: the overlap from each manifest secret's `rotation_overlap_hours`, `connector.Cleanup` destroying replaced
  versions past their overlap, every version of an uninstalled connector and a rejected or expired change's version,
  Secret Manager first, then the database. Proved by `TestInstallationConnectorsGoThroughTheLifecycle`. Scheduled hourly
  at :50 as the platform job `connectors.cleanup` with the wiring of task 6.1. The overlap's webhook check comes with
  task 6.2's webhooks)

## 5. The runtime

- [x] 5.1 `connector.Runtime.For(ctx, capability)`:
  - reads the enabled installations in order, at the start of each piece of work, never cached;
  - paused and disabled connectors are skipped, so failover moves on;
  - with none enabled, a job is re-queued, and the wait uses none of its attempts;
  - an attempt already handed over is reconciled through its provider even after a pause.
  - Tests `// req: FR-INT-20` G4-096 (`TestDisabledConnectorCommitsNothingNew`, with queued and in-flight work),
    `TestPausedConnectorHoldsItsQueue`, `TestFailoverSkipsAPausedConnector`. `// req: QA-SEC-04` (its
    connector half)
  (25 Oct 2026, the runtime: `connector.Runtime` reads the enabled installation connectors in each piece of work's unit of
  work, never cached; `Mail` gives the enabled adapters in order and every installed one (enabled, paused or disabled) by
  name to reconcile an earlier attempt; a connector whose secret cannot be read is skipped with the reason. Proved by
  `TestTheMailOfAPieceOfWorkIsTheEnabledConnectorsInOrder` and `TestTheRuntimeReadsTheConnectorsAtEachPieceOfWork`.
  The hold: migration 000143's `hold_outbox` and `outbox.ErrHeld`, which give back the attempt
  (`TestHeldWorkSpendsNoAttempt`); the email job holds a message when no email connector is enabled and reconciles an
  in-flight attempt with the connector it went to, disabled since: G4-096 in
  `TestEmailGoesThroughTheConnectorsEnabledAtEachSend`)
- [x] 5.2 The egress guard `internal/egress`:
  - an HTTP client and an SMTP dialer that refuse loopback, private, link-local, unique-local and metadata
    addresses and internal names;
  - they dial the checked address, follow no redirect, and limit time and size;
  - a configured URL must match a consented destination;
  - the local flavour allows loopback for its substitutes only;
  - a test action returns pass or fail with a reason only.
  - Tests `TestEgressRefusals` (the metadata address, loopback, private ranges, an internal name, a redirect
    to one, a name that changes between check and connect), `TestTestActionNeverReturnsTheBody`.
    `// req: SEC-36` (connector traffic), `// req: QA-SEC-04`
  (25 Oct 2026: `internal/egress`, `connector.Allows` on the consented destinations, the SMTP adapter's `Dial` hook and
  `Runtime.TestEmail`. Tests: `TestEgressRefusals`, `TestEgressDialsTheAddressItChecked`,
  `TestEgressFollowsNoRedirectAndBoundsTheAnswer`, `TestTestActionNeverReturnsTheBody`)
- [x] 5.3 The connector principal, rate limits and circuit breaker:
  - work is audited as `integration:connector/<id>` on behalf of its trigger;
  - per-instance limits and a breaker from the manifest;
  - an open breaker counts as "not accepted" only for sends never handed over.
  - Tests `TestConnectorWorkIsAuditedAsItsOwnPrincipal`, `TestBreakerFailsOverOnlyWhatWasNeverHandedOver`.
    `// req: FR-INT-27`
  (25 Oct 2026: `connector.Principal`; a per-instance token bucket and breaker per installed connector in
  `connector.Runtime`, a held send a refusal (`ErrHeld`), reconciliation never held. Tests: the two named above)
- [x] 5.4 Upgrades and consent:
  - the installation keeps its consented manifest;
  - a non-widening release runs under it, and its first run is recorded as `connector.upgraded`;
  - a widening release runs under the old consent (destinations and data classes) until the dual-controlled
    consent step;
  - the manifest diff is computed for the screen.
  - Tests `TestWideningManifestRunsUnderTheOldConsent`, `TestManifestDiff`. `// req: FR-INT-26` (upgrade shows
    the manifest diff; new scopes or data classes need consent)
  (25 Oct 2026: each installed connector keeps its `consent` (migration 000141), the runtime binds its traffic to it, and
  `connector.Diff` and `Widens` show what a release asks. Tests: `TestWideningManifestRunsUnderTheOldConsent`, which also
  checks the diff, and `TestPlanFollowsTheLifecycle`'s consent step)
- [x] 5.5 Who may trigger a capability: the runtime refuses work whose trigger does not match the manifest's
  (a human authority, a system rule or nobody), and a connector approves nothing.
  - Tests `TestTriggerMustMatchTheManifest`, and the never-list test extended to connectors.
    `// req: FR-INT-27`, `// req: AI-66`
  (25 Oct 2026: `connector.Authority`, checked against each capability's trigger before an adapter is built; work from an
  AI proposal needs the approval of the underlying action. No AI never-list test exists yet (the AI tools arrive later), so
  `TestTriggerMustMatchTheManifest` proves the connector's part)

## 6. The email providers move in

- [x] 6.1 The bootstrap (the owner's decision of 25 Oct 2026):
  - under an advisory lock, when no outbound-email connector was ever installed, the API installs the
    providers `FYNIQ_EMAIL_PROVIDERS` names, enabled, in order;
  - their configuration comes from today's variables, and their secrets as references from
    `FYNIQ_CONNECTOR_SECRETS`, pinned to the latest version, never the values;
  - it is audited as the service actor;
  - afterwards the variables are ignored, with one warning if they differ;
  - until the bootstrap succeeds, the worker keeps the deployment's variables.
  - Tests `TestBootstrapFromTheDeployment`, `TestBootstrapRunsOnce`, `TestFailedBootstrapKeepsSending`.
    `// req: FR-INT-21`, `// req: FR-INT-20`
  (25 Oct 2026: `wiring.BootstrapConnectors`, run at the API's start and on each tick until done (before first-run setup
  there is no trail to audit in), stopping before Secret Manager once anything is installed; an install that loses a
  race counts as done; `wiring.EmailDrift` warns once at start. The Kit gains the catalogue, the secret store (Secret
  Manager in the project of `FYNIQ_GCP`'s job, or `.data/secrets`), the names and the egress guard. Tests:
  `TestBootstrapFromTheDeployment`, which also covers running once, a failed bootstrap leaving nothing installed, the
  wait before setup and the drift warning; the fallback to the deployment's providers in
  `TestEmailGoesThroughTheConnectorsEnabledAtEachSend`)
- [x] 6.2 The worker sends through `Runtime.For("outbound-email")` per attempt instead of providers built at
  start.
  - The API's webhooks read their connector's secret per request; they are accepted while it is enabled or
    paused, and refused once it is disabled or uninstalled.
  - Tests: the existing email and webhook suites pass through connectors; `TestWebhookOfADisabledConnectorIsRefused`.
    `// req: FR-INT-21`, `// req: FR-INT-20`
  (25 Oct 2026, the sending half: `email.WorkFrom` reads its providers for each message from `Kit.EmailProviders`, the
  enabled connectors through `connector.Runtime`, or the deployment's until the bootstrap. The webhooks: the receiver's
  `Keys` reads each request's keys through `Kit.WebhookKeys` (current and in overlap, newest first) and refuses a disabled
  or uninstalled connector with 404; inbound mail through `Kit.InboundMail` per fetch, held when no receiving connector is
  enabled. Tests: `TestEmailGoesThroughTheConnectorsEnabledAtEachSend`, `TestWebhookKeysAreReadForEachRequest`,
  `TestTheConnectorsDecideTheWebhookKeysProvidersAndRecords`; the existing email, webhook and inbound suites)
- [x] 6.3 The platform sender and sending domains become the outbound-email capability's installation
  settings, and each provider's DNS records live in its configuration.
  - `email.domains` and `getEmailSettings` read them from there; the capture domain stays the deployment's.
  - Tests: the domain-check and email-settings suites. `// req: FR-INT-21`
  (25 Oct 2026: each provider's DNS records in its configuration (the bootstrap puts the deployment's on the primary);
  `Kit.MailDNS` feeds the nightly `email.domains` job and the settings' check, `Kit.MailProviders` the settings' list.
  *Changed while building:* the platform sender and the sending domains stay the deployment's, as `design.md` records.
  Tests: `TestTheConnectorsDecideTheWebhookKeysProvidersAndRecords` and the existing domain and settings suites)
- [x] 6.4 Upgrade notes in the commit body: operators add `FYNIQ_CONNECTOR_SECRETS` before the deployment,
  and may remove the provider variables after the bootstrap (`design.md`, Migration Plan).
  - `deploy/local/` and `make dev` bootstrap SMTP to Mailpit; `make up` and `scripts/local-smoke.sh` still
    send.
  - Verified by `scripts/local-smoke.sh` (QA-LOCAL-01).
  (25 Oct 2026: the upgrade notes are in the commit of tasks 4.4, 5.1 and 6.1 and in `infra/README.md`; local mode
  bootstraps SMTP to Mailpit through `mailEnv`'s defaults once first-run setup is done, and sends through the deployment's
  Mailpit before. `scripts/local-smoke.sh` runs in CI, not here)

## 7. The API

- [x] 7.1 The operations of `design.md`:
  - `getConnectorCatalogue`;
  - installation and organisation connectors: list (cursor-paginated), get, install, configuration with
    `If-Match`, write-only secrets (`204`), test, enable, pause, resume, disable, uninstall, consent;
  - the installation's connector order;
  - approving and rejecting an installation change;
  - `Idempotency-Key` on every write, `202` with the pending change where a second person decides;
  - `CONNECTOR-nn` codes in `codeFacts`;
  - policies in `authz/policy.go` (Super admin; Appendix D "Organisation integrations") and matrix rows.
  - Tests `TestConnectorsThroughTheAPI`, `TestNoAnswerCarriesASecret`, the matrix rows, Spectral clean
    (`make api-check`), `TestEveryOperationIsDocumented`. `// req: FR-INT-01`, `// req: FR-INT-20`
  (25 Oct 2026: `internal/api/connectors.go`, 18 operations: `listConnectorCatalogue`; the installation's list, get,
  install, configure, set a secret (write-only, `writeOnly` in the contract), take a step (one `steps` operation for
  enable, pause, resume, disable, uninstall, consent and order, rather than one operation each), test, and the changes'
  list, approve and reject (the envelope's SHA-256 signed); the organisation's list, get, install, configure, set a
  secret, take a step and test, every step but pause and test answering 202 with the approval request. The test sends to
  the organisation's own email address from its profile, as FR-INT-21's test send does, rather than the person's sign-in
  address (the manifests at 1.0.1 say so). Tests: `TestConnectorsThroughTheAPI` (which also proves that no answer, log
  or audit event carries a key), `TestOrganisationConnectorsThroughTheAPI`, the 18 matrix rows,
  `TestEveryOperationIsDocumented`, `make api-check`)

## 8. Local mode

- [x] 8.1 The `partners` Compose profile: Prism, pinned by digest, serving every OpenAPI document in
  `contracts/partners/` (with its `README.md`: each connector adds its partner's document with itself).
  - Documented in the `fyniq-local-dev` skill, and `make up` unchanged without the profile.
  - Verified by starting the profile with a sample document in a test, and by `make check`.
  - `// req: platform/local-mode Local edge, email, messaging and partner substitutes`
  (25 Oct 2026: `partner-example-bank` in the `partners` profile with `contracts/partners/example-bank.yaml`, the
  template, and `contracts/partners/README.md`. Prism has no multi-platform image (NFR-IMG-08), so it runs at 5.14.2 from
  the web build's pinned Node, cached in the `prism-npm` volume. Verified by starting the profile here and reading the
  document's example back from `/accounts`; `TestEveryPartnerDocumentIsMocked` keeps documents and services in step, and
  `make up` is unchanged)

## 9. The screens

- [x] 9.1 `connector-catalogue` at `/settings/integrations` (fragment `docs/design/screens/src/ConnectorCatalogue.html`):
  - grouped by capability, with installed status, health, credential expiry and "upgrade available";
  - the manifest summary (publisher, version, data classes, regions);
  - an installation section for Super admins and an organisation section for Owners and Administrators;
  - Expert mode.
  - Registry route and help keys (`settings.integrations`, `connector.health`, `connector.data-classes`).
  - Tests: component tests with axe (`noViolations`). `// req: FR-INT-25`, `// req: FR-INT-26`
  (25 Oct 2026: `apps/web/src/screens/settings/Integrations.tsx`, with the words in `connectorWords.ts`: the tiles, the
  failing-test banner, the installed table with scope filter, the catalogue by kind with search, the install dialog
  showing the manifest, and the installation changes waiting with approve and reject over the envelope's hash. No
  shipped connector's credential carries an expiry date, so the Secrets column and tile show which are not set and when
  each was set. Seen in the dev stack against the real API, with the bootstrapped SMTP connector.)
- [x] 9.2 `connector-detail` at `/settings/integrations/$connector` (fragment `ConnectorDetail.html`):
  - the lifecycle stepper;
  - the configuration form from the manifest schema, with write-only secrets (set, and when replaced);
  - the test result (pass or fail and the reason);
  - health, and the "awaiting second approver" banner;
  - the upgrade diff and consent;
  - activity and audit;
  - for email: senders by purpose, domain checks, the failover order and the suppression list, through the
    existing email operations.
  - Help keys for every field and action.
  - Tests: component tests with axe, and that the same action sends the same request in both modes (QA-UI-04).
    `// req: FR-INT-20`, `// req: FR-INT-26`, `// req: FR-INT-27`
  (25 Oct 2026: `IntegrationDetail.tsx`, at `/settings/integrations/$connector?scope=installation|organisation`; tabs for
  the overview, configuration and secrets, senders and domain, upgrade, activity and audit. Activity is the email
  provider's messages from `listEmailMessages`; Audit is the connector's installation changes or approval requests, its
  test and its install. The full before-and-after trail stays in the audit trail screen, and the API exposes no breaker
  or rate-limit state, so the page shows neither. Tests in `Integrations.test.tsx`, including QA-UI-04 on the pause.)
- [x] 9.3 The user-guide page `docs/user-guide/integrations.md` (front-matter key `settings.integrations`),
  compiled into `kb/pages/task/`, with `make kb-index` and `make kb-lint` clean. `// req: QA-UI-05`
  (25 Oct 2026: compiled to `kb/pages/task/settings-integrations.md`, linked from `settings.notifications`; every
  registry help key and guide anchor of the two screens points into it.)

## 10. Finish

- [ ] 10.1 The threat model's abuse cases each have their test, and the security checklist is updated.
  `make check`, `make test`, `make scan` and `make infra-check` pass; a conventional commit with upgrade
  notes.
  - Not verified here: a live send through each production provider after the bootstrap, which is a staging
    check.

## Workflow follow-up

- `/opsx:verify`, then `/opsx:archive p1-connector-framework --skip-specs` once every task is done.
- `p1-bank-import-reconcile` proves the organisation-level path with its first connector, and confirms the
  Secret Manager IAM condition (task 3.3).
