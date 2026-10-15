# req: CTL-CHG-05
# req: CTL-AUD-02
# req: NFR-COST-01
# req: CTL-IAM-03

mock_provider "google" {
  mock_data "google_project" {
    defaults = { number = "123456789012" }
  }
  mock_data "google_storage_project_service_account" {
    defaults = {
      email_address = "service-123456789012@gs-project-accounts.iam.gserviceaccount.com"
      member        = "serviceAccount:service-123456789012@gs-project-accounts.iam.gserviceaccount.com"
    }
  }
  mock_resource "google_kms_crypto_key" {
    defaults = { id = "projects/fyniq-audit/locations/asia-southeast1/keyRings/audit-production/cryptoKeys/anchor" }
  }
  mock_resource "google_compute_network" {
    defaults = { id = "projects/fyniq-prod/global/networks/platform" }
  }
  mock_resource "google_service_account" {
    defaults = { member = "serviceAccount:run-jobs@fyniq-prod.iam.gserviceaccount.com" }
  }
}

mock_provider "google-beta" {
  mock_resource "google_project_service_identity" {
    defaults = { member = "serviceAccount:service-123456789012@gcp-sa-cloud-sql.iam.gserviceaccount.com" }
  }
}

variables {
  billing_account = "012345-6789AB-CDEF01"
  public_origin   = "https://fyniq.example"
}

# Production: its own project, regional HA, anchors locked in fyniq-audit, US$400, and break-glass only.
run "production" {
  command = plan
  variables {
    environment   = "production"
    project       = "fyniq-prod"
    audit_project = "fyniq-audit"
    anchors       = { retention_days = 2557, lock = true }
    database      = { tier = "db-custom-1-3840", availability = "REGIONAL", disk_gb = 10 }
    budget        = { amount_usd = 400, emails = ["founder-one@example.com", "founder-two@example.com"] }
    break_glass   = { requesters = ["group:platform-admins@example.com"], approvers = ["group:founders@example.com"], role = "roles/editor", duration = "3600s" }
  }
  assert {
    condition     = module.anchors.bucket == "fyniq-production-audit-anchors"
    error_message = "production's anchors"
  }
  assert {
    condition     = jsondecode(output.gcp_config).anchors.bucket == module.anchors.bucket && strcontains(jsondecode(output.gcp_config).anchors.key_version, "/cryptoKeyVersions/1")
    error_message = "the services sign and store anchors where the anchors module made them"
  }
  assert {
    condition     = startswith(jsondecode(output.gcp_config).tasks.worker.queue, "projects/fyniq-prod/")
    error_message = "production's queues are in production's own project (CTL-CHG-05)"
  }
}

run "staging" {
  command = plan
  variables {
    environment   = "staging"
    project       = "fyniq-staging"
    audit_project = "fyniq-staging"
    anchors       = { retention_days = 30, lock = false }
    database      = { tier = "db-custom-1-3840", availability = "ZONAL", disk_gb = 10 }
    budget        = { amount_usd = 120, emails = ["founder-one@example.com"] }
  }
  assert {
    condition     = startswith(jsondecode(output.gcp_config).tasks.executor.queue, "projects/fyniq-staging/") && module.anchors.bucket == "fyniq-staging-audit-anchors"
    error_message = "staging is wired to its own project"
  }
}
