# req: DR-07
# req: NFR-RET-01
# req: CTL-AUD-03
# req: FR-DOC-02

mock_provider "google" {
  mock_resource "google_kms_crypto_key" {
    defaults = { id = "projects/fyniq-prod/locations/asia-southeast1/keyRings/evidence-production/cryptoKeys/evidence" }
  }
  mock_resource "google_project_iam_custom_role" {
    defaults = { id = "projects/fyniq-prod/roles/fyniqEvidenceCommitter" }
  }
  mock_data "google_storage_project_service_account" {
    defaults = { member = "serviceAccount:service-123@gs-project-accounts.iam.gserviceaccount.com" }
  }
}

variables {
  project     = "fyniq-prod"
  environment = "production"
  members = {
    api    = "serviceAccount:run-api@fyniq-prod.iam.gserviceaccount.com"
    worker = "serviceAccount:run-worker@fyniq-prod.iam.gserviceaccount.com"
    jobs   = "serviceAccount:run-jobs@fyniq-prod.iam.gserviceaccount.com"
  }
}

# The evidence bucket locks each object: Object Retention Lock on, versioned, no bucket-wide policy and no lifecycle
# deletion; both buckets in Singapore under their own key, never public.
run "the_records_bucket_locks_each_object" {
  command = plan
  assert {
    condition = (google_storage_bucket.evidence.enable_object_retention && google_storage_bucket.evidence.versioning[0].enabled &&
    length(google_storage_bucket.evidence.retention_policy) == 0 && length(google_storage_bucket.evidence.lifecycle_rule) == 0)
    error_message = "the evidence bucket keeps each object until its own Locked retain-until, and deletes nothing by age"
  }
  assert {
    condition = alltrue([for b in [google_storage_bucket.staging, google_storage_bucket.evidence] :
      b.location == "asia-southeast1" && b.public_access_prevention == "enforced" && b.uniform_bucket_level_access &&
    b.encryption[0].default_kms_key_name == "projects/fyniq-prod/locations/asia-southeast1/keyRings/evidence-production/cryptoKeys/evidence"])
    error_message = "both buckets are in Singapore, never public, under the evidence key"
  }
  assert {
    condition     = output.buckets == { staging = "fyniq-production-evidence-staging", bucket = "fyniq-production-evidence" }
    error_message = "the buckets' names are the ones FYNIQ_GCP gives the services"
  }
}

# What waits in staging is deleted by the app in its time; only a missed report export goes by age.
run "staging_deletes_only_missed_exports" {
  command = plan
  assert {
    condition = (length([for r in google_storage_bucket.staging.lifecycle_rule : r if one(r.action).type == "Delete"]) == 1 &&
    one([for r in google_storage_bucket.staging.lifecycle_rule : one(r.condition).matches_prefix if one(r.action).type == "Delete"]) == tolist(["exports/"]))
    error_message = "staging deletes by age only under exports/: an upload waits there until its record is final"
  }
}

# The worker and the jobs commit and may never delete; the api only reads committed files; all three use staging.
run "nobody_deletes_a_committed_file" {
  command = plan
  assert {
    condition     = !anytrue([for p in google_project_iam_custom_role.committer.permissions : strcontains(p, "delete") || strcontains(p, "overrideUnlockedRetention")])
    error_message = "the committer's role may not delete an object or shorten its retention"
  }
  assert {
    condition = (toset([for m in google_storage_bucket_iam_member.committers : m.member]) == toset(["serviceAccount:run-worker@fyniq-prod.iam.gserviceaccount.com", "serviceAccount:run-jobs@fyniq-prod.iam.gserviceaccount.com"]) &&
    google_storage_bucket_iam_member.api_reads.role == "roles/storage.objectViewer" && google_storage_bucket_iam_member.api_reads.member == "serviceAccount:run-api@fyniq-prod.iam.gserviceaccount.com")
    error_message = "the worker and the jobs commit; the api reads"
  }
  assert {
    condition     = length(google_storage_bucket_iam_member.staging) == 3 && alltrue([for m in google_storage_bucket_iam_member.staging : m.role == "roles/storage.objectUser"])
    error_message = "the api, the worker and the jobs use the staging bucket"
  }
}
