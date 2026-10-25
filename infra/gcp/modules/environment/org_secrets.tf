# The organisations' connector secrets (FR-INT-20, SEC-12; p1-bank-import-reconcile task 8.1, design D12, the owner's
# decision of 25 Oct 2026): in a project of their own beside the environment's, which the founder creates
# (infra/README.md). An organisation's connector key is a secret of its own, created by the api the first time
# someone types it, under the environment's secrets key; so the api's account manages secrets there, and nowhere
# else (scripts/infra.py refuses that right on any other project). The api and the worker read them; no other
# account holds a role there.

resource "google_project_service" "org_secrets" {
  project            = var.secrets_project
  service            = "secretmanager.googleapis.com"
  disable_on_destroy = false
}

resource "google_project_service_identity" "org_secrets" {
  provider   = google-beta
  project    = var.secrets_project
  service    = "secretmanager.googleapis.com"
  depends_on = [google_project_service.org_secrets]
}

# The secrets project's Secret Manager encrypts under the environment's secrets key (CTL-DP-01).
resource "google_kms_crypto_key_iam_member" "org_secrets_agent" {
  crypto_key_id = google_kms_crypto_key.cmek["secrets"].id
  role          = "roles/cloudkms.cryptoKeyEncrypterDecrypter"
  member        = google_project_service_identity.org_secrets.member
}

resource "google_project_iam_member" "org_secrets_admin" {
  project    = var.secrets_project
  role       = "roles/secretmanager.admin"
  member     = local.member["api"]
  depends_on = [google_service_account.all, google_project_service.org_secrets]
}

resource "google_project_iam_member" "org_secrets_reader" {
  for_each   = toset(["worker"])
  project    = var.secrets_project
  role       = "roles/secretmanager.secretAccessor"
  member     = local.member[each.value]
  depends_on = [google_service_account.all, google_project_service.org_secrets]
}
