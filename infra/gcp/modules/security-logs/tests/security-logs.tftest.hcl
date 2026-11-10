# req: SEC-30
# req: AUD-06

mock_provider "google" {
  mock_resource "google_logging_project_bucket_config" {
    defaults = {
      id = "projects/fyniq-audit/locations/asia-southeast1/buckets/fyniq-production-security"
    }
  }
  mock_resource "google_logging_project_sink" {
    defaults = {
      writer_identity = "serviceAccount:service-123@gcp-sa-logging.iam.gserviceaccount.com"
    }
  }
}

variables {
  project       = "fyniq-production"
  audit_project = "fyniq-audit"
  environment   = "production"
  lock          = true
}

# Production: data-access logs for the sensitive services, Cloud Storage included (its anchors live in fyniq-audit); the
# audit logs and the security alerts go to a bucket in fyniq-audit, in Singapore, kept 400 days and locked, written by
# the sink's own identity on that bucket only.
run "production_keeps_its_security_logs_in_the_audit_project" {
  command = apply
  assert {
    condition = (toset(keys(google_project_iam_audit_config.sensitive)) == toset(["sqladmin.googleapis.com", "secretmanager.googleapis.com",
    "cloudkms.googleapis.com", "iam.googleapis.com", "storage.googleapis.com"]))
    error_message = "data-access logs for Cloud SQL, Secret Manager, KMS, IAM and Cloud Storage"
  }
  assert {
    condition = (alltrue([for c in google_project_iam_audit_config.sensitive :
    toset([for l in c.audit_log_config : l.log_type]) == toset(["ADMIN_READ", "DATA_READ", "DATA_WRITE"]) && c.project == "fyniq-production"]))
    error_message = "every read and write, in the environment's project"
  }
  assert {
    condition = (google_logging_project_bucket_config.security.project == "fyniq-audit" && google_logging_project_bucket_config.security.location == "asia-southeast1" &&
    google_logging_project_bucket_config.security.retention_days == 400 && google_logging_project_bucket_config.security.locked)
    error_message = "a bucket in fyniq-audit, in Singapore, kept 400 days, locked"
  }
  assert {
    condition = (google_logging_project_sink.security.unique_writer_identity && strcontains(google_logging_project_sink.security.filter, "cloudaudit.googleapis.com") &&
      strcontains(google_logging_project_sink.security.filter, "fyniq.security_alert") &&
    google_logging_project_sink.security.destination == "logging.googleapis.com/projects/fyniq-audit/locations/asia-southeast1/buckets/fyniq-production-security")
    error_message = "the sink sends the audit logs and the security alerts to the bucket"
  }
  assert {
    condition = (length(google_project_iam_member.sink_writer) == 1 && google_project_iam_member.sink_writer[0].role == "roles/logging.bucketWriter" &&
    google_project_iam_member.sink_writer[0].project == "fyniq-audit" && strcontains(google_project_iam_member.sink_writer[0].condition[0].expression, "buckets/fyniq-production-security"))
    error_message = "the sink's own identity writes to that bucket only"
  }
}

# Staging keeps its anchors and its security logs in its own project: the anchors module logs Cloud Storage there, the
# sink needs no grant, and nothing is locked.
run "staging_keeps_them_in_its_own_project" {
  command = plan
  variables {
    project       = "fyniq-staging"
    audit_project = "fyniq-staging"
    environment   = "staging"
    lock          = false
  }
  assert {
    condition     = !contains(keys(google_project_iam_audit_config.sensitive), "storage.googleapis.com") && length(google_project_iam_audit_config.sensitive) == 4
    error_message = "Cloud Storage is the anchors module's in staging"
  }
  assert {
    condition     = length(google_project_iam_member.sink_writer) == 0 && !google_logging_project_bucket_config.security.locked
    error_message = "no grant, and no lock, in staging"
  }
}
