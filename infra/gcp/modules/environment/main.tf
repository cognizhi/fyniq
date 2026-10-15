# One environment of the platform on Google Cloud (§12.3, ADR-T7), from the cloud-neutral spec
# (infra/modules/platform-spec): its own project (CTL-CHG-05), customer-managed keys (CTL-DP-01), a
# private network and Cloud SQL for PostgreSQL 18 with backups and point-in-time recovery, every secret in
# Secret Manager (SEC-12), one service account per service and job (SEC-03), Cloud Run services and jobs
# at min instances 0, one Cloud Tasks queue per worker class, and the Cloud Scheduler tick.
#
# Images: the services start from a placeholder image; the release workflow deploys each image by digest,
# so OpenTofu ignores the image (and the drift check does too).

terraform {
  required_version = ">= 1.13.0"
  required_providers {
    google      = { source = "hashicorp/google", version = "~> 8.5.0" }
    google-beta = { source = "hashicorp/google-beta", version = "~> 8.5.0" } # the service agents CMEK needs, created on demand
  }
}

variable "spec" {
  description = "The platform, from infra/modules/platform-spec."
  type        = any
}

variable "project" {
  description = "The environment's own project (fyniq-staging, fyniq-prod)."
  type        = string
}

variable "anchors" {
  description = "The audit anchors' bucket and signing key version (infra/gcp/modules/anchors)."
  type = object({
    bucket      = string
    key_version = string
  })
}

variable "artifact_registry" {
  description = "The repository the release images come from (the CI project's), so this project's Cloud Run can pull them; null before CI exists."
  type = object({
    project    = string
    location   = string
    repository = string
  })
  default = null
}

variable "public_origin" {
  description = "The web app's https origin (FYNIQ_PUBLIC_ORIGIN): where people sign in, and the base of the API, MCP and OAuth issuer URLs."
  type        = string
  validation {
    condition     = can(regex("^https://[a-z0-9.-]+$", var.public_origin))
    error_message = "public_origin must be an https origin with no path, such as https://staging.fyniq.example."
  }
}

variable "signin_providers" {
  description = "The sign-in providers on Identity Platform, in order (FYNIQ_SIGNIN_PROVIDERS; FR-ADM-08)."
  type = list(object({
    id        = string
    name      = string
    idp       = string
    client_id = string
    issuer    = string
  }))
  default = []
}

variable "service_env" {
  description = "Plain settings per service or job, e.g. api's FYNIQ_PUBLIC_ORIGIN and FYNIQ_SIGNIN, or Hydra's URLS_SELF_ISSUER. Never a secret: those go in service_secrets."
  type        = map(map(string))
  default     = {}
}

variable "service_secrets" {
  description = "Further secrets per service or job: environment variable → secret name (created here, its value added out of band)."
  type        = map(map(string))
  default     = {}
}

variable "service_args" {
  description = "Container arguments per service or job, e.g. Hydra's [\"serve\", \"public\"]."
  type        = map(list(string))
  default     = {}
}

variable "placeholder_image" {
  description = "The image a service starts from, before the release workflow deploys one by digest."
  type        = string
  default     = "us-docker.pkg.dev/cloudrun/container/hello"
}

variable "break_glass" {
  description = "Just-in-time elevation (CTL-IAM-03, SEC-03): who may request the role, who approves, and for how long. No human holds a standing role here."
  type = object({
    requesters = list(string)
    approvers  = list(string)
    role       = string
    duration   = string
  })
  default = null
}

data "google_project" "this" {
  project_id = var.project
}

locals {
  spec    = var.spec
  region  = local.spec.region
  number  = data.google_project.this.number
  apis    = ["run", "sqladmin", "secretmanager", "cloudkms", "cloudtasks", "cloudscheduler", "compute", "servicenetworking", "iam", "iamcredentials", "monitoring", "logging", "identitytoolkit", "privilegedaccessmanager"]
  runners = merge(local.spec.services, local.spec.jobs)

  # One identity per service and job (SEC-03), one invoker per worker class and one for the Scheduler.
  # Their addresses are fixed by their IDs, so the wiring below names each one without a cycle.
  accounts = merge(
    { for name in keys(local.runners) : name => "run-${name}" },
    { for class in keys(local.spec.classes) : "tasks-${class}" => "tasks-${class}" },
    { scheduler = "scheduler" },
  )
  email  = { for k, id in local.accounts : k => "${id}@${var.project}.iam.gserviceaccount.com" }
  member = { for k, e in local.email : k => "serviceAccount:${e}" }

  # Cloud Run's deterministic URL (https://SERVICE-PROJECT_NUMBER.REGION.run.app) [verify in staging]:
  # a service's own address is known before it exists, so each one's FYNIQ_GCP can name every class.
  url = { for name in keys(local.spec.services) : name => "https://${name}-${local.number}.${local.region}.run.app" }

  # FYNIQ_GCP (internal/wiring/release.go Config): the worker classes' queues, handlers and invokers, the
  # Scheduler's, the anchors and the jobs image's Cloud Run job.
  gcp_config = jsonencode({
    tasks = { for class, c in local.spec.classes : class => {
      queue           = "projects/${var.project}/locations/${local.region}/queues/${class}"
      url             = local.url[c.service]
      service_account = local.email["tasks-${class}"]
      audience        = local.url[c.service]
    } }
    invokers = merge(
      { for class, c in local.spec.classes : class => { service_account = local.email["tasks-${class}"], audience = local.url[c.service] } },
      { scheduler = { service_account = local.email["scheduler"], audience = "${local.url["api"]}/internal/tick" } },
    )
    anchors = var.anchors
    job     = "projects/${var.project}/locations/${local.region}/jobs/jobs"
  })
  # Who reads FYNIQ_GCP (wiring.Build), and who reaches Hydra's admin port.
  wired       = toset(["api", "worker", "executor", "jobs"])
  oauth_admin = toset(["api", "worker"])
  with_db     = { for name, r in local.runners : name => r if r.database_role != null }

  # The api's sign-in and audiences (cmd/api/release.go), and Hydra's settings as the local stack has them
  # (deploy/local/compose.yaml), without --dev: the issuer is https here.
  hydra_env = {
    URLS_SELF_ISSUER        = "${var.public_origin}/oauth/"
    URLS_LOGIN              = "${var.public_origin}/api/auth/login"
    URLS_CONSENT            = "${var.public_origin}/api/auth/consent"
    OAUTH2_PKCE_ENFORCED    = "true"   # the authorisation code with PKCE only (AI-36)
    STRATEGIES_ACCESS_TOKEN = "opaque" # introspected, so revocation is seen (AI-38)
    TTL_ACCESS_TOKEN        = "1h"     # at most 60 minutes (AI-38)
    TTL_REFRESH_TOKEN       = "336h"   # 14 days, rotated on use (AI-38)
  }
  default_env = {
    api = {
      FYNIQ_SIGNIN           = "identity-platform"
      FYNIQ_PUBLIC_ORIGIN    = var.public_origin
      FYNIQ_IDP_PROJECT      = var.project
      FYNIQ_SIGNIN_PROVIDERS = jsonencode(var.signin_providers)
      FYNIQ_API_AUDIENCE     = "${var.public_origin}/api"
      FYNIQ_MCP_AUDIENCE     = "${var.public_origin}/mcp"
    }
    oauth         = local.hydra_env
    oauth-admin   = local.hydra_env
    oauth-migrate = {}
  }
  default_args = {
    oauth         = ["serve", "public", "--sqa-opt-out"]
    oauth-admin   = ["serve", "admin", "--sqa-opt-out"]
    oauth-migrate = ["migrate", "sql", "up", "-e", "--yes"]
  }
  args = merge(local.default_args, var.service_args)

  # Each service's secrets, environment variable → secret (SEC-12): the database as its own role, Hydra's
  # DSN and system secret, and any the root adds. The values are added out of band, never in state.
  secret_env = { for name in keys(local.runners) : name => merge(
    contains(keys(local.with_db), name) ? { FYNIQ_DATABASE_URL = "${name}-database-url" } : {},
    contains(["oauth", "oauth-admin"], name) ? { DSN = "oauth-dsn", SECRETS_SYSTEM = "oauth-system-secret" } : {},
    name == "oauth-migrate" ? { DSN = "oauth-dsn" } : {},
    name == "api" ? { FYNIQ_IDP_API_KEY = "idp-api-key" } : {},
    lookup(var.service_secrets, name, {}),
  ) }
  secrets = toset(flatten([for name, m in local.secret_env : values(m)]))
  secret_access = { for pair in flatten([for name, m in local.secret_env : [for secret in values(m) : { name = name, secret = secret }]]) :
  "${pair.secret}/${pair.name}" => pair }
}

resource "google_project_service" "apis" {
  for_each           = toset(local.apis)
  project            = var.project
  service            = "${each.value}.googleapis.com"
  disable_on_destroy = false
}

resource "google_service_account" "all" {
  for_each     = local.accounts
  project      = var.project
  account_id   = each.value
  display_name = "Fyniq ${local.spec.environment}: ${each.key}"
  depends_on   = [google_project_service.apis]
}
