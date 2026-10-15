# The bucket that holds every other root's state (task 1.1), in the CI project. Applied once by a founder
# with local state; that state holds only this bucket, so if it is lost the bucket is imported again
# (`tofu import google_storage_bucket.state <name>`). Every other root locks its state in this bucket.

terraform {
  required_version = ">= 1.13.0"
  required_providers {
    google = { source = "hashicorp/google", version = "= 8.5.0" }
  }
}

variable "project" {
  description = "The CI project (fyniq-ci, §12.4)."
  type        = string
  default     = "fyniq-ci"
}

variable "bucket" {
  description = "The state bucket's name (globally unique): every other root's `-backend-config=bucket=…`."
  type        = string
}

provider "google" {
  project        = var.project
  region         = "asia-southeast1"
  default_labels = { managed-by = "opentofu", root = "bootstrap" }
}

resource "google_project_service" "storage" {
  project            = var.project
  service            = "storage.googleapis.com"
  disable_on_destroy = false
}

resource "google_storage_bucket" "state" {
  name                        = var.bucket
  location                    = "asia-southeast1"
  uniform_bucket_level_access = true
  public_access_prevention    = "enforced"
  versioning {
    enabled = true # every state is kept; a bad apply is undone from the previous version
  }
  lifecycle_rule {
    condition {
      num_newer_versions = 50
    }
    action {
      type = "Delete"
    }
  }
  lifecycle {
    prevent_destroy = true
  }
  depends_on = [google_project_service.storage]
}

# Every read and write of the state is in Cloud Audit Logs (data access).
resource "google_project_iam_audit_config" "storage" {
  project = var.project
  service = "storage.googleapis.com"
  audit_log_config {
    log_type = "DATA_READ"
  }
  audit_log_config {
    log_type = "DATA_WRITE"
  }
}

output "bucket" {
  description = "Pass to every other root: tofu init -backend-config=bucket=<this>."
  value       = google_storage_bucket.state.name
}
