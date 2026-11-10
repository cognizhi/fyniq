# req: CTL-CHG-05
# req: DEV-08

mock_provider "google" {
  mock_resource "google_service_account" {
    defaults = {
      email  = "scan-export@fyniq-staging.iam.gserviceaccount.com"
      member = "serviceAccount:scan-export@fyniq-staging.iam.gserviceaccount.com"
    }
  }
  mock_resource "google_project_iam_custom_role" {
    defaults = {
      id = "projects/fyniq-staging/roles/fyniqDatabaseExport"
    }
  }
  mock_data "google_storage_project_service_account" {
    defaults = {
      member = "serviceAccount:service-123@gs-project-accounts.iam.gserviceaccount.com"
    }
  }
}

mock_provider "google-beta" {
  mock_resource "google_project_service_identity" {
    defaults = {
      member = "serviceAccount:service-123@dlp-api.iam.gserviceaccount.com"
    }
  }
}

variables {
  project        = "fyniq-staging"
  environment    = "staging"
  instance       = "fyniq-staging"
  database_agent = "p123-abc@gcp-sa-cloud-sql.iam.gserviceaccount.com"
}

# Each week staging's database is exported to a bucket of its own in Singapore, under its own key, kept a week, and a
# Sensitive Data Protection job inspects it for production data, reporting to Security Command Center and by email; the
# export's identity may export and nothing else.
run "staging_is_scanned_each_week_for_production_data" {
  command = plan
  assert {
    condition = (google_data_loss_prevention_job_trigger.scan.triggers[0].schedule[0].recurrence_period_duration == "604800s" &&
    google_data_loss_prevention_job_trigger.scan.inspect_job[0].storage_config[0].cloud_storage_options[0].file_set[0].url == "gs://fyniq-staging-database-export/**")
    error_message = "a weekly scan of the export's bucket"
  }
  assert {
    condition = (toset([for t in google_data_loss_prevention_job_trigger.scan.inspect_job[0].inspect_config[0].info_types : t.name]) ==
    toset(["SINGAPORE_NATIONAL_REGISTRATION_ID_NUMBER", "SINGAPORE_PASSPORT", "CREDIT_CARD_NUMBER", "IBAN_CODE"]))
    error_message = "NRIC and FIN numbers, passports, card and bank numbers"
  }
  assert {
    condition = (length([for a in google_data_loss_prevention_job_trigger.scan.inspect_job[0].actions : a if length(a.publish_summary_to_cscc) == 1]) == 1 &&
    length([for a in google_data_loss_prevention_job_trigger.scan.inspect_job[0].actions : a if length(a.job_notification_emails) == 1]) == 1)
    error_message = "its summary goes to Security Command Center and by email"
  }
  assert {
    condition = (jsondecode(base64decode(google_cloud_scheduler_job.export.http_target[0].body)).exportContext.uri == "gs://fyniq-staging-database-export/fyniq.sql.gz" &&
      google_cloud_scheduler_job.export.http_target[0].uri == "https://sqladmin.googleapis.com/v1/projects/fyniq-staging/instances/fyniq-staging/export" &&
    google_cloud_scheduler_job.export.http_target[0].oauth_token[0].service_account_email == "scan-export@fyniq-staging.iam.gserviceaccount.com")
    error_message = "each week the database is exported to the bucket, as the export's own identity"
  }
  assert {
    condition     = google_project_iam_custom_role.export.permissions == toset(["cloudsql.instances.export"])
    error_message = "the export's identity may export the database and nothing else"
  }
  assert {
    condition = (google_storage_bucket.export.location == "asia-southeast1" && google_storage_bucket.export.public_access_prevention == "enforced" &&
      google_storage_bucket.export.encryption[0].default_kms_key_name != "" &&
    length([for r in google_storage_bucket.export.lifecycle_rule : r if one(r.condition).age == 8 && one(r.action).type == "Delete"]) == 1)
    error_message = "the export is kept in Singapore, under its own key, never public, for a week"
  }
}

# Production is never scanned this way: the module refuses it.
run "production_is_refused" {
  command = plan
  variables {
    project     = "fyniq-production"
    environment = "production"
  }
  expect_failures = [var.environment]
}
