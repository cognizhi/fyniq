# req: CTL-AUD-02
# req: CTL-DP-01

mock_provider "google" {
  mock_data "google_storage_project_service_account" {
    defaults = {
      email_address = "service-123456789@gs-project-accounts.iam.gserviceaccount.com"
      member        = "serviceAccount:service-123456789@gs-project-accounts.iam.gserviceaccount.com"
    }
  }
  mock_resource "google_kms_crypto_key" {
    defaults = {
      id = "projects/fyniq-audit/locations/asia-southeast1/keyRings/audit-production/cryptoKeys/anchor"
    }
  }
}

variables {
  project        = "fyniq-audit"
  environment    = "production"
  retention_days = 2557
  lock           = true
  writers        = ["serviceAccount:jobs-mock@fyniq-prod.iam.gserviceaccount.com"]
}

# Production's anchors: a locked retention policy, CMEK, no public access, and a jobs identity that can
# add, read and sign, but holds no role that deletes or overwrites.
run "production_anchors_are_locked" {
  command = plan
  assert {
    condition     = google_storage_bucket.anchors.retention_policy[0].is_locked && google_storage_bucket.anchors.retention_policy[0].retention_period == tostring(2557 * 86400)
    error_message = "production's anchors must be retention-locked for the whole period"
  }
  assert {
    condition     = google_storage_bucket.anchors.public_access_prevention == "enforced" && google_storage_bucket.anchors.uniform_bucket_level_access
    error_message = "the anchors must never be public"
  }
  assert {
    condition     = google_storage_bucket.anchors.encryption[0].default_kms_key_name != "" && google_kms_crypto_key.storage.rotation_period == "7776000s"
    error_message = "the anchors' bucket must use a rotated customer-managed key (CTL-DP-01)"
  }
  assert {
    condition     = google_kms_crypto_key.anchor.version_template[0].algorithm == "EC_SIGN_P256_SHA256" && google_kms_crypto_key.anchor.purpose == "ASYMMETRIC_SIGN"
    error_message = "anchors are signed with EC P-256 (§11.5 L3)"
  }
  assert {
    condition = alltrue([for m in concat(values(google_storage_bucket_iam_member.creator), values(google_storage_bucket_iam_member.viewer)) :
    contains(["roles/storage.objectCreator", "roles/storage.objectViewer"], m.role)])
    error_message = "the writer may add and read anchors only"
  }
  assert {
    condition     = endswith(output.key_version, "/cryptoKeys/anchor/cryptoKeyVersions/1")
    error_message = "the key version is what FYNIQ_GCP anchors.key_version names"
  }
}

run "staging_anchors_are_not_locked" {
  command = plan
  variables {
    environment    = "staging"
    project        = "fyniq-staging"
    retention_days = 30
    lock           = false
  }
  assert {
    condition     = !google_storage_bucket.anchors.retention_policy[0].is_locked
    error_message = "a lock is irreversible: only production locks"
  }
}
