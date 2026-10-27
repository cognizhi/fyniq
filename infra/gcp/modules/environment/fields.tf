# The application-level field keys (SEC-11; PRD-001 rev 0.152): the api seals the most sensitive fields (vendor bank
# account numbers first) with a fresh data key that `fields` wraps, and fingerprints them with `fingerprints`
# (HMAC-SHA256) so equal values are found without them (FR-CT-04). Only the api holds a role on either, and neither key
# leaves KMS (gcp.KMSFields). The wrapping key rotates every 90 days, and every version still unwraps; the fingerprint
# key never rotates, its version 1 pinned in FYNIQ_GCP, because a new version would change every fingerprint.

resource "google_kms_crypto_key" "fields" {
  name            = "fields"
  key_ring        = google_kms_key_ring.platform.id
  purpose         = "ENCRYPT_DECRYPT"
  rotation_period = "7776000s" # 90 days
  lifecycle {
    prevent_destroy = true # every sealed field is unreadable without it
  }
}

resource "google_kms_crypto_key" "fingerprints" {
  name     = "fingerprints"
  key_ring = google_kms_key_ring.platform.id
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
  member        = local.member["api"]
  depends_on    = [google_service_account.all]
}

resource "google_kms_crypto_key_iam_member" "fingerprints" {
  crypto_key_id = google_kms_crypto_key.fingerprints.id
  role          = "roles/cloudkms.signer" # macSign only
  member        = local.member["api"]
  depends_on    = [google_service_account.all]
}
