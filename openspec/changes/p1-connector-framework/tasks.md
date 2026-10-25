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
- [ ] 4.2 The lifecycle in `internal/connector`:
  - install, configure (validated against the manifest's schema), replace a secret, test, enable, pause,
    resume, disable, uninstall, order, consent;
  - disable cancels what the manifest marks cancellable, and holds the rest;
  - uninstall destroys the secrets, and removes the provider-side webhooks where the manifest registers them
    or lists them for the operator;
  - audit events of `design.md`, each in the same transaction.
  - Tests `TestConnectorLifecycle`, `TestDisableCancelsOnlyWhatTheManifestAllows`,
    `TestUninstallDestroysSecretsAndKeepsHistory`, `TestEveryStepIsAudited`. `// req: FR-INT-26`,
    `// req: AUD-01`
- [ ] 4.3 Dual control:
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
- [ ] 4.4 Secret rotation:
  - a replaced secret is a new version referenced by the pending change, and switched on approval;
  - the old version stays enabled for the manifest's overlap (both verify webhooks), then is disabled and
    destroyed by a job;
  - a rejected or expired change destroys its version.
  - Tests `TestRotationOverlapAcceptsBothWebhookKeys`, `TestRejectedChangeDestroysItsSecretVersion`.
    `// req: FR-INT-26` (secrets rotated without downtime)

## 5. The runtime

- [ ] 5.1 `connector.Runtime.For(ctx, capability)`:
  - reads the enabled installations in order, at the start of each piece of work, never cached;
  - paused and disabled connectors are skipped, so failover moves on;
  - with none enabled, a job is re-queued, and the wait uses none of its attempts;
  - an attempt already handed over is reconciled through its provider even after a pause.
  - Tests `// req: FR-INT-20` G4-096 (`TestDisabledConnectorCommitsNothingNew`, with queued and in-flight work),
    `TestPausedConnectorHoldsItsQueue`, `TestFailoverSkipsAPausedConnector`. `// req: QA-SEC-04` (its
    connector half)
- [ ] 5.2 The egress guard `internal/egress`:
  - an HTTP client and an SMTP dialer that refuse loopback, private, link-local, unique-local and metadata
    addresses and internal names;
  - they dial the checked address, follow no redirect, and limit time and size;
  - a configured URL must match a consented destination;
  - the local flavour allows loopback for its substitutes only;
  - a test action returns pass or fail with a reason only.
  - Tests `TestEgressRefusals` (the metadata address, loopback, private ranges, an internal name, a redirect
    to one, a name that changes between check and connect), `TestTestActionNeverReturnsTheBody`.
    `// req: SEC-36` (connector traffic), `// req: QA-SEC-04`
- [ ] 5.3 The connector principal, rate limits and circuit breaker:
  - work is audited as `integration:connector/<id>` on behalf of its trigger;
  - per-instance limits and a breaker from the manifest;
  - an open breaker counts as "not accepted" only for sends never handed over.
  - Tests `TestConnectorWorkIsAuditedAsItsOwnPrincipal`, `TestBreakerFailsOverOnlyWhatWasNeverHandedOver`.
    `// req: FR-INT-27`
- [ ] 5.4 Upgrades and consent:
  - the installation keeps its consented manifest;
  - a non-widening release runs under it, and its first run is recorded as `connector.upgraded`;
  - a widening release runs under the old consent (destinations and data classes) until the dual-controlled
    consent step;
  - the manifest diff is computed for the screen.
  - Tests `TestWideningManifestRunsUnderTheOldConsent`, `TestManifestDiff`. `// req: FR-INT-26` (upgrade shows
    the manifest diff; new scopes or data classes need consent)
- [ ] 5.5 Who may trigger a capability: the runtime refuses work whose trigger does not match the manifest's
  (a human authority, a system rule or nobody), and a connector approves nothing.
  - Tests `TestTriggerMustMatchTheManifest`, and the never-list test extended to connectors.
    `// req: FR-INT-27`, `// req: AI-66`

## 6. The email providers move in

- [ ] 6.1 The bootstrap (the owner's decision of 25 Oct 2026):
  - under an advisory lock, when no outbound-email connector was ever installed, the API installs the
    providers `FYNIQ_EMAIL_PROVIDERS` names, enabled, in order;
  - their configuration comes from today's variables, and their secrets as references from
    `FYNIQ_CONNECTOR_SECRETS`, pinned to the latest version, never the values;
  - it is audited as the service actor;
  - afterwards the variables are ignored, with one warning if they differ;
  - until the bootstrap succeeds, the worker keeps the deployment's variables.
  - Tests `TestBootstrapFromTheDeployment`, `TestBootstrapRunsOnce`, `TestFailedBootstrapKeepsSending`.
    `// req: FR-INT-21`, `// req: FR-INT-20`
- [ ] 6.2 The worker sends through `Runtime.For("outbound-email")` per attempt instead of providers built at
  start.
  - The API's webhooks read their connector's secret per request; they are accepted while it is enabled or
    paused, and refused once it is disabled or uninstalled.
  - Tests: the existing email and webhook suites pass through connectors; `TestWebhookOfADisabledConnectorIsRefused`.
    `// req: FR-INT-21`, `// req: FR-INT-20`
- [ ] 6.3 The platform sender and sending domains become the outbound-email capability's installation
  settings, and each provider's DNS records live in its configuration.
  - `email.domains` and `getEmailSettings` read them from there; the capture domain stays the deployment's.
  - Tests: the domain-check and email-settings suites. `// req: FR-INT-21`
- [ ] 6.4 Upgrade notes in the commit body: operators add `FYNIQ_CONNECTOR_SECRETS` before the deployment,
  and may remove the provider variables after the bootstrap (`design.md`, Migration Plan).
  - `deploy/local/` and `make dev` bootstrap SMTP to Mailpit; `make up` and `scripts/local-smoke.sh` still
    send.
  - Verified by `scripts/local-smoke.sh` (QA-LOCAL-01).

## 7. The API

- [ ] 7.1 The operations of `design.md`:
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

## 8. Local mode

- [ ] 8.1 The `partners` Compose profile: Prism, pinned by digest, serving every OpenAPI document in
  `contracts/partners/` (with its `README.md`: each connector adds its partner's document with itself).
  - Documented in the `fyniq-local-dev` skill, and `make up` unchanged without the profile.
  - Verified by starting the profile with a sample document in a test, and by `make check`.
  - `// req: platform/local-mode Local edge, email, messaging and partner substitutes`

## 9. The screens

- [ ] 9.1 `connector-catalogue` at `/settings/integrations` (fragment `docs/design/screens/src/ConnectorCatalogue.html`):
  - grouped by capability, with installed status, health, credential expiry and "upgrade available";
  - the manifest summary (publisher, version, data classes, regions);
  - an installation section for Super admins and an organisation section for Owners and Administrators;
  - Expert mode.
  - Registry route and help keys (`settings.integrations`, `connector.health`, `connector.data-classes`).
  - Tests: component tests with axe (`noViolations`). `// req: FR-INT-25`, `// req: FR-INT-26`
- [ ] 9.2 `connector-detail` at `/settings/integrations/$connector` (fragment `ConnectorDetail.html`):
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
- [ ] 9.3 The user-guide page `docs/user-guide/integrations.md` (front-matter key `settings.integrations`),
  compiled into `kb/pages/task/`, with `make kb-index` and `make kb-lint` clean. `// req: QA-UI-05`

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
