# The weekly export of the database (DR-02, DR-04, NFR-DR-01; p1-operations-dr design D1): two buckets in the audit project
# (fyniq-audit in production, so the copies survive the loss of the production project), under their own key, in
# Singapore. The weekly bucket keeps each Sunday's export 35 days; the monthly bucket keeps each month's first export a
# year, DR-02's monthly backups. Both retention policies are locked in production. The jobs' account runs the export
# (the platform job db.export) with a role that may only export and read the instance; the instance's own account writes
# the file [live-verify].

terraform {
  required_version = ">= 1.13.0"
  required_providers {
    google = { source = "hashicorp/google", version = "~> 8.5.0" }
  }
}

variable "project" {
  description = "The environment's own project, which holds the database."
  type        = string
}

variable "audit_project" {
  description = "Where the exports live: fyniq-audit for production (§12.4), the environment's own project for staging."
  type        = string
}

variable "environment" {
  description = "staging or production: part of the buckets' names."
  type        = string
}

variable "region" {
  description = "Singapore (CTL-DP-07, DR-03)."
  type        = string
  default     = "asia-southeast1"
}

variable "database_agent" {
  description = "The Cloud SQL instance's own service account, which writes the exports."
  type        = string
}

variable "jobs_member" {
  description = "The jobs' account, as an IAM member, which runs the export."
  type        = string
}

variable "lock" {
  description = "Lock the buckets' retention. IRREVERSIBLE: no export can be deleted before its retention ends. True only in production."
  type        = bool
}

locals {
  # Each bucket and how long it keeps an export, in days; the names the environment's FYNIQ_GCP gives the job.
  buckets = {
    weekly  = { name = "fyniq-${var.environment}-db-weekly", days = 35 }
    monthly = { name = "fyniq-${var.environment}-db-monthly", days = 365 }
  }
}

resource "google_kms_key_ring" "exports" {
  project  = var.audit_project
  name     = "exports-${var.environment}"
  location = var.region
}

resource "google_kms_crypto_key" "exports" {
  name            = "db-export"
  key_ring        = google_kms_key_ring.exports.id
  purpose         = "ENCRYPT_DECRYPT"
  rotation_period = "7776000s" # 90 days (CTL-DP-01)
  lifecycle {
    prevent_destroy = true # an export is readable only while its key exists
  }
}

data "google_storage_project_service_account" "gcs" {
  project = var.audit_project
}

resource "google_kms_crypto_key_iam_member" "storage_agent" {
  crypto_key_id = google_kms_crypto_key.exports.id
  role          = "roles/cloudkms.cryptoKeyEncrypterDecrypter"
  member        = data.google_storage_project_service_account.gcs.member
}

resource "google_storage_bucket" "exports" {
  for_each                    = local.buckets
  project                     = var.audit_project
  name                        = each.value.name
  location                    = var.region
  storage_class               = "STANDARD"
  uniform_bucket_level_access = true
  public_access_prevention    = "enforced"
  encryption {
    default_kms_key_name = google_kms_crypto_key.exports.id
  }
  versioning {
    enabled = true
  }
  retention_policy {
    retention_period = each.value.days * 86400
    is_locked        = var.lock
  }
  lifecycle_rule {
    condition {
      age = each.value.days + 1
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

# The instance writes each export; dated names, so it never overwrites one.
resource "google_storage_bucket_iam_member" "database_writes" {
  for_each = local.buckets
  bucket   = google_storage_bucket.exports[each.key].name
  role     = "roles/storage.objectCreator"
  member   = "serviceAccount:${var.database_agent}"
}

# The jobs' account may start an export and read its operation, and nothing else of Cloud SQL.
resource "google_project_iam_custom_role" "export" {
  project     = var.project
  role_id     = "fyniqWeeklyExport"
  title       = "Fyniq weekly export"
  description = "Export the database to Cloud Storage and read the operation (DR-02, DR-04)."
  permissions = ["cloudsql.instances.export", "cloudsql.instances.get"]
}

resource "google_project_iam_member" "export" {
  project = var.project
  role    = google_project_iam_custom_role.export.id
  member  = var.jobs_member
}

output "buckets" {
  description = "The weekly and monthly buckets, as FYNIQ_GCP's exports name them."
  value       = { weekly = google_storage_bucket.exports["weekly"].name, monthly = google_storage_bucket.exports["monthly"].name }
}
