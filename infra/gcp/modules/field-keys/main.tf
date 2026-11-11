# The application-level field keys (SEC-11; PRD-001 rev 0.152, rev 0.250): the api seals the most sensitive fields
# (vendor bank account numbers first) with a fresh data key that `fields` wraps, and fingerprints them with
# `fingerprints` (HMAC-SHA256) so equal values are found without them (FR-CT-04). Only the api holds a role on either,
# and neither key leaves KMS (gcp.KMSFields). The wrapping key rotates every 90 days, and every version still unwraps;
# the fingerprint key never rotates, its version 1 pinned in FYNIQ_GCP, because a new version would change every
# fingerprint. The key ring lives in fyniq-audit in production, beside the weekly export (the owner's decision of 11 Nov
# 2026, rev 0.250): losing the production project does not lose the means to open the export's sealed fields (DR-04).

terraform {
  required_version = ">= 1.13.0"
  required_providers {
    google = { source = "hashicorp/google", version = "~> 8.5.0" }
  }
}

variable "project" {
  description = "Where the keys live: fyniq-audit for production (§12.4), the environment's own project for staging."
  type        = string
}

variable "environment" {
  description = "staging or production: part of the key ring's name."
  type        = string
}

variable "region" {
  description = "Singapore (CTL-DP-07)."
  type        = string
  default     = "asia-southeast1"
}

variable "api_member" {
  description = "The api's account, as an IAM member: the only one that seals and fingerprints."
  type        = string
}

resource "google_project_service" "kms" {
  project            = var.project
  service            = "cloudkms.googleapis.com"
  disable_on_destroy = false
}

resource "google_kms_key_ring" "fields" {
  project    = var.project
  name       = "fields-${var.environment}"
  location   = var.region
  depends_on = [google_project_service.kms]
}

resource "google_kms_crypto_key" "fields" {
  name            = "fields"
  key_ring        = google_kms_key_ring.fields.id
  purpose         = "ENCRYPT_DECRYPT"
  rotation_period = "7776000s" # 90 days
  lifecycle {
    prevent_destroy = true # every sealed field is unreadable without it
  }
}

resource "google_kms_crypto_key" "fingerprints" {
  name     = "fingerprints"
  key_ring = google_kms_key_ring.fields.id
  purpose  = "MAC"
  version_template {
    algorithm = "HMAC_SHA256"
  }
  lifecycle {
    prevent_destroy = true # duplicate detection and the audit trail's proofs depend on it
  }
}

resource "google_kms_crypto_key_iam_member" "fields" {
  crypto_key_id = google_kms_crypto_key.fields.id
  role          = "roles/cloudkms.cryptoKeyEncrypterDecrypter"
  member        = var.api_member
}

resource "google_kms_crypto_key_iam_member" "fingerprints" {
  crypto_key_id = google_kms_crypto_key.fingerprints.id
  role          = "roles/cloudkms.signer" # macSign only
  member        = var.api_member
}

output "config" {
  description = "What the services read (FYNIQ_GCP's fields): the wrapping key and the fingerprint key's pinned version."
  value       = { key = google_kms_crypto_key.fields.id, mac_key_version = "${google_kms_crypto_key.fingerprints.id}/cryptoKeyVersions/1" }
}

output "project" {
  description = "The project that holds the keys."
  value       = google_kms_key_ring.fields.project
}
