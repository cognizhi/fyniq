# Staging or production, whole: the cloud-neutral spec, the environment on Google Cloud, its audit
# anchors and weekly export (in fyniq-audit for production, §12.4), its evidence buckets and its budget. The roots in infra/gcp/envs/ only choose
# the values.

terraform {
  required_version = ">= 1.13.0"
  required_providers {
    google      = { source = "hashicorp/google", version = "~> 8.5.0" }
    google-beta = { source = "hashicorp/google-beta", version = "~> 8.5.0" }
  }
}

variable "environment" {
  description = "staging or production."
  type        = string
}

variable "project" {
  description = "The environment's own project."
  type        = string
}

variable "secrets_project" {
  description = "The project of the organisations' connector secrets, which the founder creates (design D12)."
  type        = string
}

variable "audit_project" {
  description = "Where the audit anchors live: fyniq-audit for production (§12.4), the environment's own project for staging."
  type        = string
}

variable "anchors" {
  description = "How long anchors are kept, and whether the retention is locked (irreversible; production only)."
  type = object({
    retention_days = number
    lock           = bool
  })
}

variable "database" {
  description = "The database's tier, availability and disk (§11.3, §12.6)."
  type = object({
    tier         = string
    availability = string
    disk_gb      = number
  })
}

variable "billing_account" {
  description = "The billing account (from the founder)."
  type        = string
}

variable "budget" {
  description = "The monthly budget in US dollars and who is told."
  type = object({
    amount_usd = number
    emails     = list(string)
  })
}

variable "platform_owner_emails" {
  description = "The named platform owner, whom the heartbeat's alerts tell first (NFR-OBS-02); empty: the Owners."
  type        = list(string)
  default     = []
}

variable "public_origin" {
  description = "The web app's https origin."
  type        = string
}

variable "signin_providers" {
  description = "The sign-in providers (FYNIQ_SIGNIN_PROVIDERS)."
  type = list(object({
    id        = string
    name      = string
    idp       = string
    client_id = string
    issuer    = string
  }))
  default = []
}

variable "artifact_registry" {
  description = "The CI project's image repository; null until it exists."
  type = object({
    project    = string
    location   = string
    repository = string
  })
  default = null
}

variable "break_glass" {
  description = "Just-in-time elevation (CTL-IAM-03); null for none."
  type = object({
    requesters = list(string)
    approvers  = list(string)
    role       = string
    duration   = string
  })
  default = null
}

module "spec" {
  source      = "../../../modules/platform-spec"
  environment = var.environment
  database    = var.database
  budget      = var.budget
  anchors     = var.anchors
}

module "anchors" {
  source         = "../anchors"
  project        = var.audit_project
  environment    = var.environment
  retention_days = module.spec.spec.anchors.retention_days
  lock           = module.spec.spec.anchors.lock
  writers        = [module.environment.members["jobs"]]
}

# The weekly export of the database beside the anchors (DR-02, DR-04): in fyniq-audit for production, locked where the
# anchors are.
module "db_export" {
  source         = "../db-export"
  project        = var.project
  audit_project  = var.audit_project
  environment    = var.environment
  database_agent = module.environment.database_agent
  jobs_member    = module.environment.members["jobs"]
  lock           = module.spec.spec.anchors.lock
}

# The records' and attachments' buckets (DR-07, CTL-AUD-03): staging, and the evidence bucket that locks each object.
module "evidence" {
  source      = "../evidence"
  project     = var.project
  environment = var.environment
  members     = { api = module.environment.members["api"], worker = module.environment.members["worker"], jobs = module.environment.members["jobs"] }
}

module "environment" {
  source            = "../environment"
  spec              = module.spec.spec
  project           = var.project
  secrets_project   = var.secrets_project
  anchors           = { bucket = module.anchors.bucket, key_version = module.anchors.key_version }
  exports           = module.db_export.buckets
  evidence          = module.evidence.buckets
  public_origin     = var.public_origin
  signin_providers  = var.signin_providers
  artifact_registry = var.artifact_registry
  break_glass       = var.break_glass
}

# The central log store (SEC-30): data-access audit logs, and the audit logs and security alerts kept 400 days beside the
# anchors, locked where they are.
module "security_logs" {
  source        = "../security-logs"
  project       = var.project
  audit_project = var.audit_project
  environment   = var.environment
  lock          = module.spec.spec.anchors.lock
}

# Staging only: the weekly scan of its database for production data (CTL-CHG-05 W, DEV-08).
module "nonprod_scan" {
  count          = var.environment == "production" ? 0 : 1
  source         = "../nonprod-scan"
  project        = var.project
  environment    = var.environment
  instance       = module.environment.database_instance
  database_agent = module.environment.database_agent
}

# The edge (SEC-23): the load balancer and Cloud Armor in front of the public services, on the public origin's host, with
# the local stack's paths (deploy/local/Caddyfile).
locals {
  edge_paths = {
    api   = { paths = ["/api/*"], strip = true }
    oauth = { paths = ["/oauth/*"], strip = true }
    ai    = { paths = ["/ai/*"], strip = true }
    mcp   = { paths = ["/mcp", "/mcp/*"], strip = false }
  }
}

module "edge" {
  source   = "../edge"
  project  = var.project
  name     = "fyniq-${var.environment}"
  domain   = trimprefix(var.public_origin, "https://")
  services = { for name, s in module.spec.spec.services : name => local.edge_paths[name] if s.public }
}

# The public services' objectives and their burn-rate alerts to the founders (NFR-AVAIL-01, NFR-PERF-01, CTL-RES-03).
module "slo" {
  source      = "../slo"
  project     = var.project
  environment = var.environment
  services    = [for name, s in module.spec.spec.services : name if s.public]
  channels    = module.environment.alert_channels
  # NFR-COST-02: every service is idle when nobody uses Fyniq, but production's warm scanner, a listed fixed fee.
  idle_services = keys(module.spec.spec.services)
  warm_services = var.environment == "production" ? ["scanner"] : []
}

# The heartbeat's absence alerts: the platform owner first, the Owners by escalation (NFR-OBS-02, CTL-RES-05).
module "heartbeat" {
  source                = "../heartbeat"
  project               = var.project
  environment           = var.environment
  platform_owner_emails = var.platform_owner_emails
  owner_channels        = module.environment.alert_channels
}

module "budget" {
  source          = "../budget"
  billing_account = var.billing_account
  project         = var.project
  name            = "fyniq-${var.environment}"
  amount          = module.spec.spec.budget.amount_usd
  thresholds      = module.spec.spec.budget.thresholds
  emails          = module.spec.spec.budget.emails
}

output "gcp_config" {
  description = "FYNIQ_GCP as the services read it."
  value       = module.environment.gcp_config
}

output "urls" {
  description = "Each service's URL."
  value       = module.environment.urls
}

output "edge_address" {
  description = "The edge's address: the public origin's DNS A record."
  value       = module.edge.address
}

output "accounts" {
  description = "Each service's, job's and invoker's service account."
  value       = module.environment.accounts
}
