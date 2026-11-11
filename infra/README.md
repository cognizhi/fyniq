# Infrastructure (OpenTofu)

Every cloud resource of Fyniq is code here (NFR-PORT-05, CTL-CHG-02). The OpenSpec change is
`openspec/changes/p0-infra-opentofu`; the architecture is PRD-001 §12.3, §12.4, §12.6 and §12.11.

**Status (16 Oct 2026): built and tested without a cloud account, not applied.** Applying it needs
the founder's Google Cloud organisation, billing account and projects. Until they exist, the
Infrastructure workflow does nothing.

## Layout

| Path | What |
|---|---|
| `modules/platform-spec/` | The platform as cloud-neutral data: services, jobs, worker classes, sizes, the tick, budget and anchor retention. It names no cloud resource, so AWS and Azure modules can share it later (ADR-T8) |
| `gcp/modules/org-policy/` | The organisation policies of §12.4: Singapore only, no service-account keys, uniform bucket access, no public database IP, and CMEK required in staging and production |
| `gcp/modules/ci-identity/` | `fyniq-ci`: Workload Identity Federation for this repository's `main` branch and tags only, Artifact Registry (immutable tags), and one service account per CI job. No keys |
| `gcp/modules/environment/` | One environment: APIs, CMEK keys, a private network with Cloud NAT, Cloud SQL for PostgreSQL 18, secrets, one service account per service and job, Cloud Run services and jobs, Cloud Tasks queues, the Scheduler tick, and break-glass through Privileged Access Manager; and, in the environment's secrets project, the organisations' connector secrets, which only the api's account manages (`org_secrets.tf`, design D12 of `p1-bank-import-reconcile`) |
| `gcp/modules/anchors/` | The audit anchors: a retention-locked bucket and the EC P-256 key that signs them (§11.5 L3) |
| `gcp/modules/binauthz/` | Binary Authorization (SEC-22, CTL-CHG-04): the `fyniq-release` attestor and its Cloud KMS key in `fyniq-ci`, and each running environment's policy requiring its attestation for every Cloud Run service and job, which must use that policy. The release workflow attests an image only after it verified its signature, SBOM and SLSA provenance |
| `gcp/modules/edge/` | The edge (SEC-23): a global external Application Load Balancer on the public origin's host (managed certificate, TLS 1.2+, HTTP redirected), with the local stack's paths to the public services' serverless NEGs, and Cloud Armor: the OWASP rules (in preview first), Log4Shell denied, a per-IP rate-based ban on sign-in and the routes that need no session, adaptive protection; the client's address, country and region as request headers. The public services take traffic from it and from inside Google Cloud only |
| `gcp/modules/db-export/` | The weekly export of the database (DR-02, DR-04): two buckets beside the anchors (`fyniq-audit` in production) under their own CMEK key, in Singapore: each Sunday's export kept 35 days and each month's first kept a year, both locked in production. The instance's own account writes the exports; the jobs' account may only start an export and read it (the platform job `db.export`) |
| `gcp/modules/evidence/` | The records' and attachments' buckets (DR-07, CTL-AUD-03), in the environment's project, in Singapore, under their own CMEK key: staging (what is not yet a record; the app deletes each file in its time, and a backstop deletes report exports after 7 days) and evidence, with Object Retention Lock on, so the API sets each committed file's retain-until in Locked mode. The api reads committed files; the worker and the jobs commit them with a role that may never delete one |
| `gcp/modules/security-logs/` | The central log store (SEC-30): data-access audit logs for Cloud SQL, Secret Manager, KMS, IAM and Cloud Storage, and a sink of Cloud Audit Logs, the database's pgAudit entries and the platform's security alerts to a Cloud Logging bucket beside the anchors (`fyniq-audit` in production), kept 400 days and locked in production |
| `gcp/modules/nonprod-scan/` | Staging only (CTL-CHG-05 W): each Monday the database is exported to a bucket of its own, kept a week, and a Sensitive Data Protection job inspects it each week for NRIC and FIN numbers, passports, card and bank numbers, reporting to Security Command Center and by email |
| `gcp/modules/slo/` | Service levels (NFR-AVAIL-01, NFR-PERF-01, CTL-RES-03): for each public service, 99.9% of requests without a server error each month and 95% within 300 ms over 28 days, from Cloud Run's metrics; burn-rate alerts to the founders (fast: 14.4 times over an hour; slow: 6 times over six hours); a dashboard |
| `gcp/modules/budget/` | The monthly budget: alerts at 50%, 80% and 100% by email, never a cap |
| `gcp/modules/stack/` | Staging or production, whole: spec, environment, anchors, the weekly export, the evidence buckets, security logs, the service levels, the edge, the budget and, in staging, the non-production scan |
| `gcp/bootstrap/` | The state bucket (local state, applied once) |
| `gcp/shared/` | The organisation policies, CI's identity and Binary Authorization |
| `gcp/envs/{dev,staging,production}/` | One root per environment, each in its own project (CTL-CHG-05). `dev` is a sandbox with a budget and no platform: the release build refuses `FYNIQ_ENV=dev` (RUNTIME-FLAVOUR-01) |

## Check it locally

`make infra-check` runs without a cloud account. It formats, initialises from the committed lock
files, validates, runs each module's `tofu test` against mock providers, and applies the rules no
plan can show. It downloads the providers, so it needs the network; that is why it is not part of
`make test`. `make scan` runs Checkov on `infra/`.

The tests check the controls on the planned configuration:

| Control | What the tests check |
|---|---|
| SEC-03 | Every service and job has its own service account. The MCP server and the AI orchestrator read no secret |
| §12.11 | Only the api's account may create tasks or act as an invoker. The executor is invoked only by its own invoker |
| SEC-04, CTL-IAM-04 | Workload Identity Federation names the repository and its owner by ID and allows only `main` and tags. There are no keys |
| SEC-23 | Every request to a public service passes Cloud Armor: the OWASP rules, a per-IP ban on sign-in and the public routes, adaptive protection; each service learns the client's address, country and region; HTTPS only, TLS 1.2 or later; the public services' ingress is the load balancer and Google Cloud only |
| SEC-22, CTL-CHG-04 | Staging's and production's policies require the release attestation for every image and allow-list none; every Cloud Run service and job uses the policy, without breakglass; only `release.yml` on a version tag can reach the attester, which may sign attestations and read images, nothing more |
| CTL-DP-01 | The database and secrets use rotated CMEK. Backups are pinned to Singapore with point-in-time recovery. The database has a private IP only |
| CTL-DP-07 | No region outside Singapore, in the spec and anywhere in the code |
| CTL-IAM-03 | No grant to a person or group. Break-glass needs another person's approval |
| SEC-12 | The api's account holds `roles/secretmanager.admin` on the secrets project only (a rule of `make infra-check`), the worker reads there, and its Secret Manager encrypts under the environment's secrets key |
| CTL-AUD-02 | Production's anchors are retention-locked. Staging's are not |
| DR-01, DR-02, DR-03, CTL-RES-01 | Production's database is regional HA, and a zonal one is refused. Point-in-time recovery keeps 7 days of logs, and 35 daily backups are kept in Singapore |
| DR-07, CTL-AUD-03, FR-DOC-02 | The evidence bucket locks each object until its own retain-until, deletes nothing by age and is never public; no account may delete a committed file or shorten its retention |
| DR-06, CTL-RES-02 | The jobs' account may clone the instance, and read, update and delete only its drill copies (`<instance>-drill-<time>`), by IAM conditions |
| DR-02, DR-04 | The weekly export is kept 35 days and each month's first a year, in `fyniq-audit` for production, locked there; only the instance writes it, and the jobs' account may only start it |
| NFR-COST-01 | Budgets are variables, with alerts at 50%, 80% and 100% and no Pub/Sub cap |

## First apply (the founder)

1. Create the organisation, the billing account and the projects of §12.4: `fyniq-ci`,
   `fyniq-dev`, `fyniq-staging`, `fyniq-prod` and `fyniq-audit`, and each environment's secrets
   project for the organisations' connector secrets, `fyniq-staging-secrets` and `fyniq-prod-secrets`
   (the owner's decision of 25 Oct 2026). If an ID is taken, pass the new one as a variable
   (`project`, `secrets_project`).
2. `gcp/bootstrap`: `tofu init && tofu apply -var bucket=<state bucket name>`.
3. `gcp/shared`: `tofu init -backend-config=bucket=<state bucket>`, then
   `tofu apply -var organization_id=<id>`. Do this through just-in-time elevation: this root gives
   CI its access.
4. Grant each `tofu-<env>` service account `roles/billing.costsManager` on the billing account, so
   that it can manage that environment's budget.
5. Set the GitHub repository variables from the shared root's outputs:
   - `GCP_WORKLOAD_IDENTITY_PROVIDER`
   - `TOFU_STATE_BUCKET`
   - `GCP_TOFU_SA_DEV`, `GCP_TOFU_SA_STAGING` and `GCP_TOFU_SA_PRODUCTION`
   - for the release workflow (`.github/workflows/release.yml`): `GCP_PUBLISHER_SA` and `GCP_ATTESTER_SA`
     (`service_accounts`), `ARTIFACT_REGISTRY` (the repository's URL,
     `asia-southeast1-docker.pkg.dev/fyniq-ci/images`), and `BINAUTHZ_ATTESTOR` and `BINAUTHZ_KEY_VERSION` (`binauthz`)
   - `TOFU_TFVARS_DEV`, `TOFU_TFVARS_STAGING` and `TOFU_TFVARS_PRODUCTION`: each root's values as
     JSON, such as `billing_account`, `budget_emails` and `public_origin`.

   For manual runs, create the GitHub environments `dev`, `staging` and `production`, each with
   its own `GCP_TOFU_SA` and `TOFU_TFVARS`. `production` requires a reviewer.
6. Create the secrets before the services: Cloud Run will not start a revision whose secret has no
   version yet. Apply the staging root once with
   `-target=module.stack.module.environment.google_secret_manager_secret.all` (and the database),
   then add each secret's first version out of band:
   - each service's `…-database-url`, with its role's password;
   - `oauth-dsn` and `oauth-system-secret` for Hydra;
   - `idp-api-key`.

   The `migrate` and `oauth-migrate` jobs need their URLs before they run.

   Outbound email (FR-INT-21, PRD-001 rev 0.102) is the deployment's to set: in the root's values,
   `service_env` gives the `api` and the `worker` `FYNIQ_EMAIL_PROVIDERS` (`zeptomail,resend`: the
   first primary, the next its failover), `FYNIQ_MAIL_FROM` (the platform's sender) and
   `FYNIQ_MAIL_DOMAINS` (the domains organisations may choose senders on); the `api` and `jobs` also
   get `FYNIQ_MAIL_DOMAINS` and `FYNIQ_MAIL_DNS`, each domain's records as its providers' dashboards
   list them (`{"mail.example.com":[{"type":"TXT","name":"…._domainkey.mail.example.com","value":"v=DKIM1"}]}`),
   which the settings and the nightly `email.domains` job check (rev 0.104); the `api` and the `worker`
   get `FYNIQ_CAPTURE_DOMAIN`, the email-in addresses' domain, such as `in.cognizhi.com`, whose MX
   records point at Resend's receiving (rev 0.105), with Resend's webhook also sending `email.received`
   (rev 0.106); and `service_secrets`
   gives the `worker` `FYNIQ_ZEPTOMAIL_KEY = "zeptomail-key"` and `FYNIQ_RESEND_KEY = "resend-key"`,
   and the `api` the webhooks' secrets, `FYNIQ_ZEPTOMAIL_WEBHOOK_KEY = "zeptomail-webhook-key"` and
   `FYNIQ_RESEND_WEBHOOK_SECRET = "resend-webhook-secret"`; point each provider's webhook at
   `https://<origin>/api/webhooks/email/zeptomail` or `…/resend`. Add those secrets' versions out of
   band like the others; no key ever passes through the API's answers.

   Connector secrets (FR-INT-20, PRD-001 rev 0.139) come from the manifests in `connectors/`: each
   installation connector's secret is a secret of its own, `connector-<connector>-<secret>`, read only
   by the services its manifest lists, and the `api` may add, disable and destroy their versions
   (`roles/secretmanager.secretVersionManager`, which cannot read a value) and no other secret's, when
   a Super admin replaces a key in Settings › Integrations. The `api`, `worker` and `jobs` get
   `FYNIQ_CONNECTOR_SECRETS`, which secret holds each one (names, never values). To keep the email
   providers' keys where they are, the root's `connector_secret_sources` names them, such as
   `{ "zeptomail.send_token" = "zeptomail-key", "zeptomail.webhook_key" = "zeptomail-webhook-key",
   "resend.api_key" = "resend-key", "resend.webhook_secret" = "resend-webhook-secret" }`, so they
   are referenced, never copied. On first start (and each tick until it has), the API installs the
   email connectors from these settings with references to those secrets; until it has, email is
   sent as above, and once it has, `FYNIQ_EMAIL_PROVIDERS`, the keys' variables, the providers'
   URLs and `FYNIQ_MAIL_DNS` are no longer read and may be removed (the API warns once if they
   differ). `FYNIQ_MAIL_FROM`, `FYNIQ_MAIL_DOMAINS` and `FYNIQ_CAPTURE_DOMAIN` stay the deployment's. The module gives the `worker`
   and `jobs` `FYNIQ_PUBLIC_ORIGIN` from `public_origin`, for the links in notifications' emails
   (rev 0.108).
7. Run the Infrastructure workflow by hand for `staging` with **apply**. After that, pushes to
   `main` plan every environment, and the nightly run fails on drift.
8. Binary Authorization starts as a dry run: the policies log every image without the release attestation and
   block none, because the services start from the placeholder image. Once `cloud-first-deployment` deploys an
   attested release, re-apply the shared root with `-var 'binauthz_enforced=["staging"]'`, then with production too.

Values (billing account, emails) live only in GitHub variables, never in git. The workflow logs
show which resources change and how, never their values, because this repository's logs are public.

## Decisions (see the change's design.md)

- **Images.** A service starts from a placeholder image, and the release workflow deploys each image
  by digest. OpenTofu ignores the image, so the drift check does too.
- **URLs.** Every URL and token audience is Cloud Run's deterministic
  `https://SERVICE-PROJECT_NUMBER.REGION.run.app`. That lets each service's `FYNIQ_GCP` name the
  others, and itself, without a cycle.
- **Hydra.** It runs as two services: `oauth` is the public port, and `oauth-admin` is the admin
  port, reachable only from inside the network. The api and the worker reach it through Direct VPC
  egress. Outbound calls leave through Cloud NAT.
- **Database access.** Each service's `FYNIQ_DATABASE_URL` is a secret, with a password for its own
  role, over the private IP and TLS. The value is added out of band, never in state.
- **The anchors' lock** is irreversible, so only production sets it. Its retention is a variable
  with no default. The weekly export's buckets are locked with it.

## Not verified yet (needs the cloud)

Each item stays open in the change's tasks until staging proves it. Items marked `[verify]` are
assumptions, not facts.

- The org policy refusing a resource outside Singapore.
- A CI plan per root.
- A drift found after a console change.
- The lock refusing a delete.
- The budget channels receiving a test notification.
- The roles each account actually holds.
- `[verify in staging]` Cloud Run's deterministic URL as each token's audience, and that Cloud
  Tasks reaches services with internal-only ingress.
- `[verify in staging]` How the services authenticate to the database (password or IAM
  authentication).
- `[verify in staging]` That Direct VPC egress counts as internal traffic for Hydra's admin port.
- `[verify in staging]` That Cloud Tasks queues and the Scheduler's OIDC tokens work without an App
  Engine app or an explicit token-creator grant.
- `[live-verify]` That the instance's account writes the weekly export into `fyniq-audit`'s bucket
  (a write across projects under that project's key), that `db.export` succeeds with the custom role
  alone, and how long an export of production takes.
- `[live-verify]` The service levels: that Cloud Monitoring reads the objectives from Cloud Run's request metrics as
  written, and that a burn-rate alert reaches the founders.
- `[live-verify]` Traces (NFR-OBS-01): that the Telemetry API takes OTLP/JSON at `telemetry.googleapis.com/v1/traces`
  with the resource attribute `gcp.project_id`, that `roles/telemetry.tracesWriter` is its role (`[verify]`), and
  where Cloud Trace keeps the traces (`[verify]`: CTL-DP-07 allows Singapore only; a trace holds no personal data).
- `[live-verify]` That Cloud SQL writes pgAudit's entries to its `postgres.log` with `AUDIT:`, so the security
  sink's filter keeps them (CTL-AUD-04).
- `[live-verify]` The restore drill: that the clone's permissions are enough (`[verify]` whether Cloud SQL
  also asks for `cloudsql.instances.create`), that the IAM conditions match Cloud SQL's resource names, that
  the jobs reach the copy's private address as their own database role (and its TLS), and how long a
  clone of production takes against NFR-DR-02's 4 hours.
- `[live-verify]` That the evidence bucket refuses to delete a committed file or shorten its Locked
  retention, and that the committer's custom role is enough to commit one and set its retention.
- `[verify]` The billing account's currency. A budget must use it; USD is assumed.
- `[verify]` The production anchor retention, with the accountant (§14, AUD-06).
- `[verify]` The project IDs.
- `[live-verify]` The release workflow end to end on the first tag: keyless signatures and SBOM attestations in
  Artifact Registry, the SLSA generator's provenance pushed as the publisher, `attribute.release` reaching the
  attester from `release.yml` on a tag only, and `gcloud container binauthz attestations sign-and-create` with the
  attestor and key version by full resource name.
- `[live-verify]` The edge: the managed certificate after the DNS record, the prefix rewrite of `/api/*` to `/`, Cloud
  Scheduler and Cloud Tasks reaching services whose ingress is the load balancer and internal traffic, the OWASP rules'
  hits in preview before they deny, and that the load balancer's `X-Fyniq-Client-IP` cannot be set by the client (the
  api reads its last value). The web app's bucket replaces the API as the default route with `cloud-first-deployment`.
- `[live-verify]` Which digest Cloud Run checks for a multi-platform image: the release attests the index and each
  platform's manifest.
- `[live-verify]` That `run.allowedBinaryAuthorizationPolicies` = `default` refuses a service without the policy, and
  an enforced policy refuses an image without the attestation.
- `[live-verify]` The security logs: data-access audit logs arriving for each sensitive service, the sink's writer
  identity writing to the bucket in `fyniq-audit`, and the bucket's lock (irreversible: apply production once the 400
  days are confirmed).
- `[live-verify]` The posture: Security Command Center's Standard tier on the projects, and the jobs' account listing
  findings (`roles/securitycenter.findingsViewer`) and reading the services' logs (`roles/logging.viewer`).
- `[live-verify]` The scanner service (rev 0.247): Cloud Run's sidecar start order and the front's readiness waiting for
  clamd's signatures (up to 5 minutes), clamd's memory while it reloads them (3 GiB given), freshclam reaching
  database.clamav.net through Cloud NAT within ClamAV's rate limits, and the warm instance's cost.
- `[live-verify]` The non-production scan in staging: the Scheduler's export as `scan-export` with only
  `cloudsql.instances.export`, the instance's account writing to the export bucket, the Sensitive Data Protection
  infoType names, and its summary reaching Security Command Center.
