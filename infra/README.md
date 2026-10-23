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
| `gcp/modules/environment/` | One environment: APIs, CMEK keys, a private network with Cloud NAT, Cloud SQL for PostgreSQL 18, secrets, one service account per service and job, Cloud Run services and jobs, Cloud Tasks queues, the Scheduler tick, and break-glass through Privileged Access Manager |
| `gcp/modules/anchors/` | The audit anchors: a retention-locked bucket and the EC P-256 key that signs them (§11.5 L3) |
| `gcp/modules/budget/` | The monthly budget: alerts at 50%, 80% and 100% by email, never a cap |
| `gcp/modules/stack/` | Staging or production, whole: spec, environment, anchors and budget |
| `gcp/bootstrap/` | The state bucket (local state, applied once) |
| `gcp/shared/` | The organisation policies and CI's identity |
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
| CTL-DP-01 | The database and secrets use rotated CMEK. Backups are pinned to Singapore with point-in-time recovery. The database has a private IP only |
| CTL-DP-07 | No region outside Singapore, in the spec and anywhere in the code |
| CTL-IAM-03 | No grant to a person or group. Break-glass needs another person's approval |
| CTL-AUD-02 | Production's anchors are retention-locked. Staging's are not |
| NFR-COST-01 | Budgets are variables, with alerts at 50%, 80% and 100% and no Pub/Sub cap |

## First apply (the founder)

1. Create the organisation, the billing account and the projects of §12.4: `fyniq-ci`,
   `fyniq-dev`, `fyniq-staging`, `fyniq-prod` and `fyniq-audit`. If an ID is taken, pass the new
   one as a variable.
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
   which the settings and the nightly `email.domains` job check (rev 0.104); and `service_secrets`
   gives the `worker` `FYNIQ_ZEPTOMAIL_KEY = "zeptomail-key"` and `FYNIQ_RESEND_KEY = "resend-key"`,
   and the `api` the webhooks' secrets, `FYNIQ_ZEPTOMAIL_WEBHOOK_KEY = "zeptomail-webhook-key"` and
   `FYNIQ_RESEND_WEBHOOK_SECRET = "resend-webhook-secret"`; point each provider's webhook at
   `https://<origin>/api/webhooks/email/zeptomail` or `…/resend`. Add those secrets' versions out of
   band like the others; no key ever passes through the API's answers.
7. Run the Infrastructure workflow by hand for `staging` with **apply**. After that, pushes to
   `main` plan every environment, and the nightly run fails on drift.

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
  with no default.

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
- `[verify]` The billing account's currency. A budget must use it; USD is assumed.
- `[verify]` The production anchor retention, with the accountant (§14, AUD-06).
- `[verify]` The project IDs.
