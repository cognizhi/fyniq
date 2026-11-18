# SEC-11, DR-04 (rev 0.250): the field keys live in the project they are given (fyniq-audit in production), the wrapping
# key rotates every 90 days, the fingerprint key is HMAC-SHA256, only the api holds a role on either, and the services
# read the fingerprint key's pinned version. [live-verify]
# req: SEC-11
# req: DR-04

mock_provider "google" {
  mock_resource "google_kms_crypto_key" {
    defaults = { id = "projects/fyniq-audit/locations/asia-southeast1/keyRings/fields-production/cryptoKeys/mock" }
  }
}

variables {
  project     = "fyniq-audit"
  environment = "production"
  api_member  = "serviceAccount:run-api@fyniq-prod.iam.gserviceaccount.com"
}

run "the_sensitive_fields_are_sealed_by_the_api_only" {
  command = plan
  assert {
    condition     = google_kms_key_ring.fields.project == "fyniq-audit" && google_kms_key_ring.fields.name == "fields-production" && google_kms_key_ring.fields.location == "asia-southeast1"
    error_message = "the field keys live in fyniq-audit, in Singapore (rev 0.250, CTL-DP-07)"
  }
  assert {
    condition     = google_kms_crypto_key.fields.purpose == "ENCRYPT_DECRYPT" && google_kms_crypto_key.fields.rotation_period == "7776000s"
    error_message = "the wrapping key encrypts and rotates every 90 days"
  }
  assert {
    condition     = google_kms_crypto_key.fingerprints.purpose == "MAC" && google_kms_crypto_key.fingerprints.version_template[0].algorithm == "HMAC_SHA256"
    error_message = "the fingerprint key is an HMAC-SHA256 key"
  }
  assert {
    condition     = google_kms_crypto_key_iam_member.fields.member == "serviceAccount:run-api@fyniq-prod.iam.gserviceaccount.com" && google_kms_crypto_key_iam_member.fingerprints.member == google_kms_crypto_key_iam_member.fields.member && google_kms_crypto_key_iam_member.fingerprints.role == "roles/cloudkms.signer"
    error_message = "only the api seals and fingerprints"
  }
  assert {
    condition     = endswith(output.config.mac_key_version, "/cryptoKeyVersions/1") && output.config.key == google_kms_crypto_key.fields.id
    error_message = "the services read the wrapping key and the fingerprint key's pinned version"
  }
}
