# req: SEC-04
# req: CTL-IAM-04

mock_provider "google" {
  mock_resource "google_service_account" {
    defaults = {
      email  = "ci-mock@fyniq-ci.iam.gserviceaccount.com"
      member = "serviceAccount:ci-mock@fyniq-ci.iam.gserviceaccount.com"
      name   = "projects/fyniq-ci/serviceAccounts/ci-mock@fyniq-ci.iam.gserviceaccount.com"
    }
  }
}

variables {
  project      = "fyniq-ci"
  environments = { staging = "fyniq-staging", production = "fyniq-prod" }
}

# Workload Identity Federation trusts this repository's main branch and tags only, by repository and
# owner ID; every CI identity is impersonated through it, and none has a key.
run "ci_gets_credentials_only_from_main_and_tags" {
  command = plan
  assert {
    condition     = google_iam_workload_identity_pool_provider.github.oidc[0].issuer_uri == "https://token.actions.githubusercontent.com"
    error_message = "the provider must trust GitHub's OIDC issuer only"
  }
  assert {
    condition = alltrue([for part in [
      "assertion.repository == 'cognizhi/fyniq'",
      "assertion.repository_id == '1415057474'",
      "assertion.repository_owner_id == '273236245'",
      "(assertion.ref == 'refs/heads/main' || assertion.ref_type == 'tag')",
    ] : strcontains(google_iam_workload_identity_pool_provider.github.attribute_condition, part)])
    error_message = "the condition must name the repository and owner by ID, and allow only main and tags"
  }
  assert {
    condition     = toset(keys(google_service_account_iam_member.workload_identity)) == toset(["publisher", "tofu-staging", "tofu-production"])
    error_message = "every CI identity must be reached through Workload Identity Federation"
  }
  assert {
    condition     = alltrue([for m in google_service_account_iam_member.workload_identity : m.role == "roles/iam.workloadIdentityUser"])
    error_message = "CI may only impersonate, never hold another role on its identities"
  }
  assert {
    condition     = google_project_iam_member.tofu["production"].project == "fyniq-prod" && google_project_iam_member.tofu["staging"].project == "fyniq-staging"
    error_message = "each OpenTofu identity must own its own environment only"
  }
  assert {
    condition     = google_artifact_registry_repository.images.docker_config[0].immutable_tags
    error_message = "release image tags must be immutable: images deploy by digest (NFR-IMG-05)"
  }
}
