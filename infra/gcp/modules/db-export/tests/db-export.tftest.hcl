# req: DR-02
# req: DR-04
# req: NFR-DR-01

mock_provider "google" {
  mock_resource "google_kms_crypto_key" {
    defaults = { id = "projects/fyniq-audit/locations/asia-southeast1/keyRings/exports-production/cryptoKeys/db-export" }
  }
  mock_resource "google_project_iam_custom_role" {
    defaults = { id = "projects/fyniq-prod/roles/fyniqWeeklyExport" }
  }
  mock_data "google_storage_project_service_account" {
    defaults = { member = "serviceAccount:service-123@gs-project-accounts.iam.gserviceaccount.com" }
  }
}

variables {
  project        = "fyniq-prod"
  audit_project  = "fyniq-audit"
  environment    = "production"
  database_agent = "p123-abc@gcp-sa-cloud-sql.iam.gserviceaccount.com"
  jobs_member    = "serviceAccount:run-jobs@fyniq-prod.iam.gserviceaccount.com"
  lock           = true
}

# Production: the exports live in fyniq-audit, in Singapore, under their own key; the week's kept 35 days and the
# month's a year, both locked; the instance writes them, and the jobs' account may only export.
run "production_exports_outlive_the_production_project" {
  command = plan
  assert {
    condition = alltrue([for b in google_storage_bucket.exports : b.project == "fyniq-audit" && b.location == "asia-southeast1" &&
    b.public_access_prevention == "enforced" && b.retention_policy[0].is_locked])
    error_message = "the exports are in fyniq-audit, in Singapore, never public, their retention locked"
  }
  assert {
    condition     = google_storage_bucket.exports["weekly"].retention_policy[0].retention_period == tostring(35 * 86400) && google_storage_bucket.exports["monthly"].retention_policy[0].retention_period == tostring(365 * 86400)
    error_message = "the week's export kept 35 days, the month's a year"
  }
  assert {
    condition     = length([for r in google_storage_bucket.exports["weekly"].lifecycle_rule : r if one(r.condition).age == 36]) == 1 && length([for r in google_storage_bucket.exports["monthly"].lifecycle_rule : r if one(r.condition).age == 366]) == 1
    error_message = "each export goes the day after its retention ends"
  }
  assert {
    condition     = output.buckets == { weekly = "fyniq-production-db-weekly", monthly = "fyniq-production-db-monthly" }
    error_message = "the buckets' names are the ones FYNIQ_GCP gives the job"
  }
  assert {
    condition = (toset([for m in google_storage_bucket_iam_member.database_writes : m.member]) == toset(["serviceAccount:p123-abc@gcp-sa-cloud-sql.iam.gserviceaccount.com"]) &&
    alltrue([for m in google_storage_bucket_iam_member.database_writes : m.role == "roles/storage.objectCreator"]))
    error_message = "the instance's own account writes the exports, and may not overwrite or delete one"
  }
  assert {
    condition     = google_project_iam_custom_role.export.permissions == toset(["cloudsql.instances.export", "cloudsql.instances.get"]) && google_project_iam_member.export.member == "serviceAccount:run-jobs@fyniq-prod.iam.gserviceaccount.com"
    error_message = "the jobs' account may start an export and read it, and nothing else"
  }
}

run "staging_is_not_locked" {
  command = plan
  variables {
    project       = "fyniq-staging"
    audit_project = "fyniq-staging"
    environment   = "staging"
    lock          = false
  }
  assert {
    condition     = !anytrue([for b in google_storage_bucket.exports : b.retention_policy[0].is_locked])
    error_message = "nothing is locked in staging"
  }
}
