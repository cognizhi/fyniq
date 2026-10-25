# Every secret in Secret Manager (SEC-12), kept in Singapore under the secrets key. OpenTofu creates the
# secret only; its value is added out of band, so no secret is ever in the state or the code. Each
# service may read its own secrets and no others.

resource "google_secret_manager_secret" "all" {
  for_each  = local.secrets
  project   = var.project
  secret_id = each.value
  replication {
    user_managed {
      replicas {
        location = local.region
        customer_managed_encryption {
          kms_key_name = google_kms_crypto_key.cmek["secrets"].id
        }
      }
    }
  }
  depends_on = [google_kms_crypto_key_iam_member.agent]
}

resource "google_secret_manager_secret_iam_member" "access" {
  for_each   = local.secret_access
  project    = var.project
  secret_id  = google_secret_manager_secret.all[each.value.secret].secret_id
  role       = "roles/secretmanager.secretAccessor"
  member     = local.member[each.value.name]
  depends_on = [google_service_account.all]
}

# The api adds, disables and destroys versions of the installation connectors' secrets, and of no other secret, when a
# Super admin replaces a key in Settings › Integrations (FR-INT-20, rev 0.139). secretVersionManager cannot read a
# value; the api reads only the secrets whose manifest lists it.
resource "google_secret_manager_secret_iam_member" "connector_versions" {
  for_each   = toset(values(local.connector_secret_names))
  project    = var.project
  secret_id  = google_secret_manager_secret.all[each.value].secret_id
  role       = "roles/secretmanager.secretVersionManager"
  member     = local.member["api"]
  depends_on = [google_service_account.all]
}
