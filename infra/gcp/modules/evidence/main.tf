# The records' and attachments' storage (DR-07, NFR-RET-01, CTL-AUD-03; p1-operations-dr design D3): two buckets in the
# audit project (fyniq-audit in production, beside the weekly export, so documents outlive the production project: the
# owner's decision of 11 Nov 2026, rev 0.250), in Singapore, under their own key, never public.
#   - The staging bucket holds what is not yet a record: uploads waiting for their record to be final, report exports,
#     imported statements, brand files and outgoing documents. The app deletes each in its time (the sweeper, an
#     export's expiry), so it has no age-based deletion but a backstop on report exports.
#   - The evidence bucket holds committed files. Object Retention Lock is on: the API sets each object's retain-until in
#     Locked mode at commit (FR-DOC-02), seven years from its financial year's end, so no bucket-wide policy, which
#     could not follow each record's expiry.
# The api reads committed files and signs their download links; the worker and the jobs commit them and may set their
# retention and holds, and nobody may delete one [live-verify].

terraform {
  required_version = ">= 1.13.0"
  required_providers {
    google = { source = "hashicorp/google", version = "~> 8.5.0" }
  }
}

variable "project" {
  description = "Where the buckets live: fyniq-audit for production (rev 0.250), the environment's own project for staging."
  type        = string
}

variable "environment" {
  description = "staging or production: part of the buckets' names."
  type        = string
}

variable "region" {
  description = "Singapore (CTL-DP-07)."
  type        = string
  default     = "asia-southeast1"
}

variable "members" {
  description = "The api's, the worker's and the jobs' accounts, as IAM members."
  type = object({
    api    = string
    worker = string
    jobs   = string
  })
}

resource "google_kms_key_ring" "evidence" {
  project  = var.project
  name     = "evidence-${var.environment}"
  location = var.region
}

resource "google_kms_crypto_key" "evidence" {
  name            = "evidence"
  key_ring        = google_kms_key_ring.evidence.id
  purpose         = "ENCRYPT_DECRYPT"
  rotation_period = "7776000s" # 90 days (CTL-DP-01)
  lifecycle {
    prevent_destroy = true # the records are readable only while their key exists
  }
}

data "google_storage_project_service_account" "gcs" {
  project = var.project
}

resource "google_kms_crypto_key_iam_member" "storage_agent" {
  crypto_key_id = google_kms_crypto_key.evidence.id
  role          = "roles/cloudkms.cryptoKeyEncrypterDecrypter"
  member        = data.google_storage_project_service_account.gcs.member
}

resource "google_storage_bucket" "staging" {
  project                     = var.project
  name                        = "fyniq-${var.environment}-evidence-staging"
  location                    = var.region
  storage_class               = "STANDARD"
  uniform_bucket_level_access = true
  public_access_prevention    = "enforced"
  encryption {
    default_kms_key_name = google_kms_crypto_key.evidence.id
  }
  # A report export lives 24 hours and the worker deletes it each hour (FR-RPT-10); this deletes one the worker missed.
  lifecycle_rule {
    condition {
      age            = 7
      matches_prefix = ["exports/"]
    }
    action {
      type = "Delete"
    }
  }
  lifecycle_rule {
    condition {
      age = 1
    }
    action {
      type = "AbortIncompleteMultipartUpload"
    }
  }
  depends_on = [google_kms_crypto_key_iam_member.storage_agent]
}

resource "google_storage_bucket" "evidence" {
  project                     = var.project
  name                        = "fyniq-${var.environment}-evidence"
  location                    = var.region
  storage_class               = "STANDARD"
  uniform_bucket_level_access = true
  public_access_prevention    = "enforced"
  enable_object_retention     = true
  encryption {
    default_kms_key_name = google_kms_crypto_key.evidence.id
  }
  versioning {
    enabled = true
  }
  depends_on = [google_kms_crypto_key_iam_member.storage_agent]
}

# The staging bucket: the api, the worker and the jobs write, read and delete what waits there.
resource "google_storage_bucket_iam_member" "staging" {
  for_each = var.members
  bucket   = google_storage_bucket.staging.name
  role     = "roles/storage.objectUser"
  member   = each.value
}

# Committing: write a new object, read it, list them, set its Locked retention and its hold; never delete or replace one.
resource "google_project_iam_custom_role" "committer" {
  project     = var.project
  role_id     = "fyniqEvidenceCommitter"
  title       = "Fyniq evidence committer"
  description = "Commit evidence into the locked bucket: create, read, list, set retention and holds; never delete (FR-DOC-02, FR-DOC-13)."
  permissions = ["storage.objects.create", "storage.objects.get", "storage.objects.list", "storage.objects.setRetention", "storage.objects.update"]
}

resource "google_storage_bucket_iam_member" "committers" {
  for_each = { worker = var.members.worker, jobs = var.members.jobs }
  bucket   = google_storage_bucket.evidence.name
  role     = google_project_iam_custom_role.committer.id
  member   = each.value
}

# The api opens committed files and signs their download links as itself (SEC-24): it reads, nothing more.
resource "google_storage_bucket_iam_member" "api_reads" {
  bucket = google_storage_bucket.evidence.name
  role   = "roles/storage.objectViewer"
  member = var.members.api
}

output "buckets" {
  description = "FYNIQ_GCP's evidence: the staging bucket and the locked evidence bucket."
  value       = { staging = google_storage_bucket.staging.name, bucket = google_storage_bucket.evidence.name }
}

output "project" {
  description = "The project that holds the buckets."
  value       = google_storage_bucket.evidence.project
}
