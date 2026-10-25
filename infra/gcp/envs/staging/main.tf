# The staging environment (§12.4): its own project, planned and applied from CI on main through Workload
# Identity Federation. The whole platform, zonal, with the US$120 budget.

terraform {
  required_version = ">= 1.13.0"
  required_providers {
    google      = { source = "hashicorp/google", version = "= 8.5.0" }
    google-beta = { source = "hashicorp/google-beta", version = "= 8.5.0" }
  }
  backend "gcs" {
    prefix = "envs/staging" # bucket: -backend-config=bucket=<the bootstrap root's output>
  }
}

variable "project" {
  description = "The staging project. [verify the project ID once the founder creates it]"
  type        = string
  default     = "fyniq-staging"
}

variable "secrets_project" {
  description = "The project of the organisations' connector secrets (design D12). [verify the project ID once the founder creates it]"
  type        = string
  default     = "fyniq-staging-secrets"
}

variable "billing_account" {
  description = "The billing account (from the founder)."
  type        = string
}

variable "budget_emails" {
  description = "Who gets the budget alerts: both founders."
  type        = list(string)
}

variable "budget_usd" {
  description = "The monthly budget (open question 9: production US$400, development and staging US$120)."
  type        = number
  default     = 120
}

variable "public_origin" {
  description = "The web app's https origin for staging."
  type        = string
}

variable "signin_providers" {
  description = "The sign-in providers on Identity Platform (FR-ADM-08)."
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
  description = "The CI project's image repository (the shared root's output); null until it exists."
  type = object({
    project    = string
    location   = string
    repository = string
  })
  default = null
}

provider "google" {
  project        = var.project
  region         = "asia-southeast1"
  default_labels = { managed-by = "opentofu", environment = "staging" }
}

provider "google-beta" {
  project        = var.project
  region         = "asia-southeast1"
  default_labels = { managed-by = "opentofu", environment = "staging" }
}

module "stack" {
  source            = "../../modules/stack"
  environment       = "staging"
  project           = var.project
  secrets_project   = var.secrets_project
  audit_project     = var.project
  anchors           = { retention_days = 30, lock = false } # staging's anchors are test data: kept 30 days, never locked
  database          = { tier = "db-custom-1-3840", availability = "ZONAL", disk_gb = 10 }
  billing_account   = var.billing_account
  budget            = { amount_usd = var.budget_usd, emails = var.budget_emails }
  public_origin     = var.public_origin
  signin_providers  = var.signin_providers
  artifact_registry = var.artifact_registry
}

output "gcp_config" {
  description = "FYNIQ_GCP as the services read it."
  value       = module.stack.gcp_config
}

output "urls" {
  description = "Each service's URL."
  value       = module.stack.urls
}
