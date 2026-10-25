# CI's identity in the fyniq-ci project (§12.4): Workload Identity Federation for this repository's main
# branch and tags only, so no service-account key exists anywhere (SEC-04, CTL-IAM-04); Artifact Registry
# for the release images; one service account per job CI does.

terraform {
  required_version = ">= 1.13.0"
  required_providers {
    google = { source = "hashicorp/google", version = "~> 8.5.0" }
  }
}

variable "project" {
  description = "The CI project (fyniq-ci)."
  type        = string
}

variable "region" {
  description = "Where Artifact Registry keeps the images."
  type        = string
  default     = "asia-southeast1"
}

variable "repository" {
  description = "The GitHub repository CI runs for, by name and by ID (the ID survives a rename or a re-created repository of the same name)."
  type = object({
    name     = string
    id       = string
    owner_id = string
  })
  default = { name = "cognizhi/fyniq", id = "1415057474", owner_id = "273236245" }
}

variable "environments" {
  description = "The projects CI plans and applies, by environment: each gets its own OpenTofu service account, owner of that project only."
  type        = map(string)
}

variable "secrets_projects" {
  description = "Each environment's project of the organisations' connector secrets (p1-bank-import-reconcile design D12): its OpenTofu service account owns it too."
  type        = map(string)
  default     = {}
}

# The APIs CI calls through this project: token exchange, images, and the budgets of every environment.
resource "google_project_service" "apis" {
  for_each           = toset(["iam", "iamcredentials", "sts", "artifactregistry", "billingbudgets", "cloudresourcemanager", "serviceusage", "orgpolicy"])
  project            = var.project
  service            = "${each.value}.googleapis.com"
  disable_on_destroy = false
}

resource "google_iam_workload_identity_pool" "github" {
  project                   = var.project
  workload_identity_pool_id = "github"
  display_name              = "GitHub Actions"
  depends_on                = [google_project_service.apis]
  description               = "CI for ${var.repository.name}: main and tags only (SEC-04)"
}

resource "google_iam_workload_identity_pool_provider" "github" {
  project                            = var.project
  workload_identity_pool_id          = google_iam_workload_identity_pool.github.workload_identity_pool_id
  workload_identity_pool_provider_id = "github"
  display_name                       = "GitHub OIDC"
  oidc {
    issuer_uri = "https://token.actions.githubusercontent.com"
  }
  attribute_mapping = {
    "google.subject"       = "assertion.sub"
    "attribute.repository" = "assertion.repository"
    "attribute.ref"        = "assertion.ref"
    "attribute.workflow"   = "assertion.workflow_ref"
  }
  # Only this repository (by ID, not only by name), and only its main branch or a tag: a pull request,
  # a fork or another branch gets no Google credential at all.
  attribute_condition = join(" && ", [
    "assertion.repository == '${var.repository.name}'",
    "assertion.repository_id == '${var.repository.id}'",
    "assertion.repository_owner_id == '${var.repository.owner_id}'",
    "(assertion.ref == 'refs/heads/main' || assertion.ref_type == 'tag')",
  ])
}

resource "google_artifact_registry_repository" "images" {
  project       = var.project
  location      = var.region
  repository_id = "images"
  description   = "Release images, deployed by digest (NFR-IMG-05)"
  depends_on    = [google_project_service.apis]
  format        = "DOCKER"
  docker_config {
    immutable_tags = true
  }
}

# One identity per CI job: publishing images, and planning or applying each environment.
resource "google_service_account" "publisher" {
  project      = var.project
  account_id   = "ci-publisher"
  display_name = "CI: pushes release images"
}

resource "google_service_account" "tofu" {
  for_each     = var.environments
  project      = var.project
  account_id   = "tofu-${each.key}"
  display_name = "CI: OpenTofu for ${each.key}"
}

resource "google_artifact_registry_repository_iam_member" "publisher" {
  project    = var.project
  location   = var.region
  repository = google_artifact_registry_repository.images.repository_id
  role       = "roles/artifactregistry.writer"
  member     = google_service_account.publisher.member
}

resource "google_project_iam_member" "tofu" {
  for_each = var.environments
  project  = each.value
  role     = "roles/owner" # a machine identity used only from main and tags; no human holds it (CTL-IAM-03)
  member   = google_service_account.tofu[each.key].member
}

resource "google_project_iam_member" "tofu_secrets" {
  for_each = { for env, project in var.secrets_projects : env => project if contains(keys(var.environments), env) }
  project  = each.value
  role     = "roles/owner" # its environment's secrets project, configured with it (org_secrets.tf)
  member   = google_service_account.tofu[each.key].member
}

locals {
  principals = "principalSet://iam.googleapis.com/${google_iam_workload_identity_pool.github.name}/attribute.repository/${var.repository.name}"
  impersonated = merge(
    { publisher = google_service_account.publisher.name },
    { for env, sa in google_service_account.tofu : "tofu-${env}" => sa.name },
  )
}

resource "google_service_account_iam_member" "workload_identity" {
  for_each           = local.impersonated
  service_account_id = each.value
  role               = "roles/iam.workloadIdentityUser"
  member             = local.principals
}

output "provider" {
  description = "The workload identity provider CI names (google-github-actions/auth's workload_identity_provider)."
  value       = google_iam_workload_identity_pool_provider.github.name
}

output "service_accounts" {
  description = "The CI service accounts by job."
  value = merge(
    { publisher = google_service_account.publisher.email },
    { for env, sa in google_service_account.tofu : "tofu-${env}" => sa.email },
  )
}

output "repository" {
  description = "Artifact Registry's repository, where the release images go."
  value       = google_artifact_registry_repository.images.id
}
