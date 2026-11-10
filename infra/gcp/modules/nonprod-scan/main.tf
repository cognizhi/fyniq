# The weekly scan of non-production for production data (CTL-CHG-05 W, rev 0.241; p1-security-hardening task 9.3,
# design D8) [live-verify]: staging only. Each Monday Cloud Scheduler exports staging's database to a bucket of its own,
# and a Sensitive Data Protection job inspects it each week for NRIC and FIN numbers, passports, and bank and card
# numbers, which synthetic data never holds; its summary goes to Security Command Center, where posture.findings reads
# it, and to the project's owners by email. Production never runs it: there it is not applicable.

terraform {
  required_version = ">= 1.13.0"
  required_providers {
    google      = { source = "hashicorp/google", version = "~> 8.5.0" }
    google-beta = { source = "hashicorp/google-beta", version = "~> 8.5.0" }
  }
}

variable "project" {
  description = "Staging's project."
  type        = string
}

variable "environment" {
  description = "staging: the scan never runs in production."
  type        = string
  validation {
    condition     = var.environment != "production"
    error_message = "The non-production scan never runs in production (CTL-CHG-05)."
  }
}

variable "region" {
  description = "Singapore (CTL-DP-07)."
  type        = string
  default     = "asia-southeast1"
}

variable "instance" {
  description = "The Cloud SQL instance's name."
  type        = string
}

variable "database" {
  description = "The database to export."
  type        = string
  default     = "fyniq"
}

variable "database_agent" {
  description = "The Cloud SQL instance's own service account, which writes the export."
  type        = string
}

locals {
  bucket = "${var.project}-database-export"
  export = "gs://${local.bucket}/${var.database}.sql.gz"
  # What production data would look like [live-verify the infoType names]; Singapore's bank account numbers have no
  # infoType of their own.
  info_types = ["SINGAPORE_NATIONAL_REGISTRATION_ID_NUMBER", "SINGAPORE_PASSPORT", "CREDIT_CARD_NUMBER", "IBAN_CODE"]
}

resource "google_project_service" "dlp" {
  project            = var.project
  service            = "dlp.googleapis.com"
  disable_on_destroy = false
}

resource "google_kms_key_ring" "scan" {
  project  = var.project
  name     = "scan-${var.environment}"
  location = var.region
}

resource "google_kms_crypto_key" "export" {
  name            = "database-export"
  key_ring        = google_kms_key_ring.scan.id
  purpose         = "ENCRYPT_DECRYPT"
  rotation_period = "7776000s" # 90 days (CTL-DP-01)
  lifecycle {
    prevent_destroy = true
  }
}

data "google_storage_project_service_account" "gcs" {
  project = var.project
}

resource "google_kms_crypto_key_iam_member" "storage_agent" {
  crypto_key_id = google_kms_crypto_key.export.id
  role          = "roles/cloudkms.cryptoKeyEncrypterDecrypter"
  member        = data.google_storage_project_service_account.gcs.member
}

# The latest export only: each overwrites the last, and nothing is kept past a week.
resource "google_storage_bucket" "export" {
  project                     = var.project
  name                        = local.bucket
  location                    = var.region
  storage_class               = "STANDARD"
  uniform_bucket_level_access = true
  public_access_prevention    = "enforced"
  encryption {
    default_kms_key_name = google_kms_crypto_key.export.id
  }
  versioning {
    enabled = true
  }
  lifecycle_rule {
    condition {
      age = 8
    }
    action {
      type = "Delete"
    }
  }
  lifecycle_rule {
    condition {
      days_since_noncurrent_time = 1
    }
    action {
      type = "Delete"
    }
  }
  depends_on = [google_kms_crypto_key_iam_member.storage_agent]
}

resource "google_storage_bucket_iam_member" "database_writes" {
  bucket = google_storage_bucket.export.name
  role   = "roles/storage.objectAdmin" # an export overwrites the last one
  member = "serviceAccount:${var.database_agent}"
}

resource "google_project_service_identity" "dlp" {
  provider   = google-beta
  project    = var.project
  service    = "dlp.googleapis.com"
  depends_on = [google_project_service.dlp]
}

resource "google_storage_bucket_iam_member" "scanner_reads" {
  bucket = google_storage_bucket.export.name
  role   = "roles/storage.objectViewer"
  member = google_project_service_identity.dlp.member
}

# The export's own identity, which may export the database and nothing else.
resource "google_service_account" "export" {
  project      = var.project
  account_id   = "scan-export"
  display_name = "Fyniq ${var.environment}: the weekly database export the non-production scan reads"
}

resource "google_project_iam_custom_role" "export" {
  project     = var.project
  role_id     = "fyniqDatabaseExport"
  title       = "Fyniq database export"
  description = "Export a Cloud SQL instance's database to Cloud Storage, and nothing else (CTL-CHG-05)."
  permissions = ["cloudsql.instances.export"]
}

resource "google_project_iam_member" "export" {
  project = var.project
  role    = google_project_iam_custom_role.export.id
  member  = google_service_account.export.member
}

resource "google_cloud_scheduler_job" "export" {
  project     = var.project
  region      = var.region
  name        = "fyniq-database-export"
  description = "The weekly export of staging's database that the non-production scan reads (CTL-CHG-05)"
  schedule    = "0 2 * * 1"
  time_zone   = "Asia/Singapore"
  http_target {
    http_method = "POST"
    uri         = "https://sqladmin.googleapis.com/v1/projects/${var.project}/instances/${var.instance}/export"
    headers     = { "Content-Type" = "application/json" }
    body        = base64encode(jsonencode({ exportContext = { fileType = "SQL", uri = local.export, databases = [var.database] } }))
    oauth_token {
      service_account_email = google_service_account.export.email
      scope                 = "https://www.googleapis.com/auth/cloud-platform"
    }
  }
}

resource "google_data_loss_prevention_job_trigger" "scan" {
  parent       = "projects/${var.project}/locations/${var.region}"
  display_name = "fyniq-${var.environment}-nonprod-scan"
  description  = "Production data in ${var.environment}: NRIC and FIN numbers, passports, bank and card numbers (CTL-CHG-05)"
  triggers {
    schedule {
      recurrence_period_duration = "604800s" # each week
    }
  }
  inspect_job {
    storage_config {
      cloud_storage_options {
        file_set {
          url = "gs://${google_storage_bucket.export.name}/**"
        }
      }
    }
    inspect_config {
      min_likelihood = "LIKELY"
      dynamic "info_types" {
        for_each = local.info_types
        content {
          name = info_types.value
        }
      }
    }
    actions {
      publish_summary_to_cscc {}
    }
    actions {
      job_notification_emails {}
    }
  }
  depends_on = [google_project_service.dlp, google_storage_bucket_iam_member.scanner_reads]
}

output "bucket" {
  description = "The bucket the weekly export lands in and the scan reads."
  value       = google_storage_bucket.export.name
}
