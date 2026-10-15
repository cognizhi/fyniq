# What the whole organisation shares (§12.4): the organisation policies and CI's identity in fyniq-ci.
# A founder applies it, through just-in-time elevation, because it is what gives CI its own access.

terraform {
  required_version = ">= 1.13.0"
  required_providers {
    google = { source = "hashicorp/google", version = "= 8.5.0" }
  }
  backend "gcs" {
    prefix = "shared" # bucket: -backend-config=bucket=<the bootstrap root's output>
  }
}

variable "organization_id" {
  description = "The founder's Google Cloud organisation ID."
  type        = string
}

variable "ci_project" {
  description = "The CI project: Artifact Registry and CI identities (§12.4)."
  type        = string
  default     = "fyniq-ci"
}

variable "environments" {
  description = "Each environment's own project (CTL-CHG-05; §12.4 names). [verify the project IDs once the founder creates them]"
  type        = map(string)
  default     = { dev = "fyniq-dev", staging = "fyniq-staging", production = "fyniq-prod" }
}

provider "google" {
  project        = var.ci_project
  region         = "asia-southeast1"
  default_labels = { managed-by = "opentofu", root = "shared" }
}

module "org_policy" {
  source       = "../modules/org-policy"
  parent       = "organizations/${var.organization_id}"
  cmek_parents = [for env in ["staging", "production"] : "projects/${var.environments[env]}"]
}

module "ci_identity" {
  source       = "../modules/ci-identity"
  project      = var.ci_project
  environments = var.environments
}

output "workload_identity_provider" {
  description = "For the GitHub repository variable GCP_WORKLOAD_IDENTITY_PROVIDER."
  value       = module.ci_identity.provider
}

output "service_accounts" {
  description = "For the GitHub repository variables: the identity each CI job impersonates."
  value       = module.ci_identity.service_accounts
}

output "artifact_registry" {
  description = "Where the release workflow pushes images."
  value       = module.ci_identity.repository
}
