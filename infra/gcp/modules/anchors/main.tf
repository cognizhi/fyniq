# The audit anchors (§11.5 L3, CTL-AUD-02): a retention-locked bucket in Singapore and the Cloud KMS key that
# signs each anchor (EC P-256: the public key allows offline verification). In production they live in
# fyniq-audit, administered apart from the platform (§12.4). The jobs identity may add and read anchors
# and sign with the key; nobody can alter or delete an anchor before its retention ends.

terraform {
  required_version = ">= 1.13.0"
  required_providers {
    google = { source = "hashicorp/google", version = "~> 8.5.0" }
  }
}

variable "project" {
  description = "The project that holds the anchors: fyniq-audit in production, the environment's own project elsewhere."
  type        = string
}

variable "environment" {
  description = "staging or production: part of the bucket's and key ring's names."
  type        = string
}

variable "region" {
  description = "Singapore (CTL-DP-07)."
  type        = string
  default     = "asia-southeast1"
}

variable "retention_days" {
  description = "How long an anchor is kept. Audit events about accounting records are kept as long as the records (AUD-06, §14). [verify the number with the accountant before locking]"
  type        = number
}

variable "lock" {
  description = "Lock the retention policy. IRREVERSIBLE: the bucket and its project cannot be deleted until every anchor's retention ends. True only in production."
  type        = bool
}

variable "writers" {
  description = "The identities that anchor and verify (the jobs service account), as IAM members."
  type        = list(string)
}

variable "protection_level" {
  description = "The signing key's protection: SOFTWARE or HSM."
  type        = string
  default     = "SOFTWARE"
}

resource "google_project_service" "apis" {
  for_each           = toset(["cloudkms.googleapis.com", "storage.googleapis.com"])
  project            = var.project
  service            = each.value
  disable_on_destroy = false
}

resource "google_kms_key_ring" "audit" {
  project    = var.project
  name       = "audit-${var.environment}"
  location   = var.region
  depends_on = [google_project_service.apis]
}

resource "google_kms_crypto_key" "anchor" {
  name     = "anchor"
  key_ring = google_kms_key_ring.audit.id
  purpose  = "ASYMMETRIC_SIGN"
  version_template {
    algorithm        = "EC_SIGN_P256_SHA256"
    protection_level = var.protection_level
  }
  lifecycle {
    prevent_destroy = true # an anchor is verifiable only while its key exists
  }
}

resource "google_kms_crypto_key" "storage" {
  name            = "anchors-storage"
  key_ring        = google_kms_key_ring.audit.id
  purpose         = "ENCRYPT_DECRYPT"
  rotation_period = "7776000s" # 90 days (CTL-DP-01: key rotation)
  lifecycle {
    prevent_destroy = true
  }
}

data "google_storage_project_service_account" "gcs" {
  project = var.project
}

resource "google_kms_crypto_key_iam_member" "storage_agent" {
  crypto_key_id = google_kms_crypto_key.storage.id
  role          = "roles/cloudkms.cryptoKeyEncrypterDecrypter"
  member        = data.google_storage_project_service_account.gcs.member
}

resource "google_storage_bucket" "anchors" {
  project                     = var.project
  name                        = "fyniq-${var.environment}-audit-anchors"
  location                    = var.region
  storage_class               = "STANDARD"
  uniform_bucket_level_access = true
  public_access_prevention    = "enforced"
  encryption {
    default_kms_key_name = google_kms_crypto_key.storage.id
  }
  retention_policy {
    retention_period = var.retention_days * 86400
    is_locked        = var.lock
  }
  versioning {
    enabled = true
  }
  depends_on = [google_kms_crypto_key_iam_member.storage_agent]
}

# Every read and write of an anchor is in Cloud Audit Logs (data access), so access needs no separate
# usage-log bucket.
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

# Add and read, never overwrite or delete: objectCreator has no delete, and the lock refuses one anyway.
resource "google_storage_bucket_iam_member" "creator" {
  for_each = toset(var.writers)
  bucket   = google_storage_bucket.anchors.name
  role     = "roles/storage.objectCreator"
  member   = each.value
}

resource "google_storage_bucket_iam_member" "viewer" {
  for_each = toset(var.writers)
  bucket   = google_storage_bucket.anchors.name
  role     = "roles/storage.objectViewer"
  member   = each.value
}

resource "google_kms_crypto_key_iam_member" "signer" {
  for_each      = toset(var.writers)
  crypto_key_id = google_kms_crypto_key.anchor.id
  role          = "roles/cloudkms.signer"
  member        = each.value
}

resource "google_kms_crypto_key_iam_member" "public_key" {
  for_each      = toset(var.writers)
  crypto_key_id = google_kms_crypto_key.anchor.id
  role          = "roles/cloudkms.publicKeyViewer"
  member        = each.value
}

output "bucket" {
  description = "The anchors' bucket (FYNIQ_GCP anchors.bucket)."
  value       = google_storage_bucket.anchors.name
}

output "key_version" {
  description = "The key version that signs the anchors (FYNIQ_GCP anchors.key_version)."
  value       = "${google_kms_crypto_key.anchor.id}/cryptoKeyVersions/1"
}
