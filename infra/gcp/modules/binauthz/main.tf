# Binary Authorization (SEC-22, NFR-IMG-05, CTL-CHG-04; p1-security-hardening design D1). The attestor
# `fyniq-release` and its Cloud KMS signing key live in the CI project. The release workflow verifies each image's
# keyless signature, SBOM and SLSA provenance, then attests it with the key (only the ci-attester identity, reachable
# only from release.yml on a tag, may). Each environment's project gets a policy that requires that attestation for
# every Cloud Run service and job, and an organisation-policy rule that every service uses the default policy, so
# none opts out. Cloud Run's Binary Authorization reads attestors, not keyless signatures, hence verify-then-attest.
# Applied from the shared root (a founder, through just-in-time elevation), so an environment's own CI identity
# does not manage its admission policy. [live-verify]

terraform {
  required_version = ">= 1.13.0"
  required_providers {
    google = { source = "hashicorp/google", version = "~> 8.5.0" }
  }
}

variable "project" {
  description = "The CI project (fyniq-ci): the attestor, its note and its key."
  type        = string
}

variable "region" {
  description = "Singapore (CTL-DP-07)."
  type        = string
  default     = "asia-southeast1"
}

variable "attester" {
  description = "The identity that creates attestations (the ci-attester service account), as an IAM member."
  type        = string
}

variable "registry" {
  description = "The Artifact Registry repository of the release images (projects/<p>/locations/<l>/repositories/<r>): the attester reads the signatures there."
  type        = string
}

variable "environments" {
  description = "Each environment's project, and whether its policy blocks (true) or only logs (false). Dry run until cloud-first-deployment deploys the first attested release in place of the placeholder image; then true [live-verify]."
  type = map(object({
    project = string
    enforce = bool
  }))
}

variable "protection_level" {
  description = "The attestor key's protection: SOFTWARE or HSM."
  type        = string
  default     = "SOFTWARE"
}

resource "google_project_service" "apis" {
  for_each           = toset(["binaryauthorization", "containeranalysis", "cloudkms"])
  project            = var.project
  service            = "${each.value}.googleapis.com"
  disable_on_destroy = false
}

resource "google_project_service" "environment" {
  for_each           = var.environments
  project            = each.value.project
  service            = "binaryauthorization.googleapis.com"
  disable_on_destroy = false
}

resource "google_kms_key_ring" "release" {
  project    = var.project
  name       = "release"
  location   = var.region
  depends_on = [google_project_service.apis]
}

resource "google_kms_crypto_key" "attestor" {
  name     = "attestor"
  key_ring = google_kms_key_ring.release.id
  purpose  = "ASYMMETRIC_SIGN"
  version_template {
    algorithm        = "EC_SIGN_P256_SHA256"
    protection_level = var.protection_level
  }
  lifecycle {
    prevent_destroy = true # every deployed image's attestation is verified against it
  }
}

data "google_kms_crypto_key_version" "attestor" {
  crypto_key = google_kms_crypto_key.attestor.id
}

resource "google_container_analysis_note" "release" {
  project = var.project
  name    = "fyniq-release"
  attestation_authority {
    hint {
      human_readable_name = "Fyniq release workflow: signature, SBOM and SLSA provenance verified"
    }
  }
  depends_on = [google_project_service.apis]
}

resource "google_binary_authorization_attestor" "release" {
  project     = var.project
  name        = "fyniq-release"
  description = "Attests a release image only after release.yml verified its keyless signature, its SBOM and its SLSA Build L3 provenance (SEC-22)"
  attestation_authority_note {
    note_reference = google_container_analysis_note.release.name
    public_keys {
      id = data.google_kms_crypto_key_version.attestor.id
      pkix_public_key {
        public_key_pem      = data.google_kms_crypto_key_version.attestor.public_key[0].pem
        signature_algorithm = data.google_kms_crypto_key_version.attestor.public_key[0].algorithm
      }
    }
  }
}

# The attester signs with the key, attaches occurrences to the note and reads the images; nothing else.
resource "google_kms_crypto_key_iam_member" "attester" {
  for_each      = toset(["roles/cloudkms.signer", "roles/cloudkms.publicKeyViewer"])
  crypto_key_id = google_kms_crypto_key.attestor.id
  role          = each.value
  member        = var.attester
}

resource "google_container_analysis_note_iam_member" "attester" {
  project = var.project
  note    = google_container_analysis_note.release.name
  role    = "roles/containeranalysis.notes.attacher"
  member  = var.attester
}

resource "google_binary_authorization_attestor_iam_member" "attester" {
  project  = var.project
  attestor = google_binary_authorization_attestor.release.name
  role     = "roles/binaryauthorization.attestorsViewer"
  member   = var.attester
}

resource "google_project_iam_member" "attester_occurrences" {
  project = var.project
  role    = "roles/containeranalysis.occurrences.editor" # the attestation is an occurrence in this project
  member  = var.attester
}

resource "google_artifact_registry_repository_iam_member" "attester" {
  project    = split("/", var.registry)[1]
  location   = split("/", var.registry)[3]
  repository = split("/", var.registry)[5]
  role       = "roles/artifactregistry.reader"
  member     = var.attester
}

# Each environment's Binary Authorization service agent verifies against the attestor; the CI project's own reads
# the note's occurrences for it.
data "google_project" "ci" {
  project_id = var.project
}

data "google_project" "environment" {
  for_each   = var.environments
  project_id = each.value.project
}

resource "google_container_analysis_note_iam_member" "occurrences" {
  project = var.project
  note    = google_container_analysis_note.release.name
  role    = "roles/containeranalysis.notes.occurrences.viewer"
  member  = "serviceAccount:service-${data.google_project.ci.number}@gcp-sa-binaryauthorization.iam.gserviceaccount.com"
}

resource "google_binary_authorization_attestor_iam_member" "verifier" {
  for_each   = var.environments
  project    = var.project
  attestor   = google_binary_authorization_attestor.release.name
  role       = "roles/binaryauthorization.attestorsVerifier"
  member     = "serviceAccount:service-${data.google_project.environment[each.key].number}@gcp-sa-binaryauthorization.iam.gserviceaccount.com"
  depends_on = [google_project_service.environment]
}

# The policy: every image needs the release attestation. No allow-listed image and no per-cluster rule; Google's
# own system images are exempt (global policy evaluation), which Cloud Run does not run anyway.
resource "google_binary_authorization_policy" "environment" {
  for_each                      = var.environments
  project                       = each.value.project
  description                   = "Only images the release workflow attested (SEC-22, CTL-CHG-04)"
  global_policy_evaluation_mode = "ENABLE"
  default_admission_rule {
    evaluation_mode         = "REQUIRE_ATTESTATION"
    enforcement_mode        = each.value.enforce ? "ENFORCED_BLOCK_AND_AUDIT_LOG" : "DRYRUN_AUDIT_LOG_ONLY"
    require_attestations_by = [google_binary_authorization_attestor.release.name]
  }
  depends_on = [google_project_service.environment]
}

# Every Cloud Run service and job must use the project's default policy: none may run without one.
resource "google_org_policy_policy" "run_uses_default" {
  for_each = var.environments
  name     = "projects/${each.value.project}/policies/run.allowedBinaryAuthorizationPolicies"
  parent   = "projects/${each.value.project}"
  spec {
    rules {
      values {
        allowed_values = ["default"]
      }
    }
  }
}

output "attestor" {
  description = "The attestor the release workflow attests with (projects/<ci>/attestors/fyniq-release): the GitHub variable BINAUTHZ_ATTESTOR."
  value       = google_binary_authorization_attestor.release.id
}

output "key_version" {
  description = "The key version that signs attestations (gcloud's --keyversion): the GitHub variable BINAUTHZ_KEY_VERSION."
  value       = "${google_kms_crypto_key.attestor.id}/cryptoKeyVersions/1"
}
