# Staging or production, whole: the cloud-neutral spec, the environment on Google Cloud, its audit
# anchors (in fyniq-audit for production, §12.4) and its budget. The roots in infra/gcp/envs/ only choose
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

module "environment" {
  source            = "../environment"
  spec              = module.spec.spec
  project           = var.project
  anchors           = { bucket = module.anchors.bucket, key_version = module.anchors.key_version }
  public_origin     = var.public_origin
  signin_providers  = var.signin_providers
  artifact_registry = var.artifact_registry
  break_glass       = var.break_glass
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

output "accounts" {
  description = "Each service's, job's and invoker's service account."
  value       = module.environment.accounts
}
