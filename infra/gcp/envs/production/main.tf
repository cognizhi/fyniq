# The production environment (§12.4): its own project, planned in P0 and applied only before go-live, from
# CI with the manual approval of §12.4. Regional HA (DR-01), anchors locked in fyniq-audit, the US$400
# budget, and no standing human access (break-glass only).

terraform {
  required_version = ">= 1.13.0"
  required_providers {
    google      = { source = "hashicorp/google", version = "= 8.5.0" }
    google-beta = { source = "hashicorp/google-beta", version = "= 8.5.0" }
  }
  backend "gcs" {
    prefix = "envs/production" # bucket: -backend-config=bucket=<the bootstrap root's output>
  }
}

variable "project" {
  description = "The production project. [verify the project ID once the founder creates it]"
  type        = string
  default     = "fyniq-prod"
}

variable "secrets_project" {
  description = "The project of the organisations' connector secrets (design D12). [verify the project ID once the founder creates it]"
  type        = string
  default     = "fyniq-prod-secrets"
}

variable "audit_project" {
  description = "fyniq-audit: the audit anchors, administered apart from the platform (§12.4)."
  type        = string
  default     = "fyniq-audit"
}

variable "anchor_retention_days" {
  description = "How long an anchor is kept, locked. Audit events about accounting records are kept as long as the records (AUD-06; §14: at least 5 years after the financial year's end, 5 to 6 for income tax). The lock cannot be undone. [verify the number with the accountant before the first apply]"
  type        = number
}

variable "break_glass" {
  description = "Who may request just-in-time elevation in production, who approves (another person), the role and how long (CTL-IAM-03)."
  type = object({
    requesters = list(string)
    approvers  = list(string)
    role       = string
    duration   = string
  })
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
  default     = 400
}

variable "public_origin" {
  description = "The web app's https origin for production."
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
  default_labels = { managed-by = "opentofu", environment = "production" }
}

provider "google-beta" {
  project        = var.project
  region         = "asia-southeast1"
  default_labels = { managed-by = "opentofu", environment = "production" }
}

module "stack" {
  source            = "../../modules/stack"
  environment       = "production"
  project           = var.project
  secrets_project   = var.secrets_project
  audit_project     = var.audit_project
  anchors           = { retention_days = var.anchor_retention_days, lock = true }
  database          = { tier = "db-custom-1-3840", availability = "REGIONAL", disk_gb = 10 }
  billing_account   = var.billing_account
  budget            = { amount_usd = var.budget_usd, emails = var.budget_emails }
  public_origin     = var.public_origin
  signin_providers  = var.signin_providers
  artifact_registry = var.artifact_registry
  break_glass       = var.break_glass
}

output "gcp_config" {
  description = "FYNIQ_GCP as the services read it."
  value       = module.stack.gcp_config
}

output "urls" {
  description = "Each service's URL."
  value       = module.stack.urls
}
