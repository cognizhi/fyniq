# Customer-managed keys (CTL-DP-01) for the database, its backups, and Secret Manager, rotated every 90 days.
# Each Google service agent may use its own key only, and gets it before the resource that needs it.

resource "google_kms_key_ring" "platform" {
  project    = var.project
  name       = "platform-${local.spec.environment}"
  location   = local.region
  depends_on = [google_project_service.apis]
}

resource "google_kms_crypto_key" "cmek" {
  for_each        = toset(["database", "secrets"])
  name            = each.value
  key_ring        = google_kms_key_ring.platform.id
  purpose         = "ENCRYPT_DECRYPT"
  rotation_period = "7776000s" # 90 days
  lifecycle {
    prevent_destroy = true # the database, its backups and the secrets are unreadable without it
  }
}

resource "google_project_service_identity" "agent" {
  for_each = toset(["sqladmin", "secretmanager"])
  provider = google-beta
  project  = var.project
  service  = "${each.value}.googleapis.com"
}

resource "google_kms_crypto_key_iam_member" "agent" {
  for_each      = { database = "sqladmin", secrets = "secretmanager" }
  crypto_key_id = google_kms_crypto_key.cmek[each.key].id
  role          = "roles/cloudkms.cryptoKeyEncrypterDecrypter"
  member        = google_project_service_identity.agent[each.value].member
}
